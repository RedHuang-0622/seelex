package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RedHuang-0622/Seele/session"
	"github.com/RedHuang-0622/Seele/types"
	"github.com/RedHuang-0622/seelex/seelebridge"
	"github.com/RedHuang-0622/seelex/seelexctx/snapshot"
)

type fakeEngine struct {
	mu                sync.Mutex
	history           []EngineMessage
	historyBeforeChat []EngineMessage
	chunks            []string
	prompt            string
	chatErr           error
	chatErrors        []error
	chatInputs        []string
	appendChatHistory bool
	cleared           bool
	lazyStart         bool
	sessionID         string
	starts            int
	lastInput         string
	maxLoops          int
	releaseCalls      int
	nodeContext       *snapshot.ContextSnapshot
	subAgentTree      []seelebridge.SubAgentTreeNode
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

func TestSnapshotNeverSerializesSystemPrompt(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	const privateInstruction = "private-system-instruction-must-not-reach-frontend"
	service.promptStack.Push("base", "private", privateInstruction)
	service.deps.Engine.SetSystemPrompt(service.promptStack.Render())
	projection := service.collectRuntimeProjection(context.Background())
	service.mu.Lock()
	service.applyRuntimeProjectionLocked(projection)
	service.mu.Unlock()

	payload, err := json.Marshal(service.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(payload), "prompt_stack") || strings.Contains(string(payload), privateInstruction) {
		t.Fatalf("snapshot leaked system prompt data: %s", payload)
	}
}

func TestReActBudgetStopsOnlyAfterItsToolBudget(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	defer service.Shutdown()
	service.mu.Lock()
	service.startReActBudgetLocked("budget-request", ReActBudget{MaxToolRounds: 3, MaxToolCalls: 2})
	service.mu.Unlock()

	bridge := NewToolHookBridge()
	bridge.Bind(service)
	hooks := bridge.Hooks()
	hooks.OnToolStart(context.Background(), session.ToolCallInfo{Turn: 0, Name: "write_file", Arguments: `{"path":"report.md","content":"done"}`})
	if !hooks.OnIterationComplete(context.Background(), 0) {
		t.Fatal("first delivery tool should remain within budget")
	}
	hooks.OnToolStart(context.Background(), session.ToolCallInfo{Turn: 1, Name: "read_file", Arguments: `{"path":"report.md"}`})
	if hooks.OnIterationComplete(context.Background(), 1) {
		t.Fatal("tool-call budget should stop the next ReAct iteration")
	}
	if err := service.reactBudgetError("budget-request"); !errors.Is(err, ErrReActBudgetExceeded) {
		t.Fatalf("budget error = %v, want ErrReActBudgetExceeded", err)
	}
}

func TestReActBudgetUsesReservedFinalDeliveryTurn(t *testing.T) {
	rawResult := strings.Repeat("oversized-result", defaultToolResultLimit())
	engine := &fakeEngine{history: []EngineMessage{
		{Role: "assistant", ToolCalls: []EngineToolCall{{ID: "call-large", Name: "bash"}}},
		{Role: "tool", ToolCallID: "call-large", Name: "bash", Content: rawResult, ContentSet: true},
	}}
	service := newTestService(t, engine)
	defer service.Shutdown()
	service.mu.Lock()
	service.startReActBudgetLocked("budget-request", ReActBudget{MaxToolRounds: 1})
	service.reactBudget.reason = "tool-round limit reached (1)"
	service.snapshot.Chat = ChatState{Running: true, RequestID: "budget-request"}
	service.taskExecution = newTaskExecutionState("budget-request", "deliver result", "high")
	stored := service.components.tasks.storeToolResultLocked("bash", rawResult)
	service.resultRefsByToolCallID["call-large"] = stored.Ref
	service.mu.Unlock()

	if err := service.finalizeReActBudget(context.Background(), "budget-request"); err != nil {
		t.Fatal(err)
	}
	for _, message := range engine.History() {
		if message.Content == reactBudgetFinalizationInput {
			t.Fatal("internal budget-finalization input leaked into history")
		}
	}
	engine.mu.Lock()
	prepared := append([]EngineMessage(nil), engine.historyBeforeChat...)
	engine.mu.Unlock()
	for _, message := range prepared {
		if strings.Contains(message.Content, "oversized-result") {
			t.Fatal("reserved final delivery turn received raw oversized tool output")
		}
	}
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
func (*fakeEngine) TraceText() string                                      { return "trace" }
func (*fakeEngine) TokenCount() string                                     { return "12" }
func (*fakeEngine) NodeSessionConversation(string) ([]types.Message, bool) { return nil, false }
func (engine *fakeEngine) NodeContextSnapshot(string) (*snapshot.ContextSnapshot, bool) {
	if engine.nodeContext == nil {
		return nil, false
	}
	return engine.nodeContext, true
}
func (engine *fakeEngine) SubAgentTree() []seelebridge.SubAgentTreeNode {
	engine.mu.Lock()
	defer engine.mu.Unlock()
	return append([]seelebridge.SubAgentTreeNode(nil), engine.subAgentTree...)
}
func (engine *fakeEngine) ReleaseWorkingHistory() {
	engine.mu.Lock()
	engine.releaseCalls++
	engine.mu.Unlock()
}

type fakeRuntime struct {
	account        string
	fullAccess     bool
	binding        seelebridge.PlanBranchBinding
	planPolicy     seelebridge.PlanPolicy
	visibility     seelebridge.RuntimeVisibilityProjection
	evidence       seelebridge.ParentEvidenceProjection
	mailbox        []string
	replans        []seelebridge.ReplanRequest
	replanResult   seelebridge.PlanPreflight
	replanErr      error
	replanMetrics  seelebridge.ReplanMetrics
	projectRoot    string
	todoItems      []seelebridge.TodoItem
	scheduledTasks []seelebridge.ScheduledTaskStatus
	scheduledSpecs []seelebridge.ScheduledTaskSpec
	cancelledTasks []string
	scheduleErr    error
}

func (*fakeRuntime) Model() string    { return "test-model" }
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
func (*fakeRuntime) ActivePlugin() string          { return "default" }
func (runtime *fakeRuntime) FullAccess() bool      { return runtime.fullAccess }
func (runtime *fakeRuntime) SetFullAccess(on bool) { runtime.fullAccess = on }
func (runtime *fakeRuntime) SetRuntimeVisibilityProjection(projection seelebridge.RuntimeVisibilityProjection) {
	runtime.visibility = projection
}
func (runtime *fakeRuntime) SetParentEvidenceProjection(projection seelebridge.ParentEvidenceProjection) {
	runtime.evidence = projection
}
func (runtime *fakeRuntime) DrainSubagentContexts() []string {
	items := append([]string(nil), runtime.mailbox...)
	runtime.mailbox = nil
	return items
}
func (runtime *fakeRuntime) SetPlanPolicy(policy seelebridge.PlanPolicy) {
	runtime.planPolicy = policy
}
func (runtime *fakeRuntime) PrepareReplan(_ context.Context, request seelebridge.ReplanRequest) (seelebridge.PlanPreflight, error) {
	runtime.replans = append(runtime.replans, request)
	return runtime.replanResult, runtime.replanErr
}
func (runtime *fakeRuntime) ReplanMetrics() seelebridge.ReplanMetrics { return runtime.replanMetrics }
func (runtime *fakeRuntime) SetPlanBranchBinding(binding seelebridge.PlanBranchBinding) {
	runtime.binding = binding
}
func (runtime *fakeRuntime) TodoSnapshot() []seelebridge.TodoItem {
	return append([]seelebridge.TodoItem(nil), runtime.todoItems...)
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
func (runtime *fakeRuntime) BindProjectRoot(rootPath string) error {
	runtime.projectRoot = rootPath
	return nil
}
func (runtime *fakeRuntime) UnbindProjectRoot() { runtime.projectRoot = "" }

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
func (fakeSessions) Resume(string) error      { return errors.New("resume unsupported") }
func (fakeSessions) List() []SessionInfo {
	return []SessionInfo{{ID: "saved", UpdatedAt: time.Unix(1, 0), TokenCount: 4}}
}
func (fakeSessions) LoadHistory(string) ([]EngineMessage, error) {
	return []EngineMessage{{Role: "assistant", Content: "saved answer"}}, nil
}
func (fakeSessions) LoadHistoryRange(string, int, int) ([]EngineMessage, int, error) {
	return []EngineMessage{{Role: "assistant", Content: "saved answer"}}, 1, nil
}
func (fakeSessions) Delete(string) error              { return nil }
func (fakeSessions) MessageCount(string) (int, error) { return 1, nil }
func (fakeSessions) SetWorkspace(string)              {}
func (fakeSessions) Workspace() string                { return "" }

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

type testServiceOption func(*Dependencies)

func withTestSessions(sessions SessionPort) testServiceOption {
	return func(deps *Dependencies) { deps.Sessions = sessions }
}

func withTestRuntime(runtime RuntimePort) testServiceOption {
	return func(deps *Dependencies) { deps.Runtime = runtime }
}

// newTestService injects every dependency before New starts the asynchronous
// session catalog worker and always drains it when the test finishes.
func newTestService(t testing.TB, engine ChatEngine, options ...testServiceOption) *Service {
	t.Helper()
	deps := Dependencies{
		Engine:   engine,
		Runtime:  &fakeRuntime{},
		Plugins:  &fakePlugins{current: PluginInfo{Name: "default"}},
		Skills:   fakeSkills{},
		Sessions: fakeSessions{},
	}
	for _, apply := range options {
		apply(&deps)
	}
	return mustNew(t, deps)
}

// mustNew gives bespoke dependency fixtures the same worker cleanup guarantee
// as newTestService.
func mustNew(t testing.TB, deps Dependencies) *Service {
	t.Helper()
	service, err := New(deps)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(service.Shutdown)
	return service
}

func TestNewTestServiceCleansUpCatalogWorker(t *testing.T) {
	var service *Service
	t.Run("fixture", func(t *testing.T) {
		service = newTestService(t, &fakeEngine{})
	})
	select {
	case <-service.sessionCatalogDone:
	case <-time.After(time.Second):
		t.Fatal("test fixture did not stop the session catalog worker")
	}
}

func waitForSnapshot(t *testing.T, service *Service, ready func(Snapshot) bool) Snapshot {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for {
		snapshot := service.Snapshot()
		if ready(snapshot) {
			return snapshot
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for asynchronous snapshot projection: %+v", snapshot)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestSessionCatalogAllowsDuplicateNamesWithDistinctIDs(t *testing.T) {
	updatedAt := time.Unix(2, 0)
	sessions := &scopedSessions{
		catalog: map[string][]SessionInfo{
			"project-1": {
				{ID: "session-a", UpdatedAt: updatedAt},
				{ID: "session-b", UpdatedAt: updatedAt},
			},
		},
		histories: map[string]map[string][]EngineMessage{
			"project-1": {
				"session-a": {{Role: "user", Content: "same question"}},
				"session-b": {{Role: "user", Content: "same question"}},
			},
		},
	}
	workspaces := newFakeWorkspace()
	workspaces.items["project-1"] = WorkspaceInfo{ID: "project-1", Name: "project", RootPath: t.TempDir()}
	service := mustNew(t, Dependencies{
		Engine: &fakeEngine{}, Runtime: &fakeRuntime{}, Plugins: &fakePlugins{current: PluginInfo{Name: "default"}},
		Skills: fakeSkills{}, Sessions: sessions, Workspace: workspaces,
	})
	defer service.Shutdown()

	catalog := waitForSnapshot(t, service, func(snapshot Snapshot) bool { return len(snapshot.Sessions) == 2 }).Sessions
	if len(catalog) != 2 {
		t.Fatalf("session count = %d, want 2", len(catalog))
	}
	if catalog[0].Name != "same question" || catalog[1].Name != "same question" {
		t.Fatalf("duplicate display names were not preserved: %#v", catalog)
	}
	if catalog[0].ID == catalog[1].ID {
		t.Fatalf("session IDs must remain distinct: %#v", catalog)
	}
}

func TestSessionTitleUsesFirstUserQuestion(t *testing.T) {
	history := []EngineMessage{
		{Role: "system", Content: "system"},
		{Role: "assistant", Content: "assistant"},
		{Role: "user", Content: wrapModelInput("\n  first   question  \nsecond line", "model context")},
	}
	if got := sessionTitleFromHistory(history); got != "first question" {
		t.Fatalf("session title = %q, want %q", got, "first question")
	}
	long := strings.Repeat("界", 60)
	got := sessionTitle(long)
	if len([]rune(got)) != 48 || !strings.HasSuffix(got, "…") {
		t.Fatalf("long title was not rune-truncated: %q (%d runes)", got, len([]rune(got)))
	}
}

func TestCurrentSessionNameUsesFirstQuestion(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	if err := service.Submit(context.Background(), "  first live question  "); err != nil {
		t.Fatal(err)
	}
	if got := service.Snapshot().Session.Name; got != "first live question" {
		t.Fatalf("current session name = %q", got)
	}
	if err := service.WaitForIdle(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestWorkingHistoryReleasesOnlyAfterSuccessfulPersistence(t *testing.T) {
	tests := []struct {
		name         string
		sessions     SessionPort
		wantReleases int
	}{
		{name: "success", sessions: fakeSessions{}, wantReleases: 1},
		{name: "failure", sessions: persistenceFailingSessions{}, wantReleases: 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			engine := &fakeEngine{}
			service := mustNew(t, Dependencies{
				Engine: engine, Runtime: &fakeRuntime{}, Plugins: &fakePlugins{current: PluginInfo{Name: "default"}},
				Skills: fakeSkills{}, Sessions: test.sessions,
			})
			defer service.Shutdown()
			if err := service.Submit(context.Background(), "persist before release"); err != nil {
				t.Fatal(err)
			}
			if err := service.WaitForIdle(context.Background()); err != nil {
				t.Fatal(err)
			}
			engine.mu.Lock()
			releases := engine.releaseCalls
			engine.mu.Unlock()
			if releases != test.wantReleases {
				t.Fatalf("working history releases = %d, want %d", releases, test.wantReleases)
			}
		})
	}
}

func TestBeginNewSessionIsLazyAndFirstQuestionMaterializesIt(t *testing.T) {
	engine := &fakeEngine{history: []EngineMessage{{Role: "user", Content: "old question"}}}
	sessions := &trackingSessions{}
	service := mustNew(t, Dependencies{
		Engine: engine, Runtime: &fakeRuntime{}, Plugins: &fakePlugins{current: PluginInfo{Name: "default"}},
		Skills: fakeSkills{}, Sessions: sessions,
	})
	defer service.Shutdown()

	if err := service.BeginNewSession(); err != nil {
		t.Fatal(err)
	}
	if err := service.BeginNewSession(); err != nil {
		t.Fatal(err)
	}
	engine.mu.Lock()
	startsBeforeSubmit := engine.starts
	clearedForDraft := engine.cleared
	engine.mu.Unlock()
	if startsBeforeSubmit != 0 || !clearedForDraft {
		t.Fatalf("draft engine state: starts=%d cleared=%v", startsBeforeSubmit, clearedForDraft)
	}
	snapshot := service.Snapshot()
	if !snapshot.Session.Draft || snapshot.Session.ID != "" || snapshot.Session.Name != "新会话" {
		t.Fatalf("draft session = %+v", snapshot.Session)
	}
	if len(snapshot.Conversation) != 0 || snapshot.Runtime.Plan != nil {
		t.Fatalf("draft must clear conversation and plan: %+v", snapshot)
	}
	sessions.mu.Lock()
	if len(sessions.savedIDs) != 1 || sessions.savedIDs[0] != "session-1" {
		t.Fatalf("saved sessions after repeated draft clicks = %v", sessions.savedIDs)
	}
	sessions.mu.Unlock()

	if err := service.Submit(context.Background(), "first lazy question"); err != nil {
		t.Fatal(err)
	}
	snapshot = service.Snapshot()
	if snapshot.Session.Draft || snapshot.Session.ID != "session-new" || snapshot.Session.Name != "first lazy question" {
		t.Fatalf("materialized session = %+v", snapshot.Session)
	}
	engine.mu.Lock()
	startsAfterSubmit := engine.starts
	engine.mu.Unlock()
	if startsAfterSubmit != 1 {
		t.Fatalf("StartSession calls after first request = %d, want 1", startsAfterSubmit)
	}
	if err := service.WaitForIdle(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestLazySessionInheritsProjectOnlyWhenMaterialized(t *testing.T) {
	engine := &fakeEngine{}
	runtime := &fakeRuntime{}
	sessions := &scopedSessions{}
	workspaces := newFakeWorkspace()
	service := mustNew(t, Dependencies{
		Engine: engine, Runtime: runtime, Plugins: &fakePlugins{current: PluginInfo{Name: "default"}},
		Skills: fakeSkills{}, Sessions: sessions, Workspace: workspaces,
	})
	defer service.Shutdown()

	if err := service.BeginNewSession(); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := service.CreateWorkspace("project", root, ""); err != nil {
		t.Fatal(err)
	}
	if _, exists := workspaces.bindings[""]; exists {
		t.Fatalf("draft session created an empty-ID binding: %v", workspaces.bindings)
	}
	if _, exists := workspaces.bindings["session-new"]; exists {
		t.Fatalf("draft session was bound before first request: %v", workspaces.bindings)
	}
	if err := service.Submit(context.Background(), "project question"); err != nil {
		t.Fatal(err)
	}
	if got := workspaces.bindings["session-new"]; got != "project-1" {
		t.Fatalf("materialized session workspace = %q, want project-1; bindings=%v", got, workspaces.bindings)
	}
	if sessions.Workspace() != "project-1" || runtime.projectRoot != root {
		t.Fatalf("materialized project scope: workspace=%q root=%q", sessions.Workspace(), runtime.projectRoot)
	}
	if err := service.WaitForIdle(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestInitialLazySessionIsDraftAndFirstSubmitMaterializes(t *testing.T) {
	engine := &fakeEngine{lazyStart: true}
	sessions := &trackingSessions{}
	service := mustNew(t, Dependencies{
		Engine: engine, Runtime: &fakeRuntime{}, Plugins: &fakePlugins{current: PluginInfo{Name: "default"}},
		Skills: fakeSkills{}, Sessions: sessions,
	})
	defer service.Shutdown()

	// Startup with a lazy engine must present an unmaterialized draft so the
	// first submission creates a real session instead of persisting an empty ID.
	initial := service.Snapshot()
	if !initial.Session.Draft || initial.Session.ID != "" {
		t.Fatalf("initial lazy session = %+v, want Draft=true with empty ID", initial.Session)
	}

	if err := service.Submit(context.Background(), "first question"); err != nil {
		t.Fatal(err)
	}
	snapshot := service.Snapshot()
	if snapshot.Session.Draft || snapshot.Session.ID != "session-new" {
		t.Fatalf("materialized session = %+v", snapshot.Session)
	}
	engine.mu.Lock()
	starts := engine.starts
	engine.mu.Unlock()
	if starts != 1 {
		t.Fatalf("StartSession calls after first submit = %d, want 1", starts)
	}
}

func TestResumeSessionLeavesLazyDraft(t *testing.T) {
	engine := &fakeEngine{}
	sessions := &scopedSessions{
		catalog: map[string][]SessionInfo{"": {{ID: "saved", UpdatedAt: time.Now()}}},
		histories: map[string]map[string][]EngineMessage{
			"": {"saved": {{Role: "user", Content: "saved question"}, {Role: "assistant", Content: "saved answer"}}},
		},
	}
	service := mustNew(t, Dependencies{
		Engine: engine, Runtime: &fakeRuntime{}, Plugins: &fakePlugins{current: PluginInfo{Name: "default"}},
		Skills: fakeSkills{}, Sessions: sessions,
	})
	defer service.Shutdown()

	if err := service.BeginNewSession(); err != nil {
		t.Fatal(err)
	}
	if err := service.Submit(context.Background(), "/resume saved"); err != nil {
		t.Fatal(err)
	}
	snapshot := waitForSnapshot(t, service, func(snapshot Snapshot) bool { return len(snapshot.Sessions) == 1 })
	if snapshot.Session.Draft || snapshot.Session.ID != "saved" || snapshot.Session.Name != "saved question" {
		t.Fatalf("resumed session = %+v", snapshot.Session)
	}
	engine.mu.Lock()
	starts := engine.starts
	engine.mu.Unlock()
	if starts != 0 {
		t.Fatalf("resume from draft called StartSession %d times", starts)
	}
}

func TestProjectBindingCreatesScopesAndNewSessionInheritsProject(t *testing.T) {
	engine := &fakeEngine{}
	runtime := &fakeRuntime{}
	sessions := &scopedSessions{}
	workspaces := newFakeWorkspace()
	service := mustNew(t, Dependencies{
		Engine: engine, Runtime: runtime, Plugins: &fakePlugins{current: PluginInfo{Name: "default"}},
		Skills: fakeSkills{}, Sessions: sessions, Workspace: workspaces,
	})
	defer service.Shutdown()

	root := t.TempDir()
	if err := service.CreateWorkspace("project", root, ""); err != nil {
		t.Fatal(err)
	}
	if runtime.projectRoot != root || sessions.Workspace() != "project-1" || workspaces.bindings["session-1"] != "project-1" {
		t.Fatalf("create project did not bind all scope state: root=%q sessionStore=%q bindings=%v", runtime.projectRoot, sessions.Workspace(), workspaces.bindings)
	}
	if err := service.Submit(context.Background(), "/new"); err != nil {
		t.Fatal(err)
	}
	snapshot := service.Snapshot()
	if snapshot.CurrentWorkspace == nil || snapshot.CurrentWorkspace.ID != "project-1" {
		t.Fatalf("new session lost project binding: %+v", snapshot.CurrentWorkspace)
	}
	if !snapshot.Session.Draft || snapshot.Session.ID != "" {
		t.Fatalf("/new must remain an unmaterialized draft: %+v", snapshot.Session)
	}
	if _, exists := workspaces.bindings["session-new"]; exists {
		t.Fatalf("draft session bound before first request: %v", workspaces.bindings)
	}
	if err := service.Submit(context.Background(), "first project question"); err != nil {
		t.Fatal(err)
	}
	snapshot = service.Snapshot()
	if workspaces.bindings["session-new"] != "project-1" || sessions.Workspace() != "project-1" {
		t.Fatalf("materialized session did not inherit project: bindings=%v sessionStore=%q", workspaces.bindings, sessions.Workspace())
	}
	if snapshot.SessionWorkspaces["session-new"] != "project-1" || snapshot.Session.Name != "first project question" {
		t.Fatalf("materialized snapshot = %+v", snapshot)
	}
	if err := service.WaitForIdle(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestResumeRestoresProjectScope(t *testing.T) {
	engine := &fakeEngine{}
	runtime := &fakeRuntime{}
	sessions := &scopedSessions{}
	workspaces := newFakeWorkspace()
	root := t.TempDir()
	workspaces.items["project-1"] = WorkspaceInfo{ID: "project-1", Name: "project", RootPath: root}
	workspaces.BindSession("saved", "project-1")
	service := mustNew(t, Dependencies{
		Engine: engine, Runtime: runtime, Plugins: &fakePlugins{current: PluginInfo{Name: "default"}},
		Skills: fakeSkills{}, Sessions: sessions, Workspace: workspaces,
	})
	defer service.Shutdown()
	if err := service.Submit(context.Background(), "/resume saved"); err != nil {
		t.Fatal(err)
	}
	if runtime.projectRoot != root || sessions.Workspace() != "project-1" {
		t.Fatalf("resume did not restore project scope: root=%q store=%q", runtime.projectRoot, sessions.Workspace())
	}
}

func TestNewHydratesPersistedWorkspaceSessions(t *testing.T) {
	engine := &fakeEngine{}
	runtime := &fakeRuntime{}
	sessions := &scopedSessions{catalog: map[string][]SessionInfo{
		"project-1": {{ID: "saved-1", UpdatedAt: time.Unix(2, 0), TokenCount: 10}},
		"project-2": {{ID: "saved-2", UpdatedAt: time.Unix(3, 0), TokenCount: 20}},
	}}
	workspaces := newFakeWorkspace()
	workspaces.items["project-1"] = WorkspaceInfo{ID: "project-1", Name: "one", RootPath: t.TempDir()}
	workspaces.items["project-2"] = WorkspaceInfo{ID: "project-2", Name: "two", RootPath: t.TempDir()}
	workspaces.BindSession("saved-1", "project-1")
	workspaces.BindSession("saved-2", "project-2")

	service := mustNew(t, Dependencies{
		Engine: engine, Runtime: runtime, Plugins: &fakePlugins{current: PluginInfo{Name: "default"}},
		Skills: fakeSkills{}, Sessions: sessions, Workspace: workspaces,
	})
	defer service.Shutdown()

	snapshot := waitForSnapshot(t, service, func(snapshot Snapshot) bool { return len(snapshot.Sessions) == 2 })
	if len(snapshot.Workspaces) != 2 || len(snapshot.Sessions) != 2 {
		t.Fatalf("hydrated snapshot workspaces=%v sessions=%v", snapshot.Workspaces, snapshot.Sessions)
	}
	if snapshot.Sessions[0].ID != "saved-2" || snapshot.SessionWorkspaces["saved-1"] != "project-1" {
		t.Fatalf("hydrated session catalog=%v bindings=%v", snapshot.Sessions, snapshot.SessionWorkspaces)
	}
}

func TestResumeReadsSessionFromItsPersistedWorkspace(t *testing.T) {
	engine := &fakeEngine{}
	runtime := &fakeRuntime{}
	sessions := &scopedSessions{
		workspace: "project-2",
		catalog:   map[string][]SessionInfo{"project-1": {{ID: "saved", UpdatedAt: time.Unix(2, 0)}}},
		histories: map[string]map[string][]EngineMessage{
			"project-1": {"saved": {{Role: "assistant", Content: "project one history"}}},
		},
	}
	workspaces := newFakeWorkspace()
	root := t.TempDir()
	workspaces.items["project-1"] = WorkspaceInfo{ID: "project-1", Name: "one", RootPath: root}
	workspaces.items["project-2"] = WorkspaceInfo{ID: "project-2", Name: "two", RootPath: t.TempDir()}
	workspaces.BindSession("saved", "project-1")

	service := mustNew(t, Dependencies{
		Engine: engine, Runtime: runtime, Plugins: &fakePlugins{current: PluginInfo{Name: "default"}},
		Skills: fakeSkills{}, Sessions: sessions, Workspace: workspaces,
	})
	defer service.Shutdown()

	if err := service.Submit(context.Background(), "/resume saved"); err != nil {
		t.Fatal(err)
	}
	if sessions.LoadedWorkspace() != "project-1" || sessions.Workspace() != "project-1" {
		t.Fatalf("resume read workspace=%q active=%q", sessions.LoadedWorkspace(), sessions.Workspace())
	}
	if runtime.projectRoot != root || engine.History()[0].Content != "project one history" {
		t.Fatalf("resume root=%q history=%v", runtime.projectRoot, engine.History())
	}
}

func TestResumeRepairsBindingWhenHistoryLivesInAnotherWorkspace(t *testing.T) {
	sessions := &scopedSessions{
		catalog: map[string][]SessionInfo{"project-1": {{ID: "saved", UpdatedAt: time.Unix(2, 0)}}},
		histories: map[string]map[string][]EngineMessage{
			"project-1": {"saved": {{Role: "assistant", Content: "recover me"}}},
		},
	}
	workspaces := newFakeWorkspace()
	workspaces.items["project-1"] = WorkspaceInfo{ID: "project-1", Name: "one", RootPath: t.TempDir()}
	workspaces.items["project-2"] = WorkspaceInfo{ID: "project-2", Name: "two", RootPath: t.TempDir()}
	workspaces.BindSession("saved", "project-2")
	service := mustNew(t, Dependencies{
		Engine: &fakeEngine{}, Runtime: &fakeRuntime{}, Plugins: &fakePlugins{current: PluginInfo{Name: "default"}},
		Skills: fakeSkills{}, Sessions: sessions, Workspace: workspaces,
	})
	defer service.Shutdown()

	if err := service.Submit(context.Background(), "/resume saved"); err != nil {
		t.Fatal(err)
	}
	if workspaces.bindings["saved"] != "project-1" {
		t.Fatalf("stale binding was not repaired: %v", workspaces.bindings)
	}
}

func TestLoadMoreHistoryUsesResumedSessionWorkspace(t *testing.T) {
	history := make([]EngineMessage, 250)
	for index := range history {
		history[index] = EngineMessage{Role: "assistant", Content: fmt.Sprintf("message-%d", index)}
	}
	sessions := &scopedSessions{
		catalog: map[string][]SessionInfo{"project-1": {{ID: "saved", UpdatedAt: time.Unix(2, 0)}}},
		histories: map[string]map[string][]EngineMessage{
			"project-1": {"saved": history},
		},
	}
	workspaces := newFakeWorkspace()
	workspaces.items["project-1"] = WorkspaceInfo{ID: "project-1", Name: "one", RootPath: t.TempDir()}
	workspaces.items["project-2"] = WorkspaceInfo{ID: "project-2", Name: "two", RootPath: t.TempDir()}
	workspaces.BindSession("saved", "project-1")
	service := mustNew(t, Dependencies{
		Engine: &fakeEngine{}, Runtime: &fakeRuntime{}, Plugins: &fakePlugins{current: PluginInfo{Name: "default"}},
		Skills: fakeSkills{}, Sessions: sessions, Workspace: workspaces,
	})
	defer service.Shutdown()

	if err := service.Submit(context.Background(), "/resume saved"); err != nil {
		t.Fatal(err)
	}
	sessions.SetWorkspace("project-2") // simulate unrelated active-scope drift
	if err := service.LoadMoreHistory(50); err != nil {
		t.Fatal(err)
	}
	if sessions.LoadedWorkspace() != "project-1" || service.Snapshot().HistoryOffset != 0 {
		t.Fatalf("history range workspace=%q offset=%d", sessions.LoadedWorkspace(), service.Snapshot().HistoryOffset)
	}
}

func TestSwitchProjectStartsIndependentSessionWhenHistoryExists(t *testing.T) {
	engine := &fakeEngine{history: []EngineMessage{{Role: "user", Content: "old project"}}}
	runtime := &fakeRuntime{}
	sessions := &scopedSessions{workspace: "project-1"}
	workspaces := newFakeWorkspace()
	workspaces.items["project-1"] = WorkspaceInfo{ID: "project-1", Name: "one", RootPath: t.TempDir()}
	workspaces.items["project-2"] = WorkspaceInfo{ID: "project-2", Name: "two", RootPath: t.TempDir()}
	workspaces.BindSession("session-1", "project-1")
	service := mustNew(t, Dependencies{
		Engine: engine, Runtime: runtime, Plugins: &fakePlugins{current: PluginInfo{Name: "default"}},
		Skills: fakeSkills{}, Sessions: sessions, Workspace: workspaces,
	})
	defer service.Shutdown()

	if err := service.BindWorkspace("project-2"); err != nil {
		t.Fatal(err)
	}
	snapshot := service.Snapshot()
	if snapshot.Session.ID != "session-new" || snapshot.CurrentWorkspace == nil || snapshot.CurrentWorkspace.ID != "project-2" {
		t.Fatalf("switched snapshot=%+v workspace=%+v", snapshot.Session, snapshot.CurrentWorkspace)
	}
	if workspaces.bindings["session-1"] != "project-1" || workspaces.bindings["session-new"] != "project-2" {
		t.Fatalf("project bindings=%v", workspaces.bindings)
	}
	if savedIDs := sessions.SavedIDs(); len(savedIDs) != 1 || savedIDs[0] != "session-1" {
		t.Fatalf("saved sessions=%v", savedIDs)
	}
}

func TestSnapshotIncludesPersistedSessions(t *testing.T) {
	t.Parallel()
	service := newTestService(t, &fakeEngine{})
	defer service.Shutdown()

	snapshot := waitForSnapshot(t, service, func(snapshot Snapshot) bool { return len(snapshot.Sessions) == 1 })
	if len(snapshot.Sessions) != 1 {
		t.Fatalf("sessions = %d, want 1", len(snapshot.Sessions))
	}
	if snapshot.Sessions[0].ID != "saved" || snapshot.Sessions[0].TokenCount != 4 {
		t.Fatalf("unexpected session metadata: %+v", snapshot.Sessions[0])
	}
}

func TestSnapshotDoesNotReadBlockedSessionCatalog(t *testing.T) {
	sessions := &blockingCatalogSessions{entered: make(chan struct{}), release: make(chan struct{})}
	service := mustNew(t, Dependencies{
		Engine: &fakeEngine{}, Runtime: &fakeRuntime{}, Plugins: &fakePlugins{current: PluginInfo{Name: "default"}},
		Skills: fakeSkills{}, Sessions: sessions,
	})
	defer service.Shutdown()
	defer close(sessions.release)

	select {
	case <-sessions.entered:
	case <-time.After(time.Second):
		t.Fatal("catalog worker did not start")
	}
	for index := 0; index < 10; index++ {
		_ = service.Snapshot()
	}
	if calls := sessions.calls.Load(); calls != 1 {
		t.Fatalf("Snapshot invoked SessionPort.List %d times while catalog was blocked", calls)
	}
}

func TestShutdownDoesNotWaitForBlockedSessionCatalog(t *testing.T) {
	sessions := &blockingCatalogSessions{entered: make(chan struct{}), release: make(chan struct{})}
	service := mustNew(t, Dependencies{
		Engine: &fakeEngine{}, Runtime: &fakeRuntime{}, Plugins: &fakePlugins{current: PluginInfo{Name: "default"}},
		Skills: fakeSkills{}, Sessions: sessions,
	})
	defer close(sessions.release)
	select {
	case <-sessions.entered:
	case <-time.After(time.Second):
		t.Fatal("catalog worker did not start")
	}
	started := time.Now()
	service.Shutdown()
	if elapsed := time.Since(started); elapsed > 250*time.Millisecond {
		t.Fatalf("shutdown waited for blocked catalog I/O: %s", elapsed)
	}
}

func TestRuntimeMailboxDrainsIntoHistoryOutsideServiceLock(t *testing.T) {
	engine := &fakeEngine{}
	runtime := &fakeRuntime{mailbox: []string{"child conclusion"}}
	service := mustNew(t, Dependencies{
		Engine: engine, Runtime: runtime, Plugins: &fakePlugins{current: PluginInfo{Name: "default"}},
		Skills: fakeSkills{}, Sessions: fakeSessions{},
	})
	defer service.Shutdown()

	service.injectPendingSubagentContexts()
	history := engine.History()
	if len(history) != 1 || !strings.Contains(history[0].Content, "child conclusion") {
		t.Fatalf("merge-back was not injected into Engine history: %#v", history)
	}
	snapshot := service.Snapshot()
	if len(snapshot.Conversation) != 1 || !strings.Contains(snapshot.Conversation[0].Content, "child conclusion") {
		t.Fatalf("merge-back was not projected to the frontend snapshot: %#v", snapshot.Conversation)
	}
	if pending := runtime.DrainSubagentContexts(); len(pending) != 0 {
		t.Fatalf("runtime mailbox was not drained: %#v", pending)
	}
}

func TestResumedChatPersistsToSelectedSession(t *testing.T) {
	engine := &fakeEngine{}
	sessions := &trackingSessions{}
	service := mustNew(t, Dependencies{
		Engine: engine, Runtime: &fakeRuntime{},
		Plugins: &fakePlugins{current: PluginInfo{Name: "default"}},
		Skills:  fakeSkills{}, Sessions: sessions,
	})
	defer service.Shutdown()

	if err := service.Submit(context.Background(), "/resume saved"); err != nil {
		t.Fatal(err)
	}
	if err := service.Submit(context.Background(), "continue"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for service.Snapshot().Chat.Running && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	sessions.mu.Lock()
	defer sessions.mu.Unlock()
	if len(sessions.savedIDs) != 1 || sessions.savedIDs[0] != "saved" {
		t.Fatalf("saved session IDs = %v, want [saved]", sessions.savedIDs)
	}
}

func TestNewRejectsMissingDependencies(t *testing.T) {
	if _, err := New(Dependencies{}); err == nil {
		t.Fatal("expected missing dependency error")
	}
}

func TestEventHubOrdersAndResyncs(t *testing.T) {
	hub := NewEventHub()
	subscription := hub.Subscribe(1)
	defer subscription.Close()
	hub.Publish(EventMessageAdded, 1, "", nil)
	hub.Publish(EventMessageDelta, 2, "", nil)
	event := <-subscription.Events
	if event.Kind != EventResyncRequired || event.Seq != 2 {
		t.Fatalf("expected resync at seq 2, got %#v", event)
	}
	if event.ProtocolVersion != ProtocolVersion {
		t.Fatalf("protocol version = %d, want %d", event.ProtocolVersion, ProtocolVersion)
	}
}

func TestMessageDeltaIncludesStableMessageID(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	defer service.Shutdown()
	subscription := service.Subscribe(8)
	defer subscription.Close()

	service.mu.Lock()
	service.snapshot.Chat = ChatState{Running: true, RequestID: "request-1"}
	message := service.appendMessageLocked("assistant", "", nil)
	messageID := message.ID
	service.mu.Unlock()

	service.appendDelta("request-1", "next")
	var event Event
	deadline := time.After(time.Second)
	for event.Kind != EventMessageDelta {
		select {
		case event = <-subscription.Events:
		case <-deadline:
			t.Fatal("did not receive message.delta event")
		}
	}
	var delta MessageDelta
	if err := json.Unmarshal(event.Payload, &delta); err != nil {
		t.Fatal(err)
	}
	if delta.MessageID != messageID || delta.Delta != "next" {
		t.Fatalf("unexpected delta payload: %+v", delta)
	}
}

func TestSuggestionsAndSkillRouting(t *testing.T) {
	engine := &fakeEngine{}
	service := newTestService(t, engine)
	defer service.Shutdown()
	suggestions := service.Suggestions("/R")
	if len(suggestions) != 3 || suggestions[0].Kind != "command" || suggestions[1].Kind != "tool" || suggestions[2].Kind != "skill" {
		t.Fatalf("unexpected suggestions: %#v", suggestions)
	}
	if err := service.Submit(context.Background(), "/review strict"); err != nil {
		t.Fatal(err)
	}
	waitForChatCompletion(t, service)
	engine.mu.Lock()
	prompt := engine.prompt
	modelInput := engine.lastInput
	engine.mu.Unlock()
	if !strings.Contains(prompt, "## Trusted Active Skill: review") || !strings.Contains(prompt, "review prompt") || strings.Contains(prompt, "strict") {
		t.Fatalf("trusted Skill system prompt = %q", prompt)
	}
	if !strings.Contains(prompt, "Seelex") {
		t.Fatalf("prompt missing identity: %q", prompt)
	}
	if !strings.Contains(prompt, "## Effort: High") {
		t.Fatalf("prompt missing effort: %q", prompt)
	}
	if modelInput != "/review strict" {
		t.Fatalf("slash Skill model input = %q", modelInput)
	}
	if err := service.Submit(context.Background(), "#review focused"); err != nil {
		t.Fatal(err)
	}
	waitForChatCompletion(t, service)
	engine.mu.Lock()
	prompt = engine.prompt
	modelInput = engine.lastInput
	engine.mu.Unlock()
	if modelInput != "#review focused" || !strings.Contains(prompt, "## Trusted Active Skill: review") || !strings.Contains(prompt, "review prompt") {
		t.Fatalf("hash Skill input=%q prompt=%q", modelInput, prompt)
	}
}

func TestChatPublishesSnapshotWithoutUI(t *testing.T) {
	engine := &fakeEngine{chunks: []string{"an", "swer"}}
	service := newTestService(t, engine)
	defer service.Shutdown()
	if err := service.Submit(context.Background(), "hello"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		snapshot := service.Snapshot()
		if !snapshot.Chat.Running {
			if len(snapshot.Conversation) < 2 || snapshot.Conversation[len(snapshot.Conversation)-1].Content != "answer" {
				t.Fatalf("unexpected conversation: %#v", snapshot.Conversation)
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("chat did not complete")
}

type gracefulShutdownEngine struct {
	*fakeEngine
	firstStarted  chan struct{}
	secondStarted chan struct{}
	releaseFirst  chan struct{}
	releaseSecond chan struct{}
	mu            sync.Mutex
	calls         int
}

func newGracefulShutdownEngine() *gracefulShutdownEngine {
	return &gracefulShutdownEngine{
		fakeEngine:    &fakeEngine{},
		firstStarted:  make(chan struct{}),
		secondStarted: make(chan struct{}),
		releaseFirst:  make(chan struct{}),
		releaseSecond: make(chan struct{}),
	}
}

func (engine *gracefulShutdownEngine) ChatStream(ctx context.Context, input string, onChunk func(string)) (string, error) {
	engine.mu.Lock()
	engine.calls++
	call := engine.calls
	engine.mu.Unlock()
	if call == 1 {
		close(engine.firstStarted)
		select {
		case <-engine.releaseFirst:
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	if call == 2 {
		close(engine.secondStarted)
		select {
		case <-engine.releaseSecond:
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	return engine.fakeEngine.ChatStream(ctx, input, onChunk)
}

func TestGracefulShutdownWaitsForQueuedChat(t *testing.T) {
	engine := newGracefulShutdownEngine()
	service := newTestService(t, engine)

	if err := service.Submit(context.Background(), "first"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-engine.firstStarted:
	case <-time.After(time.Second):
		t.Fatal("first chat did not start")
	}
	if err := service.Submit(context.Background(), "queued"); err != nil {
		t.Fatal(err)
	}
	service.BeginGracefulShutdown()
	if err := service.Submit(context.Background(), "rejected"); !errors.Is(err, ErrApplicationDraining) {
		t.Fatalf("Submit after graceful shutdown = %v, want ErrApplicationDraining", err)
	}

	idle := make(chan error, 1)
	go func() { idle <- service.WaitForIdle(context.Background()) }()
	close(engine.releaseFirst)
	select {
	case <-engine.secondStarted:
	case <-time.After(time.Second):
		t.Fatal("queued chat did not start")
	}
	select {
	case err := <-idle:
		t.Fatalf("WaitForIdle returned before queued chat completed: %v", err)
	default:
	}
	if !service.Snapshot().Chat.Running {
		t.Fatal("queued chat left an observable idle gap")
	}
	close(engine.releaseSecond)
	select {
	case err := <-idle:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("WaitForIdle did not return after queued chat completed")
	}
}

func TestSessionBackedQueueIsAcknowledgedWhenLoopReturns(t *testing.T) {
	engine := &sessionBackedBlockingEngine{
		fakeEngine: &fakeEngine{},
		started:    make(chan struct{}),
		release:    make(chan struct{}),
	}
	sessions := &blockingSaveSessions{entered: make(chan struct{}), release: make(chan struct{})}
	service := newTestService(t, engine, withTestSessions(sessions))

	if err := service.Submit(context.Background(), "first"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-engine.started:
	case <-time.After(time.Second):
		t.Fatal("first chat did not start")
	}
	if err := service.Submit(context.Background(), "queued"); err != nil {
		t.Fatal(err)
	}
	close(engine.release)
	select {
	case <-sessions.entered:
	case <-time.After(time.Second):
		t.Fatal("current turn did not reach persistence")
	}
	if got := service.Snapshot().Chat.QueuedCount; got != 0 {
		t.Fatalf("queued count while persistence is draining = %d, want 0", got)
	}
	service.mu.RLock()
	resume := service.taskService.ResumeRecord()
	service.mu.RUnlock()
	if len(resume.QueuedRefs) != 1 || resume.QueuedRefs[0] != "queued" {
		t.Fatalf("persistence resume refs = %#v, want queued input", resume.QueuedRefs)
	}
	close(sessions.release)
	if err := service.WaitForIdle(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestCancelChatInterruptsContextAwareEngine(t *testing.T) {
	service := newTestService(t, &blockingEngine{fakeEngine: &fakeEngine{}})
	if err := service.Submit(context.Background(), "interrupt me"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && !service.Snapshot().Chat.Running {
		time.Sleep(time.Millisecond)
	}
	if !service.Snapshot().Chat.Running {
		t.Fatal("chat did not start")
	}
	if !service.CancelChat("") {
		t.Fatal("CancelChat returned false for the active request")
	}
	waitContext, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := service.WaitForIdle(waitContext); err != nil {
		t.Fatal(err)
	}
	if task := service.Snapshot().Task; task == nil || task.Status != TaskInterrupted {
		t.Fatalf("cancelled task = %#v, want interrupted", task)
	}
}

func waitForChatCompletion(t *testing.T, service *Service) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if !service.Snapshot().Chat.Running {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("chat did not complete")
}

func TestToolEventsUpdateSnapshot(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	defer service.Shutdown()
	service.handleToolStart("read", "read-1", `{"path":"a"}`)
	service.handleToolComplete("read", "read-1", "ok", nil, time.Second)
	snapshot := service.Snapshot()
	found := false
	for _, message := range snapshot.Conversation {
		if message.Tool != nil && message.Tool.ID == "read-1" && message.Tool.Status == "success" {
			found = true
		}
	}
	if !found {
		t.Fatalf("completed tool call not found: %#v", snapshot.Conversation)
	}
}

func TestToolCompletionDoesNotReenterServiceLockForGoalSkillVisibility(t *testing.T) {
	runtime := &goalVisibilityRuntime{fakeRuntime: &fakeRuntime{}}
	service := newTestService(t, &fakeEngine{}, withTestRuntime(runtime))

	service.mu.Lock()
	service.taskExecution = newTaskExecutionState("task-goal", "plan work", "high")
	service.components.tasks.activateTaskSkillsLocked(service.taskExecution, []PromptLayer{{Kind: "skill", Name: "goal", Text: "goal prompt"}})
	service.mu.Unlock()
	if !service.GoalSkillActive() {
		t.Fatal("goal skill state was not projected")
	}
	service.publishRuntimeProjections()

	service.handleToolStart("bash", "bash-goal", `{"command":"echo ok"}`)
	done := make(chan struct{})
	go func() {
		service.handleToolComplete("bash", "bash-goal", "ok", nil, time.Millisecond)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("tool completion deadlocked while refreshing goal-skill visibility")
	}
	if visible := service.Snapshot().Runtime.VisibleTools; len(visible) != 1 || visible[0].Name != "plan_load" {
		t.Fatalf("visible tools = %#v, want goal-skill policy result", visible)
	}
}

func TestPlanRunJSONFailureOpensRecoveryInteraction(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	defer service.Shutdown()
	service.handleToolStart("plan_load", "load-1", `{"entry":"build","nodes":{"build":{"input":"build it"}},"edges":{}}`)
	service.handleToolComplete("plan_load", "load-1", `{"status":"loaded"}`, nil, 0)

	service.handleToolStart("plan_run", "run-1", `{}`)
	service.handleToolComplete("plan_run", "run-1", `{"status":"failed","error":"node \"build\": failed"}`, nil, 0)

	snapshot := service.Snapshot()
	if snapshot.Runtime.Plan == nil || snapshot.Runtime.Plan.Status != PlanFailed {
		t.Fatalf("plan = %+v, want failed", snapshot.Runtime.Plan)
	}
	if snapshot.Runtime.Plan.Nodes[0].Status != NodeFailed {
		t.Fatalf("node status = %q, want %q", snapshot.Runtime.Plan.Nodes[0].Status, NodeFailed)
	}
	if snapshot.Interaction == nil || snapshot.Interaction.Kind != "plan_retry" {
		t.Fatalf("interaction = %+v, want plan_retry", snapshot.Interaction)
	}
}

func TestPlanRunToolErrorDoesNotDeadlock(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	defer service.Shutdown()
	service.handleToolStart("plan_load", "load-1", `{"entry":"build","nodes":{"build":{"input":"build it"}},"edges":{}}`)
	service.handleToolComplete("plan_load", "load-1", `{"status":"loaded"}`, nil, 0)

	done := make(chan struct{})
	go func() {
		service.handleToolStart("plan_run", "run-1", `{}`)
		service.handleToolComplete("plan_run", "run-1", "", errors.New(`node "build": interrupted`), 0)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("plan_run tool error deadlocked")
	}
	if interaction := service.Snapshot().Interaction; interaction == nil || interaction.Kind != "plan_retry" {
		t.Fatalf("interaction = %+v, want plan_retry", interaction)
	}
}

func TestResolvePlanFailureReplansWithoutRunningReplacement(t *testing.T) {
	engine := &fakeEngine{}
	runtime := &fakeRuntime{replanResult: seelebridge.PlanPreflight{
		Arguments: `{"entry":"recover","nodes":{"recover":{"input":"diagnose the failed build"}},"edges":{}}`,
		Result:    `{"status":"loaded","node_count":1}`,
	}}
	service := newTestService(t, engine, withTestRuntime(runtime))

	service.mu.Lock()
	service.appendMessageLocked("user", "build and verify the release", nil)
	service.mu.Unlock()
	service.handleToolStart("plan_load", "load-1", `{"entry":"build","nodes":{"build":{"input":"build release"}},"edges":{}}`)
	service.handleToolComplete("plan_load", "load-1", `{"status":"loaded"}`, nil, 0)
	service.handleToolStart("plan_run", "run-1", `{}`)
	service.handleToolComplete("plan_run", "run-1", `{"status":"failed","error":"node \"build\": compiler failed"}`, nil, 0)

	interaction := service.Snapshot().Interaction
	if interaction == nil {
		t.Fatal("expected failed plan interaction")
	}
	if err := service.ResolveInteraction(context.Background(), interaction.ID, "replan"); err != nil {
		t.Fatal(err)
	}
	if len(runtime.replans) != 1 {
		t.Fatalf("replan calls = %d, want 1", len(runtime.replans))
	}
	request := runtime.replans[0]
	if request.Objective != "build and verify the release" || !strings.Contains(request.PreviousPlan, `"build"`) {
		t.Fatalf("replan request lost task or plan: %+v", request)
	}
	if !strings.Contains(request.Failure, "compiler failed") || !strings.Contains(request.Evidence, "node=build status=failed") {
		t.Fatalf("replan request lost failure evidence: %+v", request)
	}
	snapshot := service.Snapshot()
	if snapshot.Interaction != nil {
		t.Fatalf("interaction was not closed: %+v", snapshot.Interaction)
	}
	if snapshot.Runtime.Plan == nil || snapshot.Runtime.Plan.EntryNodeID != "recover" || snapshot.Runtime.Plan.Status != PlanPending {
		t.Fatalf("replacement plan = %+v", snapshot.Runtime.Plan)
	}
	if engine.lastInput != "" {
		t.Fatalf("replan unexpectedly entered ChatStream with %q", engine.lastInput)
	}
}

func TestResolvePlanFailureKeepsInteractionWhenReplanFails(t *testing.T) {
	runtime := &fakeRuntime{replanErr: errors.New("planner unavailable")}
	service := newTestService(t, &fakeEngine{}, withTestRuntime(runtime))

	service.handleToolStart("plan_load", "load-1", `{"entry":"build","nodes":{"build":{"input":"build release"}},"edges":{}}`)
	service.handleToolComplete("plan_load", "load-1", `{"status":"loaded"}`, nil, 0)
	service.handleToolStart("plan_run", "run-1", `{}`)
	service.handleToolComplete("plan_run", "run-1", `{"status":"failed","error":"node \"build\": compiler failed"}`, nil, 0)

	interaction := service.Snapshot().Interaction
	if interaction == nil {
		t.Fatal("expected failed plan interaction")
	}
	if err := service.ResolveInteraction(context.Background(), interaction.ID, "replan"); err == nil || !strings.Contains(err.Error(), "planner unavailable") {
		t.Fatalf("replan error = %v, want planner unavailable", err)
	}
	if current := service.Snapshot().Interaction; current == nil || current.ID != interaction.ID {
		t.Fatalf("failed replan closed recovery interaction: %+v", current)
	}
}

func TestResolvePlanFailureStopsAfterPlanChainReplanLimit(t *testing.T) {
	runtime := &fakeRuntime{replanResult: seelebridge.PlanPreflight{
		Arguments: `{"entry":"recover","nodes":{"recover":{"input":"diagnose"}},"edges":{}}`,
		Result:    `{"status":"loaded","node_count":1}`,
	}}
	service := newTestService(t, &fakeEngine{}, withTestRuntime(runtime))

	service.handleToolStart("plan_load", "load-1", `{"entry":"build","nodes":{"build":{"input":"build"}},"edges":{}}`)
	service.handleToolComplete("plan_load", "load-1", `{"status":"loaded"}`, nil, 0)
	for attempt := 0; attempt < Limits().MaxReplansPerPlanChain; attempt++ {
		service.handleToolStart("plan_run", fmt.Sprintf("run-%d", attempt), `{}`)
		service.handleToolComplete("plan_run", fmt.Sprintf("run-%d", attempt), `{"status":"failed","error":"node \"recover\": failed"}`, nil, 0)
		interaction := service.Snapshot().Interaction
		if interaction == nil {
			t.Fatalf("attempt %d did not open recovery interaction", attempt)
		}
		if err := service.ResolveInteraction(context.Background(), interaction.ID, "replan"); err != nil {
			t.Fatalf("attempt %d replan: %v", attempt, err)
		}
	}
	service.handleToolStart("plan_run", "run-limit", `{}`)
	service.handleToolComplete("plan_run", "run-limit", `{"status":"failed","error":"node \"recover\": failed"}`, nil, 0)
	interaction := service.Snapshot().Interaction
	if interaction == nil {
		t.Fatal("expected recovery interaction after limit")
	}
	if err := service.ResolveInteraction(context.Background(), interaction.ID, "replan"); err == nil || !strings.Contains(err.Error(), "recovery limit") {
		t.Fatalf("limit error = %v", err)
	}
	if len(runtime.replans) != Limits().MaxReplansPerPlanChain {
		t.Fatalf("replan calls = %d, want %d", len(runtime.replans), Limits().MaxReplansPerPlanChain)
	}
	if plan := service.Snapshot().Runtime.Plan; plan == nil || plan.ReplanCount != Limits().MaxReplansPerPlanChain {
		t.Fatalf("plan replan count = %+v", plan)
	}
}

func TestRuntimeSnapshotIncludesReplanMonitor(t *testing.T) {
	runtime := &fakeRuntime{replanMetrics: seelebridge.ReplanMetrics{
		InFlight: 1, ConcurrentLimit: 2, WindowAttempts: 3, WindowLimit: 6,
		Accepted: 3, Succeeded: 2, Failed: 1, Rejected: 4, DuplicateRejected: 1, ProviderRequests: 5,
	}}
	service := newTestService(t, &fakeEngine{}, withTestRuntime(runtime))
	projection := service.collectRuntimeProjection(context.Background())
	service.mu.Lock()
	service.applyRuntimeProjectionLocked(projection)
	service.mu.Unlock()
	monitor := service.Snapshot().Runtime.Replan
	if monitor.InFlight != 1 || monitor.WindowAttempts != 3 || monitor.Rejected != 4 || monitor.ProviderRequests != 5 {
		t.Fatalf("replan monitor = %+v", monitor)
	}
}

func TestNormalizePlanToolCallInfoUsesCanonicalAdapterJSON(t *testing.T) {
	info := session.ToolCallInfo{Name: "plan_load", Arguments: `{"entry":"inspect","nodes":[{"id":"inspect","input":"inspect"},{"id":"report","input":"report"}],"edges":[{"from":"inspect","to":"report"}]}`}
	normalized := normalizePlanToolCallInfo(info)
	if normalized.Arguments == info.Arguments || !strings.Contains(normalized.Arguments, `"nodes":{"inspect"`) || !strings.Contains(normalized.Arguments, `"edges":{"inspect":["report"]}`) {
		t.Fatalf("normalized plan args = %q", normalized.Arguments)
	}
	invalid := session.ToolCallInfo{Name: "plan_load", Arguments: `{"entry":"inspect","nodes":[],"edges":[]}`}
	if got := normalizePlanToolCallInfo(invalid); got.Arguments != invalid.Arguments {
		t.Fatalf("invalid plan arguments must remain visible: %q", got.Arguments)
	}
}

func TestHandlePlanBranchEventUpdatesLifecycleAndRuntime(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	defer service.Shutdown()
	service.handleToolStart("plan_load", "load-1", `{"entry":"start","nodes":{"start":{"input":"start"},"left":{"input":"left"}},"edges":{"start":["left"]}}`)

	subscription := service.Subscribe(8)
	defer subscription.Close()
	service.HandlePlanBranchEvent(seelebridge.PlanBranchEvent{Type: "queued", BranchID: "left", NodeID: "left"})
	service.HandlePlanBranchEvent(seelebridge.PlanBranchEvent{Type: "started", BranchID: "left", NodeID: "left"})
	service.HandlePlanBranchEvent(seelebridge.PlanBranchEvent{Type: "completed", BranchID: "left", NodeID: "left"})

	snapshot := service.Snapshot()
	if snapshot.Runtime.Plan == nil || snapshot.Runtime.Plan.Status != PlanRunning {
		t.Fatalf("plan status = %+v, want running", snapshot.Runtime.Plan)
	}
	if snapshot.Runtime.Plan.Nodes[0].Status != NodePending || snapshot.Runtime.Plan.Nodes[1].Status != NodeCompleted {
		t.Fatalf("node statuses = %+v", snapshot.Runtime.Plan.Nodes)
	}
	if snapshot.Runtime.Plan.Progress != 0.5 {
		t.Fatalf("progress = %v, want 0.5", snapshot.Runtime.Plan.Progress)
	}
	seen := 0
	deadline := time.After(time.Second)
	for seen < 3 {
		var event Event
		select {
		case event = <-subscription.Events:
		case <-deadline:
			t.Fatalf("received %d subagent.changed events, want 3", seen)
		}
		if event.Kind != EventSubagentChanged {
			continue
		}
		var payload SubagentEvent
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			t.Fatal(err)
		}
		if payload.NodeID != "left" || payload.Node.ID != "left" {
			t.Fatalf("event %d payload = %#v", seen, payload)
		}
		seen++
	}

	service.HandlePlanBranchEvent(seelebridge.PlanBranchEvent{Type: "panicked", BranchID: "start", NodeID: "start"})
	snapshot = service.Snapshot()
	if snapshot.Runtime.Plan.Status != PlanFailed || snapshot.Runtime.Plan.Nodes[0].Status != NodePanicked {
		t.Fatalf("panic state = %+v", snapshot.Runtime.Plan)
	}
}

func TestHandleSubagentToolEventProjectsBoundedIncrementals(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	defer service.Shutdown()
	service.handleToolStart("plan_load", "load-1", `{"entry":"start","nodes":{"start":{"input":"start"},"worker":{"input":"worker"}},"edges":{"start":["worker"]}}`)

	subscription := service.Subscribe(4)
	defer subscription.Close()
	long := strings.Repeat("x", Limits().EvidenceChars+20)
	started := seelebridge.SubagentToolEvent{
		ID: "subtool-1", NodeID: "worker", Name: "read_file", Arguments: long,
		Status: "running", StartedAt: time.Now(),
	}
	service.HandleSubagentToolEvent(started)
	completed := started
	completed.Status = "success"
	completed.Result = long
	completed.Duration = time.Second
	service.HandleSubagentToolEvent(completed)

	snapshot := service.Snapshot()
	node := findPlanNodeByID(snapshot.Runtime.Plan.Nodes, "worker")
	if node == nil || len(node.ToolEvents) != 1 {
		t.Fatalf("worker tool events = %#v", node)
	}
	if node.ToolEvents[0].Status != "success" || len(node.ToolEvents[0].Arguments) > Limits().EvidenceChars+3 || len(node.ToolEvents[0].Result) > Limits().EvidenceChars+3 {
		t.Fatalf("projected tool event = %#v", node.ToolEvents[0])
	}

	var first, second Event
	deadline := time.After(time.Second)
	for first.Kind == "" || second.Kind == "" {
		select {
		case received := <-subscription.Events:
			switch received.Kind {
			case EventSubagentToolStarted:
				first = received
			case EventSubagentToolCompleted:
				second = received
			}
		case <-deadline:
			t.Fatalf("did not receive subagent tool events; started=%q completed=%q", first.Kind, second.Kind)
		}
	}
	var payload SubagentToolEvent
	if err := json.Unmarshal(second.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.ID != "subtool-1" || payload.NodeID != "worker" || payload.Status != "success" {
		t.Fatalf("completed payload = %#v", payload)
	}
}

func TestToolHookBridgeAssignsUniqueStableIDs(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	defer service.Shutdown()
	bridge := NewToolHookBridge()
	bridge.Bind(service)
	hooks := bridge.Hooks()
	info := session.ToolCallInfo{Turn: 1, Name: "read", Arguments: `{"path":"a"}`}

	hooks.OnToolStart(context.Background(), info)
	hooks.OnToolComplete(context.Background(), info)
	hooks.OnToolStart(context.Background(), info)
	hooks.OnToolComplete(context.Background(), info)

	ids := make([]string, 0, 2)
	for _, message := range service.Snapshot().Conversation {
		if message.Role == "tool" && message.Tool != nil {
			ids = append(ids, message.Tool.ID)
		}
	}
	if len(ids) != 2 || ids[0] == ids[1] {
		t.Fatalf("tool IDs = %v, want two unique IDs", ids)
	}
}

func TestLoadMoreHistoryAssignsStableMessageIDs(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	defer service.Shutdown()
	service.mu.Lock()
	service.snapshot.HistoryOffset = 1
	service.mu.Unlock()

	if err := service.LoadMoreHistory(1); err != nil {
		t.Fatal(err)
	}
	conversation := service.Snapshot().Conversation
	if len(conversation) == 0 || conversation[0].ID == "" {
		t.Fatalf("loaded history message has no stable ID: %#v", conversation)
	}
}

func TestApprovalBrokerResolve(t *testing.T) {
	hub := NewEventHub()
	subscription := hub.Subscribe(1)
	defer subscription.Close()
	broker := NewApprovalBroker(hub)
	result := make(chan ApprovalDecision, 1)
	go func() {
		decision, err := broker.Request(context.Background(), ApprovalRequest{ID: "approval-1", Question: "continue?", Options: []InteractionOption{{ID: "yes", Label: "Yes"}}})
		if err == nil {
			result <- decision
		}
	}()
	select {
	case event := <-subscription.Events:
		if event.Kind != EventInteractionOpened {
			t.Fatalf("opened event kind = %q, want %q", event.Kind, EventInteractionOpened)
		}
	case <-time.After(time.Second):
		t.Fatal("approval request was not opened")
	}
	if err := broker.Resolve("approval-1", ApprovalDecision{OptionID: "yes"}); err != nil {
		t.Fatal(err)
	}
	select {
	case decision := <-result:
		if decision.OptionID != "yes" {
			t.Fatalf("unexpected decision %#v", decision)
		}
	case <-time.After(time.Second):
		t.Fatal("approval did not resolve")
	}
}

func TestResumeCommandOpensSessionInteraction(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	defer service.Shutdown()
	waitForSnapshot(t, service, func(snapshot Snapshot) bool { return len(snapshot.Sessions) == 1 })

	if err := service.Submit(context.Background(), "/resume"); err != nil {
		t.Fatal(err)
	}
	snapshot := service.Snapshot()
	if snapshot.Interaction == nil || snapshot.Interaction.Kind != "session" {
		t.Fatalf("resume should open a session interaction: %#v", snapshot.Interaction)
	}
}
