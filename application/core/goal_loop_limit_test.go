package core

import (
	"context"
	"testing"

	goaldomain "github.com/RedHuang-0622/seelex/application/core/goal"
)

// TestGoalLoopRoundLimitDefaults：治理循环的轮次上限解析——未配置（0）时落到
// 默认上限（**不能是无限**），显式正值原样生效，显式负值是主动放弃上限。
// 这条护栏是"循环必须有逃生路径"里最后一道：没有它，早期实现里
// deps.MaxRounds 无人赋值就等于无限循环。
func TestGoalLoopRoundLimitDefaults(t *testing.T) {
	cases := []struct {
		configured int
		want       int
	}{
		{0, defaultGoalLoopMaxRounds},
		{7, 7},
		{1, 1},
		{-1, 0},
		{-100, 0},
	}
	for _, item := range cases {
		if got := goalLoopRoundLimit(item.configured); got != item.want {
			t.Fatalf("goalLoopRoundLimit(%d) = %d, want %d", item.configured, got, item.want)
		}
	}
	if defaultGoalLoopMaxRounds <= 0 {
		t.Fatalf("默认轮次上限必须是正数，得到 %d", defaultGoalLoopMaxRounds)
	}
}

// TestGoalGovernanceViewCarriesRoundLimit：治理视图必须把轮次上限一并下发——
// 前端才能显示"轮次 n/limit"并在接近上限时提示逃生（否则用户看不到循环何时收束）。
func TestGoalGovernanceViewCarriesRoundLimit(t *testing.T) {
	service := newTestService(t, &fakeEngine{}, withTestSessions(&teamRecordingSessions{}))
	sessionID := "sess-goal-limit"
	if _, err := service.GoalBeginFor(context.Background(), sessionID, goaldomain.BeginRequest{Title: "轮次上限"}); err != nil {
		t.Skipf("goal 装配不可用于该夹具: %v", err)
	}
	view := service.GoalGovernanceViewFor(sessionID)
	if view == nil {
		t.Skip("该夹具没有 goal 治理视图")
	}
	if view.RoundLimit != defaultGoalLoopMaxRounds {
		t.Fatalf("RoundLimit = %d, want %d", view.RoundLimit, defaultGoalLoopMaxRounds)
	}
}

// TestTeamRuntimeSharesGovernorRoundLimit：团队环的逃生上限与 Governor 的
// maxRounds 必须同源——否则前端看到的"n/limit"和真正兜底的数字会打架。
func TestTeamRuntimeSharesGovernorRoundLimit(t *testing.T) {
	service := newTestService(t, &fakeEngine{}, withTestSessions(&teamRecordingSessions{}))
	sessionID := "sess-team-limit"
	if _, err := service.GoalBeginFor(context.Background(), sessionID, goaldomain.BeginRequest{Title: "同源上限"}); err != nil {
		t.Skipf("goal 装配不可用于该夹具: %v", err)
	}
	runtime := service.teamRuntimeBySession(sessionID)
	if runtime == nil {
		t.Skip("该夹具没有团队环（未装配 AgentTeam 存储）")
	}
	view, err := service.AgentTeamView(sessionID)
	if err != nil {
		t.Fatalf("AgentTeamView: %v", err)
	}
	if view.Schedule == nil {
		t.Fatal("成员表必须带发言调度运行态（前端据此显示下一个谁发言/逃生状态）")
	}
	if view.Schedule.RoundLimit != goalLoopRoundLimit(service.components.goal.deps.MaxRounds) {
		t.Fatalf("环的轮次上限 = %d，与 Governor 上限不一致", view.Schedule.RoundLimit)
	}
	if view.Schedule.RoundLimit <= 0 {
		t.Fatal("默认策略下轮次上限必须是正数（逃生路径不能在默认配置里退化成无穷）")
	}
}
