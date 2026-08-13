package scenario

import (
	"context"
	"fmt"
	"sync"

	frameworkSession "github.com/RedHuang-0622/Seele/session"
	"github.com/RedHuang-0622/seelex/application"
	"github.com/RedHuang-0622/seelex/application/contract/dto"
	"github.com/RedHuang-0622/seelex/seelebridge"
	seelexctxsearch "github.com/RedHuang-0622/seelex/seelexctx/search"
)

func NewHarnessRunner(value Scenario) (*Runner, error) {
	return newHarnessRunner(value, nil)
}

// NewHarnessRunnerWithToolFactory builds the normal Application harness while
// wiring scripted tool calls to real tool handlers.
func NewHarnessRunnerWithToolFactory(value Scenario, factory ToolExecutorFactory) (*Runner, error) {
	return newHarnessRunner(value, factory)
}

func newHarnessRunner(value Scenario, factory ToolExecutorFactory) (*Runner, error) {
	if err := value.Validate(); err != nil {
		return nil, err
	}
	events := application.NewEventHub()
	approvals := application.NewApprovalBroker(events)
	hookBridge := application.NewToolHookBridge()
	tools := &applicationToolLifecycle{hooks: hookBridge.Hooks()}
	scriptedEngine := NewScriptedEngine(value.EngineScript, tools, approvals)
	scriptedEngine.SetSessionID(initialSessionID(value))
	plugin := initialPlugin(value)
	service, err := application.New(application.Dependencies{
		Engine: scriptedEngine, Runtime: harnessRuntime{plugin: plugin},
		Plugins: &harnessPlugins{current: application.PluginInfo{Name: plugin}},
		Skills:  harnessSkills{}, Sessions: &harnessSessions{},
		Events: events, Approval: approvals,
	})
	if err != nil {
		return nil, fmt.Errorf("initialize application harness: %w", err)
	}
	hookBridge.Bind(service)
	if factory != nil {
		scriptedEngine.SetToolExecutor(factory(approvals, service.HandlePlanBranchEvent))
	}
	if value.Initial.Effort != "" {
		if err := service.SwitchEffort(context.Background(), value.Initial.Effort); err != nil {
			service.Shutdown()
			return nil, fmt.Errorf("set initial effort: %w", err)
		}
	}
	return NewRunner(value, service, scriptedEngine), nil
}

func initialPlugin(value Scenario) string {
	if value.Initial.Plugin != "" {
		return value.Initial.Plugin
	}
	return "default"
}

func initialSessionID(value Scenario) string {
	if value.Initial.ActiveSessionID != "" {
		return value.Initial.ActiveSessionID
	}
	if len(value.Initial.OpenSessionIDs) == 1 {
		return value.Initial.OpenSessionIDs[0]
	}
	return "session-e2e-1"
}

type applicationToolLifecycle struct {
	hooks *frameworkSession.LoopHooks
}

func (lifecycle *applicationToolLifecycle) Started(ctx context.Context, execution ToolExecution) {
	if lifecycle.hooks == nil || lifecycle.hooks.OnToolStart == nil {
		return
	}
	lifecycle.hooks.OnToolStart(ctx, frameworkSession.ToolCallInfo{
		Turn: execution.Turn, Name: execution.Name, Arguments: execution.Arguments,
	})
}

func (lifecycle *applicationToolLifecycle) Completed(ctx context.Context, execution ToolExecution) {
	if lifecycle.hooks == nil || lifecycle.hooks.OnToolComplete == nil {
		return
	}
	lifecycle.hooks.OnToolComplete(ctx, frameworkSession.ToolCallInfo{
		Turn: execution.Turn, Name: execution.Name, Arguments: execution.Arguments,
		Result: execution.Result, Error: execution.Err, Duration: execution.Duration,
	})
}

type harnessRuntime struct {
	plugin string
}

func (harnessRuntime) Model() string                                                          { return "scripted-e2e" }
func (harnessRuntime) Provider() string                                                       { return "local" }
func (harnessRuntime) Accounts() []application.AccountInfo                                    { return nil }
func (harnessRuntime) SelectAccount(string) bool                                              { return false }
func (harnessRuntime) VisibleTools(context.Context) []application.Tool                        { return nil }
func (runtime harnessRuntime) ActivePlugin() string                                           { return runtime.plugin }
func (harnessRuntime) FullAccess() bool                                                       { return false }
func (harnessRuntime) SetFullAccess(bool)                                                     {}
func (harnessRuntime) SetRuntimeVisibilityProjection(seelebridge.RuntimeVisibilityProjection) {}
func (harnessRuntime) SetParentEvidenceProjection(seelebridge.ParentEvidenceProjection)       {}
func (harnessRuntime) DrainSubagentContexts() []string                                        { return nil }
func (harnessRuntime) SetPlanPolicy(dto.PlanPolicy)                                           {}
func (harnessRuntime) PrepareReplan(context.Context, dto.ReplanRequest) (dto.PlanPreflight, error) {
	return dto.PlanPreflight{}, nil
}
func (harnessRuntime) ReplanMetrics() dto.ReplanMetrics            { return dto.ReplanMetrics{} }
func (harnessRuntime) SetPlanBranchBinding(dto.PlanBranchBinding)  {}
func (harnessRuntime) BindProjectRoot(string) error                { return nil }
func (harnessRuntime) UnbindProjectRoot()                          {}
func (harnessRuntime) TodoSnapshot() []dto.TodoItem                { return nil }
func (harnessRuntime) SetTodoStatus(int, dto.TodoItemStatus) error { return nil }
func (harnessRuntime) TaskSnapshot() []dto.TaskRecord              { return nil }
func (harnessRuntime) TaskAdd(dto.TaskSpec) (dto.TaskRecord, bool, error) {
	return dto.TaskRecord{}, false, nil
}
func (harnessRuntime) ResolveTaskByKey(string) (dto.TaskRecord, bool, error) {
	return dto.TaskRecord{}, false, nil
}
func (harnessRuntime) TaskSetStatus(string, dto.TaskStatus, string) (dto.TaskRecord, error) {
	return dto.TaskRecord{}, nil
}
func (harnessRuntime) TaskAttachParticipant(string, string) (dto.TaskRecord, error) {
	return dto.TaskRecord{}, nil
}
func (harnessRuntime) TaskChangedChannel() <-chan dto.TaskRecord { return nil }
func (harnessRuntime) SubagentTreeEvents() <-chan struct{}       { return nil }
func (harnessRuntime) PlanNodeEventChannel() <-chan dto.PlanNodeEvent {
	return nil
}
func (harnessRuntime) SwitchSessionTasks([]dto.TaskRecord) {}
func (harnessRuntime) ScheduledCommands() []seelebridge.ScheduledCommandInfo {
	return nil
}
func (harnessRuntime) ScheduledTasksSnapshot() []seelebridge.ScheduledTaskStatus { return nil }

// ScheduleTask 显式报错（诚实桩）：e2e 场景不使用定时任务，静默返回
// nil,nil 会让下游 NPE；fail-fast 暴露误用。
func (harnessRuntime) ScheduleTask(context.Context, seelebridge.ScheduledTaskSpec) (*seelebridge.ScheduledTaskStatus, error) {
	return nil, fmt.Errorf("harness: scheduled tasks are not supported in e2e scenarios")
}
func (harnessRuntime) CancelScheduledTask(string) error { return nil }
func (harnessRuntime) ClearSubagentTree() error         { return nil }

func (harnessRuntime) SearchHistory(context.Context, string, int) (seelexctxsearch.Result, error) {
	return seelexctxsearch.Result{}, nil
}

type harnessPlugins struct {
	mu      sync.Mutex
	current application.PluginInfo
}

func (plugins *harnessPlugins) All() []application.PluginInfo {
	plugins.mu.Lock()
	defer plugins.mu.Unlock()
	return []application.PluginInfo{plugins.current}
}

func (plugins *harnessPlugins) Activate(_ context.Context, name string) error {
	plugins.mu.Lock()
	plugins.current = application.PluginInfo{Name: name}
	plugins.mu.Unlock()
	return nil
}

func (plugins *harnessPlugins) Deactivate(context.Context) error {
	plugins.mu.Lock()
	plugins.current = application.PluginInfo{}
	plugins.mu.Unlock()
	return nil
}

func (plugins *harnessPlugins) Current() (application.PluginInfo, bool) {
	plugins.mu.Lock()
	defer plugins.mu.Unlock()
	return plugins.current, plugins.current.Name != ""
}

type harnessSkills struct{}

func (harnessSkills) All() []application.SkillInfo { return nil }
func (harnessSkills) Get(string) (application.SkillInfo, bool) {
	return application.SkillInfo{}, false
}

type harnessSessions struct {
	mu      sync.Mutex
	history map[string][]application.EngineMessage
}

func (sessions *harnessSessions) SaveCurrent(string) error { return nil }
func (*harnessSessions) SetWorkspace(string)               {}
func (*harnessSessions) Workspace() string                 { return "" }
func (sessions *harnessSessions) Delete(sessionID string) error {
	sessions.mu.Lock()
	defer sessions.mu.Unlock()
	if sessions.history != nil {
		delete(sessions.history, sessionID)
	}
	return nil
}
func (sessions *harnessSessions) List() []application.SessionInfo { return nil }
func (sessions *harnessSessions) LoadHistory(sessionID string) ([]application.EngineMessage, error) {
	sessions.mu.Lock()
	defer sessions.mu.Unlock()
	if sessions.history == nil {
		return nil, fmt.Errorf("session %q not found", sessionID)
	}
	history, ok := sessions.history[sessionID]
	if !ok {
		return nil, fmt.Errorf("session %q not found", sessionID)
	}
	return append([]application.EngineMessage(nil), history...), nil
}

func (sessions *harnessSessions) LoadHistoryRange(sessionID string, offset, limit int) ([]application.EngineMessage, int, error) {
	history, err := sessions.LoadHistory(sessionID)
	if err != nil {
		return nil, 0, err
	}
	if offset < 0 || offset > len(history) || limit < 0 {
		return nil, len(history), fmt.Errorf("invalid history range")
	}
	end := offset + limit
	if end > len(history) {
		end = len(history)
	}
	return append([]application.EngineMessage(nil), history[offset:end]...), len(history), nil
}

var _ ToolLifecycle = (*applicationToolLifecycle)(nil)
var _ application.RuntimePort = harnessRuntime{}
var _ application.PluginPort = (*harnessPlugins)(nil)
var _ application.SkillPort = harnessSkills{}
var _ application.SessionPort = (*harnessSessions)(nil)
