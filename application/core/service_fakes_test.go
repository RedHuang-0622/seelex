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
	return engine.sessionID
}

func (engine *fakeEngine) ReplaceHistory(sessionID string, history []EngineMessage) error {
	engine.mu.Lock()
	defer engine.mu.Unlock()
	engine.sessionID = sessionID
	engine.history = append([]EngineMessage(nil), history...)
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
	return engine.sessionID == sessionID || engine.sessionID != ""
}

type fakeRuntime struct {
	account       string
	fullAccess    bool
	binding       dto.PlanBranchBinding
	planPolicy    dto.PlanPolicy
	visibility    seelebridge.RuntimeVisibilityProjection
	evidence      seelebridge.ParentEvidenceProjection
	mailbox       []string
	mailboxMu     sync.Mutex
	replans       []dto.ReplanRequest
	replanResult  dto.PlanPreflight
	replanErr     error
	replanMetrics dto.ReplanMetrics
	projectRoot   string
	currentBatch  string
	todoMu        sync.Mutex
	todoItems     []dto.TodoItem
	tasks         map[string]dto.TaskRecord
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

func (*fakeRuntime) VisibleTools(context.Context) []Tool {
	return []Tool{{Name: "read", Description: "read files"}}
}

func (*fakeRuntime) ActivePlugin() string { return "default" }

func (runtime *fakeRuntime) FullAccess() bool { return runtime.fullAccess }

func (runtime *fakeRuntime) SetFullAccess(on bool) { runtime.fullAccess = on }

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

func (runtime *fakeRuntime) PrepareReplan(_ context.Context, request dto.ReplanRequest) (dto.PlanPreflight, error) {
	runtime.replans = append(runtime.replans, request)
	return runtime.replanResult, runtime.replanErr
}

func (runtime *fakeRuntime) ReplanMetrics() dto.ReplanMetrics { return runtime.replanMetrics }

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

func (runtime *fakeRuntime) TaskSnapshot() []dto.TaskRecord {
	runtime.todoMu.Lock()
	defer runtime.todoMu.Unlock()
	return runtime.snapshotLocked()
}

func (runtime *fakeRuntime) TaskSnapshotFor(sessionID string) []dto.TaskRecord {
	runtime.todoMu.Lock()
	defer runtime.todoMu.Unlock()
	if sessionID == "" || sessionID == runtime.currentTaskSession {
		return runtime.snapshotLocked()
	}
	return append([]dto.TaskRecord(nil), runtime.sessionTaskSnapshots[sessionID]...)
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
		Status: dto.TaskPending, Assignee: spec.Assignee, Kind: spec.Kind,
		Dependencies: append([]string(nil), spec.Dependencies...),
		Attachments:  append([]string(nil), spec.Attachments...),
	}
	runtime.tasks[id] = record
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

func (runtime *fakeRuntime) SearchHistory(_ context.Context, _ string, _ int) (seelexctxsearch.Result, error) {
	return runtime.searchResult, runtime.searchErr
}

func (runtime *fakeRuntime) BindProjectRoot(rootPath string) error {
	runtime.projectRoot = rootPath
	return nil
}

func (runtime *fakeRuntime) UnbindProjectRoot() { runtime.projectRoot = "" }

// SetCurrentTaskBatch 会被并行会话的多个 runChat 并发调用（M2：每个会话
// 各自 SetCurrentTaskBatch），fake 需加锁镜像生产 Runtime 的线程安全。
func (runtime *fakeRuntime) SetCurrentTaskBatch(batchID string) {
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

func (sessions *scopedSessions) ListWorkspace(workspaceID string) []SessionInfo {
	sessions.mu.RLock()
	defer sessions.mu.RUnlock()
	return append([]SessionInfo(nil), sessions.catalog[workspaceID]...)
}

func (sessions *scopedSessions) LoadedWorkspace() string {
	sessions.mu.RLock()
	defer sessions.mu.RUnlock()
	return sessions.loadedWorkspace
}

func (sessions *scopedSessions) LoadHistoryWorkspace(workspaceID, sessionID string) ([]EngineMessage, error) {
	sessions.mu.Lock()
	sessions.loadedWorkspace = workspaceID
	var (
		history  []EngineMessage
		found    bool
		hasScope bool
	)
	if bySession := sessions.histories[workspaceID]; bySession != nil {
		hasScope = true
		history, found = bySession[sessionID]
		history = append([]EngineMessage(nil), history...)
	}
	sessions.mu.Unlock()
	if hasScope {
		if found {
			return history, nil
		}
		return nil, errors.New("session missing from workspace")
	}
	return sessions.fakeSessions.LoadHistory(sessionID)
}

func (sessions *scopedSessions) LoadHistoryRangeWorkspace(workspaceID, sessionID string, offset, limit int) ([]EngineMessage, int, error) {
	history, err := sessions.LoadHistoryWorkspace(workspaceID, sessionID)
	if err != nil {
		return nil, 0, err
	}
	end := min(offset+limit, len(history))
	return append([]EngineMessage(nil), history[offset:end]...), len(history), nil
}

func (sessions *scopedSessions) DeleteWorkspace(workspaceID, sessionID string) error {
	sessions.mu.Lock()
	defer sessions.mu.Unlock()
	delete(sessions.histories[workspaceID], sessionID)
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
