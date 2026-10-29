package task_context

// continuation_state_test.go —— 续跑控制位的会话粒度读写面（docs/arch/goal-auto-continue.md
// §2.8.0 的四条不变量 + 开新回合的清位点）。
//
// 这里钉的是**接线层**的事实：状态按 sessionID 隔离（I3）、⏸ 绑定 goal（I2 的写入侧）、
// 开新回合清闸位但**不动**续跑账（幂等/计数是跨回合事实）。

import (
	"testing"
	"time"

	model "github.com/RedHuang-0622/seelex/contract/protocol"
	"github.com/RedHuang-0622/seelex/kernel/state"
)

func newContinuationTestCoordinator() *Coordinator {
	return NewCoordinator(Deps{Core: &state.Core{}})
}

func TestContinuationPausedBitIsSessionScopedAndBoundToGoal(t *testing.T) {
	coordinator := newContinuationTestCoordinator()
	at := time.Unix(1_700_000_000, 0)

	coordinator.SetContinuationPausedFor("session-a", "goal-1", true, at)

	a := coordinator.ContinuationStateFor("session-a")
	if !a.Paused || a.PausedGoalID != "goal-1" || !a.PausedAt.Equal(at) {
		t.Fatalf("A 的暂停位没写进去：%+v", a)
	}
	// I3 会话粒度：A 的暂停不让 B 也暂停。
	if b := coordinator.ContinuationStateFor("session-b"); b.Paused || b.PausedGoalID != "" {
		t.Fatalf("B 被 A 的暂停串到了：%+v", b)
	}

	// 换 goal 后清位：绑定值一起被改写（读侧 I2 因此不会把旧绑定套到新 goal 上）。
	coordinator.SetContinuationPausedFor("session-a", "goal-2", false, at)
	a = coordinator.ContinuationStateFor("session-a")
	if a.Paused || a.PausedGoalID != "goal-2" {
		t.Fatalf("清位没有改写绑定：%+v", a)
	}
}

func TestClearContinuationGatesDropsPauseButKeepsLedger(t *testing.T) {
	coordinator := newContinuationTestCoordinator()
	at := time.Unix(1_700_000_000, 0)

	coordinator.SetContinuationPausedFor("session-a", "goal-1", true, at)
	coordinator.MarkStoppedByUserFor("session-a", at)
	coordinator.AppendContinuationAttemptFor("session-a", model.ContinuationAttempt{
		At: at, GoalID: "goal-1", RequestID: "req-1", Attempt: 1,
	})

	// 开新回合（turn_process.BeginLocked 调的就是这一条）：闸位清零。
	coordinator.ClearContinuationGatesFor("session-a")

	state := coordinator.ContinuationStateFor("session-a")
	if state.Paused || state.PausedGoalID != "" || !state.PausedAt.IsZero() {
		t.Fatalf("新回合继承了上一轮的暂停位：%+v", state)
	}
	if state.StoppedByUser || !state.StoppedAt.IsZero() {
		t.Fatalf("新回合继承了上一轮的停止位：%+v", state)
	}
	// 续跑账是跨回合的幂等/计数事实：清位不能把它一起清掉。
	if len(state.Continuations) != 1 || state.Continuations[0].RequestID != "req-1" {
		t.Fatalf("清位动了续跑账：%+v", state.Continuations)
	}

	ledger := model.DeriveContinuationLedger("goal-1", state.Continuations)
	if ledger.Attempts != 1 || !ledger.Continued[model.ContinuationKey("goal-1", "req-1")] {
		t.Fatalf("续跑账派生口径不对：%+v", ledger)
	}
}
