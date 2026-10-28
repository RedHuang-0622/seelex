package integration

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	assembly "github.com/RedHuang-0622/seelex/application/assembly"
	applicationevent "github.com/RedHuang-0622/seelex/application/event"
	"github.com/RedHuang-0622/seelex/application/prompt"
	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/contract/protocol"
	protocol_event "github.com/RedHuang-0622/seelex/middle/protocol/event"
)

func TestSnapshotNeverSerializesSystemPrompt(t *testing.T) {
	service := newLegacyTestService(t, &fakeEngine{})
	const privateInstruction = "private-system-instruction-must-not-reach-frontend"
	service.promptStack.Push("base", "private", privateInstruction)
	service.Deps.Engine.SetSystemPrompt(service.promptStack.Render())
	projection := service.components.view.CollectRuntimeProjection(context.Background())
	service.ViewMu.Lock()
	service.components.view.ApplyRuntimeProjectionLocked(projection)
	service.ViewMu.Unlock()

	payload, err := json.Marshal(service.components.frontendSnapshot.Snapshot())
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
	hub.Publish(assembly.EventMessageAdded, 1, "", nil)
	hub.Publish(assembly.EventMessageDelta, 2, "", nil)
	event := <-subscription.Events
	if event.Kind != assembly.EventResyncRequired || event.Seq != 2 {
		t.Fatalf("expected resync at seq 2, got %#v", event)
	}
	if event.ProtocolVersion != assembly.ProtocolVersion {
		t.Fatalf("protocol version = %d, want %d", event.ProtocolVersion, assembly.ProtocolVersion)
	}
}

func TestMessageDeltaIncludesStableMessageID(t *testing.T) {
	service := newLegacyTestService(t, &fakeEngine{})
	subscription := service.components.view.Subscribe(8)
	defer subscription.Close()

	service.ViewMu.Lock()
	service.Core.Snapshot.Chat = model.ChatState{Running: true, RequestID: "request-1"}
	message := service.components.view.AppendMessageLocked("assistant", "", nil)
	messageID := message.ID
	service.ViewMu.Unlock()

	service.components.conversationStreamRuntime.AppendDelta("request-1", "next")
	// 局部变量不能叫 `event`：会遮蔽 `middle/protocol/event` 包名（R10b 之后类型改成限定名）。
	var evt protocol_event.Event
	deadline := time.After(time.Second)
	for evt.Kind != assembly.EventMessageDelta {
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
	service := newLegacyTestService(t, &fakeEngine{})
	service.components.conversationTurn.projection.Start(context.Background(), "read", "read-1", `{"path":"a"}`)
	completeToolForTest(service, "read", "read-1", "ok", nil, time.Second)
	snapshot := service.components.frontendSnapshot.Snapshot()
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
	service := newLegacyTestService(t, &fakeEngine{}, withTestRuntime(runtime))

	service.ViewMu.Lock()
	taskState := service.components.tasks.BeginTask("task-goal", "plan work", "high", nil, model.TaskCheckpoint{})
	service.components.tasks.ActivateTaskSkillsLocked(taskState, []prompt.PromptLayer{{Kind: "skill", Name: "goal", Text: "goal prompt"}})
	service.ViewMu.Unlock()
	if !service.components.tasks.GoalSkillActive() {
		t.Fatal("goal skill state was not projected")
	}
	// 迁移前调用 service.publishRuntimeProjections()（未导出）。装配期与冷加载
	// 路径用的是同一个公开触发点：PublishInitialRuntimeProjections。
	assembly.PublishInitialRuntimeProjections(service.Service)

	service.components.conversationTurn.projection.Start(context.Background(), "bash", "bash-goal", `{"command":"echo ok"}`)
	done := make(chan struct{})
	go func() {
		completeToolForTest(service, "bash", "bash-goal", "ok", nil, time.Millisecond)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("tool completion deadlocked while refreshing goal-skill visibility")
	}
	if visible := service.components.frontendSnapshot.Snapshot().Runtime.VisibleTools; len(visible) != 1 || visible[0].Name != "plan_load" {
		t.Fatalf("visible tools = %#v, want goal-skill policy result", visible)
	}
}
