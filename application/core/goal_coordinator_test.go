package core

import (
	"context"
	"testing"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
	goaldomain "github.com/RedHuang-0622/seelex/application/core/goal"
)

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

// TestGoalCoordinatorHeartbeatMonotonic 验证治理推进即心跳：Begin/Update
// 后 heartbeat_seq 单调递增且视图携带 active 状态。
func TestGoalCoordinatorHeartbeatMonotonic(t *testing.T) {
	coordinator := newGoalCoordinator(goalCoordinatorDeps{})
	if _, err := coordinator.Begin(context.Background(), "session-hb", goaldomain.BeginRequest{
		Title: "心跳目标",
	}); err != nil {
		t.Fatalf("begin: %v", err)
	}
	first := coordinator.GoalGovernanceViewFor("session-hb")
	if first == nil || first.HeartbeatSeq != 1 || first.Status != string(goaldomain.StatusActive) {
		t.Fatalf("begin 后视图 = %+v", first)
	}
	if _, err := coordinator.Update(context.Background(), "session-hb", goaldomain.UpdateRequest{
		ProgressKind: goaldomain.ProgressMilestone, ProgressContent: "打点",
	}); err != nil {
		t.Fatalf("update: %v", err)
	}
	second := coordinator.GoalGovernanceViewFor("session-hb")
	if second == nil || second.HeartbeatSeq <= first.HeartbeatSeq {
		t.Fatalf("心跳应单调递增: first=%+v second=%+v", first, second)
	}
}

// TestSessionRuntimeCarriesGoalGovernance 验证 GoalGovernanceView 进入
// SessionRuntime 槽并随 clone 深拷贝（前端快照字段归属）。
func TestSessionRuntimeCarriesGoalGovernance(t *testing.T) {
	runtime := RuntimeState{
		GoalGovernance: &dto.GoalGovernanceView{Active: true, GoalID: "g-1", HeartbeatSeq: 7},
	}
	cloned := cloneRuntimeState(runtime)
	if cloned.GoalGovernance == nil || cloned.GoalGovernance.GoalID != "g-1" {
		t.Fatalf("clone 丢失治理视图: %+v", cloned.GoalGovernance)
	}
	cloned.GoalGovernance.HeartbeatSeq = 8
	if runtime.GoalGovernance.HeartbeatSeq != 7 {
		t.Fatalf("治理视图应深拷贝（互不影响）: %+v", runtime.GoalGovernance)
	}
	session := sessionRuntimeOf(cloned)
	if session.GoalGovernance == nil || session.GoalGovernance.HeartbeatSeq != 8 {
		t.Fatalf("sessionRuntimeOf 应携带治理视图: %+v", session.GoalGovernance)
	}
}
