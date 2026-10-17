package teamwork

// unit_record_test.go 是 teammate 单元记录写侧口径的域级用例（P21-34：随实现从
// `backend/engine/assemble/workunit_team.go` 搬进本域）。

import (
	"testing"

	"github.com/RedHuang-0622/seelex/backend/workunit"
	"github.com/RedHuang-0622/seelex/contract/dto"
)

// TestUnitStatusWordsComeFromTheContract：记录那一格的词只能来自对外契约（记录词 = wire 词）——
// "在跑"复用 workunit 的词，两个终态来自 dto.SubAgent*。
func TestUnitStatusWordsComeFromTheContract(t *testing.T) {
	if UnitStatusRunning != workunit.StatusRunning.String() {
		t.Fatalf("在跑的词应当复用契约：%q", UnitStatusRunning)
	}
	if UnitStatusDone != dto.SubAgentDone.String() || UnitStatusFailed != dto.SubAgentFailed.String() {
		t.Fatalf("终态词应当来自 dto.SubAgent*：%q / %q", UnitStatusDone, UnitStatusFailed)
	}
}

// TestUnitStatusForOnlyFailedIsDead：收尾分类 → 记录终态：**只有判死那一类记 failed**；
// 未提交 / 主工作区挡路都记 done（这一轮跑完了、产出还在现场），已落定也记 done。
func TestUnitStatusForOnlyFailedIsDead(t *testing.T) {
	cases := map[workunit.OutcomeKind]string{
		workunit.OutcomeSettled:      UnitStatusDone,
		workunit.OutcomeUncommitted:  UnitStatusDone,
		workunit.OutcomeMergeBlocked: UnitStatusDone,
		workunit.OutcomeFailed:       UnitStatusFailed,
	}
	for kind, want := range cases {
		if got := UnitStatusFor(kind); got != want {
			t.Fatalf("UnitStatusFor(%v) = %q, want %q", kind, got, want)
		}
	}
}

// TestUnitNodeIDFollowsTheSceneNaming：单元身份——有独立现场时取指派名去前缀；
// 降级共享工作区（没有指派名）时按 `<role>-<itemID>` 拼；连工作项都没有就退到角色名。
func TestUnitNodeIDFollowsTheSceneNaming(t *testing.T) {
	withScene := WorkerRequest{Role: "exec", WorkItemID: "wi-1", Worktree: WorkItemWorktreeName("exec", "wi-1")}
	if got := UnitNodeID(withScene); got != "exec-wi-1" {
		t.Fatalf("有现场时取指派名去前缀：%q", got)
	}
	shared := WorkerRequest{Role: "exec", WorkItemID: " wi-2 "}
	if got := UnitNodeID(shared); got != "exec-wi-2" {
		t.Fatalf("共享工作区时按角色 + 工作项拼：%q", got)
	}
	roleOnly := WorkerRequest{Role: " pm "}
	if got := UnitNodeID(roleOnly); got != "pm" {
		t.Fatalf("没有工作项时退到角色名（trim）：%q", got)
	}
	if got := UnitNodeID(WorkerRequest{}); got != "" {
		t.Fatalf("什么都没有时回空串：%q", got)
	}
}
