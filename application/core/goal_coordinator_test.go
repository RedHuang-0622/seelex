package core

import (
	"context"
	"testing"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
	goaldomain "github.com/RedHuang-0622/seelex/application/core/goal"
)

// goal_coordinator_test.go — 会话级 goal 协调器的边界用例。
//
// 席位轮转退场后（2026-10-01 阶段三 W3），本文件只剩与"会话隔离 + 只读视图进
// SessionRuntime"有关的两条用例；原先钉住 Governor 回合/轮次/RoundError 的用例
// （AdvanceAfterChat 驱动 TL 回合、常规 verdict_done 收口、断环 governor 重置、
// 回合失败可见）随席位轮转一并删除——那些口径不再存在。

// TestGoalCoordinatorSessionIsolation 验证 P1 会话级协调器：两会话各自
// Begin/Status 不串；无 goal 的会话视图为 nil（前端隐藏）。
func TestGoalCoordinatorSessionIsolation(t *testing.T) {
	coordinator := newGoalCoordinator(goalCoordinatorDeps{})
	if view := coordinator.GoalGovernanceViewFor("session-a"); view != nil {
		t.Fatalf("未 begin 的会话不应有治理视图: %+v", view)
	}
	if _, err := coordinator.Begin(context.Background(), "session-a", goaldomain.BeginRequest{
		Title: "A 的治理目标",
	}); err != nil {
		t.Fatalf("begin A: %v", err)
	}
	if _, err := coordinator.Begin(context.Background(), "session-b", goaldomain.BeginRequest{
		Title: "B 的治理目标",
	}); err != nil {
		t.Fatalf("begin B: %v", err)
	}
	viewA := coordinator.GoalGovernanceViewFor("session-a")
	if viewA == nil || !viewA.Active || viewA.Title != "A 的治理目标" {
		t.Fatalf("A 治理视图 = %+v", viewA)
	}
	viewB := coordinator.GoalGovernanceViewFor("session-b")
	if viewB == nil || viewB.Title != "B 的治理目标" {
		t.Fatalf("B 治理视图 = %+v", viewB)
	}
	if status := coordinator.StatusFor("session-a"); status.Active == nil || status.Active.Title != "A 的治理目标" {
		t.Fatalf("A 状态 = %+v", status)
	}
	if status := coordinator.StatusFor("session-b"); status.Active == nil || status.Active.Title != "B 的治理目标" {
		t.Fatalf("B 状态 = %+v", status)
	}
}

// TestSessionRuntimeCarriesGoalGovernance 验证 GoalGovernanceView 进入
// SessionRuntime 槽并随 clone 深拷贝（前端快照字段归属）。
func TestSessionRuntimeCarriesGoalGovernance(t *testing.T) {
	runtime := RuntimeState{
		GoalGovernance: &dto.GoalGovernanceView{Active: true, GoalID: "g-1", Title: "看板"},
	}
	cloned := cloneRuntimeState(runtime)
	if cloned.GoalGovernance == nil || cloned.GoalGovernance.GoalID != "g-1" {
		t.Fatalf("clone 丢失治理视图: %+v", cloned.GoalGovernance)
	}
	cloned.GoalGovernance.Title = "改了"
	if runtime.GoalGovernance.Title != "看板" {
		t.Fatalf("治理视图应深拷贝（互不影响）: %+v", runtime.GoalGovernance)
	}
	session := sessionRuntimeOf(cloned)
	if session.GoalGovernance == nil || session.GoalGovernance.Title != "改了" {
		t.Fatalf("sessionRuntimeOf 应携带治理视图: %+v", session.GoalGovernance)
	}
}

// stubTLEvaluator 是一次性 TL 评估器（测试用）。
type stubTLEvaluator struct {
	directives []goaldomain.TLDirective
}

func (e *stubTLEvaluator) Evaluate(context.Context, goaldomain.TLSessionEmbed) (goaldomain.TLDirective, error) {
	if len(e.directives) == 0 {
		return goaldomain.TLDirective{Kind: goaldomain.DirectiveCheckpointOK, Content: "继续"}, nil
	}
	directive := e.directives[0]
	e.directives = e.directives[1:]
	return directive, nil
}
