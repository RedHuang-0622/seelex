package core

import (
	"errors"
	"github.com/RedHuang-0622/seelex/application/core/context_runtime"
	"github.com/RedHuang-0622/seelex/application/core/task_context"
	"github.com/RedHuang-0622/seelex/application/model"
	"reflect"
	"strings"
	"testing"
)

type runtimeWithContextLimits struct {
	*fakeRuntime
	window int
	output int
}

func (runtime runtimeWithContextLimits) ContextWindow() int   { return runtime.window }
func (runtime runtimeWithContextLimits) MaxOutputTokens() int { return runtime.output }

func TestRejectToolResultsPreservesPairingWithoutPreview(t *testing.T) {
	const rawOutput = "secret source detail"
	history := []EngineMessage{
		{Role: "assistant", ToolCalls: []EngineToolCall{{ID: "call-1", Name: "read_file", Arguments: `{"path":"source.txt"}`}}},
		{Role: "tool", ToolCallID: "call-1", Name: "read_file", Content: strings.Repeat(rawOutput, 300), ContentSet: true},
	}

	filtered, changed := context_runtime.RejectToolResults(history, 200)
	if !changed {
		t.Fatal("expected oversized tool result to be rejected")
	}
	if got := filtered[1]; got.ToolCallID != "call-1" || got.Name != "read_file" ||
		!strings.HasPrefix(got.Content, context_runtime.ToolResultOmittedPrefix) || strings.Contains(got.Content, rawOutput) {
		t.Fatalf("filtered tool result = %#v", got)
	}
	if got := filtered[0].ToolCalls[0]; got.ID != "call-1" || got.Arguments != `{"path":"source.txt"}` {
		t.Fatalf("tool call protocol changed: %#v", got)
	}
}

func TestPrepareExecutionContextCountsActiveSystemPrompt(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	defer service.Shutdown()
	budget := task_context.DefaultContextBudget()
	service.promptStack.Push("base", "oversized-system", strings.Repeat("s", budget.Budget*3))
	service.ViewMu.Lock()
	service.Core.Snapshot.Chat = ChatState{Running: true, RequestID: "task-1"}
	service.components.tasks.BeginTask("task-1", "inspect", "high", nil, TaskCheckpoint{})
	service.ViewMu.Unlock()

	if _, err := service.components.context.PrepareExecutionContext("task-1", "continue"); !errors.Is(err, context_runtime.ErrProviderContextBudgetExceeded) {
		t.Fatalf("prepare error = %v, want provider budget exceeded", err)
	}
}

func TestPrepareExecutionContextUsesRuntimeContextLimits(t *testing.T) {
	runtime := runtimeWithContextLimits{
		fakeRuntime: &fakeRuntime{}, window: 200_000, output: 8_192,
	}
	service := newTestService(t, &fakeEngine{}, withTestRuntime(runtime))
	legacyBudget := task_context.DefaultContextBudget()
	service.promptStack.Push("base", "large-system", strings.Repeat("s", legacyBudget.Budget*3))
	service.ViewMu.Lock()
	service.Core.Snapshot.Chat = ChatState{Running: true, RequestID: "task-1"}
	service.components.tasks.BeginTask("task-1", "inspect", "high", nil, TaskCheckpoint{})
	service.ViewMu.Unlock()

	if _, err := service.components.context.PrepareExecutionContext("task-1", "continue"); err != nil {
		t.Fatalf("prepare with configured context window: %v", err)
	}
	service.ViewMu.RLock()
	audit := service.components.tasks.CurrentTaskExecution().TokenAudit
	service.ViewMu.RUnlock()
	if audit.Budget != 166_808 {
		t.Fatalf("token audit budget = %d, want 166808", audit.Budget)
	}
}

func TestPreparedRequestNeverExceedsSafeBudget(t *testing.T) {
	engine := &fakeEngine{}
	service := newTestService(t, engine)
	defer service.Shutdown()
	service.ViewMu.Lock()
	service.Core.Snapshot.Chat = ChatState{Running: true, RequestID: "task-1"}
	service.components.tasks.BeginTask("task-1", "inspect", "high", nil, TaskCheckpoint{})
	// 夹具：每个已定稿轮次本身都超过全部预算（小预算兜底账号）。此时
	// 协议单元不可拆分、没有可发送的窗口，装配必须显式拒绝（旧行为是
	// 静默清空历史后照发，模型“失忆”，见 2026-09-08 上下文恢复评审 §3）。
	for round := 0; round < 8; round++ {
		callID := "call-" + string(rune('a'+round))
		service.components.tasks.AppendTranscriptEventLocked(TranscriptEvent{TaskID: "old-task", Role: "user", Content: strings.Repeat("request ", 2500)})
		service.components.tasks.AppendTranscriptEventLocked(TranscriptEvent{TaskID: "old-task", Role: "assistant", ToolCalls: []TranscriptToolCall{{ID: callID, Name: "read"}}})
		service.components.tasks.AppendTranscriptEventLocked(TranscriptEvent{TaskID: "old-task", Role: "tool", ToolCallID: callID, Name: "read", Content: strings.Repeat("result ", 2500)})
		service.components.tasks.AppendTranscriptEventLocked(TranscriptEvent{TaskID: "old-task", Role: "assistant", Content: "round complete"})
	}
	service.ViewMu.Unlock()

	if _, err := service.components.context.PrepareExecutionContext("task-1", "continue with verification"); !errors.Is(err, context_runtime.ErrProviderContextBudgetExceeded) {
		t.Fatalf("prepare error = %v, want ErrProviderContextBudgetExceeded (single round exceeds full budget)", err)
	}
}

func TestPrepareExecutionContextOrderAndNoCheckpoint(t *testing.T) {
	engine := &fakeEngine{}
	service := newTestService(t, engine)
	defer service.Shutdown()
	service.ViewMu.Lock()
	service.Core.Snapshot.Chat = ChatState{Running: true, RequestID: "task-1"}
	service.components.tasks.BeginTask("task-1", "inspect", "high", nil, TaskCheckpoint{})
	// 已定稿轮次 = 累积 context 源。
	service.components.tasks.AppendTranscriptEventLocked(TranscriptEvent{TaskID: "task-0", Role: "user", Content: "first request", TokenCount: 2})
	service.components.tasks.AppendTranscriptEventLocked(TranscriptEvent{TaskID: "task-0", Role: "assistant", Content: "first answer", TokenCount: 2})
	service.components.tasks.AppendTranscriptEventLocked(TranscriptEvent{TaskID: "task-0", Role: "user", Content: "second request", TokenCount: 2})
	service.components.tasks.AppendTranscriptEventLocked(TranscriptEvent{TaskID: "task-0", Role: "assistant", Content: "second answer", TokenCount: 2})
	plan := &model.PlanState{
		Name: "audit", Status: model.PlanRunning,
		Nodes: []model.PlanNode{{ID: "n1", Label: "inspect", Status: model.NodeRunning}},
	}
	service.Core.Snapshot.Runtime.Plan = plan
	service.components.tasks.SetPlanStateLocked([]model.SessionPlanFrame{{
		ID: "plan-1", Plan: plan,
		Arguments: `{"nodes":{"n1":{"input":"read source"}},"edges":{}}`,
	}}, "plan-1")
	service.ViewMu.Unlock()

	if _, err := service.components.context.PrepareExecutionContext("task-1", "continue with verification"); err != nil {
		t.Fatal(err)
	}
	history := engine.History()
	// 正常路径：checkpoint 消息不再注入 LLM 上下文。
	for _, message := range history {
		if context_runtime.IsTaskContextCheckpoint(message.Content) {
			t.Fatalf("normal path must not inject checkpoint message: %#v", message)
		}
	}
	// 顺序：累积 context（已定稿轮次）在前，plan 尾部消息在后。
	planIndex := -1
	tailIndex := -1
	for index, message := range history {
		switch {
		case strings.HasPrefix(message.Content, "<!-- seelex:active-plan:v1 -->"):
			planIndex = index
		case message.Content == "second answer":
			tailIndex = index
		}
	}
	if planIndex < 0 {
		t.Fatal("normal path must keep the active-plan tail message")
	}
	if tailIndex < 0 {
		t.Fatal("accumulated context tail missing from engine history")
	}
	if planIndex <= tailIndex {
		t.Fatalf("plan must follow accumulated context: tail=%d plan=%d", tailIndex, planIndex)
	}
}

func TestTranscriptTailKeepsInterruptedRoundAndDropsOrphanToolProtocols(t *testing.T) {
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
	history := task_context.TranscriptTailHistory(events, 100, 4)
	gotSeq := make([]string, len(history))
	for index, message := range history {
		gotSeq[index] = message.Role + ":" + message.ToolCallID
	}
	// 中断（残缺）轮 1-3 是 UI 可见轮次，作为开放单元进入冷加载尾窗（缺失
	// 结果由装配层补齐）；孤儿 tool 9 仍不构成单元、不进入 provider 上下文。
	want := []string{"user:", "assistant:", "tool:a", "user:", "assistant:", "tool:d", "tool:c", "assistant:"}
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
	history := task_context.TranscriptTailHistory(events, 100, 2)
	got := make([]string, len(history))
	for index, message := range history {
		got[index] = message.Content
	}
	if want := []string{"first", "answer", "please continue from the report"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("tail content=%v, want %v", got, want)
	}
}

func TestTranscriptTailAccumulatesAllSettledRoundsWhenUnlimited(t *testing.T) {
	events := make([]TranscriptEvent, 0, 16)
	for round := 0; round < 8; round++ {
		events = append(events,
			TranscriptEvent{Seq: uint64(2*round + 1), TaskID: "task-0", Role: "user", Content: "request", TokenCount: 2},
			TranscriptEvent{Seq: uint64(2*round + 2), TaskID: "task-0", Role: "assistant", Content: "answer", TokenCount: 2},
		)
	}
	// maxUnits <= 0 = 全量累积（append-only 已定稿轮次，不再 ≤4 轮有界窗口）。
	history := task_context.TranscriptTailHistory(events, 100_000, 0)
	if len(history) != 16 {
		t.Fatalf("history = %d messages, want all 16 settled-round messages", len(history))
	}
	if history[0].Content != "request" || history[15].Content != "answer" {
		t.Fatalf("accumulated context order changed: first=%q last=%q", history[0].Content, history[15].Content)
	}
}

func TestPrepareExecutionContextAccumulatesAllSettledRoundsAndByteStable(t *testing.T) {
	engine := &fakeEngine{}
	service := newTestService(t, engine)
	defer service.Shutdown()
	service.ViewMu.Lock()
	service.Core.Snapshot.Chat = ChatState{Running: true, RequestID: "task-1"}
	service.components.tasks.BeginTask("task-1", "inspect", "high", nil, TaskCheckpoint{})
	// 8 个已定稿轮次（远超 ContextMaxUnits=4），总 token 在软阈值内。
	for round := 0; round < 8; round++ {
		service.components.tasks.AppendTranscriptEventLocked(TranscriptEvent{
			TaskID: "task-0", Role: "user", Content: "request", TokenCount: 2,
		})
		service.components.tasks.AppendTranscriptEventLocked(TranscriptEvent{
			TaskID: "task-0", Role: "assistant", Content: "answer", TokenCount: 2,
		})
	}
	service.ViewMu.Unlock()

	if _, err := service.components.context.PrepareExecutionContext("task-1", "next"); err != nil {
		t.Fatal(err)
	}
	first := engine.History()
	// 达峰前：首个已定稿轮次必须仍在累积 context 中（不再被 ≤4 轮窗口丢弃）。
	if !strings.Contains(strings.Join(historyContents(first), "\n"), "request") {
		t.Fatalf("accumulated context dropped the first settled round: %#v", first)
	}
	// 字节稳定：同一已定稿轮次再次装配产生逐字节相同的历史。
	if _, err := service.components.context.PrepareExecutionContext("task-1", "next"); err != nil {
		t.Fatal(err)
	}
	second := engine.History()
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("accumulated context not byte-stable:\nfirst=%#v\nsecond=%#v", first, second)
	}
}

func historyContents(history []EngineMessage) []string {
	contents := make([]string, 0, len(history))
	for _, message := range history {
		contents = append(contents, message.Content)
	}
	return contents
}

func TestRejectToolResultsRecognizesFrameworkTruncationMarker(t *testing.T) {
	history := []EngineMessage{{
		Role: "tool", ToolCallID: "call-1", Name: "bash",
		Content: strings.Repeat("x", 4000) + context_runtime.FrameworkToolOutputTruncatedMarker,
	}}
	filtered, changed := context_runtime.RejectToolResults(history, 4000)
	if !changed || !strings.HasPrefix(filtered[0].Content, context_runtime.ToolResultOmittedPrefix) {
		t.Fatalf("framework-truncated result = %#v, changed=%v", filtered, changed)
	}
}
