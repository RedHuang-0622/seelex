package core

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/application/core/context_runtime"
)

// TestContextBudgetOvershootKeepsNewestSettledRound：达峰装配时单个已定稿
// 轮次估算大于压缩目标但小于全量预算，不得静默裁掉最新轮（旧缺陷：引擎
// 历史被替换为空，请求带空历史照发）；修复后应保留最新轮并继续，只有真正
// 超出全量预算才返回 ErrProviderContextBudgetExceeded。
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
	// 3 个已定稿轮：assistant 正文约 60 万 ASCII 字符（≈150k tokens，低于
	// 全量预算 166808、高于压缩目标 100084）。
	huge := strings.Repeat("A", 600_000)
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

// TestContextBudgetOvershootRefusesWhenNewestExceedsFullBudget：最新轮本身
// 就大于全量预算时，装配必须显式返回 ErrProviderContextBudgetExceeded，而
// 不是带空历史继续。
func TestContextBudgetOvershootRefusesWhenNewestExceedsFullBudget(t *testing.T) {
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

	if _, err := service.components.context.PrepareExecutionContext("task-budget-2", "next"); !errors.Is(err, context_runtime.ErrProviderContextBudgetExceeded) {
		t.Fatalf("prepare error = %v, want ErrProviderContextBudgetExceeded", err)
	}
}
