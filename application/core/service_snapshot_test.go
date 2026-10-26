package core

import (
	"context"
	"encoding/json"
	applicationevent "github.com/RedHuang-0622/seelex/application/event"
	applicationobservability "github.com/RedHuang-0622/seelex/application/observability"
	"github.com/RedHuang-0622/seelex/application/prompt"
	"github.com/RedHuang-0622/seelex/contract/dto"
	protocol_event "github.com/RedHuang-0622/seelex/middle/protocol/event"
	"github.com/RedHuang-0622/seelex/middle/protocol/model"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestPerfStatsApplicationIsAssembled(t *testing.T) {
	components := reflect.TypeOf(serviceComponents{})
	field, ok := components.FieldByName("perfStatsApplication")
	if !ok {
		t.Fatal("serviceComponents 未装配 perfStatsApplication")
	}
	if field.Type != reflect.TypeOf((*applicationobservability.PerfApplication)(nil)) {
		t.Fatalf("perfStatsApplication 字段类型 = %s", field.Type)
	}
}

func TestPerfStatsFacadeOnlyDelegatesToApplication(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("无法定位测试源码")
	}
	path := filepath.Join(filepath.Dir(file), "..", "compat", "frontend.go")
	parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, declaration := range parsed.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Recv == nil || function.Name == nil || function.Name.Name != "PerfStats" ||
			!functionHasReceiver(function, "Frontend") {
			continue
		}
		if len(function.Body.List) != 1 {
			t.Fatalf("PerfStats 有 %d 条语句，want 一行 perfStatsApplication 转发", len(function.Body.List))
		}
		if _, ok := function.Body.List[0].(*ast.ReturnStmt); !ok {
			t.Fatal("PerfStats 不是单行 return 转发")
		}
		return
	}
	t.Fatal("未找到 PerfStats")
}

func TestSnapshotNeverSerializesSystemPrompt(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	const privateInstruction = "private-system-instruction-must-not-reach-frontend"
	service.promptStack.Push("base", "private", privateInstruction)
	service.Deps.Engine.SetSystemPrompt(service.promptStack.Render())
	projection := service.components.view.CollectRuntimeProjection(context.Background())
	service.ViewMu.Lock()
	service.components.view.ApplyRuntimeProjectionLocked(projection)
	service.ViewMu.Unlock()

	payload, err := json.Marshal(service.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(payload), "prompt_stack") || strings.Contains(string(payload), privateInstruction) {
		t.Fatalf("snapshot leaked system prompt data: %s", payload)
	}
}

func TestEventHubOrdersAndResyncs(t *testing.T) {
	hub := applicationevent.NewEventHub()
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
	defer frontendOfTestService(service).Shutdown()
	subscription := frontendOfTestService(service).Subscribe(8)
	defer subscription.Close()

	service.ViewMu.Lock()
	service.Core.Snapshot.Chat = model.ChatState{Running: true, RequestID: "request-1"}
	message := service.components.view.AppendMessageLocked("assistant", "", nil)
	messageID := message.ID
	service.ViewMu.Unlock()

	service.appendDelta("request-1", "next")
	// 局部变量不能叫 `event`：会遮蔽 `middle/protocol/event` 包名（R10b 之后类型改成限定名）。
	var evt protocol_event.Event
	deadline := time.After(time.Second)
	for evt.Kind != EventMessageDelta {
		select {
		case evt = <-subscription.Events:
		case <-deadline:
			t.Fatal("did not receive message.delta event")
		}
	}
	var delta protocol_event.MessageDelta
	if err := json.Unmarshal(evt.Payload, &delta); err != nil {
		t.Fatal(err)
	}
	if delta.MessageID != messageID || delta.Delta != "next" {
		t.Fatalf("unexpected delta payload: %+v", delta)
	}
}

func TestToolEventsUpdateSnapshot(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	defer frontendOfTestService(service).Shutdown()
	service.handleToolStart(context.Background(), "read", "read-1", `{"path":"a"}`)
	service.handleToolComplete("read", "read-1", "ok", nil, time.Second)
	snapshot := service.Snapshot()
	found := false
	for _, message := range snapshot.Conversation {
		if message.Tool != nil && message.Tool.ID == "read-1" && message.Tool.Status == dto.ToolEventSuccess {
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

	service.ViewMu.Lock()
	taskState := service.components.tasks.BeginTask("task-goal", "plan work", "high", nil, model.TaskCheckpoint{})
	service.components.tasks.ActivateTaskSkillsLocked(taskState, []prompt.PromptLayer{{Kind: "skill", Name: "goal", Text: "goal prompt"}})
	service.ViewMu.Unlock()
	if !frontendOfTestService(service).GoalSkillActive() {
		t.Fatal("goal skill state was not projected")
	}
	service.publishRuntimeProjections()

	service.handleToolStart(context.Background(), "bash", "bash-goal", `{"command":"echo ok"}`)
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
