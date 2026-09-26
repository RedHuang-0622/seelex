package core

import (
	"testing"

	"github.com/RedHuang-0622/seelex/application/core/task_context"
)

// TestContextBudgetDoesNotRecompactWithoutNewContent 钉住回合边界达峰判据的
// 幂等边界：一次折叠已经把保留窗口收敛到软阈值以下后，**没有新增 transcript
// 内容**的下一回合不得再压一次。
//
// 复现的真实链路（见 context_runtime.prepareExecutionContextFor）：
//  1. 回合 1 的 transcript 尾窗落在预算内、软阈值之上 → 折叠并落一条压缩记录；
//  2. 回合 2 只有新一轮的用户输入（装配前已被 excludeCurrentInputEvent 排除），
//     transcript 事实未增长；
//  3. 判据若仍从**整个 transcript 头部**重新累积，就会再次越过软阈值 → 每个
//     回合都压一次（现场表现：一发消息一条压缩记录）。
func TestContextBudgetDoesNotRecompactWithoutNewContent(t *testing.T) {
	pinMechanismCompactionRatios(t)
	applyWindowConfig(t, WindowConfig{})
	service, engine, sessionID := compactTestService(t, "task-frequency-1")
	// 20 轮 × 约 8k token ≈ 160k token：高于生效软阈值、低于生效预算 ——
	// 首轮必须折叠，且折叠落点必须从"占比保留"（ratio × all）再
	// 收口到生效配置的压缩目标（context_target_percent × 预算），给下一轮留出
	// 确定余量。两个数字都从生效预算读取，不在用例里写死。
	appendWindowRounds(t, service, "task-frequency-1", 20, 32_000)

	if _, err := service.components.context.PrepareExecutionContextFor(sessionID, "task-frequency-1", "next"); err != nil {
		t.Fatalf("首个回合装配: %v", err)
	}
	if records := compactionRecords(service, sessionID); len(records) != 1 {
		t.Fatalf("首轮压缩记录 = %d 条, want 1（用例前提：首轮达峰折叠一次）", len(records))
	}
	// 折叠后的历史（判据/装配同款估算器）必须收口到**配置的压缩目标**，而不是
	// 停在占比保留窗口。目标从生效预算读取，不在用例里写死百分比。
	budget := task_context.ContextBudgetFor(service.Deps.Runtime)
	if budget.TargetAfterCompaction <= 0 {
		t.Fatalf("生效预算没有压缩目标：%+v", budget)
	}
	if historyTokens := service.components.tasks.CountRequestTokens("", engine.History(), "", nil); historyTokens > budget.TargetAfterCompaction {
		t.Fatalf("折叠后历史 = %d tokens, want ≤ %d（context_target_percent 应决定落点）",
			historyTokens, budget.TargetAfterCompaction)
	}

	// 第二个回合：只推进请求纪元（模拟 Submit → BeginTaskFor），transcript
	// 事实一行未增。装配不得再落压缩记录。
	service.ViewMu.Lock()
	previous := service.components.tasks.CurrentTaskExecutionFor(sessionID)
	service.components.tasks.BeginTaskFor(sessionID, "task-frequency-2", "next request", "high", previous, TaskCheckpoint{})
	service.ViewMu.Unlock()
	if _, err := service.components.context.PrepareExecutionContextFor(sessionID, "task-frequency-2", "next request"); err != nil {
		t.Fatalf("第二个回合装配: %v", err)
	}
	if records := compactionRecords(service, sessionID); len(records) != 1 {
		t.Fatalf("无新增内容却再次压缩：压缩记录 = %d 条, want 1", len(records))
	}

	// 第三个回合：只追加一轮约 4k token 的新内容。保留窗口（约 0.7 × 144k）
	// 加上新增量仍应低于软阈值 125,106 —— 新增内容本身不等于“立刻再压一次”。
	appendWindowRounds(t, service, "task-frequency-2", 1, 16_000)
	service.ViewMu.Lock()
	previous = service.components.tasks.CurrentTaskExecutionFor(sessionID)
	service.components.tasks.BeginTaskFor(sessionID, "task-frequency-3", "next request", "high", previous, TaskCheckpoint{})
	service.ViewMu.Unlock()
	if _, err := service.components.context.PrepareExecutionContextFor(sessionID, "task-frequency-3", "next request"); err != nil {
		t.Fatalf("第三个回合装配: %v", err)
	}
	if records := compactionRecords(service, sessionID); len(records) != 1 {
		t.Fatalf("仅新增一轮就再次压缩：压缩记录 = %d 条, want 1", len(records))
	}
}
