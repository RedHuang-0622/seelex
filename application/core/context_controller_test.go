package core

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestRejectToolResultsPreservesPairingWithoutPreview(t *testing.T) {
	const rawOutput = "secret source detail"
	history := []EngineMessage{
		{Role: "assistant", ToolCalls: []EngineToolCall{{ID: "call-1", Name: "read_file", Arguments: `{"path":"source.txt"}`}}},
		{Role: "tool", ToolCallID: "call-1", Name: "read_file", Content: strings.Repeat(rawOutput, 300), ContentSet: true},
	}

	filtered, changed := rejectToolResults(history, 200)
	if !changed {
		t.Fatal("expected oversized tool result to be rejected")
	}
	if got := filtered[1]; got.ToolCallID != "call-1" || got.Name != "read_file" ||
		!strings.HasPrefix(got.Content, toolResultOmittedPrefix) || strings.Contains(got.Content, rawOutput) {
		t.Fatalf("filtered tool result = %#v", got)
	}
	if got := filtered[0].ToolCalls[0]; got.ID != "call-1" || got.Arguments != `{"path":"source.txt"}` {
		t.Fatalf("tool call protocol changed: %#v", got)
	}
}

func TestPrepareExecutionContextCountsActiveSystemPrompt(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	defer service.Shutdown()
	budget := defaultContextBudget()
	service.promptStack.Push("base", "oversized-system", strings.Repeat("s", budget.Budget*3))
	service.mu.Lock()
	service.snapshot.Chat = ChatState{Running: true, RequestID: "task-1"}
	service.taskExecution = newTaskExecutionState("task-1", "inspect", "high")
	service.mu.Unlock()

	if _, err := service.components.context.prepareExecutionContext("task-1", "continue"); !errors.Is(err, errProviderContextBudgetExceeded) {
		t.Fatalf("prepare error = %v, want provider budget exceeded", err)
	}
}

func TestPrepareExecutionContextUsesRuntimeContextLimits(t *testing.T) {
	runtime := runtimeWithContextLimits{
		fakeRuntime: &fakeRuntime{}, window: 200_000, output: 8_192,
	}
	service := newTestService(t, &fakeEngine{}, withTestRuntime(runtime))
	legacyBudget := defaultContextBudget()
	service.promptStack.Push("base", "large-system", strings.Repeat("s", legacyBudget.Budget*3))
	service.mu.Lock()
	service.snapshot.Chat = ChatState{Running: true, RequestID: "task-1"}
	service.taskExecution = newTaskExecutionState("task-1", "inspect", "high")
	service.mu.Unlock()

	if _, err := service.components.context.prepareExecutionContext("task-1", "continue"); err != nil {
		t.Fatalf("prepare with configured context window: %v", err)
	}
	service.mu.RLock()
	audit := service.taskExecution.tokenAudit
	service.mu.RUnlock()
	if audit.Budget != 166_808 {
		t.Fatalf("token audit budget = %d, want 166808", audit.Budget)
	}
}

func TestPreparedRequestNeverExceedsSafeBudget(t *testing.T) {
	engine := &fakeEngine{}
	service := newTestService(t, engine)
	defer service.Shutdown()
	service.mu.Lock()
	service.snapshot.Chat = ChatState{Running: true, RequestID: "task-1"}
	service.taskExecution = newTaskExecutionState("task-1", "inspect", "high")
	for round := 0; round < 8; round++ {
		callID := "call-" + string(rune('a'+round))
		service.components.tasks.appendTranscriptEventLocked(TranscriptEvent{TaskID: "old-task", Role: "user", Content: strings.Repeat("request ", 2500)})
		service.components.tasks.appendTranscriptEventLocked(TranscriptEvent{TaskID: "old-task", Role: "assistant", ToolCalls: []TranscriptToolCall{{ID: callID, Name: "read"}}})
		service.components.tasks.appendTranscriptEventLocked(TranscriptEvent{TaskID: "old-task", Role: "tool", ToolCallID: callID, Name: "read", Content: strings.Repeat("result ", 2500)})
		service.components.tasks.appendTranscriptEventLocked(TranscriptEvent{TaskID: "old-task", Role: "assistant", Content: "round complete"})
	}
	service.mu.Unlock()

	preparedInput, err := service.components.context.prepareExecutionContext("task-1", "continue with verification")
	if err != nil {
		t.Fatal(err)
	}
	service.mu.RLock()
	systemPrompt := service.components.prompts.systemPromptForActiveTaskLocked()
	service.mu.RUnlock()
	tools := service.deps.Runtime.VisibleTools(t.Context())
	estimated := service.tokenCounter.CountRequest(systemPrompt, engine.History(), preparedInput, tools)
	if estimated > defaultContextBudget().Budget {
		t.Fatalf("final request tokens = %d, budget = %d", estimated, defaultContextBudget().Budget)
	}
}

func TestTranscriptTailDropsIncompleteAndOrphanToolProtocols(t *testing.T) {
	events := []TranscriptEvent{
		{Seq: 1, Role: "user", Content: "incomplete", TokenCount: 1},
		{Seq: 2, Role: "assistant", ToolCalls: []TranscriptToolCall{{ID: "a"}, {ID: "b"}}, TokenCount: 1},
		{Seq: 3, Role: "tool", ToolCallID: "a", TokenCount: 1},
		{Seq: 4, Role: "user", Content: "complete", TokenCount: 1},
		{Seq: 5, Role: "assistant", ToolCalls: []TranscriptToolCall{{ID: "c"}, {ID: "d"}}, TokenCount: 1},
		{Seq: 6, Role: "tool", ToolCallID: "d", TokenCount: 1},
		{Seq: 7, Role: "tool", ToolCallID: "c", TokenCount: 1},
		{Seq: 8, Role: "assistant", Content: "finished", TokenCount: 1},
		{Seq: 9, Role: "tool", ToolCallID: "orphan", TokenCount: 1},
	}
	history := transcriptTailHistory(events, 100, 4)
	gotSeq := make([]string, len(history))
	for index, message := range history {
		gotSeq[index] = message.Role + ":" + message.ToolCallID
	}
	want := []string{"user:", "assistant:", "tool:d", "tool:c", "assistant:"}
	if !reflect.DeepEqual(gotSeq, want) {
		t.Fatalf("history protocol = %v, want %v", gotSeq, want)
	}
}

func TestTranscriptTailKeepsTrailingUnansweredUserInput(t *testing.T) {
	events := []TranscriptEvent{
		{Seq: 1, Role: "user", Content: "first", TokenCount: 1},
		{Seq: 2, Role: "assistant", Content: "answer", TokenCount: 1},
		{Seq: 3, Role: "user", Content: "please continue from the report", TokenCount: 1},
	}
	history := transcriptTailHistory(events, 100, 2)
	got := make([]string, len(history))
	for index, message := range history {
		got[index] = message.Content
	}
	if want := []string{"first", "answer", "please continue from the report"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("tail content=%v, want %v", got, want)
	}
}

func TestRejectToolResultsRecognizesFrameworkTruncationMarker(t *testing.T) {
	history := []EngineMessage{{
		Role: "tool", ToolCallID: "call-1", Name: "bash",
		Content: strings.Repeat("x", 4000) + frameworkToolOutputTruncatedMarker,
	}}
	filtered, changed := rejectToolResults(history, 4000)
	if !changed || !strings.HasPrefix(filtered[0].Content, toolResultOmittedPrefix) {
		t.Fatalf("framework-truncated result = %#v, changed=%v", filtered, changed)
	}
}
