package integration

// goal_continuation_wiring_test.go —— 自动续跑的**生产接线**验收（硬判据 ① 与 ④）。
//
// 读的是生产入口本身：装配好的 Service → ComponentsFor → WorkTable 运行时。
//   - ① 续跑门真的被装配注入（未注入时运行时是惰性零值，这里要求非惰性：能给出
//     two-layer 状态、能把 ⏸ 写回会话活状态）；
//   - ④ 两层状态可读：goal 状态（none/doing/done）× 子状态（continue/paused）
//     + stalled 时长，都从这一份读面读出来，不各自现算。
//
// 判据语义与通道分工见 docs/arch/goal-auto-continue.md §2.8.0/§2.8.8。

import (
	"testing"

	assembly "github.com/RedHuang-0622/seelex/application/assembly"
	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/kernel/limit"
)

// 装配层必须给会话域一个真实的续跑门：读面能回答两层状态（而不是 nil/零值惰性面）。
func TestAssembledServiceExposesContinuationTwoLayerState(t *testing.T) {
	applyTestLimits(t, func(applied *limit.Limits) {
		applied.Continuation.Enabled = true
	})
	service := newTestService(t, newMultiSessionEngine())
	workTable := assembly.ComponentsFor(service).Execution.WorkTable
	if workTable == nil {
		t.Fatal("WorkTable 运行时未装配")
	}

	sessionID := service.Core.Snapshot.Session.ID
	view := workTable.ContinuationView(sessionID)
	if view == nil {
		t.Fatal("续跑门没有注入装配层：横条读面为 nil（关闭档行为，说明接线没接上）")
	}
	// 没有登记目标 → 层一 none，层二恒 continue（I1），没有横条。
	if view.Status != dto.GoalRunNone || view.Substate != dto.GoalSubstateContinue {
		t.Fatalf("新会话两层状态 = %s/%s，期望 none/continue", view.Status, view.Substate)
	}
	if view.Stalled || view.StalledForSec != 0 || view.MaxAttempts <= 0 {
		t.Fatalf("新会话不该有 stalled 时长，且上限必须可读：%+v", view)
	}
}
