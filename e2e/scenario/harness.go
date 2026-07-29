package scenario

import (
	"context"
	"fmt"
	"sync"

	seeleengine "github.com/RedHuang-0622/Seele/engine"
	"github.com/RedHuang-0622/seelex/application"
	"github.com/RedHuang-0622/seelex/seelebridge"
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
	service := application.New(application.Dependencies{
		Engine: scriptedEngine, Runtime: harnessRuntime{plugin: plugin},
		Plugins: &harnessPlugins{current: application.PluginInfo{Name: plugin}},
		Skills:  harnessSkills{}, Sessions: &harnessSessions{},
		Events: events, Approval: approvals,
	})
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
	hooks *seeleengine.LoopHooks
}

func (lifecycle *applicationToolLifecycle) Started(ctx context.Context, execution ToolExecution) {
	if lifecycle.hooks == nil || lifecycle.hooks.OnToolStart == nil {
		return
	}
	lifecycle.hooks.OnToolStart(ctx, seeleengine.ToolCallInfo{
		Turn: execution.Turn, Name: execution.Name, Arguments: execution.Arguments,
	})
}

func (lifecycle *applicationToolLifecycle) Completed(ctx context.Context, execution ToolExecution) {
	if lifecycle.hooks == nil || lifecycle.hooks.OnToolComplete == nil {
		return
	}
	lifecycle.hooks.OnToolComplete(ctx, seeleengine.ToolCallInfo{
		Turn: execution.Turn, Name: execution.Name, Arguments: execution.Arguments,
		Result: execution.Result, Error: execution.Err, Duration: execution.Duration,
	})
}

type harnessRuntime struct {
	plugin string
}

func (harnessRuntime) Model() string                                   { return "scripted-e2e" }
func (harnessRuntime) Provider() string                                { return "local" }
func (harnessRuntime) Accounts() []application.AccountInfo             { return nil }
func (harnessRuntime) SelectAccount(string) bool                       { return false }
func (harnessRuntime) VisibleTools(context.Context) []application.Tool { return nil }
func (runtime harnessRuntime) ActivePlugin() string                    { return runtime.plugin }
func (harnessRuntime) SetFullAccess(bool)                              {}
func (harnessRuntime) SetPlanPolicy(seelebridge.PlanPolicy)            {}
func (harnessRuntime) PreparePlan(context.Context, string) (seelebridge.PlanPreflight, error) {
	return seelebridge.PlanPreflight{}, nil
}
func (harnessRuntime) PrepareReplan(context.Context, seelebridge.ReplanRequest) (seelebridge.PlanPreflight, error) {
	return seelebridge.PlanPreflight{}, nil
}
func (harnessRuntime) SetPlanBranchBinding(seelebridge.PlanBranchBinding) {}
func (harnessRuntime) BindProjectRoot(string) error                       { return nil }
func (harnessRuntime) UnbindProjectRoot()                                 {}

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
