package core

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/RedHuang-0622/seelex/application/core/context_runtime"
	"github.com/RedHuang-0622/seelex/application/core/task_context"
	"strings"
	"testing"
	"time"
)

func TestTaskTerminalHandlerRecordsBoundedCompletion(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	defer service.Shutdown()
	service.Mu.Lock()
	service.Core.Snapshot.Chat = ChatState{Running: true, RequestID: "task-1"}
	service.components.tasks.BeginTask("task-1", "write report", "high", nil, TaskCheckpoint{})
	service.Mu.Unlock()

	result, err := service.TaskTerminalHandler(task_context.ToolComplete)(context.Background(), `{
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
	service.Mu.RLock()
	state := service.components.tasks.CurrentTaskExecution()
	service.Mu.RUnlock()
	if state.Status != task_context.StatusCompleted || state.Terminal == nil || state.Terminal.Summary != "report is ready" {
		t.Fatalf("terminal state = %+v", state)
	}
}

func TestTaskFailedRequiresFailureType(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	defer service.Shutdown()
	service.Mu.Lock()
	service.Core.Snapshot.Chat = ChatState{Running: true, RequestID: "task-1"}
	service.components.tasks.BeginTask("task-1", "verify", "high", nil, TaskCheckpoint{})
	service.Mu.Unlock()

	if _, err := service.TaskTerminalHandler(task_context.ToolFailed)(context.Background(), `{"summary":"blocked"}`); err == nil || !strings.Contains(err.Error(), "failure_type") {
		t.Fatalf("task_failed error = %v, want failure_type validation", err)
	}
}

func TestTaskNeedsUserDecisionRecordsDistinctTerminalState(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	defer service.Shutdown()
	service.Mu.Lock()
	service.Core.Snapshot.Chat = ChatState{Running: true, RequestID: "task-1"}
	service.components.tasks.BeginTask("task-1", "choose migration", "high", nil, TaskCheckpoint{})
	service.Mu.Unlock()

	_, err := service.TaskTerminalHandler(task_context.ToolNeedsUserDecision)(context.Background(), `{
		"summary":"Two compatible migration paths remain.",
		"decision_question":"Choose incremental or breaking migration.",
		"decision_options":["incremental","breaking"]
	}`)
	if err != nil {
		t.Fatal(err)
	}
	service.Mu.RLock()
	state := service.components.tasks.CurrentTaskExecution()
	service.Mu.RUnlock()
	if state.Status != task_context.StatusNeedsUserDecision {
		t.Fatalf("terminal status = %q", state.Status)
	}
	if visible := service.Snapshot().Task; visible == nil || visible.Status != TaskNeedsUserDecision {
		t.Fatalf("visible task state = %#v", visible)
	}
}

func TestTaskCompleteRequiresAllAuthoritativePlanNodes(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	defer service.Shutdown()
	service.Mu.Lock()
	service.Core.Snapshot.Chat = ChatState{Running: true, RequestID: "task-1"}
	service.Core.Snapshot.Runtime.Plan = &PlanState{Nodes: []PlanNode{{ID: "inspect"}, {ID: "verify"}}}
	service.components.tasks.BeginTask("task-1", "audit", "high", nil, TaskCheckpoint{})
	service.Mu.Unlock()

	_, err := service.TaskTerminalHandler(task_context.ToolComplete)(context.Background(), `{"summary":"done","completed_nodes":["inspect"]}`)
	if err == nil || !strings.Contains(err.Error(), "verify") {
		t.Fatalf("incomplete plan completion error = %v", err)
	}
	_, err = service.TaskTerminalHandler(task_context.ToolComplete)(context.Background(), `{"summary":"done","completed_nodes":["inspect","verify"]}`)
	if err != nil {
		t.Fatal(err)
	}
	service.Mu.RLock()
	plan := service.Core.Snapshot.Runtime.Plan
	service.Mu.RUnlock()
	if plan.Status != PlanCompleted || plan.Progress != 1 || plan.Nodes[0].Status != NodeCompleted || plan.Nodes[1].Status != NodeCompleted {
		t.Fatalf("completed plan = %#v", plan)
	}
}

func TestNaturalStopWithPendingAuthoritativePlanNeedsUserDecision(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	defer service.Shutdown()
	service.Mu.Lock()
	service.Core.Snapshot.Chat = ChatState{Running: true, RequestID: "task-1"}
	service.Core.Snapshot.Runtime.Plan = &PlanState{Status: PlanPending, Nodes: []PlanNode{{ID: "inspect"}}}
	service.components.tasks.BeginTask("task-1", "prepare a plan", "high", nil, TaskCheckpoint{})
	service.Mu.Unlock()

	if err := service.finalizeTaskExecution("task-1"); err != nil {
		t.Fatal(err)
	}
	service.Mu.RLock()
	state := service.components.tasks.CurrentTaskExecution()
	service.Mu.RUnlock()
	if state.Status != task_context.StatusNeedsUserDecision || state.Terminal == nil || state.Terminal.Kind != task_context.ToolNeedsUserDecision {
		t.Fatalf("task terminal = %#v, want needs-user-decision", state)
	}
	visible := service.Snapshot().Task
	if visible == nil || visible.Status != TaskNeedsUserDecision || !strings.Contains(visible.Summary, "not executed") {
		t.Fatalf("visible task state = %#v", visible)
	}
}

func TestContextControllerCompactsAndCleansInternalCheckpoint(t *testing.T) {
	engine := &fakeEngine{history: []EngineMessage{
		{Role: "system", Content: "system instruction", ContentSet: true},
		{Role: "user", Content: strings.Repeat("older context ", 1600)},
		{Role: "assistant", Content: strings.Repeat("older answer ", 1600)},
		{Role: "user", Content: "inspect project"},
		{Role: "assistant", ToolCalls: []EngineToolCall{{ID: "call-1", Name: "read_file", Arguments: `{"path":"source.go"}`}}},
		{Role: "tool", ToolCallID: "call-1", Name: "read_file", Content: "found the current call path"},
	}}
	service := newTestService(t, engine)
	defer service.Shutdown()
	service.Mu.Lock()
	service.Core.Snapshot.Chat = ChatState{Running: true, RequestID: "task-1"}
	service.components.tasks.BeginTask("task-1", "inspect project", "high", nil, TaskCheckpoint{})
	service.components.tasks.SetTaskStateLocked("task-1", TaskProgressing, "Task is in progress.")
	service.components.tasks.CurrentTaskExecution().Checkpoint("inspect", "inspect source", "completed", "found call path", "")
	service.Mu.Unlock()

	if err := service.components.context.CompactTaskContext("task-1"); err != nil {
		t.Fatal(err)
	}
	history := engine.History()
	if len(history) < 5 || history[0].Role != "system" || !strings.HasPrefix(history[1].Content, context_runtime.TaskContextCheckpointPrefix) {
		t.Fatalf("history = %+v, want system, checkpoint, and recent complete units", history)
	}
	lastAssistant, lastTool := history[len(history)-2], history[len(history)-1]
	if len(lastAssistant.ToolCalls) != 1 || lastAssistant.ToolCalls[0].ID != "call-1" || lastTool.ToolCallID != "call-1" || lastTool.Content != "found the current call path" {
		t.Fatalf("recent complete tool round was not preserved: %+v", history)
	}
	for _, message := range history {
		if strings.Contains(message.Content, "older context") || strings.Contains(message.Content, "older answer") {
			t.Fatalf("old raw transcript survived checkpoint compaction: %+v", history)
		}
	}
	payload, err := json.Marshal(service.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(payload), context_runtime.TaskContextCheckpointPrefix) {
		t.Fatalf("frontend snapshot leaked internal checkpoint: %s", payload)
	}
	compactions := service.Snapshot().Task.ContextCompactions
	if len(compactions) != 1 || compactions[0].Version != 2 || compactions[0].Reason != "context_budget" || compactions[0].MessagesBefore != 6 || compactions[0].EstimatedTokens == 0 {
		t.Fatalf("visible compactions = %#v", compactions)
	}
	if err := service.components.context.RemoveTaskContextCheckpoints(); err != nil {
		t.Fatal(err)
	}
	for _, message := range engine.History() {
		if context_runtime.IsTaskContextCheckpoint(message.Content) {
			t.Fatalf("internal checkpoint leaked into retained history: %+v", message)
		}
	}
}

func TestContextControllerRepeatedCompactionDoesNotAccumulateCheckpoints(t *testing.T) {
	engine := &fakeEngine{history: []EngineMessage{{Role: "system", Content: "system instruction", ContentSet: true}}}
	service := newTestService(t, engine)
	defer service.Shutdown()
	service.Mu.Lock()
	service.Core.Snapshot.Chat = ChatState{Running: true, RequestID: "task-1"}
	service.components.tasks.BeginTask("task-1", "inspect project", "high", nil, TaskCheckpoint{})
	service.components.tasks.SetTaskStateLocked("task-1", TaskProgressing, "Task is in progress.")
	service.components.tasks.CurrentTaskExecution().Checkpoint("inspect", "inspect source", "completed", "found call path", "")
	service.Mu.Unlock()

	for round := 0; round < 2; round++ {
		history := engine.History()
		history = append(history,
			EngineMessage{Role: "assistant", ToolCalls: []EngineToolCall{{ID: fmt.Sprintf("call-%d", round), Name: "bash", Arguments: strings.Repeat("argument ", 3000)}}},
			EngineMessage{Role: "tool", ToolCallID: fmt.Sprintf("call-%d", round), Name: "bash", Content: strings.Repeat("tool output ", 3000)},
		)
		if err := engine.ReplaceHistory(engine.SessionID(), history); err != nil {
			t.Fatal(err)
		}
		service.Mu.Lock()
		service.components.tasks.CurrentTaskExecution().RecordTool("bash", fmt.Sprintf("observed repository state %d", round), nil)
		service.components.tasks.AppendTranscriptEventLocked(TranscriptEvent{TaskID: "task-1", Role: "assistant", ToolCalls: []TranscriptToolCall{{ID: fmt.Sprintf("call-%d", round), Name: "bash", Arguments: `{"summary":true}`}}})
		service.components.tasks.AppendTranscriptEventLocked(TranscriptEvent{TaskID: "task-1", Role: "tool", ToolCallID: fmt.Sprintf("call-%d", round), Name: "bash", Content: "bounded repository observation"})
		service.Mu.Unlock()
		if err := service.components.context.CompactTaskContext("task-1"); err != nil {
			t.Fatal(err)
		}
		compacted := engine.History()
		checkpointCount := 0
		for _, message := range compacted {
			if context_runtime.IsTaskContextCheckpoint(message.Content) {
				checkpointCount++
			}
		}
		if checkpointCount != 1 {
			t.Fatalf("round %d compacted history = %+v, want one checkpoint", round, compacted)
		}
	}
	compactions := service.Snapshot().Task.ContextCompactions
	if len(compactions) != 2 || compactions[0].Version != 2 || compactions[1].Version != 3 {
		t.Fatalf("visible compactions = %#v", compactions)
	}
}

func TestTaskContextRecoveryHistoryKeepsOnlyProductSystemInstruction(t *testing.T) {
	product, staleSummary := "product instruction", "[Context summary of stale execution]"
	history := []EngineMessage{
		{Role: "system", Content: product, ContentSet: true},
		{Role: "system", Content: staleSummary, ContentSet: true},
		{Role: "user", Content: "old request", ContentSet: true},
	}
	compacted := context_runtime.TaskContextRecoveryHistory(history, "checkpoint")
	if len(compacted) != 2 || compacted[0].Content != product || compacted[1].Content != "checkpoint" {
		t.Fatalf("compacted history = %#v", compacted)
	}
}

func TestContextControllerRejectsLargeToolOutputBeforeGlobalCompaction(t *testing.T) {
	engine := &fakeEngine{history: []EngineMessage{
		{Role: "system", Content: "system instruction", ContentSet: true},
		{Role: "user", Content: "inspect project"},
		{Role: "assistant", ToolCalls: []EngineToolCall{{ID: "call-1", Name: "bash", Arguments: `{"summary":false}`}}},
		{Role: "tool", ToolCallID: "call-1", Name: "bash", Content: strings.Repeat("x", Limits().MaxToolResultChars+1)},
	}}
	service := newTestService(t, engine)
	defer service.Shutdown()
	service.Mu.Lock()
	service.Core.Snapshot.Chat = ChatState{Running: true, RequestID: "task-1"}
	service.components.tasks.BeginTask("task-1", "inspect project", "high", nil, TaskCheckpoint{})
	service.components.tasks.SetTaskStateLocked("task-1", TaskProgressing, "Task is in progress.")
	service.Mu.Unlock()

	if err := service.components.context.CompactTaskContext("task-1"); err != nil {
		t.Fatal(err)
	}
	history := engine.History()
	if len(history) < 4 || history[len(history)-1].Role != "tool" || history[len(history)-2].Role != "assistant" ||
		!strings.HasPrefix(history[len(history)-1].Content, context_runtime.ToolResultOmittedPrefix) || !strings.Contains(history[len(history)-1].Content, "result_ref=tr-") || strings.Contains(history[len(history)-1].Content, strings.Repeat("x", 32)) {
		t.Fatalf("large tool output entered provider history: %#v", history)
	}
	if compactions := service.Snapshot().Task.ContextCompactions; len(compactions) != 0 {
		t.Fatalf("a single large tool result must not trigger global compaction: %#v", compactions)
	}
}

func TestTaskContextSummaryRetainsCompletedToolEvidence(t *testing.T) {
	state := task_context.NewTaskExecutionState("task-1", "inspect source", "high")
	state.RecordTool("read_file", "found ResumeSession in application/core/session_history.go", nil)
	state.RecordTool("go_test", "application/core tests passed", nil)
	summary := state.ContextSummary()
	if !strings.Contains(summary, "completed tool outcomes") || !strings.Contains(summary, "ResumeSession") || !strings.Contains(summary, "tests passed") {
		t.Fatalf("continuation summary lost completed work: %q", summary)
	}
}

func TestTaskContextSummaryIgnoresMetadataOnlyCheckpoint(t *testing.T) {
	state := task_context.NewTaskExecutionState("task-empty", "", "high")
	state.Status = task_context.StatusInterrupted
	state.InheritedCheckpoint = &TaskCheckpoint{
		Version:          7,
		CoversEventRange: EventRange{Start: 632, End: 632},
		UpdatedAt:        time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC),
	}
	if summary := state.ContextSummary(); summary != "" {
		t.Fatalf("metadata-only checkpoint produced recoverable context: %q", summary)
	}
}

func TestInterruptedTaskContinuationCarriesCheckpointAndSkills(t *testing.T) {
	engine := &fakeEngine{}
	service := newTestService(t, engine)
	defer service.Shutdown()
	service.promptStack.Push("skill", "review", "review prompt")
	service.Mu.Lock()
	service.Core.Snapshot.Task = &TaskState{RequestID: "old-task", Status: TaskInterrupted}
	service.components.tasks.BeginTask("old-task", "inspect source", "high", nil, TaskCheckpoint{})
	service.components.tasks.CurrentTaskExecution().Status = task_context.StatusInterrupted
	service.components.tasks.CurrentTaskExecution().Checkpoint("inspect", "inspect source", string(NodeCompleted), "found call path", "")
	service.components.tasks.ActivateTaskSkillsLocked(service.components.tasks.CurrentTaskExecution(), []PromptLayer{{Kind: "skill", Name: "review", Text: "review prompt"}})
	service.Mu.Unlock()

	if err := service.Submit(t.Context(), "continue"); err != nil {
		t.Fatal(err)
	}
	waitForChatCompletion(t, service)
	engine.mu.Lock()
	history := append([]EngineMessage(nil), engine.historyBeforeChat...)
	prompt := engine.prompt
	engine.mu.Unlock()
	foundCheckpoint := false
	for _, message := range history {
		if context_runtime.IsTaskContextCheckpoint(message.Content) && strings.Contains(message.Content, "node=inspect status=completed") {
			foundCheckpoint = true
			break
		}
	}
	if !foundCheckpoint || !strings.Contains(prompt, "review prompt") {
		t.Fatalf("continuation history=%#v prompt=%q", history, prompt)
	}
}

func TestTaskContextSummaryStaysWithinProviderToolBudget(t *testing.T) {
	state := task_context.NewTaskExecutionState("task-1", "inspect source", "high")
	for index := 0; index < 20; index++ {
		state.RecordTool(fmt.Sprintf("tool-%d", index), strings.Repeat("evidence ", 200), nil)
	}
	limit := Limits().MaxToolResultChars // 与 contextSummary 同源（max_tool_result_chars）
	if summary := state.ContextSummary(); len(summary) > limit {
		t.Fatalf("context summary = %d chars, want at most %d", len(summary), limit)
	}
}

func TestNoProgressBudgetStopsRepeatedToolRounds(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	defer service.Shutdown()
	service.Mu.Lock()
	service.Core.Snapshot.Chat = ChatState{Running: true, RequestID: "task-1"}
	service.components.tasks.BeginTask("task-1", "inspect", "high", nil, TaskCheckpoint{})
	service.components.tasks.StartReActBudgetLocked("task-1", ReActBudget{MaxNoProgressRounds: 2})
	service.Mu.Unlock()

	bridge := NewToolHookBridge()
	bridge.Bind(service)
	hooks := bridge.Hooks()
	if !hooks.OnIterationComplete(context.Background(), 0) {
		t.Fatal("first no-progress round should remain available")
	}
	if hooks.OnIterationComplete(context.Background(), 1) {
		t.Fatal("second no-progress round should stop the loop")
	}
	if err := service.components.tasks.ReActBudgetError("task-1"); err == nil || !strings.Contains(err.Error(), "no observable progress") {
		t.Fatalf("budget error = %v", err)
	}
}
