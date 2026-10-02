package core

import (
	"fmt"
	"testing"

	"github.com/RedHuang-0622/seelex/application/core/internal/limits"
	"github.com/RedHuang-0622/seelex/seelexctx"
)

// pinIneffectiveCompactRatios 把压缩比例钉成「压缩落点够不到软线」的**合法**档：
// 软线 60%、目标 80%（目标 > 软线 ⇒ 保留窗口上限本身就高于软线）。
// 配置校验只要求 soft < hard（见 seelexctx.LoadLimits），因此这是能被加载的组合；
// 代码内置默认档是 8/95/98/80/50，这里注入的是同一族旋钮的另一种取值，用来钉
// **机制**（幂等/有效性校验）而不是出厂档（出厂档由 seelexctx/limits_test.go 钉住）。
func pinIneffectiveCompactRatios(t *testing.T) {
	t.Helper()
	previous := limits.Get()
	limits.Apply(seelexctx.Limits{
		ContextSafetyReserveDivisor: 8,
		ContextSoftPercent:          60,
		ContextHardPercent:          90,
		ContextTargetPercent:        80,
		ContextSingleItemPercent:    50,
	})
	t.Cleanup(func() { limits.Apply(previous) })
}

// TestContextBudgetSkipsCompactionWithoutMargin 钉住软线压缩的**幂等/有效性校验**：
// 压缩落点（保留窗口上限 + 固定开销）仍落在软线之上时，这一轮不得落压缩记录、
// 不得推帧——折了也只是把同一件事再做一遍，下一轮达峰判据会以同一个数字再越线。
//
// 现场（2026-09-29 21:19~21:23，同一会话 session-48c05322bb9e6f1a）：3.5 分钟内
// 落 3 条压缩记录（21:19:52 / 21:21:42 / 21:23:19），每条记录的区间都从
// message-1 起、`estimated_tokens` 贴着软线（205309 → 162530）。这就是
// 「没有余量的压缩每轮重来」——它同时解释了用户看到的两件事：压缩过于频繁，
// 以及每次压缩的帧正文都小得可怜（折的永远是同一段，帧里没有新事实）。
//
// 恢复行为：`retain.Retained` 就是装配层收口用的落点上限（fitExecutionHistory
// 判的是**整条请求**估算），因此「落点 = 保留区上限 + 固定开销」是这次压缩能到达
// 的最好情况；它 ≥ 软线即无余量。
func TestContextBudgetSkipsCompactionWithoutMargin(t *testing.T) {
	pinIneffectiveCompactRatios(t)
	// 占比窗口拉满（ratio = 1）让保留区上限真的顶到 target：这样"无余量"由比例本身
	// 决定，而不是由某个体积巧合决定，用例不随内容体积漂移。
	applyWindowConfig(t, WindowConfig{Ratio: 1})
	service, _, sessionID := compactTestService(t, "task-ineffective-1")

	// 16 轮 × 约 8k token ≈ 128k token：高于生效软线（预算 × 60% = 100084），
	// 却低于硬线（预算 × 90% = 150127），因此它命中的是**软线压缩**这条路，
	// 而这次压缩的落点上限（预算 × 80% = 133446）在软线之上。
	appendWindowRounds(t, service, "task-ineffective-1", 16, 32_000)

	if _, err := service.components.context.PrepareExecutionContextFor(sessionID, "task-ineffective-1", "next"); err != nil {
		t.Fatalf("首个回合装配: %v", err)
	}
	if records := compactionRecords(service, sessionID); len(records) != 0 {
		t.Fatalf("无余量的压缩应被跳过，却落了 %d 条记录: %+v", len(records), records)
	}

	// 再走两个回合（每回合约 +8k token，仍不越硬线）：判据仍在软线之上、落点仍
	// 够不到软线 ⇒ 仍不得落记录。修复前这里每回合多一条（"一发消息一条压缩记录"）。
	for index := 0; index < 2; index++ {
		appendWindowRounds(t, service, "task-ineffective-1", 1, 32_000)
		requestID := fmt.Sprintf("task-ineffective-%d", index+2)
		service.ViewMu.Lock()
		previous := service.components.tasks.CurrentTaskExecutionFor(sessionID)
		service.components.tasks.BeginTaskFor(sessionID, requestID, "next request", "high", previous, TaskCheckpoint{})
		service.ViewMu.Unlock()
		if _, err := service.components.context.PrepareExecutionContextFor(sessionID, requestID, "next request"); err != nil {
			t.Fatalf("第 %d 个回合装配: %v", index+2, err)
		}
		if records := compactionRecords(service, sessionID); len(records) != 0 {
			t.Fatalf("回合 %d 又落记录（无余量的压缩未做幂等校验）: %d 条", index+2, len(records))
		}
	}
}
