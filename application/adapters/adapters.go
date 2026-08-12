package adapters

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	frameworkSession "github.com/RedHuang-0622/Seele/session"
	"github.com/RedHuang-0622/Seele/telemetry"
	toolspermission "github.com/RedHuang-0622/Seele/tools/permission"
	"github.com/RedHuang-0622/Seele/types"
	"github.com/RedHuang-0622/Seele/workplan/sugar/approve"

	"github.com/RedHuang-0622/seelex/application"
	"github.com/RedHuang-0622/seelex/plugin"
	"github.com/RedHuang-0622/seelex/seelebridge"
	seelexctxsearch "github.com/RedHuang-0622/seelex/seelexctx/search"
	"github.com/RedHuang-0622/seelex/seelexctx/snapshot"
	"github.com/RedHuang-0622/seelex/session"
	"github.com/RedHuang-0622/seelex/sessionstore"
	"github.com/RedHuang-0622/seelex/skill"
	"github.com/RedHuang-0622/seelex/workspace"
)

type EnginePort struct {
	engine         ReactorEngine
	newEngine      ReactorEngineFactory
	tracer         *telemetry.MemoryTracer // trace 视图查询源（slice 8：telemetry）
	mu             sync.RWMutex
	sessionID      string
	activeCalls    int
	pendingHistory []seelebridge.Message
	pendingSession string
	prepareHistory func(string, []seelebridge.Message)
	systemPrompt   string
	maxLoops       int
	sessionBacked  bool
	releaseWorking bool
	// nodeConversations 是子代理会话记录查询（节点详情数据面；Runtime 注入，
	// 只读子代理 actor，安全——不经过主会话锁）。
	nodeConversations func(string) ([]types.Message, bool)
	// nodeContextSnapshot 是子代理结构化上下文查询（详情弹窗"上下文"标签；
	// Runtime 注入，只读子代理 actor，安全）。
	nodeContextSnapshot func(string) (*snapshot.ContextSnapshot, bool)
	// nodeToolResult 是子代理工具结果读回（ref 带 node:<nodeID>: 前缀；
	// Runtime 注入，只读子代理归档器，安全）。
	nodeToolResult func(string, string) (string, bool)
	// nodeWorktree 是节点 worktree 现场查询（失败现场恢复入口；Runtime 注入）。
	nodeWorktree func(string) (seelebridge.NodeWorktreeInfo, bool)
	// subAgentTree 是 fork 子代理树投影查询（GUI 树视图数据源；Runtime
	// 注入，内存态只读 actor，安全）。
	subAgentTree func() []seelebridge.SubAgentTreeNode
}

// ReactorEngine is the small framework surface the application adapter
// needs. Keeping construction behind a factory makes a new application session
// a new ReAct loop, rather than a logical ID layered over an old loop.
type ReactorEngine interface {
	ChatStream(context.Context, string, func(string)) (string, error)
	History() []types.Message
	ClearHistory()
	SessionID() string
	SetSystemPrompt(string)
	SetMaxLoops(int)
	AppendHistory(types.Message)
}

type ReactorEngineFactory func(sessionID string) ReactorEngine

func NewEnginePort(eng ReactorEngine, newEngine ReactorEngineFactory, tracer *telemetry.MemoryTracer) *EnginePort {
	port := &EnginePort{engine: eng, newEngine: newEngine, tracer: tracer}
	if eng == nil {
		return port
	}
	port.sessionID = eng.SessionID()
	if _, ok := eng.(*frameworkSession.Session); ok {
		port.sessionBacked = true
	}
	return port
}

// SessionBacked 报告底层 reactor 是否为 session.Session。
// 新 Session 装配下 OnIterationComplete 在 Session 锁内同步执行，
// 应用层不得在回调中重入 Engine 历史操作（见 chat.go ToolHookBridge）。
func (port *EnginePort) SessionBacked() bool { return port.sessionBacked }

func (port *EnginePort) ChatStream(ctx context.Context, input string, onChunk func(string)) (string, error) {
	port.mu.Lock()
	current := port.engine
	port.activeCalls++
	port.mu.Unlock()
	if current == nil {
		port.mu.Lock()
		port.activeCalls--
		port.mu.Unlock()
		return "", fmt.Errorf("engine is unavailable")
	}
	result, err := current.ChatStream(ctx, input, onChunk)

	port.mu.Lock()
	port.activeCalls--
	if port.activeCalls == 0 && len(port.pendingHistory) > 0 {
		port.installFreshHistoryLocked(port.pendingHistory, port.pendingSession)
		port.pendingHistory = nil
		port.pendingSession = ""
	}
	port.mu.Unlock()
	return result, err
}

// NodeSessionConversation 转发子代理会话记录查询（节点详情数据面；
// 查询源经 SetNodeConversationsProvider 注入，只读子代理 actor，安全）。
func (port *EnginePort) NodeSessionConversation(nodeID string) ([]types.Message, bool) {
	if port == nil || port.nodeConversations == nil {
		return nil, false
	}
	return port.nodeConversations(nodeID)
}

// SetNodeConversationsProvider 注入子代理会话记录查询源（Runtime 接线）。
func (port *EnginePort) SetNodeConversationsProvider(fn func(string) ([]types.Message, bool)) {
	if port != nil {
		port.nodeConversations = fn
	}
}

// NodeContextSnapshot 转发子代理结构化上下文查询（详情弹窗"上下文"标签；
// 查询源经 SetNodeContextProvider 注入，只读子代理 actor，安全）。
func (port *EnginePort) NodeContextSnapshot(nodeID string) (*snapshot.ContextSnapshot, bool) {
	if port == nil || port.nodeContextSnapshot == nil {
		return nil, false
	}
	return port.nodeContextSnapshot(nodeID)
}

// NodeToolResult 转发子代理工具结果读回（ref 带 node:<nodeID>: 前缀；
// 查询源经 SetNodeToolResultProvider 注入，只读子代理归档器，安全）。
func (port *EnginePort) NodeToolResult(nodeID, ref string) (string, bool) {
	if port == nil || port.nodeToolResult == nil {
		return "", false
	}
	return port.nodeToolResult(nodeID, ref)
}

// SetNodeContextProvider 注入子代理上下文快照查询源（Runtime 接线）。
func (port *EnginePort) SetNodeContextProvider(fn func(string) (*snapshot.ContextSnapshot, bool)) {
	if port != nil {
		port.nodeContextSnapshot = fn
	}
}

// NodeWorktreeInfoFor 转发节点 worktree 现场查询（失败现场恢复入口；
// 查询源经 SetNodeWorktreeProvider 注入，只读注册表，安全）。
func (port *EnginePort) NodeWorktreeInfoFor(nodeID string) (seelebridge.NodeWorktreeInfo, bool) {
	if port == nil || port.nodeWorktree == nil {
		return seelebridge.NodeWorktreeInfo{}, false
	}
	return port.nodeWorktree(nodeID)
}

// SetNodeWorktreeProvider 注入节点 worktree 现场查询源（Runtime 接线）。
func (port *EnginePort) SetNodeWorktreeProvider(fn func(string) (seelebridge.NodeWorktreeInfo, bool)) {
	if port != nil {
		port.nodeWorktree = fn
	}
}

// SetNodeToolResultProvider 注入子代理工具结果查询源（Runtime 接线）。
func (port *EnginePort) SetNodeToolResultProvider(fn func(string, string) (string, bool)) {
	if port != nil {
		port.nodeToolResult = fn
	}
}

// SubAgentTree 转发 fork 子代理树投影查询（GUI 树视图数据源；内存态
// 只读 actor，安全——不触碰主会话锁）。
func (port *EnginePort) SubAgentTree() []seelebridge.SubAgentTreeNode {
	if port == nil || port.subAgentTree == nil {
		return nil
	}
	return port.subAgentTree()
}

// SetSubAgentTreeProvider 注入子代理树投影查询源（Runtime 接线，main.go）。
func (port *EnginePort) SetSubAgentTreeProvider(fn func() []seelebridge.SubAgentTreeNode) {
	if port != nil {
		port.subAgentTree = fn
	}
}

// AppendHistory 追加消息到引擎内部对话历史。
// 由 OnIterationComplete 在 ChatStream 同 goroutine 中调用，无需加锁。
func (port *EnginePort) AppendHistory(msg types.Message) {
	port.mu.RLock()
	engine := port.engine
	port.mu.RUnlock()
	if engine != nil {
		engine.AppendHistory(msg)
	}
}

func (port *EnginePort) ClearHistory() {
	port.mu.Lock()
	if port.engine != nil {
		port.engine.ClearHistory()
	}
	port.mu.Unlock()
}
func (port *EnginePort) ReplaceHistory(sessionID string, history []application.EngineMessage) error {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return fmt.Errorf("engine: session ID is required")
	}
	return port.ReplaceRawHistory(sessionID, restoreMessages(history))
}

// SetHistoryPreparer installs the Runtime-owned one-shot handoff used by the
// framework Session's DurableHistory.Load. It is configured during startup,
// before concurrent application work begins.
func (port *EnginePort) SetHistoryPreparer(preparer func(string, []seelebridge.Message)) {
	port.mu.Lock()
	port.prepareHistory = preparer
	port.mu.Unlock()
}
func (port *EnginePort) ReplaceRawHistory(sessionID string, history []seelebridge.Message) error {
	desired := canonicalEngineHistory(history)
	port.mu.Lock()
	defer port.mu.Unlock()
	if port.engine == nil && port.newEngine == nil {
		return fmt.Errorf("engine is unavailable")
	}
	if port.activeCalls > 0 {
		// A running ReActLoop owns its in-memory slice. Keep it valid for the
		// current turn, then install a genuinely clean reactor before the next
		// request. ClearHistory deliberately retains system messages upstream,
		// so appending them again here would duplicate the prompt on every
		// compaction or recovery.
		port.replaceActiveHistoryLocked(desired)
		port.pendingHistory = append([]seelebridge.Message(nil), desired...)
		port.pendingSession = sessionID
	} else {
		port.installFreshHistoryLocked(desired, sessionID)
	}
	port.sessionID = sessionID
	return nil
}

func (port *EnginePort) replaceActiveHistoryLocked(history []seelebridge.Message) {
	port.engine.ClearHistory()
	hasSystem := false
	for _, message := range port.engine.History() {
		hasSystem = hasSystem || message.Role == "system"
	}
	for _, message := range history {
		if message.Role == "system" {
			if !hasSystem {
				port.engine.AppendHistory(message)
				hasSystem = true
			}
			continue
		}
		port.engine.AppendHistory(message)
	}
}

func (port *EnginePort) installFreshHistoryLocked(history []seelebridge.Message, sessionID string) {
	if port.newEngine == nil {
		port.replaceActiveHistoryLocked(history)
		if port.prepareHistory != nil {
			port.prepareHistory(sessionID, history)
		}
		return
	}
	fresh := port.newEngine(sessionID)
	if fresh == nil {
		port.replaceActiveHistoryLocked(history)
		if port.prepareHistory != nil {
			port.prepareHistory(sessionID, history)
		}
		return
	}
	for _, message := range history {
		fresh.AppendHistory(message)
	}
	port.engine = fresh
	_, port.sessionBacked = fresh.(*frameworkSession.Session)
	if port.systemPrompt != "" {
		fresh.SetSystemPrompt(port.systemPrompt)
	}
	if port.maxLoops > 0 {
		fresh.SetMaxLoops(port.maxLoops)
	}
	if port.prepareHistory != nil {
		port.prepareHistory(sessionID, history)
	}
}

func canonicalEngineHistory(history []seelebridge.Message) []seelebridge.Message {
	canonical := make([]seelebridge.Message, 0, len(history))
	hasSystem := false
	for _, message := range history {
		if message.Role == "system" {
			if hasSystem {
				continue
			}
			hasSystem = true
		}
		canonical = append(canonical, message)
	}
	return canonical
}
func (port *EnginePort) StartSession() string {
	port.mu.Lock()
	defer port.mu.Unlock()
	if port.newEngine == nil {
		return ""
	}
	fresh := port.newEngine("")
	if fresh == nil {
		return ""
	}
	port.engine = fresh
	port.sessionID = fresh.SessionID()
	_, port.sessionBacked = fresh.(*frameworkSession.Session)
	if port.systemPrompt != "" {
		fresh.SetSystemPrompt(port.systemPrompt)
	}
	if port.maxLoops > 0 {
		fresh.SetMaxLoops(port.maxLoops)
	}
	return port.sessionID
}

// EnableWorkingHistoryRelease marks this adapter as backed by DurableHistory.
func (port *EnginePort) EnableWorkingHistoryRelease() {
	port.mu.Lock()
	port.releaseWorking = true
	port.mu.Unlock()
}

// ReleaseWorkingHistory clears only the provider working view. The next turn
// cold-loads a bounded tail from the durable owner.
func (port *EnginePort) ReleaseWorkingHistory() {
	port.mu.Lock()
	defer port.mu.Unlock()
	if !port.releaseWorking || port.engine == nil || port.activeCalls > 0 {
		return
	}
	port.engine.ClearHistory()
}
func (port *EnginePort) SessionID() string {
	port.mu.RLock()
	defer port.mu.RUnlock()
	return port.sessionID
}
func (port *EnginePort) SetSystemPrompt(prompt string) {
	port.mu.Lock()
	port.systemPrompt = prompt
	engine := port.engine
	port.mu.Unlock()
	if engine != nil {
		engine.SetSystemPrompt(prompt)
	}
}
func (port *EnginePort) SetMaxLoops(n int) {
	port.mu.Lock()
	port.maxLoops = n
	engine := port.engine
	port.mu.Unlock()
	if engine != nil {
		engine.SetMaxLoops(n)
	}
}
func (port *EnginePort) TraceText() string {
	if port.tracer == nil {
		return ""
	}
	view, err := port.tracer.Query(context.Background(), telemetry.Query{Limit: 200})
	if err != nil {
		return ""
	}
	var builder strings.Builder
	for _, trace := range view.Traces {
		builder.WriteString(fmt.Sprintf("追踪 %s\n", trace.TraceID))
		writeSpanSnapshot(&builder, trace.Root, 0)
	}
	if len(view.Events) > 0 {
		builder.WriteString(fmt.Sprintf("\n生命周期事件 %d 条\n", len(view.Events)))
		for _, event := range view.Events {
			builder.WriteString(fmt.Sprintf("  %s %s %s\n", event.Timestamp.Format("15:04:05"), event.Type, event.Status))
		}
	}
	return builder.String()
}
func (port *EnginePort) TokenCount() string {
	if port.tracer == nil {
		return "0"
	}
	view, err := port.tracer.Query(context.Background(), telemetry.Query{Limit: 200})
	if err != nil {
		return "0"
	}
	total := 0
	for _, event := range view.Events {
		if event.Type != telemetry.EventLLMAfter {
			continue
		}
		total += attrTelemetryInt(event.Attributes, telemetry.AttributeGenAIUsageInput)
		total += attrTelemetryInt(event.Attributes, telemetry.AttributeGenAIUsageOutput)
	}
	return strconv.Itoa(total)
}

// writeSpanSnapshot 递归渲染遥测 span 树（trace 视图文本）。
func writeSpanSnapshot(builder *strings.Builder, span telemetry.SpanSnapshot, depth int) {
	if span.Name == "" {
		return
	}
	indent := strings.Repeat("  ", depth)
	status := string(span.Status)
	if status == "" {
		status = "unset"
	}
	model := span.Attributes[telemetry.AttributeGenAIRequestModel]
	if model == nil {
		model = ""
	}
	builder.WriteString(fmt.Sprintf("%s%s %s [%s] %v\n", indent, span.Name, status, span.Kind, model))
	for _, child := range span.Children {
		writeSpanSnapshot(builder, child, depth+1)
	}
}

func attrTelemetryInt(attributes telemetry.Attributes, key string) int {
	if attributes == nil {
		return 0
	}
	value, ok := attributes[key]
	if !ok {
		return 0
	}
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case string:
		n, err := strconv.Atoi(typed)
		if err != nil {
			return 0
		}
		return n
	default:
		return 0
	}
}
func (port *EnginePort) History() []application.EngineMessage {
	return adaptMessages(port.RawHistory())
}
func (port *EnginePort) RawHistory() []seelebridge.Message {
	port.mu.RLock()
	defer port.mu.RUnlock()
	if port.engine == nil {
		return nil
	}
	return append([]seelebridge.Message(nil), port.engine.History()...)
}

type RuntimePort struct{ Runtime *seelebridge.Runtime }

func (port RuntimePort) Model() string         { return port.Runtime.Model() }
func (port RuntimePort) Provider() string      { return port.Runtime.Provider() }
func (port RuntimePort) ContextWindow() int    { return port.Runtime.ContextWindow() }
func (port RuntimePort) MaxOutputTokens() int  { return port.Runtime.MaxOutputTokens() }
func (port RuntimePort) ActivePlugin() string  { return port.Runtime.ActivePlugin() }
func (port RuntimePort) FullAccess() bool      { return port.Runtime.FullAccess() }
func (port RuntimePort) SetFullAccess(on bool) { port.Runtime.SetFullAccess(on) }
func (port RuntimePort) SetRuntimeVisibilityProjection(projection seelebridge.RuntimeVisibilityProjection) {
	port.Runtime.SetRuntimeVisibilityProjection(projection)
}
func (port RuntimePort) SetParentEvidenceProjection(projection seelebridge.ParentEvidenceProjection) {
	port.Runtime.SetParentEvidenceProjection(projection)
}
func (port RuntimePort) DrainSubagentContexts() []string { return port.Runtime.DrainSubagentContexts() }
func (port RuntimePort) SetPlanPolicy(policy seelebridge.PlanPolicy) {
	port.Runtime.SetPlanPolicy(policy)
}
func (port RuntimePort) PrepareReplan(ctx context.Context, request seelebridge.ReplanRequest) (seelebridge.PlanPreflight, error) {
	return port.Runtime.PrepareReplan(ctx, request)
}
func (port RuntimePort) ReplanMetrics() seelebridge.ReplanMetrics {
	return port.Runtime.ReplanMetrics()
}
func (port RuntimePort) SetPlanBranchBinding(binding seelebridge.PlanBranchBinding) {
	port.Runtime.SetPlanBranchBinding(binding)
}
func (port RuntimePort) RestorePlan(ctx context.Context, arguments string) error {
	return port.Runtime.RestorePlan(ctx, arguments)
}
func (port RuntimePort) BindProjectRoot(rootPath string) error {
	return port.Runtime.BindProjectRoot(rootPath)
}
func (port RuntimePort) UnbindProjectRoot() { port.Runtime.UnbindProjectRoot() }
func (port RuntimePort) TodoSnapshot() []seelebridge.TodoItem {
	return port.Runtime.TodoSnapshot()
}
func (port RuntimePort) SetTodoStatus(index int, status seelebridge.TodoItemStatus) error {
	return port.Runtime.SetTodoStatus(index, status)
}
func (port RuntimePort) TaskSnapshot() []seelebridge.TaskRecord {
	return port.Runtime.TaskSnapshot()
}
func (port RuntimePort) TaskAdd(spec seelebridge.TaskSpec) (seelebridge.TaskRecord, bool, error) {
	return port.Runtime.TaskAdd(spec)
}
func (port RuntimePort) ResolveTaskByKey(key string) (seelebridge.TaskRecord, bool, error) {
	return port.Runtime.ResolveTaskByKey(key)
}
func (port RuntimePort) TaskSetStatus(id string, status seelebridge.TaskStatus, evidence string) (seelebridge.TaskRecord, error) {
	return port.Runtime.TaskSetStatus(id, status, evidence)
}
func (port RuntimePort) TaskAttachParticipant(id, participant string) (seelebridge.TaskRecord, error) {
	return port.Runtime.TaskAttachParticipant(id, participant)
}
func (port RuntimePort) TaskChangedChannel() <-chan seelebridge.TaskRecord {
	return port.Runtime.TaskChangedChannel()
}
func (port RuntimePort) SubagentTreeEvents() <-chan struct{} {
	return port.Runtime.SubagentTreeEvents()
}
func (port RuntimePort) PlanNodeEventChannel() <-chan seelebridge.PlanNodeEvent {
	return port.Runtime.PlanNodeEventChannel()
}
func (port RuntimePort) SwitchSessionTasks(records []seelebridge.TaskRecord) {
	port.Runtime.SwitchSessionTasks(records)
}
func (port RuntimePort) ScheduledCommands() []seelebridge.ScheduledCommandInfo {
	return port.Runtime.ScheduledCommands()
}
func (port RuntimePort) ScheduledTasksSnapshot() []seelebridge.ScheduledTaskStatus {
	return port.Runtime.ScheduledTasksSnapshot()
}
func (port RuntimePort) ScheduleTask(ctx context.Context, spec seelebridge.ScheduledTaskSpec) (*seelebridge.ScheduledTaskStatus, error) {
	return port.Runtime.ScheduleTask(ctx, spec)
}
func (port RuntimePort) CancelScheduledTask(id string) error {
	return port.Runtime.CancelScheduledTask(id)
}
func (port RuntimePort) ClearSubagentTree() error {
	return port.Runtime.ClearSubagentTree()
}
func (port RuntimePort) SearchHistory(ctx context.Context, query string, limit int) (seelexctxsearch.Result, error) {
	return port.Runtime.SearchHistory(ctx, query, limit)
}
func (port RuntimePort) SelectAccount(name string) bool { return port.Runtime.SelectAccount(name) }
func (port RuntimePort) VisibleTools(ctx context.Context) []application.Tool {
	tools := port.Runtime.VisibleTools(ctx)
	result := make([]application.Tool, 0, len(tools))
	for _, tool := range tools {
		result = append(result, application.Tool{Name: tool.Name, Description: tool.Description})
	}
	return result
}
func (port RuntimePort) Accounts() []application.AccountInfo {
	accounts := port.Runtime.Accounts()
	result := make([]application.AccountInfo, 0, len(accounts))
	for _, account := range accounts {
		result = append(result, application.AccountInfo{Name: account.Name, Provider: account.Provider, Model: account.Model, Disabled: account.Disabled})
	}
	return result
}

type PluginPort struct{ Manager *plugin.Manager }

func (port PluginPort) Activate(ctx context.Context, name string) error {
	return port.Manager.Activate(ctx, name)
}
func (port PluginPort) Deactivate(ctx context.Context) error { return port.Manager.Deactivate(ctx) }
func (port PluginPort) Current() (application.PluginInfo, bool) {
	current, ok := port.Manager.Current()
	if !ok {
		return application.PluginInfo{}, false
	}
	return adaptPlugin(current), true
}
func (port PluginPort) All() []application.PluginInfo {
	plugins := port.Manager.All()
	result := make([]application.PluginInfo, 0, len(plugins))
	for _, item := range plugins {
		result = append(result, adaptPlugin(item))
	}
	return result
}

type WorkspacePort struct{ Repo *workspace.Repo }

func (port WorkspacePort) Create(name, rootPath, gitRemote string) (application.WorkspaceInfo, error) {
	w, err := port.Repo.Create(name, rootPath, gitRemote)
	if err != nil {
		return application.WorkspaceInfo{}, err
	}
	return adaptWorkspace(w), nil
}
func (port WorkspacePort) Get(id string) (application.WorkspaceInfo, error) {
	w, err := port.Repo.Get(id)
	if err != nil {
		return application.WorkspaceInfo{}, err
	}
	return adaptWorkspace(w), nil
}
func (port WorkspacePort) List() []application.WorkspaceInfo {
	list := port.Repo.List()
	out := make([]application.WorkspaceInfo, len(list))
	for i, w := range list {
		out[i] = adaptWorkspace(w)
	}
	return out
}
func (port WorkspacePort) Delete(id string) error { return port.Repo.Delete(id) }
func (port WorkspacePort) BindSession(sessionID, workspaceID string) {
	port.Repo.BindSession(sessionID, workspaceID)
}
func (port WorkspacePort) UnbindSession(sessionID string) {
	port.Repo.UnbindSession(sessionID)
}
func (port WorkspacePort) SessionWorkspace(sessionID string) (application.WorkspaceInfo, bool) {
	w, ok := port.Repo.SessionWorkspace(sessionID)
	if !ok {
		return application.WorkspaceInfo{}, false
	}
	return adaptWorkspace(w), true
}
func (port WorkspacePort) AllBindings() map[string]string {
	return port.Repo.AllBindings()
}
func (port WorkspacePort) DetectGitRemote(rootPath string) string {
	return workspace.DetectGitRemote(rootPath)
}

func adaptWorkspace(item workspace.Info) application.WorkspaceInfo {
	return application.WorkspaceInfo{
		ID:        item.ID,
		Name:      workspaceDisplayName(item.RootPath, item.Name),
		RootPath:  item.RootPath,
		GitRemote: item.GitRemote,
	}
}

func workspaceDisplayName(rootPath, fallback string) string {
	cleaned := filepath.Clean(strings.TrimSpace(rootPath))
	name := strings.TrimSpace(filepath.Base(cleaned))
	if name == "" || name == "." || name == string(filepath.Separator) || name == "/" || name == `\` {
		return strings.TrimSpace(fallback)
	}
	return name
}

type SkillPort struct{ Registry *skill.Registry }

func (port SkillPort) Get(name string) (application.SkillInfo, bool) {
	item, ok := port.Registry.Get(name)
	if !ok {
		return application.SkillInfo{}, false
	}
	return adaptSkill(item), true
}
func (port SkillPort) All() []application.SkillInfo {
	skills := port.Registry.All()
	result := make([]application.SkillInfo, 0, len(skills))
	for _, item := range skills {
		result = append(result, adaptSkill(item))
	}
	return result
}

type SessionPort struct {
	Manager *session.Manager
	Runtime *seelebridge.Runtime
}

// AttachSessionContext 装配会话 context 模块（system prompt + 四栈）：
// 创建按 sessionID 的 SessionContextStore，加载持久化记录并挂接到 Runtime，
// 使下一轮 prompt 组装（stackBlocks）能使用持久化的 Plan/Task/Skill/Compact
// 栈。损坏/不兼容的 context 显式失败（不静默降级成内存栈）。
func (port SessionPort) AttachSessionContext(workspaceID, sessionID string) error {
	store := sessionstore.NewSessionContextStore(port.Manager.Router(), sessionID)
	if err := store.Load(context.Background()); err != nil {
		return fmt.Errorf("load session context %q: %w", sessionID, err)
	}
	port.Runtime.AttachSessionContextStore(store)
	return nil
}

// DetachSessionContext 解绑当前会话的 context 模块（离开会话时调用，
// Runtime 退回内存态，防止四栈串到下一个会话）。
func (port SessionPort) DetachSessionContext() {
	port.Runtime.AttachSessionContextStore(nil)
}

func (port SessionPort) SaveCurrent(id string) error     { return port.Manager.SaveCurrent(id) }
func (port SessionPort) Delete(id string) error          { return port.Manager.Delete(id) }
func (port SessionPort) Resume(id string) error          { return port.Manager.Resume(id) }
func (port SessionPort) SetWorkspace(workspaceID string) { port.Manager.SetWorkspace(workspaceID) }
func (port SessionPort) Workspace() string               { return port.Manager.Workspace() }
func (port SessionPort) StorageConfig() (sessionstore.Config, error) {
	return port.Manager.StorageConfig()
}
func (port SessionPort) TestStorage(ctx context.Context, config sessionstore.Config) error {
	return port.Manager.TestStorage(ctx, config)
}
func (port SessionPort) ConfigureStorage(ctx context.Context, config sessionstore.Config) error {
	return port.Manager.ConfigureStorage(ctx, config)
}
func (port SessionPort) LoadHistory(id string) ([]application.EngineMessage, error) {
	messages, err := port.Manager.LoadHistory(id)
	if err != nil {
		return nil, err
	}
	return adaptMessages(messages), nil
}

func (port SessionPort) SaveSessionRecord(id string, record application.SessionRecord) error {
	payload, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("encode session record: %w", err)
	}
	return port.Manager.SaveState(id, payload)
}

func (port SessionPort) SaveSessionSnapshot(
	id string,
	providerHistory []application.EngineMessage,
	record application.SessionRecord,
	events []application.TranscriptEvent,
	results []application.StoredToolResult,
) error {
	payload, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("encode session record: %w", err)
	}
	commit := sessionstore.Commit{
		ProviderHistory: restoreMessages(providerHistory),
		Events:          storeTranscriptEvents(events),
		State:           payload,
		ToolResults:     storeToolResults(results),
	}
	return port.Manager.SaveCommit(id, commit)
}

func (port SessionPort) LoadTranscriptTailWorkspace(workspaceID, id string, tokenBudget, maxUnits int) ([]application.TranscriptEvent, error) {
	events, err := port.Manager.LoadEventTailByWorkspace(workspaceID, id, tokenBudget, maxUnits)
	if err != nil {
		return nil, err
	}
	return adaptTranscriptEvents(events), nil
}

func (port SessionPort) LoadToolResultWorkspace(workspaceID, id, resultRef string) (application.StoredToolResult, error) {
	result, err := port.Manager.LoadToolResultByWorkspace(workspaceID, id, resultRef)
	if err != nil {
		return application.StoredToolResult{}, err
	}
	return application.StoredToolResult{
		ToolResultRef: application.ToolResultRef{
			Ref: result.Ref, Tool: result.Tool, Digest: result.Digest, Size: result.Size,
			TokenCount: result.TokenCount, CreatedAt: result.CreatedAt,
		},
		Content: result.Content,
	}, nil
}

func storeTranscriptEvents(events []application.TranscriptEvent) []sessionstore.Event {
	stored := make([]sessionstore.Event, len(events))
	for index, event := range events {
		calls := make([]sessionstore.EventToolCall, len(event.ToolCalls))
		for callIndex, call := range event.ToolCalls {
			calls[callIndex] = sessionstore.EventToolCall{ID: call.ID, Name: call.Name, Arguments: call.Arguments}
		}
		stored[index] = sessionstore.Event{
			Seq: event.Seq, TaskID: event.TaskID, MessageID: event.MessageID, Role: event.Role,
			ReasoningContent: event.ReasoningContent, Content: event.Content,
			ToolCallID: event.ToolCallID, Name: event.Name, ToolCalls: calls,
			ResultRef: event.ResultRef, TokenCount: event.TokenCount, CreatedAt: event.CreatedAt,
		}
	}
	return stored
}

func adaptTranscriptEvents(events []sessionstore.Event) []application.TranscriptEvent {
	adapted := make([]application.TranscriptEvent, len(events))
	for index, event := range events {
		calls := make([]application.TranscriptToolCall, len(event.ToolCalls))
		for callIndex, call := range event.ToolCalls {
			calls[callIndex] = application.TranscriptToolCall{ID: call.ID, Name: call.Name, Arguments: call.Arguments}
		}
		adapted[index] = application.TranscriptEvent{
			Seq: event.Seq, TaskID: event.TaskID, MessageID: event.MessageID, Role: event.Role,
			ReasoningContent: event.ReasoningContent, Content: event.Content,
			ToolCallID: event.ToolCallID, Name: event.Name, ToolCalls: calls,
			ResultRef: event.ResultRef, TokenCount: event.TokenCount, CreatedAt: event.CreatedAt,
		}
	}
	return adapted
}

func storeToolResults(results []application.StoredToolResult) []sessionstore.ToolResult {
	stored := make([]sessionstore.ToolResult, len(results))
	for index, result := range results {
		stored[index] = sessionstore.ToolResult{
			Ref: result.Ref, Tool: result.Tool, Content: result.Content, Digest: result.Digest,
			Size: result.Size, TokenCount: result.TokenCount, CreatedAt: result.CreatedAt,
		}
	}
	return stored
}

func (port SessionPort) LoadSessionRecord(id string) (application.SessionRecord, error) {
	payload, err := port.Manager.LoadState(id)
	if err != nil {
		return application.SessionRecord{}, err
	}
	return decodeSessionRecord(payload, id)
}

func (port SessionPort) LoadSessionRecordWorkspace(workspaceID, id string) (application.SessionRecord, error) {
	payload, err := port.Manager.LoadStateByWorkspace(workspaceID, id)
	if err != nil {
		return application.SessionRecord{}, err
	}
	return decodeSessionRecord(payload, id)
}

func (port SessionPort) LoadConversationRangeWorkspace(workspaceID, id string, offset, limit int) ([]application.Message, int, error) {
	// conversation 模块冷读：只解析 state blob 的 conversation 子树，
	// 不反序列化 Plan/Execution/Projection 等非 conversation 模块
	// （模块化方案 plan.md §阶段1：长会话翻页不加载完整 state blob）。
	messages, total, err := port.Manager.LoadConversationRangeByWorkspace(workspaceID, id, offset, limit)
	if err != nil {
		return nil, 0, err
	}
	adapted := make([]application.Message, 0, len(messages))
	for _, message := range messages {
		adapted = append(adapted, adaptStoredConversationMessage(message))
	}
	return adapted, total, nil
}

// adaptStoredConversationMessage 把存储层 conversation DTO 转为 UI 消息
// （Tool 指针独立拷贝，避免共享内部状态）。
func adaptStoredConversationMessage(message sessionstore.ConversationMessage) application.Message {
	adapted := application.Message{ID: message.ID, Role: message.Role, Content: message.Content, CreatedAt: message.CreatedAt}
	if message.Tool != nil {
		tool := *message.Tool
		adapted.Tool = &application.ToolCall{
			ID: tool.ID, Name: tool.Name, Arguments: tool.Arguments,
			Result: tool.Result, Error: tool.Error, Status: tool.Status, Duration: tool.Duration,
		}
	}
	return adapted
}

func decodeSessionRecord(payload []byte, sessionID string) (application.SessionRecord, error) {
	var version struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(payload, &version); err != nil {
		return application.SessionRecord{}, fmt.Errorf("decode session state header: %w", err)
	}
	if version.Version == 1 {
		var archive application.SessionArchive
		if err := json.Unmarshal(payload, &archive); err != nil {
			return application.SessionRecord{}, fmt.Errorf("decode legacy session archive: %w", err)
		}
		return migrateSessionArchive(sessionID, archive), nil
	}
	var record application.SessionRecord
	if err := json.Unmarshal(payload, &record); err != nil {
		return application.SessionRecord{}, fmt.Errorf("decode session record: %w", err)
	}
	if record.Version == 2 && record.ID == sessionID {
		return record, nil
	}
	if record.Version == 3 && record.ID == sessionID {
		return record, nil
	}
	return application.SessionRecord{}, fmt.Errorf("unsupported session state version %d", version.Version)
}

func migrateSessionArchive(sessionID string, archive application.SessionArchive) application.SessionRecord {
	record := application.SessionRecord{
		Version: 2,
		ID:      sessionID,
		Title: application.SessionTitle{
			Value:       archive.Name,
			Source:      "legacy_history",
			FinalizedAt: archive.UpdatedAt,
		},
		Conversation: application.ConversationRecord{
			Messages:  archive.Conversation,
			UpdatedAt: archive.UpdatedAt,
		},
		Execution: application.SessionExecutionRecord{
			Task:         archive.Task,
			ReadFiles:    archive.ReadFiles,
			Continuation: archive.Continuation,
		},
		UpdatedAt: archive.UpdatedAt,
	}
	if archive.Plan != nil || archive.PlanArguments != "" {
		record.ActivePlanID = "legacy-plan"
		record.PlanStack = []application.SessionPlanFrame{{
			ID:        record.ActivePlanID,
			Plan:      archive.Plan,
			Arguments: archive.PlanArguments,
			LoadedAt:  archive.UpdatedAt,
			UpdatedAt: archive.UpdatedAt,
		}}
	}
	return record
}
func (port SessionPort) LoadHistoryWorkspace(workspaceID, id string) ([]application.EngineMessage, error) {
	messages, err := port.Manager.LoadHistoryByWorkspace(workspaceID, id)
	if err != nil {
		return nil, err
	}
	return adaptMessages(messages), nil
}
func (port SessionPort) LoadHistoryRange(id string, offset, limit int) ([]application.EngineMessage, int, error) {
	messages, total, err := port.Manager.LoadHistoryRange(id, offset, limit)
	if err != nil {
		return nil, 0, err
	}
	return adaptMessages(messages), total, nil
}
func (port SessionPort) LoadHistoryRangeWorkspace(workspaceID, id string, offset, limit int) ([]application.EngineMessage, int, error) {
	messages, total, err := port.Manager.LoadHistoryRangeByWorkspace(workspaceID, id, offset, limit)
	if err != nil {
		return nil, 0, err
	}
	return adaptMessages(messages), total, nil
}
func (port SessionPort) MessageCount(id string) (int, error) {
	return port.Manager.MessageCount(id)
}
func (port SessionPort) List() []application.SessionInfo {
	return adaptSessionMeta(port.Manager.List())
}
func (port SessionPort) ListWorkspace(workspaceID string) []application.SessionInfo {
	return adaptSessionMeta(port.Manager.ListByWorkspace(workspaceID))
}
func (port SessionPort) DeleteWorkspace(workspaceID, id string) error {
	return port.Manager.DeleteByWorkspace(workspaceID, id)
}

func adaptSessionMeta(sessions []seelebridge.SessionMeta) []application.SessionInfo {
	result := make([]application.SessionInfo, 0, len(sessions))
	for _, item := range sessions {
		result = append(result, application.SessionInfo{ID: item.SessionID, UpdatedAt: item.UpdatedAt, TokenCount: item.TokenCount})
	}
	return result
}

func adaptPlugin(item plugin.Plugin) application.PluginInfo {
	return application.PluginInfo{Name: item.Name, Description: item.Description, Prompt: item.Prompt}
}
func adaptSkill(item skill.Skill) application.SkillInfo {
	return application.SkillInfo{Name: item.Name, Description: item.Description, Prompt: item.Prompt}
}
func adaptMessages(messages []seelebridge.Message) []application.EngineMessage {
	result := make([]application.EngineMessage, 0, len(messages))
	for _, message := range messages {
		adapted := application.EngineMessage{
			Role: message.Role, ReasoningContent: message.ReasoningContent,
			ToolCallID: message.ToolCallID, Name: message.Name,
		}
		if message.Content != nil {
			adapted.Content = *message.Content
			adapted.ContentSet = true
		}
		adapted.ToolCalls = make([]application.EngineToolCall, 0, len(message.ToolCalls))
		for _, call := range message.ToolCalls {
			adapted.ToolCalls = append(adapted.ToolCalls, application.EngineToolCall{ID: call.ID, Name: call.Function.Name, Arguments: call.Function.Arguments})
		}
		result = append(result, adapted)
	}
	return result
}

func restoreMessages(messages []application.EngineMessage) []seelebridge.Message {
	result := make([]seelebridge.Message, 0, len(messages))
	for _, message := range messages {
		adapted := seelebridge.Message{
			Role: message.Role, ReasoningContent: message.ReasoningContent,
			ToolCallID: message.ToolCallID, Name: message.Name,
		}
		if message.ContentSet || message.Content != "" {
			content := message.Content
			adapted.Content = &content
		}
		for _, call := range message.ToolCalls {
			adapted.ToolCalls = append(adapted.ToolCalls, types.ToolCall{
				ID: call.ID, Type: "function",
				Function: types.ToolCallFunction{Name: call.Name, Arguments: call.Arguments},
			})
		}
		result = append(result, adapted)
	}
	return result
}

func ApprovalOption(choice string) application.InteractionOption {
	options := map[string]application.InteractionOption{
		"execute": {ID: "execute", Label: "执行", Description: "按计划执行", Style: "primary"},
		"skip":    {ID: "skip", Label: "跳过", Description: "跳过当前节点", Style: "secondary"},
		"abort":   {ID: "abort", Label: "终止", Description: "终止工作流", Style: "danger"},
		"confirm": {ID: "confirm", Label: "确认", Description: "确认并继续", Style: "primary"},
		"retry":   {ID: "retry", Label: "重试", Description: "重新执行", Style: "warning"},
	}
	if option, ok := options[choice]; ok {
		return option
	}
	return application.InteractionOption{ID: choice, Label: choice}
}

func ApprovalAccepted(optionID string) bool {
	switch strings.ToLower(strings.TrimSpace(optionID)) {
	case "", "__cancel__", "__timeout__", "no", "deny", "reject", "refuse", "cancel", "abort", "skip", "false", "否", "拒绝", "取消", "终止", "跳过":
		return false
	default:
		return true
	}
}

// planApprovalGate 适配框架 approve.ApprovalGate → application.ApprovalBroker。
// plan_run 执行到 kind:manual 节点时，框架调用 Ask 阻塞等待用户在 UI 中选择。
type PlanApprovalGate struct {
	Broker *application.ApprovalBroker
}

// Ask 将框架审批请求转换为 ApprovalBroker.Request，阻塞等待用户选择后返回。
func (g *PlanApprovalGate) Ask(ctx context.Context, q approve.Question) (any, error) {
	options := make([]application.InteractionOption, len(q.Options))
	for i, opt := range q.Options {
		options[i] = application.InteractionOption{
			ID: opt.Key, Label: opt.Label,
			Description: opt.Description, Style: opt.Style,
		}
	}

	req := application.ApprovalRequest{
		ID:       q.ID,
		Question: q.Content,
		Options:  options,
		Timeout:  q.Timeout,
	}

	decision, err := g.Broker.Request(ctx, req)
	if err != nil {
		return "", err
	}
	return decision.OptionID, nil
}

// convertPermissionOptions 将 permission.ApproveOption 转为 application.InteractionOption。
func ConvertPermissionOptions(opts []toolspermission.ApproveOption) []application.InteractionOption {
	out := make([]application.InteractionOption, len(opts))
	for i, o := range opts {
		out[i] = application.InteractionOption{
			ID: o.Key, Label: o.Label, Description: o.Description, Style: o.Style,
		}
	}
	return out
}
