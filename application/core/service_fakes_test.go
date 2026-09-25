package core

import (
	"context"
	"errors"
	"fmt"
	"github.com/RedHuang-0622/Seele/types"
	"github.com/RedHuang-0622/seelex/application/contract/dto"
	"github.com/RedHuang-0622/seelex/internal/testutil"
	"github.com/RedHuang-0622/seelex/seelebridge"
	seelexctxsearch "github.com/RedHuang-0622/seelex/seelexctx/search"
	"github.com/RedHuang-0622/seelex/seelexctx/snapshot"
	"sync"
	"sync/atomic"
	"time"
)

type fakeEngine struct {
	*testutil.EmbeddedChatEngine
	mu                 sync.Mutex
	history            []EngineMessage
	historyBeforeChat  []EngineMessage
	chunks             []string
	prompt             string
	chatErr            error
	chatErrors         []error
	chatInputs         []string
	appendChatHistory  bool
	cleared            bool
	lazyStart          bool
	sessionID          string
	starts             int
	lastInput          string
	maxLoops           int
	releaseCalls       int
	loadedSessions     map[string]bool
	nodeContext        *snapshot.ContextSnapshot
	nodeToolResultFn   func(string, string) (string, bool)
	nodeWorktreeInfoFn func(string) (seelebridge.NodeWorktreeInfo, bool)
	subAgentTree       []dto.SubAgentTreeNode
	subagentLiveFn     func(string) ([]dto.SubagentLiveEvent, <-chan dto.SubagentLiveEvent, func(), error)
}

type sessionBackedBlockingEngine struct {
	*fakeEngine
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (engine *sessionBackedBlockingEngine) SessionBacked() bool { return true }

func (engine *sessionBackedBlockingEngine) ChatStream(ctx context.Context, input string, onChunk func(string)) (string, error) {
	engine.once.Do(func() {
		close(engine.started)
		select {
		case <-engine.release:
		case <-ctx.Done():
		}
	})
	return engine.fakeEngine.ChatStream(ctx, input, onChunk)
}

// ChatStreamFor 显式转发到自身 ChatStream（覆盖内嵌 fakeEngine 的提升方法，
// 保证会话路由面下阻塞/取消语义仍生效）。
func (engine *sessionBackedBlockingEngine) ChatStreamFor(sessionID string, ctx context.Context, input string, onChunk func(string)) (string, error) {
	return engine.ChatStream(ctx, input, onChunk)
}

type blockingSaveSessions struct {
	fakeSessions
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (sessions *blockingSaveSessions) SaveCurrent(string) error {
	sessions.once.Do(func() { close(sessions.entered) })
	<-sessions.release
	return nil
}

func (engine *fakeEngine) ChatStream(ctx context.Context, input string, onChunk func(string)) (string, error) {
	engine.mu.Lock()
	engine.historyBeforeChat = append([]EngineMessage(nil), engine.history...)
	engine.mu.Unlock()
	for _, chunk := range engine.chunks {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
			onChunk(chunk)
		}
	}
	engine.mu.Lock()
	engine.lastInput = input
	engine.chatInputs = append(engine.chatInputs, input)
	if engine.appendChatHistory {
		engine.history = append(engine.history, EngineMessage{Role: "user", Content: input}, EngineMessage{Role: "assistant", Content: "answer"})
	} else {
		engine.history = []EngineMessage{{Role: "user", Content: input}, {Role: "assistant", Content: "answer"}}
	}
	err := engine.chatErr
	if len(engine.chatErrors) > 0 {
		err = engine.chatErrors[0]
		engine.chatErrors = engine.chatErrors[1:]
	}
	engine.mu.Unlock()
	return "answer", err
}

func (engine *fakeEngine) History() []EngineMessage {
	engine.mu.Lock()
	defer engine.mu.Unlock()
	return append([]EngineMessage(nil), engine.history...)
}

func (engine *fakeEngine) ClearHistory() {
	engine.mu.Lock()
	engine.history = nil
	engine.cleared = true
	engine.mu.Unlock()
}

func (engine *fakeEngine) SessionID() string {
	engine.mu.Lock()
	defer engine.mu.Unlock()
	if engine.lazyStart && engine.sessionID == "" {
		return ""
	}
	if engine.sessionID == "" {
		return "session-1"
	}
	return engine.sessionID
}

func (engine *fakeEngine) StartSession() string {
	engine.mu.Lock()
	defer engine.mu.Unlock()
	engine.starts++
	engine.sessionID = "session-new"
	engine.history = nil
	engine.cleared = true
	if engine.loadedSessions == nil {
		engine.loadedSessions = make(map[string]bool)
	}
	engine.loadedSessions["session-new"] = true
	return engine.sessionID
}

// ActivateSession 以显式会话 ID 创建并激活引擎实例（G4 早分配 SID：
// 草稿物化复用草稿 ID，不再经 StartSession 另发新 ID）。
func (engine *fakeEngine) ActivateSession(sessionID string) error {
	engine.mu.Lock()
	defer engine.mu.Unlock()
	engine.sessionID = sessionID
	engine.history = nil
	engine.cleared = true
	if engine.loadedSessions == nil {
		engine.loadedSessions = make(map[string]bool)
	}
	engine.loadedSessions[sessionID] = true
	return nil
}

func (engine *fakeEngine) ReplaceHistory(sessionID string, history []EngineMessage) error {
	engine.mu.Lock()
	defer engine.mu.Unlock()
	engine.sessionID = sessionID
	engine.history = append([]EngineMessage(nil), history...)
	if engine.loadedSessions == nil {
		engine.loadedSessions = make(map[string]bool)
	}
	engine.loadedSessions[sessionID] = true
	return nil
}

func (engine *fakeEngine) SetSystemPrompt(prompt string) {
	engine.mu.Lock()
	engine.prompt = prompt
	engine.mu.Unlock()
}

func (engine *fakeEngine) SetMaxLoops(maxLoops int) {
	engine.mu.Lock()
	engine.maxLoops = maxLoops
	engine.mu.Unlock()
}

func (engine *fakeEngine) AppendHistory(msg types.Message) {
	engine.mu.Lock()
	defer engine.mu.Unlock()
	content := ""
	if msg.Content != nil {
		content = *msg.Content
	}
	toolCalls := make([]EngineToolCall, 0, len(msg.ToolCalls))
	for _, call := range msg.ToolCalls {
		toolCalls = append(toolCalls, EngineToolCall{ID: call.ID, Name: call.Function.Name, Arguments: call.Function.Arguments})
	}
	engine.history = append(engine.history, EngineMessage{Role: msg.Role, Content: content, ContentSet: msg.Content != nil, ReasoningContent: msg.ReasoningContent, ToolCalls: toolCalls, ToolCallID: msg.ToolCallID, Name: msg.Name})
}

func (*fakeEngine) TraceText() string { return "trace" }

func (*fakeEngine) TokenCount() string { return "12" }

func (*fakeEngine) NodeSessionConversation(string) ([]types.Message, bool) { return nil, false }

func (engine *fakeEngine) NodeContextSnapshot(string) (*snapshot.ContextSnapshot, bool) {
	if engine.nodeContext == nil {
		return nil, false
	}
	return engine.nodeContext, true
}

func (engine *fakeEngine) NodeToolResult(nodeID, ref string) (string, bool) {
	if engine.nodeToolResultFn == nil {
		return "", false
	}
	return engine.nodeToolResultFn(nodeID, ref)
}

func (engine *fakeEngine) NodeWorktreeInfoFor(nodeID string) (seelebridge.NodeWorktreeInfo, bool) {
	if engine.nodeWorktreeInfoFn == nil {
		return seelebridge.NodeWorktreeInfo{}, false
	}
	return engine.nodeWorktreeInfoFn(nodeID)
}

func (engine *fakeEngine) SubscribeSubagentLive(nodeID string) ([]dto.SubagentLiveEvent, <-chan dto.SubagentLiveEvent, func(), error) {
	if engine.subagentLiveFn == nil {
		return nil, nil, func() {}, fmt.Errorf("fake engine: live stream not configured")
	}
	return engine.subagentLiveFn(nodeID)
}

func (engine *fakeEngine) SubAgentTree() []dto.SubAgentTreeNode {
	engine.mu.Lock()
	defer engine.mu.Unlock()
	return append([]dto.SubAgentTreeNode(nil), engine.subAgentTree...)
}

func (engine *fakeEngine) ReleaseWorkingHistory() {
	engine.mu.Lock()
	engine.releaseCalls++
	engine.mu.Unlock()
}

func (engine *fakeEngine) ReleaseWorkingHistoryFor(sessionID string) {
	engine.mu.Lock()
	engine.releaseCalls++
	engine.mu.Unlock()
}

// ── SessionChatEngine 会话路由面（多会话并行测试用）────────────────────
// fakeEngine 是单引擎桩：For 变体忽略 sessionID 差异，读写同一 history，
// 方法均加锁，供 -race 竞态测试验证调用方不误清/不回退活跃引擎。

func (engine *fakeEngine) ChatStreamFor(sessionID string, ctx context.Context, input string, onChunk func(string)) (string, error) {
	return engine.ChatStream(ctx, input, onChunk)
}

func (engine *fakeEngine) HistoryFor(sessionID string) []EngineMessage {
	return engine.History()
}

func (engine *fakeEngine) AppendHistoryFor(sessionID string, msg types.Message) {
	engine.AppendHistory(msg)
}

func (engine *fakeEngine) ClearHistoryFor(sessionID string) {
	engine.ClearHistory()
}

func (engine *fakeEngine) SetSystemPromptFor(sessionID, prompt string) {
	engine.SetSystemPrompt(prompt)
}

func (engine *fakeEngine) ReplaceHistoryFor(sessionID string, history []EngineMessage) error {
	return engine.ReplaceHistory(sessionID, history)
}

func (engine *fakeEngine) HasSession(sessionID string) bool {
	engine.mu.Lock()
	defer engine.mu.Unlock()
	return engine.loadedSessions[sessionID]
}

type fakeRuntime struct {
	account    string
	fullAccess bool
	// fullAccessBySession 是会话级全权选择（镜像生产权限门的会话解析：
	// 空串键不存在 → 回退 fullAccess 进程默认）。
	fullAccessBySession map[string]bool
	// fullAccessMu 保护 fullAccess：多个会话的 runChat 起点并发 sync
	// 引擎门（G4 每会话选择 → 进程单例门镜像），fake 必须与生产
	// PermissionGate（内部 RWMutex）同构，否则 -race 报竞争。
	fullAccessMu sync.RWMutex
	binding      dto.PlanBranchBinding
	planPolicy   dto.PlanPolicy
	// visibleTools 覆盖 VisibleTools 的返回（默认给 [read]）：用例需要
	// "某个工具名真的可见"时（未知命令提示要把工具与命令分开）才设置。
	visibleTools []Tool
	// releasedAsyncSessions 记录 ReleaseSessionAsync 收到过哪些会话（"会话销毁即杀
	// 后台命令"的用例断言 core 真的转了这一道）。
	releasedAsyncSessions []string
	// asyncPending 是 AsyncPendingFor 的回答值（无进展判据的用例用它模拟在途后台命令）。
	asyncPending int
	// asyncRuns 是 AsyncRunsSnapshot 的回答值：后台行投影用例用它直接喂投影输入。
	asyncRuns []dto.AsyncRunRecord
	// asyncEvents 是 AsyncRunEvents 的回答口（nil = 该消费者不启动，同生产关闭能力时）。
	asyncEvents chan struct{}
	// planPolicyBySession 是按会话 plan 策略槽（G1-C：镜像生产
	// Runtime.SetPlanPolicyFor 语义；fake 需锁保护并发 runChat 写入）。
	planPolicyMu        sync.Mutex
	planPolicyBySession map[string]dto.PlanPolicy
	visibility          seelebridge.RuntimeVisibilityProjection
	evidence            seelebridge.ParentEvidenceProjection
	mailbox             []string
	mailboxMu           sync.Mutex
	replans             []dto.ReplanRequest
	replanResult        dto.PlanPreflight
	replanErr           error
	replanMetrics       dto.ReplanMetrics
	// replanMetricsBySession 是按会话 replan 统计（G1/M5：fake 镜像
	// 生产 Runtime.ReplanMetricsFor 的会话槽语义）。
	replanMetricsBySession map[string]dto.ReplanMetrics
	projectRoot            string
	// projectRootMu 保护 projectRoot：后台会话的 runChat（rebindViewWorkspaceWhenIdle
	// → bindProjectRootIfSafe）会在 chat goroutine 上写它，而用例在测试 goroutine 上
	// 读它——不加锁就是数据竞争（-race 实报：TestBackgroundSessionKeepsOwnProjectRoot
	// 读 vs fakeRuntime.BindProjectRoot 写）。生产 Runtime 本身线程安全，fake 必须镜像
	// 这一点（同 SetCurrentTaskBatch 的 mailboxMu 处理）。
	projectRootMu sync.RWMutex
	// sessionProjectRoots 是按会话的工具路径根（镜像生产
	// Runtime.BindProjectRootFor → seelebridge projectScope 的会话分格）。
	sessionProjectRootsMu sync.Mutex
	sessionProjectRoots   map[string]string
	currentBatch          string
	todoMu                sync.Mutex
	todoItems             []dto.TodoItem
	tasks                 map[string]dto.TaskRecord
	// sessionTaskSnapshots 是会话切换时保存的 task 快照（镜像生产 Runtime
	// 的按会话分片语义；阶段 0 持久化按会话取快照）。
	sessionTaskSnapshots map[string][]dto.TaskRecord
	currentTaskSession   string
	sessionWorkspaces    map[string]string
	scheduledTasks       []seelebridge.ScheduledTaskStatus
	scheduledSpecs       []seelebridge.ScheduledTaskSpec
	cancelledTasks       []string
	scheduleErr          error
	searchResult         seelexctxsearch.Result
	searchErr            error
	// 子代理恢复面（headless subagent.* 的测试桩投影）。
	subagentRecovery     []dto.SubagentRecoveryView
	subagentResumeReport dto.SubagentResumeReport
	subagentResumeErr    error
}

func (*fakeRuntime) Model() string { return "test-model" }

func (*fakeRuntime) Provider() string { return "test-provider" }

func (*fakeRuntime) Accounts() []AccountInfo {
	return []AccountInfo{{Name: "primary", Provider: "test", Model: "m"}}
}

func (runtime *fakeRuntime) SelectAccount(name string) bool {
	if name != "primary" {
		return false
	}
	runtime.account = name
	return true
}

func (runtime *fakeRuntime) VisibleTools(context.Context) []Tool {
	if runtime != nil && len(runtime.visibleTools) > 0 {
		return runtime.visibleTools
	}
	return []Tool{{Name: "read", Description: "read files"}}
}

func (*fakeRuntime) ActivePlugin() string { return "default" }

func (runtime *fakeRuntime) FullAccess() bool {
	runtime.fullAccessMu.RLock()
	defer runtime.fullAccessMu.RUnlock()
	return runtime.fullAccess
}

func (runtime *fakeRuntime) SetFullAccess(on bool) {
	runtime.SetFullAccessFor("", on)
}

// SetFullAccessFor 镜像生产权限门的会话级解析（空会话 ID = 进程级默认）：
// 全权是会话级用户决定，A 的开关不替 B 放行，B 的起点同步也关不掉 A。
func (runtime *fakeRuntime) SetFullAccessFor(sessionID string, on bool) {
	runtime.fullAccessMu.Lock()
	defer runtime.fullAccessMu.Unlock()
	if sessionID == "" {
		runtime.fullAccess = on
		return
	}
	if runtime.fullAccessBySession == nil {
		runtime.fullAccessBySession = make(map[string]bool)
	}
	runtime.fullAccessBySession[sessionID] = on
}

// FullAccessFor 返回指定会话生效的全权模式（会话级选择优先，未选择回退
// 进程级默认）——与生产 PermissionGate.fullAccessForSession 同源语义。
func (runtime *fakeRuntime) FullAccessFor(sessionID string) bool {
	runtime.fullAccessMu.RLock()
	defer runtime.fullAccessMu.RUnlock()
	if on, ok := runtime.fullAccessBySession[sessionID]; ok {
		return on
	}
	return runtime.fullAccess
}

// PermissionTier 返回进程级默认权限档位（fake 只有二元口径：fullAccess → full）。
func (runtime *fakeRuntime) PermissionTier() string {
	if runtime.FullAccess() {
		return dto.PermissionTierFull
	}
	return dto.PermissionTierManual
}

// SetPermissionTierFor 把档位落到 fake 的二元全权口径（full → true，其余 → false），
// 校验档位 id（与生产 SetPermissionTierFor 同源）。
func (runtime *fakeRuntime) SetPermissionTierFor(sessionID, tier string) error {
	normalized, err := dto.NormalizePermissionTier(tier)
	if err != nil {
		return err
	}
	runtime.SetFullAccessFor(sessionID, normalized == dto.PermissionTierFull)
	return nil
}

// SetRuntimeVisibilityProjection / SetParentEvidenceProjection 会被并行会话的
// 多个 runChat 并发调用（M2：每个会话各自 publishRuntimeProjections），
// 生产 Runtime 的投影存储是并发安全的；fake 需用锁镜像，否则 -race 报
// 数据竞争。
func (runtime *fakeRuntime) SetRuntimeVisibilityProjection(projection seelebridge.RuntimeVisibilityProjection) {
	runtime.mailboxMu.Lock()
	runtime.visibility = projection
	runtime.mailboxMu.Unlock()
	debugLog("fakeRuntime.SetRuntimeVisibilityProjection goalskill=%v", projection.GoalSkillActive)
}

func (runtime *fakeRuntime) SetParentEvidenceProjection(projection seelebridge.ParentEvidenceProjection) {
	runtime.mailboxMu.Lock()
	runtime.evidence = projection
	runtime.mailboxMu.Unlock()
	debugLog("fakeRuntime.SetParentEvidenceProjection sessionID=%q", projection.SessionID)
}

// DrainSubagentContexts 排空 merge-back 邮箱。M2 多会话并行下多个
// runChat 会并发调用（生产 Runtime 的 actor mailbox 线程安全），fake 需
// 用锁镜像该语义，否则 -race 报数据竞争。
func (runtime *fakeRuntime) DrainSubagentContexts() []string {
	runtime.mailboxMu.Lock()
	items := append([]string(nil), runtime.mailbox...)
	runtime.mailbox = nil
	runtime.mailboxMu.Unlock()
	if len(items) > 0 {
		debugLog("fakeRuntime.DrainSubagentContexts drained=%d items=%q", len(items), items)
	}
	return items
}

func (runtime *fakeRuntime) SetPlanPolicy(policy dto.PlanPolicy) {
	runtime.planPolicy = policy
}

func (runtime *fakeRuntime) SetPlanPolicyFor(sessionID string, policy dto.PlanPolicy) {
	runtime.planPolicyMu.Lock()
	defer runtime.planPolicyMu.Unlock()
	if runtime.planPolicyBySession == nil {
		runtime.planPolicyBySession = make(map[string]dto.PlanPolicy)
	}
	runtime.planPolicyBySession[sessionID] = policy
}

func (runtime *fakeRuntime) planPolicyFor(sessionID string) (dto.PlanPolicy, bool) {
	runtime.planPolicyMu.Lock()
	defer runtime.planPolicyMu.Unlock()
	policy, ok := runtime.planPolicyBySession[sessionID]
	return policy, ok
}

func (runtime *fakeRuntime) PrepareReplan(_ context.Context, request dto.ReplanRequest) (dto.PlanPreflight, error) {
	runtime.replans = append(runtime.replans, request)
	return runtime.replanResult, runtime.replanErr
}

func (runtime *fakeRuntime) ReplanMetrics() dto.ReplanMetrics { return runtime.replanMetrics }

func (runtime *fakeRuntime) ReplanMetricsFor(sessionID string) dto.ReplanMetrics {
	if runtime.replanMetricsBySession == nil {
		return runtime.replanMetrics
	}
	return runtime.replanMetricsBySession[sessionID]
}

func (runtime *fakeRuntime) SetPlanBranchBinding(binding dto.PlanBranchBinding) {
	runtime.binding = binding
}

func (runtime *fakeRuntime) TodoSnapshot() []dto.TodoItem {
	runtime.todoMu.Lock()
	defer runtime.todoMu.Unlock()
	return append([]dto.TodoItem(nil), runtime.todoItems...)
}

func (runtime *fakeRuntime) SetTodoStatus(index int, status dto.TodoItemStatus) error {
	runtime.todoMu.Lock()
	defer runtime.todoMu.Unlock()
	if index < 0 || index >= len(runtime.todoItems) {
		return fmt.Errorf("fake todo index %d out of range", index)
	}
	runtime.todoItems[index].Status = status
	runtime.todoItems[index].Done = status == dto.TodoItemDone
	return nil
}

// TaskSnapshot 返回**项目/全局** task 表（镜像生产 Runtime：实时注册表 +
// 各会话 scope 分区合并，按 ID 去重）——工作表格是跨会话台账。
func (runtime *fakeRuntime) TaskSnapshot() []dto.TaskRecord {
	runtime.todoMu.Lock()
	defer runtime.todoMu.Unlock()
	return runtime.globalSnapshotLocked()
}

// ReleaseSessionAsync 记下被释放的会话，供"会话销毁即杀后台命令"的用例断言。
func (runtime *fakeRuntime) ReleaseSessionAsync(sessionID string) int {
	runtime.releasedAsyncSessions = append(runtime.releasedAsyncSessions, sessionID)
	return 0
}

// AsyncPendingFor 回答测试显式设置的在途后台命令数。
func (runtime *fakeRuntime) AsyncPendingFor(string) int { return runtime.asyncPending }

// AsyncRunsSnapshot 回答测试显式设置的后台执行投影（后台行投影用例的输入源）。
func (runtime *fakeRuntime) AsyncRunsSnapshot() []dto.AsyncRunRecord { return runtime.asyncRuns }

// AsyncRunEvents 返回测试自己持有的信号口（默认 nil = 消费者不启动）。
func (runtime *fakeRuntime) AsyncRunEvents() <-chan struct{} { return runtime.asyncEvents }

// TaskSnapshotFor 保持会话粒度（持久化落盘/请求尾部打点块用）。
func (runtime *fakeRuntime) TaskSnapshotFor(sessionID string) []dto.TaskRecord {
	runtime.todoMu.Lock()
	defer runtime.todoMu.Unlock()
	if sessionID == "" || sessionID == runtime.currentTaskSession {
		return runtime.snapshotLocked()
	}
	return append([]dto.TaskRecord(nil), runtime.sessionTaskSnapshots[sessionID]...)
}

// globalSnapshotLocked 合并实时注册表与所有会话分区（跨会话身份去重：
// 幂等键优先、否则行 ID；注册表优先），并标注**归属会话**（实时 = 当前会话，
// 分区 = 分区键）——镜像生产 Runtime.TaskSnapshot 的会话筛选轴数据面。
func (runtime *fakeRuntime) globalSnapshotLocked() []dto.TaskRecord {
	live := runtime.snapshotLocked()
	records := make([]dto.TaskRecord, 0, len(live))
	seen := make(map[string]struct{}, len(live))
	identityOf := func(record dto.TaskRecord) string {
		if record.Key != "" {
			return "key:" + record.Key
		}
		return "id:" + record.ID
	}
	for _, record := range live {
		record.SessionID = runtime.currentTaskSession
		seen[identityOf(record)] = struct{}{}
		records = append(records, record)
	}
	for sessionID, partition := range runtime.sessionTaskSnapshots {
		for _, record := range partition {
			identity := identityOf(record)
			if _, exists := seen[identity]; exists {
				continue
			}
			seen[identity] = struct{}{}
			record.SessionID = sessionID
			records = append(records, record)
		}
	}
	return records
}

func (runtime *fakeRuntime) snapshotLocked() []dto.TaskRecord {
	records := make([]dto.TaskRecord, 0, len(runtime.todoItems)+len(runtime.tasks))
	for index, item := range runtime.todoItems {
		status := dto.TaskPending
		switch item.Status {
		case dto.TodoItemDoing:
			status = dto.TaskDoing
		case dto.TodoItemDone:
			status = dto.TaskCompleted
		}
		records = append(records, dto.TaskRecord{
			ID: fmt.Sprintf("todo:%d", index), Key: "todo:" + item.Text, Phase: dto.TaskPhaseTasklist,
			Task: item.Text, Status: status, Kind: "todo",
		})
	}
	for _, record := range runtime.tasks {
		records = append(records, record)
	}
	return records
}

func (runtime *fakeRuntime) TaskAdd(spec dto.TaskSpec) (dto.TaskRecord, bool, error) {
	runtime.todoMu.Lock()
	defer runtime.todoMu.Unlock()
	return runtime.addTaskLocked(spec)
}

func (runtime *fakeRuntime) addTaskLocked(spec dto.TaskSpec) (dto.TaskRecord, bool, error) {
	if runtime.tasks == nil {
		runtime.tasks = make(map[string]dto.TaskRecord)
	}
	if spec.Key != "" {
		for _, record := range runtime.tasks {
			if record.Key == spec.Key {
				return record, false, nil
			}
		}
	}
	id := spec.ID
	if id == "" {
		id = fmt.Sprintf("task:%d", len(runtime.tasks)+1)
	}
	record := dto.TaskRecord{
		ID: id, Key: spec.Key, Phase: spec.Phase, Task: spec.Task, Description: spec.Description,
		Status: dto.TaskPending, Assignee: spec.Assignee, Kind: spec.Kind, SourceID: spec.SourceID,
		Dependencies: append([]string(nil), spec.Dependencies...),
		Attachments:  append([]string(nil), spec.Attachments...),
	}
	runtime.tasks[id] = record
	return record, true, nil
}

func (runtime *fakeRuntime) TaskAddFor(sessionID string, spec dto.TaskSpec) (dto.TaskRecord, bool, error) {
	runtime.todoMu.Lock()
	defer runtime.todoMu.Unlock()
	if sessionID == "" || sessionID == runtime.currentTaskSession {
		return runtime.addTaskLocked(spec)
	}
	if runtime.sessionTaskSnapshots == nil {
		runtime.sessionTaskSnapshots = make(map[string][]dto.TaskRecord)
	}
	records := runtime.sessionTaskSnapshots[sessionID]
	if spec.Key != "" {
		for _, record := range records {
			if record.Key == spec.Key {
				return record, false, nil
			}
		}
	}
	id := spec.ID
	if id == "" {
		id = fmt.Sprintf("task:%d", len(records)+1)
	}
	record := dto.TaskRecord{
		ID: id, Key: spec.Key, Phase: spec.Phase, Task: spec.Task, Description: spec.Description,
		Status: dto.TaskPending, Assignee: spec.Assignee, Kind: spec.Kind, SourceID: spec.SourceID,
		Dependencies: append([]string(nil), spec.Dependencies...),
		Attachments:  append([]string(nil), spec.Attachments...),
	}
	runtime.sessionTaskSnapshots[sessionID] = append(records, record)
	return record, true, nil
}

func (runtime *fakeRuntime) ResolveTaskByKey(key string) (dto.TaskRecord, bool, error) {
	runtime.todoMu.Lock()
	defer runtime.todoMu.Unlock()
	for _, record := range runtime.tasks {
		if record.Key == key {
			return record, true, nil
		}
	}
	return dto.TaskRecord{}, false, nil
}

func (runtime *fakeRuntime) ResolveTaskByKeyFor(sessionID, key string) (dto.TaskRecord, bool, error) {
	runtime.todoMu.Lock()
	defer runtime.todoMu.Unlock()
	if sessionID == "" || sessionID == runtime.currentTaskSession {
		for _, record := range runtime.tasks {
			if record.Key == key {
				return record, true, nil
			}
		}
		return dto.TaskRecord{}, false, nil
	}
	for _, record := range runtime.sessionTaskSnapshots[sessionID] {
		if record.Key == key {
			return record, true, nil
		}
	}
	return dto.TaskRecord{}, false, nil
}

func (runtime *fakeRuntime) TaskSetStatus(id string, status dto.TaskStatus, evidence string) (dto.TaskRecord, error) {
	runtime.todoMu.Lock()
	defer runtime.todoMu.Unlock()
	record, ok := runtime.tasks[id]
	if !ok {
		return dto.TaskRecord{}, fmt.Errorf("fake task %s not found", id)
	}
	record.Status = status
	if status == dto.TaskRetry {
		record.RetryCount++
	}
	runtime.tasks[id] = record
	return record, nil
}

func (runtime *fakeRuntime) TaskSetStatusFor(sessionID, id string, status dto.TaskStatus, evidence string) (dto.TaskRecord, error) {
	runtime.todoMu.Lock()
	defer runtime.todoMu.Unlock()
	if sessionID == "" || sessionID == runtime.currentTaskSession {
		record, ok := runtime.tasks[id]
		if !ok {
			return dto.TaskRecord{}, fmt.Errorf("fake task %s not found", id)
		}
		record.Status = status
		if status == dto.TaskRetry {
			record.RetryCount++
		}
		runtime.tasks[id] = record
		return record, nil
	}
	records := runtime.sessionTaskSnapshots[sessionID]
	for index := range records {
		if records[index].ID != id {
			continue
		}
		record := records[index]
		record.Status = status
		if status == dto.TaskRetry {
			record.RetryCount++
		}
		records[index] = record
		runtime.sessionTaskSnapshots[sessionID] = records
		return record, nil
	}
	return dto.TaskRecord{}, fmt.Errorf("fake task %s not found in session %s scope", id, sessionID)
}

func (runtime *fakeRuntime) TaskAttachParticipant(id, participant string) (dto.TaskRecord, error) {
	runtime.todoMu.Lock()
	defer runtime.todoMu.Unlock()
	record, ok := runtime.tasks[id]
	if !ok {
		return dto.TaskRecord{}, fmt.Errorf("fake task %s not found", id)
	}
	// 认领语义（与 seelebridge/task 注册表一致）：最近接管者成为当前 Assignee。
	record.Assignee = participant
	record.Participants = append(record.Participants, participant)
	runtime.tasks[id] = record
	return record, nil
}

func (*fakeRuntime) TaskChangedChannel() <-chan dto.TaskRecord { return nil }

func (*fakeRuntime) SubagentTreeEvents() <-chan struct{} { return nil }

func (*fakeRuntime) PlanNodeEventChannel() <-chan dto.PlanNodeEvent {
	return nil
}

func (runtime *fakeRuntime) SwitchSessionTasks(sessionID string, records []dto.TaskRecord) {
	runtime.todoMu.Lock()
	defer runtime.todoMu.Unlock()
	if runtime.sessionTaskSnapshots == nil {
		runtime.sessionTaskSnapshots = make(map[string][]dto.TaskRecord)
	}
	current := runtime.snapshotLocked()
	if runtime.currentTaskSession != "" {
		runtime.sessionTaskSnapshots[runtime.currentTaskSession] = current
	}
	runtime.currentTaskSession = sessionID
	if runtime.tasks == nil {
		runtime.tasks = make(map[string]dto.TaskRecord)
	}
	for id := range runtime.tasks {
		delete(runtime.tasks, id)
	}
	for _, record := range records {
		runtime.tasks[record.ID] = record
	}
}

func (runtime *fakeRuntime) SetSessionWorkspace(sessionID, workspaceID string) {
	runtime.todoMu.Lock()
	defer runtime.todoMu.Unlock()
	if runtime.sessionWorkspaces == nil {
		runtime.sessionWorkspaces = make(map[string]string)
	}
	runtime.sessionWorkspaces[sessionID] = workspaceID
}

func (runtime *fakeRuntime) ScheduledCommands() []seelebridge.ScheduledCommandInfo {
	return []seelebridge.ScheduledCommandInfo{{Key: "auto_get_jobs", Label: "BOSS直聘自动投简历"}}
}

func (runtime *fakeRuntime) ScheduledTasksSnapshot() []seelebridge.ScheduledTaskStatus {
	return append([]seelebridge.ScheduledTaskStatus(nil), runtime.scheduledTasks...)
}

func (runtime *fakeRuntime) ScheduleTask(_ context.Context, spec seelebridge.ScheduledTaskSpec) (*seelebridge.ScheduledTaskStatus, error) {
	if runtime.scheduleErr != nil {
		return nil, runtime.scheduleErr
	}
	runtime.scheduledSpecs = append(runtime.scheduledSpecs, spec)
	created := seelebridge.ScheduledTaskStatus{
		ID: "sched_test", Name: spec.Name, Kind: string(spec.Kind),
		IntervalSec: int64(spec.Interval.Seconds()), Enabled: spec.Enabled,
	}
	runtime.scheduledTasks = append(runtime.scheduledTasks, created)
	return &created, nil
}

func (runtime *fakeRuntime) CancelScheduledTask(id string) error {
	runtime.cancelledTasks = append(runtime.cancelledTasks, id)
	for index, task := range runtime.scheduledTasks {
		if task.ID == id {
			runtime.scheduledTasks = append(runtime.scheduledTasks[:index], runtime.scheduledTasks[index+1:]...)
			break
		}
	}
	return nil
}

func (runtime *fakeRuntime) ClearSubagentTree() error            { return nil }
func (runtime *fakeRuntime) RestoreSubagentAnchors(string) error { return nil }

func (runtime *fakeRuntime) ListSubagentRecovery(string) ([]dto.SubagentRecoveryView, error) {
	return runtime.subagentRecovery, nil
}

func (runtime *fakeRuntime) ResumeInterruptedSubagents(context.Context, string) (dto.SubagentResumeReport, error) {
	return runtime.subagentResumeReport, runtime.subagentResumeErr
}

func (runtime *fakeRuntime) ResumeSubagent(context.Context, string, string) (dto.SubagentResumeResult, error) {
	return dto.SubagentResumeResult{}, runtime.subagentResumeErr
}

func (runtime *fakeRuntime) ForkSubagents(context.Context, string, []dto.SubagentForkSpec) (string, error) {
	return "", nil
}

func (runtime *fakeRuntime) SetSubagentParentRepairer(func(string) error) {}

func (runtime *fakeRuntime) SearchHistory(_ context.Context, _ string, _ int) (seelexctxsearch.Result, error) {
	return runtime.searchResult, runtime.searchErr
}

func (runtime *fakeRuntime) BindProjectRoot(rootPath string) error {
	runtime.projectRootMu.Lock()
	runtime.projectRoot = rootPath
	runtime.projectRootMu.Unlock()
	return nil
}

func (runtime *fakeRuntime) UnbindProjectRoot() {
	runtime.projectRootMu.Lock()
	runtime.projectRoot = ""
	runtime.projectRootMu.Unlock()
}

// ProjectRoot 读当前绑定的项目根（加锁）：写侧可能来自后台 chat goroutine，
// 用例读必须走同一把锁（直接读字段 = 数据竞争，-race 会报）。
func (runtime *fakeRuntime) ProjectRoot() string {
	runtime.projectRootMu.RLock()
	defer runtime.projectRootMu.RUnlock()
	return runtime.projectRoot
}

// SetCurrentTaskBatch 会被并行会话的多个 runChat 并发调用（M2：每个会话
// 各自 SetCurrentTaskBatch），fake 需加锁镜像生产 Runtime 的线程安全。
func (runtime *fakeRuntime) SetCurrentTaskBatch(sessionID, batchID string) {
	runtime.mailboxMu.Lock()
	runtime.currentBatch = batchID
	runtime.mailboxMu.Unlock()
	debugLog("fakeRuntime.SetCurrentTaskBatch batch=%q", batchID)
}

// goalVisibilityRuntime models Runtime's one-way visibility projection. Its
// VisibleTools implementation reads only Runtime-owned state; it cannot call
// back into Service while a tool hook is holding a framework session lock.

type goalVisibilityRuntime struct {
	*fakeRuntime
}

func (runtime *goalVisibilityRuntime) VisibleTools(context.Context) []Tool {
	if runtime.visibility.GoalSkillActive {
		return []Tool{{Name: "plan_load", Description: "load plan"}}
	}
	return []Tool{{Name: "read", Description: "read files"}}
}

type fakePlugins struct{ current PluginInfo }

func (*fakePlugins) All() []PluginInfo {
	return []PluginInfo{{Name: "default", Description: "default"}, {Name: "code", Description: "coding", Prompt: "code prompt"}}
}

func (plugins *fakePlugins) Activate(_ context.Context, name string) error {
	if name != "code" && name != "default" {
		return errors.New("missing plugin")
	}
	plugins.current = PluginInfo{Name: name, Prompt: name + " prompt"}
	return nil
}

func (plugins *fakePlugins) Deactivate(context.Context) error {
	plugins.current = PluginInfo{}
	return nil
}

func (plugins *fakePlugins) Current() (PluginInfo, bool) {
	return plugins.current, plugins.current.Name != ""
}

type fakeSkills struct{}

func (fakeSkills) All() []SkillInfo {
	return []SkillInfo{{Name: "review", Description: "review code", Prompt: "review prompt"}}
}

func (fakeSkills) Get(name string) (SkillInfo, bool) {
	if name != "review" {
		return SkillInfo{}, false
	}
	return SkillInfo{Name: "review", Prompt: "review prompt"}, true
}

type fakeSessions struct{}

func (fakeSessions) SaveCurrent(string) error { return nil }

func (fakeSessions) Resume(string) error { return errors.New("resume unsupported") }

func (fakeSessions) List() []SessionInfo {
	return []SessionInfo{{ID: "saved", UpdatedAt: time.Unix(1, 0), TokenCount: 4}}
}

func (fakeSessions) LoadHistory(string) ([]EngineMessage, error) {
	return []EngineMessage{{Role: "assistant", Content: "saved answer"}}, nil
}

func (fakeSessions) LoadHistoryRange(string, int, int) ([]EngineMessage, int, error) {
	return []EngineMessage{{Role: "assistant", Content: "saved answer"}}, 1, nil
}

func (fakeSessions) Delete(string) error { return nil }

func (fakeSessions) MessageCount(string) (int, error) { return 1, nil }

func (fakeSessions) SetWorkspace(string) {}

func (fakeSessions) Workspace() string { return "" }

type blockingCatalogSessions struct {
	fakeSessions
	entered     chan struct{}
	release     chan struct{}
	enteredOnce sync.Once
	calls       atomic.Int64
}

func (sessions *blockingCatalogSessions) List() []SessionInfo {
	sessions.calls.Add(1)
	sessions.enteredOnce.Do(func() { close(sessions.entered) })
	<-sessions.release
	return sessions.fakeSessions.List()
}

type persistenceFailingSessions struct{ fakeSessions }

func (persistenceFailingSessions) SaveCurrent(string) error {
	return errors.New("session store unavailable")
}

type trackingSessions struct {
	fakeSessions
	mu       sync.Mutex
	savedIDs []string
}

func (sessions *trackingSessions) SaveCurrent(sessionID string) error {
	sessions.mu.Lock()
	sessions.savedIDs = append(sessions.savedIDs, sessionID)
	sessions.mu.Unlock()
	return nil
}

type scopedSessions struct {
	fakeSessions
	mu              sync.RWMutex
	workspace       string
	catalog         map[string][]SessionInfo
	histories       map[string]map[string][]EngineMessage
	loadedWorkspace string
	savedIDs        []string
}

func (sessions *scopedSessions) SetWorkspace(workspaceID string) {
	sessions.mu.Lock()
	sessions.workspace = workspaceID
	sessions.mu.Unlock()
}

func (sessions *scopedSessions) Workspace() string {
	sessions.mu.RLock()
	defer sessions.mu.RUnlock()
	return sessions.workspace
}

func (sessions *scopedSessions) SaveCurrent(sessionID string) error {
	sessions.mu.Lock()
	sessions.savedIDs = append(sessions.savedIDs, sessionID)
	sessions.mu.Unlock()
	return nil
}

func (sessions *scopedSessions) SavedIDs() []string {
	sessions.mu.RLock()
	defer sessions.mu.RUnlock()
	return append([]string(nil), sessions.savedIDs...)
}

// SessionsOf 实现 session_runtime.SessionGranularPort：按项目索引枚举会话。
func (sessions *scopedSessions) SessionsOf(projectID string) []SessionInfo {
	sessions.mu.RLock()
	defer sessions.mu.RUnlock()
	return append([]SessionInfo(nil), sessions.catalog[projectID]...)
}

func (sessions *scopedSessions) LoadedWorkspace() string {
	sessions.mu.RLock()
	defer sessions.mu.RUnlock()
	return sessions.loadedWorkspace
}

// resolveWorkspaceFor 返回会话历史所在 workspace（扫描 histories；未找到
// 返回 ""）。
func (sessions *scopedSessions) resolveWorkspaceFor(sessionID string) (string, []EngineMessage, bool) {
	sessions.mu.Lock()
	defer sessions.mu.Unlock()
	for workspaceID, bySession := range sessions.histories {
		if bySession == nil {
			continue
		}
		if history, ok := bySession[sessionID]; ok {
			return workspaceID, append([]EngineMessage(nil), history...), true
		}
	}
	return "", nil, false
}

// LoadHistory 实现 SessionGranularPort：读取会话历史（会话粒度键；workspace
// 由存储层解析——桩按扫描 histories 解析并记录加载面供断言）。
func (sessions *scopedSessions) LoadHistory(sessionID string) ([]EngineMessage, error) {
	workspaceID, history, ok := sessions.resolveWorkspaceFor(sessionID)
	if !ok {
		sessions.mu.Lock()
		sessions.loadedWorkspace = ""
		sessions.mu.Unlock()
		return sessions.fakeSessions.LoadHistory(sessionID)
	}
	sessions.mu.Lock()
	sessions.loadedWorkspace = workspaceID
	sessions.mu.Unlock()
	return history, nil
}

// LoadHistoryRange 实现 SessionGranularPort：按窗口读取会话历史。
func (sessions *scopedSessions) LoadHistoryRange(sessionID string, offset, limit int) ([]EngineMessage, int, error) {
	history, err := sessions.LoadHistory(sessionID)
	if err != nil {
		return nil, 0, err
	}
	if offset > len(history) {
		offset = len(history)
	}
	end := offset + limit
	if limit <= 0 || end > len(history) {
		end = len(history)
	}
	return append([]EngineMessage(nil), history[offset:end]...), len(history), nil
}

// Delete 实现 SessionGranularPort：从全部项目索引删除会话。
func (sessions *scopedSessions) Delete(sessionID string) error {
	sessions.mu.Lock()
	defer sessions.mu.Unlock()
	for workspaceID, bySession := range sessions.histories {
		delete(bySession, sessionID)
		_ = workspaceID
	}
	for workspaceID, items := range sessions.catalog {
		filtered := items[:0]
		for _, item := range items {
			if item.ID != sessionID {
				filtered = append(filtered, item)
			}
		}
		sessions.catalog[workspaceID] = filtered
	}
	return nil
}

type fakeWorkspace struct {
	mu       sync.Mutex
	items    map[string]WorkspaceInfo
	bindings map[string]string
}

func newFakeWorkspace() *fakeWorkspace {
	return &fakeWorkspace{items: make(map[string]WorkspaceInfo), bindings: make(map[string]string)}
}

func (repo *fakeWorkspace) Create(name, rootPath, gitRemote string) (WorkspaceInfo, error) {
	repo.mu.Lock()
	defer repo.mu.Unlock()
	item := WorkspaceInfo{ID: "project-1", Name: name, RootPath: rootPath, GitRemote: gitRemote}
	repo.items[item.ID] = item
	return item, nil
}

func (repo *fakeWorkspace) Get(id string) (WorkspaceInfo, error) {
	repo.mu.Lock()
	defer repo.mu.Unlock()
	item, ok := repo.items[id]
	if !ok {
		return WorkspaceInfo{}, errors.New("workspace missing")
	}
	return item, nil
}

func (repo *fakeWorkspace) List() []WorkspaceInfo {
	repo.mu.Lock()
	defer repo.mu.Unlock()
	items := make([]WorkspaceInfo, 0, len(repo.items))
	for _, item := range repo.items {
		items = append(items, item)
	}
	return items
}

func (repo *fakeWorkspace) Delete(id string) error {
	repo.mu.Lock()
	defer repo.mu.Unlock()
	delete(repo.items, id)
	return nil
}

func (repo *fakeWorkspace) BindSession(sessionID, workspaceID string) {
	repo.mu.Lock()
	defer repo.mu.Unlock()
	repo.bindings[sessionID] = workspaceID
}

func (repo *fakeWorkspace) UnbindSession(sessionID string) {
	repo.mu.Lock()
	defer repo.mu.Unlock()
	delete(repo.bindings, sessionID)
}

func (repo *fakeWorkspace) SessionWorkspace(sessionID string) (WorkspaceInfo, bool) {
	repo.mu.Lock()
	defer repo.mu.Unlock()
	workspaceID, ok := repo.bindings[sessionID]
	if !ok {
		return WorkspaceInfo{}, false
	}
	item, ok := repo.items[workspaceID]
	return item, ok
}

func (repo *fakeWorkspace) AllBindings() map[string]string {
	repo.mu.Lock()
	defer repo.mu.Unlock()
	bindings := make(map[string]string, len(repo.bindings))
	for sessionID, workspaceID := range repo.bindings {
		bindings[sessionID] = workspaceID
	}
	return bindings
}

func (*fakeWorkspace) DetectGitRemote(string) string { return "" }
