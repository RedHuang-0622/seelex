package core

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestTaskTerminalHandlerRecordsBoundedCompletion(t *testing.T) {
	service := newTestService(&fakeEngine{})
	defer service.Shutdown()
	service.mu.Lock()
	service.snapshot.Chat = ChatState{Running: true, RequestID: "task-1"}
	service.taskExecution = newTaskExecutionState("task-1", "write report", "high")
	service.mu.Unlock()

	result, err := service.TaskTerminalHandler(taskCompleteTool)(context.Background(), `{
		"summary":"report is ready",
		"artifacts":["report.md"],
		"evidence":["go test ./..."]
	}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, `"accepted"`) {
		t.Fatalf("terminal result = %q", result)
	}
	service.mu.RLock()
	state := service.taskExecution
	service.mu.RUnlock()
	if state.status != taskStatusCompleted || state.terminal == nil || state.terminal.Summary != "report is ready" {
		t.Fatalf("terminal state = %+v", state)
	}
}

func TestTaskFailedRequiresFailureType(t *testing.T) {
	service := newTestService(&fakeEngine{})
	defer service.Shutdown()
	service.mu.Lock()
	service.snapshot.Chat = ChatState{Running: true, RequestID: "task-1"}
	service.taskExecution = newTaskExecutionState("task-1", "verify", "high")
	service.mu.Unlock()

	if _, err := service.TaskTerminalHandler(taskFailedTool)(context.Background(), `{"summary":"blocked"}`); err == nil || !strings.Contains(err.Error(), "failure_type") {
		t.Fatalf("task_failed error = %v, want failure_type validation", err)
	}
}

func TestContextControllerCompactsAndCleansInternalCheckpoint(t *testing.T) {
	engine := &fakeEngine{history: []EngineMessage{
		{Role: "user", Content: "inspect project"},
		{Role: "assistant", Content: "I will inspect it."},
		{Role: "tool", Name: "read_file", Content: strings.Repeat("important source detail ", taskContextToolResultChars)},
	}}
	service := newTestService(engine)
	defer service.Shutdown()
	service.mu.Lock()
	service.snapshot.Chat = ChatState{Running: true, RequestID: "task-1"}
	service.taskExecution = newTaskExecutionState("task-1", "inspect project", "high")
	service.taskExecution.checkpoint("inspect", "inspect source", "completed", "found call path", "")
	service.mu.Unlock()

	if err := service.compactTaskContext("task-1"); err != nil {
		t.Fatal(err)
	}
	history := engine.History()
	if !strings.HasPrefix(history[len(history)-1].Content, taskContextCheckpointPrefix) {
		t.Fatalf("history does not end in checkpoint: %+v", history[len(history)-1])
	}
	if !strings.Contains(history[2].Content, "tool output compacted") {
		t.Fatalf("tool output was not compacted: %q", history[2].Content)
	}
	payload, err := json.Marshal(service.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(payload), taskContextCheckpointPrefix) {
		t.Fatalf("frontend snapshot leaked internal checkpoint: %s", payload)
	}
	if err := service.removeTaskContextCheckpoints(); err != nil {
		t.Fatal(err)
	}
	for _, message := range engine.History() {
		if isTaskContextCheckpoint(message.Content) {
			t.Fatalf("internal checkpoint leaked into retained history: %+v", message)
		}
	}
}

func TestNoProgressBudgetStopsRepeatedToolRounds(t *testing.T) {
	service := newTestService(&fakeEngine{})
	defer service.Shutdown()
	service.mu.Lock()
	service.snapshot.Chat = ChatState{Running: true, RequestID: "task-1"}
	service.taskExecution = newTaskExecutionState("task-1", "inspect", "high")
	service.startReActBudgetLocked("task-1", ReActBudget{MaxNoProgressRounds: 2})
	service.mu.Unlock()

	bridge := NewToolHookBridge()
	bridge.Bind(service)
	hooks := bridge.Hooks()
	if !hooks.OnIterationComplete(context.Background(), 0) {
		t.Fatal("first no-progress round should remain available")
	}
	if hooks.OnIterationComplete(context.Background(), 1) {
		t.Fatal("second no-progress round should stop the loop")
	}
	if err := service.reactBudgetError("task-1"); err == nil || !strings.Contains(err.Error(), "no observable progress") {
		t.Fatalf("budget error = %v", err)
	}
}
