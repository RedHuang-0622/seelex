package gui

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	frameworksession "github.com/RedHuang-0622/Seele/session"
	"github.com/RedHuang-0622/Seele/types"
	"github.com/RedHuang-0622/seelex/application"
	"github.com/RedHuang-0622/seelex/application/contract/dto"
	"github.com/RedHuang-0622/seelex/internal/testutil"
	"github.com/RedHuang-0622/seelex/seelebridge"
	seelexctxsearch "github.com/RedHuang-0622/seelex/seelexctx/search"
)

type guiChainEngine struct {
	*testutil.EmbeddedChatEngine
	hooks     *frameworksession.LoopHooks
	mu        sync.Mutex
	history   []application.EngineMessage
	sessionID string
}

func (engine *guiChainEngine) ChatStream(ctx context.Context, input string, onChunk func(string)) (string, error) {
	engine.mu.Lock()
	engine.history = append(engine.history, application.EngineMessage{Role: "user", Content: input, ContentSet: true})
	engine.mu.Unlock()
	if onChunk != nil {
		onChunk("preparing")
	}
	info := frameworksession.ToolCallInfo{Turn: 0, Name: "todolist_init", Arguments: `{"items":["inspect"]}`}
	engine.hooks.OnToolStart(ctx, info)
	info.Result = `{"done":0,"items":[{"done":false,"text":"inspect"}],"total":1}`
	info.Duration = time.Millisecond
	engine.hooks.OnToolComplete(ctx, info)
	engine.mu.Lock()
	engine.history = append(engine.history,
		application.EngineMessage{Role: "assistant", ToolCalls: []application.EngineToolCall{{ID: "call-todo-1", Name: info.Name, Arguments: info.Arguments}}},
		application.EngineMessage{Role: "tool", ToolCallID: "call-todo-1", Name: info.Name, Content: info.Result, ContentSet: true},
		application.EngineMessage{Role: "assistant", Content: "done", ContentSet: true},
	)
	engine.mu.Unlock()
	return "done", nil
}

func (engine *guiChainEngine) History() []application.EngineMessage {
	engine.mu.Lock()
	defer engine.mu.Unlock()
	return append([]application.EngineMessage(nil), engine.history...)
}
func (engine *guiChainEngine) ClearHistory() {
	engine.mu.Lock()
	engine.history = nil
	engine.mu.Unlock()
}
func (*guiChainEngine) ReplaceHistory(string, []application.EngineMessage) error { return nil }
func (engine *guiChainEngine) SessionID() string                                 { return engine.sessionID }
func (engine *guiChainEngine) StartSession() string {
	engine.sessionID = "gui-chain-session"
	return engine.sessionID
}
func (*guiChainEngine) SetSystemPrompt(string)               {}
func (*guiChainEngine) SetMaxLoops(int)                      {}
func (*guiChainEngine) TraceText() string                    { return "" }
func (*guiChainEngine) TokenCount() string                   { return "0" }
func (*guiChainEngine) AppendHistory(types.Message)          {}
func (*guiChainEngine) SubAgentTree() []dto.SubAgentTreeNode { return nil }

type guiChainRuntime struct {
	mu         sync.Mutex
	fullAccess bool
}

func (*guiChainRuntime) Model() string                       { return "test-model" }
func (*guiChainRuntime) Provider() string                    { return "test-provider" }
func (*guiChainRuntime) Accounts() []application.AccountInfo { return nil }
func (*guiChainRuntime) SelectAccount(string) bool           { return true }
func (*guiChainRuntime) VisibleTools(context.Context) []application.Tool {
	return []application.Tool{{Name: "todolist_init"}}
}
func (*guiChainRuntime) ActivePlugin() string { return "default" }
func (runtime *guiChainRuntime) FullAccess() bool {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	return runtime.fullAccess
}
func (runtime *guiChainRuntime) SetFullAccess(on bool) {
	runtime.mu.Lock()
	runtime.fullAccess = on
	runtime.mu.Unlock()
}
func (*guiChainRuntime) SetRuntimeVisibilityProjection(seelebridge.RuntimeVisibilityProjection) {}
func (*guiChainRuntime) SetParentEvidenceProjection(seelebridge.ParentEvidenceProjection)       {}
func (*guiChainRuntime) DrainSubagentContexts() []string                                        { return nil }
func (*guiChainRuntime) SetPlanPolicy(dto.PlanPolicy)                                           {}
func (*guiChainRuntime) PrepareReplan(context.Context, dto.ReplanRequest) (dto.PlanPreflight, error) {
	return dto.PlanPreflight{}, nil
}
func (*guiChainRuntime) ReplanMetrics() dto.ReplanMetrics            { return dto.ReplanMetrics{} }
func (*guiChainRuntime) SetPlanBranchBinding(dto.PlanBranchBinding)  {}
func (*guiChainRuntime) BindProjectRoot(string) error                { return nil }
func (*guiChainRuntime) UnbindProjectRoot()                          {}
func (*guiChainRuntime) TodoSnapshot() []dto.TodoItem                { return nil }
func (*guiChainRuntime) SetTodoStatus(int, dto.TodoItemStatus) error { return nil }
func (*guiChainRuntime) TaskSnapshot() []dto.TaskRecord              { return nil }
func (*guiChainRuntime) TaskAdd(dto.TaskSpec) (dto.TaskRecord, bool, error) {
	return dto.TaskRecord{}, false, nil
}
func (*guiChainRuntime) ResolveTaskByKey(string) (dto.TaskRecord, bool, error) {
	return dto.TaskRecord{}, false, nil
}
func (*guiChainRuntime) TaskSetStatus(string, dto.TaskStatus, string) (dto.TaskRecord, error) {
	return dto.TaskRecord{}, nil
}
func (*guiChainRuntime) TaskAttachParticipant(string, string) (dto.TaskRecord, error) {
	return dto.TaskRecord{}, nil
}
func (*guiChainRuntime) TaskChangedChannel() <-chan dto.TaskRecord { return nil }
func (*guiChainRuntime) SubagentTreeEvents() <-chan struct{}       { return nil }
func (*guiChainRuntime) PlanNodeEventChannel() <-chan dto.PlanNodeEvent {
	return nil
}
func (*guiChainRuntime) SwitchSessionTasks([]dto.TaskRecord) {}
func (*guiChainRuntime) ScheduledCommands() []seelebridge.ScheduledCommandInfo {
	return nil
}
func (*guiChainRuntime) ScheduledTasksSnapshot() []seelebridge.ScheduledTaskStatus {
	return nil
}
func (*guiChainRuntime) ScheduleTask(context.Context, seelebridge.ScheduledTaskSpec) (*seelebridge.ScheduledTaskStatus, error) {
	return nil, nil
}
func (*guiChainRuntime) CancelScheduledTask(string) error { return nil }
func (*guiChainRuntime) ClearSubagentTree() error         { return nil }

func (*guiChainRuntime) SearchHistory(context.Context, string, int) (seelexctxsearch.Result, error) {
	return seelexctxsearch.Result{}, nil
}

type guiChainPlugins struct{}

func (*guiChainPlugins) All() []application.PluginInfo          { return nil }
func (*guiChainPlugins) Activate(context.Context, string) error { return nil }
func (*guiChainPlugins) Deactivate(context.Context) error       { return nil }
func (*guiChainPlugins) Current() (application.PluginInfo, bool) {
	return application.PluginInfo{Name: "default"}, true
}

type guiChainSkills struct{}

func (*guiChainSkills) All() []application.SkillInfo { return nil }
func (*guiChainSkills) Get(string) (application.SkillInfo, bool) {
	return application.SkillInfo{}, false
}

type guiChainSessions struct{}

func (*guiChainSessions) SaveCurrent(string) error                                { return nil }
func (*guiChainSessions) Delete(string) error                                     { return nil }
func (*guiChainSessions) List() []application.SessionInfo                         { return nil }
func (*guiChainSessions) LoadHistory(string) ([]application.EngineMessage, error) { return nil, nil }
func (*guiChainSessions) LoadHistoryRange(string, int, int) ([]application.EngineMessage, int, error) {
	return nil, 0, nil
}
func (*guiChainSessions) SetWorkspace(string) {}
func (*guiChainSessions) Workspace() string   { return "" }

func newGUIChainApplication(t *testing.T) (*application.Service, *application.ApprovalBroker) {
	t.Helper()
	events := application.NewEventHub()
	approval := application.NewApprovalBroker(events)
	hooks := application.NewToolHookBridge()
	engine := &guiChainEngine{hooks: hooks.Hooks()}
	app, err := application.New(application.Dependencies{
		Engine: engine, Runtime: &guiChainRuntime{}, Plugins: &guiChainPlugins{},
		Skills: &guiChainSkills{}, Sessions: &guiChainSessions{}, Events: events, Approval: approval,
	})
	if err != nil {
		t.Fatal(err)
	}
	hooks.Bind(app)
	t.Cleanup(app.Shutdown)
	return app, approval
}

func TestGUIBackendPortRelaysToolCompletion(t *testing.T) {
	app, _ := newGUIChainApplication(t)
	bridge, err := NewBridge(app, Options{})
	if err != nil {
		t.Fatal(err)
	}
	emitted := make(chan emittedEvent, 16)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	bridge.Start(ctx, func(_ context.Context, name string, payload any) {
		emitted <- emittedEvent{name: name, payload: payload}
	})
	t.Cleanup(bridge.Stop)
	if ready := waitEmitted(t, emitted); ready.name != "seelex:ready" {
		t.Fatalf("first GUI event = %q, want seelex:ready", ready.name)
	}
	if err := bridge.Submit("inspect the project"); err != nil {
		t.Fatal(err)
	}

	var completed application.Message
	deadline := time.After(2 * time.Second)
	for completed.Tool == nil {
		select {
		case item := <-emitted:
			if item.name != eventName {
				continue
			}
			event, ok := item.payload.(application.Event)
			if !ok || event.Kind != application.EventToolCompleted {
				continue
			}
			if err := json.Unmarshal(event.Payload, &completed); err != nil {
				t.Fatal(err)
			}
		case <-deadline:
			t.Fatal("GUI backend port did not relay tool.completed")
		}
	}
	if completed.Tool.Name != "todolist_init" || completed.Tool.Status != "success" {
		t.Fatalf("GUI tool completion = %#v", completed.Tool)
	}
	idleContext, idleCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer idleCancel()
	if err := app.WaitForIdle(idleContext); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, message := range bridge.Snapshot().Conversation {
		if message.Tool != nil && message.Tool.Name == "todolist_init" && message.Tool.Status == "success" {
			found = true
		}
	}
	if !found {
		t.Fatal("Bridge.Snapshot did not retain completed tool state")
	}
}

func TestGUIBackendFullAccessReleasesPendingApproval(t *testing.T) {
	app, approval := newGUIChainApplication(t)
	bridge, err := NewBridge(app, Options{})
	if err != nil {
		t.Fatal(err)
	}
	emitted := make(chan emittedEvent, 16)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	bridge.Start(ctx, func(_ context.Context, name string, payload any) {
		emitted <- emittedEvent{name: name, payload: payload}
	})
	t.Cleanup(bridge.Stop)
	if ready := waitEmitted(t, emitted); ready.name != "seelex:ready" {
		t.Fatalf("first GUI event = %q, want seelex:ready", ready.name)
	}
	decision := make(chan application.ApprovalDecision, 1)
	go func() {
		value, requestErr := approval.Request(context.Background(), application.ApprovalRequest{
			ID: "approval-gui", Question: "run tool?",
			Options: []application.InteractionOption{{ID: "allow", Label: "Allow"}},
		})
		if requestErr == nil {
			decision <- value
		}
	}()
	deadline := time.Now().Add(time.Second)
	for bridge.Snapshot().Interaction == nil && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if bridge.Snapshot().Interaction == nil {
		t.Fatal("pending approval did not reach Bridge.Snapshot")
	}
	bridge.SetFullAccess(true)
	select {
	case value := <-decision:
		if value.OptionID != "always" {
			t.Fatalf("full access decision = %q, want always", value.OptionID)
		}
	case <-time.After(time.Second):
		t.Fatal("Bridge.SetFullAccess did not release pending approval")
	}
	autoDecision, autoErr := approval.Request(context.Background(), application.ApprovalRequest{
		ID: "approval-after-full-access", PermissionRequest: true,
	})
	if autoErr != nil || autoDecision.OptionID != "always" {
		t.Fatalf("post-toggle permission decision = %#v, err=%v", autoDecision, autoErr)
	}
	foundRuntimeChanged := false
	eventDeadline := time.After(time.Second)
	for !foundRuntimeChanged {
		select {
		case item := <-emitted:
			if item.name != eventName {
				continue
			}
			event, ok := item.payload.(application.Event)
			if !ok || event.Kind != application.EventRuntimeChanged {
				continue
			}
			var runtime application.RuntimeState
			if err := json.Unmarshal(event.Payload, &runtime); err != nil {
				t.Fatal(err)
			}
			foundRuntimeChanged = runtime.FullAccess
		case <-eventDeadline:
			t.Fatal("Bridge.SetFullAccess did not relay runtime.changed to seelex:event")
		}
	}
	snapshot := bridge.Snapshot()
	if !snapshot.Runtime.FullAccess || snapshot.Interaction != nil {
		t.Fatalf("GUI full access snapshot = full_access:%v interaction:%#v", snapshot.Runtime.FullAccess, snapshot.Interaction)
	}
}
