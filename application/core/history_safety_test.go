package core

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestRepairEmptyHistoryContentRepairsToolCallAssistantContent(t *testing.T) {
	history := []EngineMessage{
		{Role: "assistant", ToolCalls: []EngineToolCall{{ID: "call-1", Name: "read_file", Arguments: `{"path":"a.go"}`}}},
		{Role: "tool", ToolCallID: "call-1", Name: "read_file", Content: ""},
		{Role: "assistant", Content: ""},
	}
	prepared, repaired := repairEmptyHistoryContent(history)
	if !repaired {
		t.Fatal("expected empty non-protocol messages to be repaired")
	}
	if prepared[0].Content != toolCallHistoryContent || !prepared[0].ContentSet {
		t.Fatalf("tool-call assistant was not repaired: %+v", prepared[0])
	}
	if len(prepared[0].ToolCalls) != 1 || prepared[0].ToolCalls[0].ID != "call-1" {
		t.Fatalf("tool-call assistant lost its protocol data: %+v", prepared[0])
	}
	for _, index := range []int{1, 2} {
		if prepared[index].Content != missingHistoryContent || !prepared[index].ContentSet {
			t.Fatalf("message %d was not repaired: %+v", index, prepared[index])
		}
	}
}

func TestNonEmptyProviderInputExplainsEmptySubmission(t *testing.T) {
	if got := nonEmptyProviderInput("  \n"); got == "" {
		t.Fatal("empty input must be converted to a recovery explanation")
	}
	if got := nonEmptyProviderInput("hello"); got != "hello" {
		t.Fatalf("non-empty input changed to %q", got)
	}
}

func TestPrepareProviderHistoryRepairsBeforeChat(t *testing.T) {
	engine := &fakeEngine{history: []EngineMessage{{Role: "assistant", Content: ""}}}
	service := newTestService(engine)
	defer service.Shutdown()
	if err := service.prepareProviderHistory(); err != nil {
		t.Fatal(err)
	}
	history := engine.History()
	if len(history) != 1 || history[0].Content != missingHistoryContent || !history[0].ContentSet {
		t.Fatalf("prepared history = %+v", history)
	}
}

func TestRecoverProviderContextReplacesRejectedTranscriptWithPrivateCheckpoint(t *testing.T) {
	engine := &fakeEngine{history: []EngineMessage{
		{Role: "system", Content: "private system instruction", ContentSet: true},
		{Role: "user", Content: "original request", ContentSet: true},
		{Role: "assistant", ToolCalls: []EngineToolCall{{ID: "call-1", Name: "bash", Arguments: `{"command":"Get-ChildItem"}`}}},
		{Role: "tool", ToolCallID: "call-1", Name: "bash", Content: "raw command output that must not survive", ContentSet: true},
	}}
	service := newTestService(engine)
	defer service.Shutdown()
	service.mu.Lock()
	service.taskExecution = newTaskExecutionState("task-1", "audit the repository", "high")
	service.taskExecution.checkpoint("inspect", "inspect source", "completed", "found the call path", "")
	service.mu.Unlock()

	err := errors.New("engine loop 15: invalid params, context window exceeds limit (2013)")
	if err := service.recoverProviderContext(err, "audit the repository"); err != nil {
		t.Fatal(err)
	}
	history := engine.History()
	if len(history) != 2 || history[0].Role != "system" || history[1].Role != "user" {
		t.Fatalf("recovered history = %#v", history)
	}
	if !strings.HasPrefix(history[1].Content, contextRecoveryPrefix) || !strings.Contains(history[1].Content, "node=inspect status=completed") {
		t.Fatalf("missing recovery checkpoint: %q", history[1].Content)
	}
	if strings.Contains(history[1].Content, "raw command output") || len(history[1].ToolCalls) != 0 {
		t.Fatalf("raw transcript survived recovery: %#v", history[1])
	}
	if err := service.removeProviderContextRecovery(); err != nil {
		t.Fatal(err)
	}
	history = engine.History()
	if len(history) != 2 || history[1].Content != "audit the repository" || strings.HasPrefix(history[1].Content, contextRecoveryPrefix) {
		t.Fatalf("recovery was not restored before persistence: %#v", history)
	}
}

func TestRecoverProviderContextIgnoresOtherProviderErrors(t *testing.T) {
	engine := &fakeEngine{history: []EngineMessage{{Role: "user", Content: "keep me", ContentSet: true}}}
	service := newTestService(engine)
	defer service.Shutdown()
	if err := service.recoverProviderContext(errors.New("HTTP 504 upstream timeout"), "new request"); err != nil {
		t.Fatal(err)
	}
	if history := engine.History(); len(history) != 1 || history[0].Content != "keep me" {
		t.Fatalf("non-context error rewrote history: %#v", history)
	}
}

func TestRecoverProviderTimeoutCreatesPrivateResumeCheckpoint(t *testing.T) {
	engine := &fakeEngine{history: []EngineMessage{
		{Role: "system", Content: "private system instruction", ContentSet: true},
		{Role: "user", Content: "audit source", ContentSet: true},
		{Role: "tool", Name: "bash", Content: "raw output that must not survive", ContentSet: true},
	}}
	service := newTestService(engine)
	defer service.Shutdown()
	service.mu.Lock()
	service.snapshot.Chat = ChatState{Running: true, RequestID: "task-1"}
	service.taskExecution = newTaskExecutionState("task-1", "audit source", "high")
	service.taskExecution.checkpoint("inspect", "source", "completed", "found a call path", "")
	service.mu.Unlock()

	recovered, err := service.recoverProviderFailure(errors.New("engine loop 16: ChatClient stream: HTTP 504: timeout_error"), "audit source")
	if err != nil || !recovered {
		t.Fatalf("recover timeout = %v, %v", recovered, err)
	}
	history := engine.History()
	if len(history) != 2 || !strings.HasPrefix(history[1].Content, providerRecoveryPrefix) {
		t.Fatalf("timeout recovery history = %#v", history)
	}
	if strings.Contains(history[1].Content, "raw output that must not survive") || !strings.Contains(history[1].Content, "node=inspect status=completed") {
		t.Fatalf("timeout recovery checkpoint = %q", history[1].Content)
	}
	if state := service.Snapshot().Task; state == nil || state.Status != TaskInterrupted {
		t.Fatalf("task state = %#v, want interrupted", state)
	}
}

func TestContextExhaustionLeavesNextTurnWithRecoverableHistory(t *testing.T) {
	engine := &fakeEngine{
		appendChatHistory: true,
		chatErr:           errors.New("engine loop 15: context window exceeds limit"),
	}
	service := newTestService(engine)
	defer service.Shutdown()
	if err := service.Submit(context.Background(), "finish the repository audit"); err != nil {
		t.Fatal(err)
	}
	waitForChatCompletion(t, service)

	history := engine.History()
	if len(history) == 0 || !strings.HasPrefix(history[len(history)-1].Content, contextRecoveryPrefix) {
		t.Fatalf("context failure left no recovery history: %#v", history)
	}
	if service.Snapshot().Chat.Error == "" {
		t.Fatal("provider failure must remain visible for the failed turn")
	}
}

func TestNonRecoverableProviderFailureMarksTaskFailed(t *testing.T) {
	engine := &fakeEngine{chatErr: errors.New("HTTP 401 provider rejected request")}
	service := newTestService(engine)
	defer service.Shutdown()
	if err := service.Submit(context.Background(), "inspect repository"); err != nil {
		t.Fatal(err)
	}
	waitForChatCompletion(t, service)
	if task := service.Snapshot().Task; task == nil || task.Status != TaskFailed {
		t.Fatalf("task state = %#v, want failed", task)
	}
}
