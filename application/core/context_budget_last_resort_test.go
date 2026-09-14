package core

import (
	"fmt"
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/application/core/context_runtime"
)

// TestContextBudgetOvershootKeepsNewestSettledRound：达峰装配时单个已定稿
// 轮次估算大于压缩目标但仍低于硬阈值（预算 90%），属于正常有界窗口——此时
// 必须保留最新轮继续（旧缺陷：引擎历史被替换为空，请求带空历史照发）。
// 真正逼近/超过硬阈值时才由 TestContextBudgetProactivelyCompactsAtHardThreshold
// 与 TestContextBudgetOvershootCompactsWhenNewestExceedsFullBudget 覆盖。
func TestContextBudgetOvershootKeepsNewestSettledRound(t *testing.T) {
	runtime := runtimeWithContextLimits{
		fakeRuntime: &fakeRuntime{}, window: 200_000, output: 8_192,
	}
	engine := &fakeEngine{}
	service := newTestService(t, engine, withTestRuntime(runtime))
	defer service.Shutdown()

	service.ViewMu.Lock()
	service.Core.Snapshot.Chat = ChatState{Running: true, RequestID: "task-budget-1"}
	service.components.tasks.BeginTask("task-budget-1", "inspect", "high", nil, TaskCheckpoint{})
	// 3 个已定稿轮：assistant 正文约 52 万 ASCII 字符（≈130k tokens，介于
	// 压缩目标 100084 与硬阈值 150127 之间）。
	huge := strings.Repeat("A", 520_000)
	for round := 0; round < 3; round++ {
		service.components.tasks.AppendTranscriptEventLocked(TranscriptEvent{
			TaskID: "task-budget", Role: "user", Content: fmt.Sprintf("request-%d", round),
		})
		service.components.tasks.AppendTranscriptEventLocked(TranscriptEvent{
			TaskID: "task-budget", Role: "assistant", Content: huge,
		})
	}
	service.ViewMu.Unlock()

	if _, err := service.components.context.PrepareExecutionContext("task-budget-1", "next"); err != nil {
		t.Fatal(err)
	}
	history := engine.History()
	if len(history) == 0 {
		t.Fatal("newest settled round silently dropped: engine history is empty after context assembly")
	}
	foundNewest := false
	for _, message := range history {
		if strings.Contains(message.Content, huge) {
			foundNewest = true
			break
		}
	}
	if !foundNewest {
		t.Fatalf("newest settled round missing from engine history (len=%d)", len(history))
	}
}

// TestContextBudgetProactivelyCompactsAtHardThreshold：装配结果落在硬阈值
// （预算 90% = 150127）与全量预算（166808）之间时，必须**探测即主动压缩**——
// 折叠为有界 checkpoint 帧，而不是把贴着上限的历史发出去、等超过全量预算
// 再被动兜底（旧行为：这种请求会照发，下一次超限才报错）。
func TestContextBudgetProactivelyCompactsAtHardThreshold(t *testing.T) {
	runtime := runtimeWithContextLimits{
		fakeRuntime: &fakeRuntime{}, window: 200_000, output: 8_192,
	}
	engine := &fakeEngine{}
	service := newTestService(t, engine, withTestRuntime(runtime))
	defer service.Shutdown()

	service.ViewMu.Lock()
	service.Core.Snapshot.Chat = ChatState{Running: true, RequestID: "task-budget-3"}
	service.components.tasks.BeginTask("task-budget-3", "inspect", "high", nil, TaskCheckpoint{})
	// 约 61 万 ASCII 字符 ≈152.5k tokens：高于硬阈值、低于全量预算。
	huge := strings.Repeat("A", 610_000)
	service.components.tasks.AppendTranscriptEventLocked(TranscriptEvent{
		TaskID: "task-budget", Role: "user", Content: "last request",
	})
	service.components.tasks.AppendTranscriptEventLocked(TranscriptEvent{
		TaskID: "task-budget", Role: "assistant", Content: huge,
	})
	service.ViewMu.Unlock()

	if _, err := service.components.context.PrepareExecutionContext("task-budget-3", "next"); err != nil {
		t.Fatalf("prepare error = %v, want proactive autonomous compaction", err)
	}
	history := engine.History()
	frameIndex := -1
	for index, message := range history {
		if strings.HasPrefix(message.Content, context_runtime.AutonomousCompactionPrefix) {
			frameIndex = index
		}
		if strings.Contains(message.Content, huge) {
			t.Fatal("round above the hard threshold must be folded proactively, not sent near the budget edge")
		}
	}
	if frameIndex < 0 {
		t.Fatalf("proactive compaction frame missing from engine history: %#v", history)
	}
	service.ViewMu.RLock()
	compactions := service.components.tasks.CurrentTaskExecution().ContextCompactions
	service.ViewMu.RUnlock()
	if len(compactions) != 1 || compactions[0].Reason != "context_budget_autonomous" {
		t.Fatalf("context compactions = %#v, want one context_budget_autonomous record", compactions)
	}
}

// TestContextBudgetOvershootCompactsWhenNewestExceedsFullBudget：最新轮自身
// 就大于全量预算时，装配不得直接拒绝发送，而应自主压缩——把可变 transcript
// 折叠为有界 checkpoint 帧（原始超限轮次不再进入 provider 历史），并留下
// 一条自主压缩记录。
func TestContextBudgetOvershootCompactsWhenNewestExceedsFullBudget(t *testing.T) {
	runtime := runtimeWithContextLimits{
		fakeRuntime: &fakeRuntime{}, window: 200_000, output: 8_192,
	}
	engine := &fakeEngine{}
	service := newTestService(t, engine, withTestRuntime(runtime))
	defer service.Shutdown()

	service.ViewMu.Lock()
	service.Core.Snapshot.Chat = ChatState{Running: true, RequestID: "task-budget-2"}
	service.components.tasks.BeginTask("task-budget-2", "inspect", "high", nil, TaskCheckpoint{})
	huge := strings.Repeat("A", 1_000_000) // ≈250k tokens，大于全量预算 166808
	service.components.tasks.AppendTranscriptEventLocked(TranscriptEvent{
		TaskID: "task-budget", Role: "user", Content: "last request",
	})
	service.components.tasks.AppendTranscriptEventLocked(TranscriptEvent{
		TaskID: "task-budget", Role: "assistant", Content: huge,
	})
	service.ViewMu.Unlock()

	if _, err := service.components.context.PrepareExecutionContext("task-budget-2", "next"); err != nil {
		t.Fatalf("prepare error = %v, want autonomous compaction instead of refusal", err)
	}
	history := engine.History()
	frameIndex := -1
	for index, message := range history {
		if strings.HasPrefix(message.Content, context_runtime.AutonomousCompactionPrefix) {
			frameIndex = index
		}
		if strings.Contains(message.Content, huge) {
			t.Fatal("oversized settled round must be replaced by the bounded checkpoint, not replayed")
		}
	}
	if frameIndex < 0 {
		t.Fatalf("autonomous compaction frame missing from engine history: %#v", history)
	}
	service.ViewMu.RLock()
	compactions := service.components.tasks.CurrentTaskExecution().ContextCompactions
	service.ViewMu.RUnlock()
	if len(compactions) != 1 || compactions[0].Reason != "context_budget_autonomous" {
		t.Fatalf("context compactions = %#v, want one context_budget_autonomous record", compactions)
	}
}
