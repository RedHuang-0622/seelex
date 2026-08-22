package core

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestSnapshotNeverSerializesSystemPrompt(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	const privateInstruction = "private-system-instruction-must-not-reach-frontend"
	service.promptStack.Push("base", "private", privateInstruction)
	service.Deps.Engine.SetSystemPrompt(service.promptStack.Render())
	projection := service.collectRuntimeProjection(context.Background())
	service.Mu.Lock()
	service.applyRuntimeProjectionLocked(projection)
	service.Mu.Unlock()

	payload, err := json.Marshal(service.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(payload), "prompt_stack") || strings.Contains(string(payload), privateInstruction) {
		t.Fatalf("snapshot leaked system prompt data: %s", payload)
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

	service.Mu.Lock()
	service.Core.Snapshot.Chat = ChatState{Running: true, RequestID: "request-1"}
	message := service.appendMessageLocked("assistant", "", nil)
	messageID := message.ID
	service.Mu.Unlock()

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

	service.Mu.Lock()
	taskState := service.components.tasks.BeginTask("task-goal", "plan work", "high", nil, TaskCheckpoint{})
	service.components.tasks.ActivateTaskSkillsLocked(taskState, []PromptLayer{{Kind: "skill", Name: "goal", Text: "goal prompt"}})
	service.Mu.Unlock()
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
