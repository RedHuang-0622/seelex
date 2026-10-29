package goal

// governance_continuation_test.go —— 治理视图带上**两层续跑状态**（自动续跑的横条
// 读面，docs/arch/goal-auto-continue.md §2.8.1）。
//
// 读路径只有一条：运行时投影（application/session/viewstate）→ 本包的
// GoalGovernanceViewFor → Snapshot.Runtime.GoalGovernance。GUI 与工具读的是快照里
// 那一份，不各自再拼一层状态词表；所以"能读出 none/doing/done × continue/paused +
// stalled 时长"这件事必须钉在这里。

import (
	"context"
	"testing"

	"github.com/RedHuang-0622/seelex/contract/dto"
	goaldomain "github.com/RedHuang-0622/seelex/kernel/goal"
)

type continuationGovernanceCoordinator struct{ coordinatorFake }

func (coordinator *continuationGovernanceCoordinator) GoalGovernanceViewFor(sessionID string) *dto.GoalGovernanceView {
	return &dto.GoalGovernanceView{Active: true, GoalID: "goal-1", Title: "自动续跑"}
}

func (coordinator *continuationGovernanceCoordinator) StatusFor(string) goaldomain.StatusView {
	return goaldomain.StatusView{Active: &dto.GoalRecord{ID: "goal-1", Title: "自动续跑"}}
}

func (coordinator *continuationGovernanceCoordinator) Begin(context.Context, string, goaldomain.BeginRequest) (*goaldomain.GoalRecord, error) {
	return &goaldomain.GoalRecord{ID: "goal-1"}, nil
}

func (coordinator *continuationGovernanceCoordinator) Update(context.Context, string, goaldomain.UpdateRequest) (*goaldomain.GoalRecord, error) {
	return &goaldomain.GoalRecord{ID: "goal-1"}, nil
}

func (coordinator *continuationGovernanceCoordinator) FinishDirect(context.Context, string, goaldomain.FinishRequest, bool) (*goaldomain.GoalRecord, error) {
	return &goaldomain.GoalRecord{ID: "goal-1"}, nil
}

func TestGoalGovernanceViewCarriesContinuationTwoLayerState(t *testing.T) {
	coordinator := &continuationGovernanceCoordinator{}
	continuation := &dto.GoalContinuationView{
		GoalID: "goal-1", Status: dto.GoalRunDoing, Substate: dto.GoalSubstatePaused,
		Paused: true, Attempts: 1, MaxAttempts: 3,
	}
	application := New(Deps{
		Coordinator:  func() CoordinatorPort { return coordinator },
		Continuation: func(string) *dto.GoalContinuationView { return continuation },
	})

	view := application.GovernanceViewFor("session-a")
	if view == nil || view.Continuation == nil {
		t.Fatalf("治理视图没有带上续跑状态：%+v", view)
	}
	if view.Continuation.Status != dto.GoalRunDoing || view.Continuation.Substate != dto.GoalSubstatePaused {
		t.Fatalf("两层状态 = %s/%s，期望 doing/paused", view.Continuation.Status, view.Continuation.Substate)
	}
	// 未装配续跑能力（关闭档）时字段是 nil：读的人据此不显示横条，而不是猜一个状态。
	off := New(Deps{Coordinator: func() CoordinatorPort { return coordinator }})
	if view := off.GovernanceViewFor("session-a"); view == nil || view.Continuation != nil {
		t.Fatalf("关闭档不该出现续跑状态：%+v", view)
	}
}
