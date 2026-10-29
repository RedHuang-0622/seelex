package dto

// goal_run_state_test.go — 两层状态机的唯一判定点（docs/arch/goal-auto-continue.md
// §2.8.0 的不变量 I1 / I2）。这些是纯函数用例：谁也别在别处再算一遍层一/层二。

import "testing"

func TestDeriveGoalRunStateLayerTwoOnlyUnderDoing(t *testing.T) {
	// I1：层一不是 doing 时，paused 位一律读成 continue——"暂停"在没目标的会话上
	// 没有意义，更不许拦住下一张 goal。
	for _, status := range []GoalRunStatus{GoalRunNone, GoalRunDone} {
		state := DeriveGoalRunState("", status, true, "")
		if state.Substate != GoalSubstateContinue || state.Paused() || state.AutoContinue() {
			t.Fatalf("status=%s 读成了 %+v，want continue/不自动续", status, state)
		}
	}
	if state := DeriveGoalRunState("goal-1", GoalRunDoing, false, ""); !state.AutoContinue() {
		t.Fatalf("doing/continue 应当自动续：%+v", state)
	}
	if state := DeriveGoalRunState("goal-1", GoalRunDoing, true, "goal-1"); !state.Paused() ||
		state.AutoContinue() {
		t.Fatalf("doing/paused 不应当自动续：%+v", state)
	}
}

func TestDeriveGoalRunStatePauseDoesNotResidueAcrossGoals(t *testing.T) {
	// I2：暂停绑定在**发起它的那张 goal** 上。
	if state := DeriveGoalRunState("goal-2", GoalRunDoing, true, "goal-1"); state.Paused() {
		t.Fatalf("上一张 goal 的暂停残留到了 goal-2：%+v", state)
	}
	// 旧档没有绑定（空串）：同样不继承（宁可让用户重按一次 ⏸）。
	if state := DeriveGoalRunState("goal-2", GoalRunDoing, true, ""); state.Paused() {
		t.Fatalf("没有 goal 绑定的旧暂停被继承了：%+v", state)
	}
	if state := DeriveGoalRunState("goal-1", GoalRunDoing, true, "goal-1"); !state.Paused() {
		t.Fatalf("同一张 goal 上的暂停没被认出来：%+v", state)
	}
}

func TestDeriveGoalRunStateRejectsUntrustedLayerOne(t *testing.T) {
	// 没有可信的层一就不自动续（不是默认放行）。
	if state := DeriveGoalRunState("", GoalRunStatus("watch"), false, ""); state.AutoContinue() {
		t.Fatalf("认不得的层一被当成了可续：%+v", state)
	}
	if state := DeriveGoalRunState("", GoalRunDoing, false, ""); state.AutoContinue() {
		t.Fatalf("说在跑却没有 goal 可绑定，仍被当成了可续：%+v", state)
	}
	// 兼容形态：只给 goal id（没有层一词）→ 非空 = doing。
	if state := DeriveGoalRunState("goal-1", "", false, ""); state.Status != GoalRunDoing {
		t.Fatalf("只给 goal id 时的层一 = %s，want doing", state.Status)
	}
}

func TestGoalRunStateOfReadsGoalStackView(t *testing.T) {
	active := GoalStatusView{Active: &GoalRecord{ID: "goal-1", Status: GoalActive}, History: 2}
	if state := GoalRunStateOf(active, false, ""); state.GoalID != "goal-1" || !state.AutoContinue() {
		t.Fatalf("未收口栈顶 = %+v", state)
	}
	closed := GoalStatusView{History: 3}
	if state := GoalRunStateOf(closed, true, "goal-1"); state.Status != GoalRunDone ||
		state.AutoContinue() {
		t.Fatalf("已收口 = %+v，want done/不续", state)
	}
	fresh := GoalStatusView{}
	if state := GoalRunStateOf(fresh, false, ""); state.Status != GoalRunNone || state.AutoContinue() {
		t.Fatalf("空会话 = %+v，want none/不续", state)
	}
}
