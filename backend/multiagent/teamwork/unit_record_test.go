package teamwork

// unit_record_test.go 是 teammate 单元记录写侧口径的域级用例（P21-34：随实现从
// `backend/engine/assemble/workunit_team.go` 搬进本域）。

import (
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/backend/workunit"
	"github.com/RedHuang-0622/seelex/contract/dto"
	storeworkunit "github.com/RedHuang-0622/seelex/store/workunit"
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

// TestUnitRecordKeyForFillsIdentity：记录身份从作业载荷取——nodeID 口径见 UnitNodeID、
// 四个标量 trim、Plugins 拷贝（不共享底层切片）。
func TestUnitRecordKeyForFillsIdentity(t *testing.T) {
	request := WorkerRequest{
		Role: " exec ", RoleSessionID: " role-exec ", Goal: " 实现渲染件 ",
		WorkItemID: "wi-1", Worktree: WorkItemWorktreeName("exec", "wi-1"),
		Plugins: []string{"cad"},
	}
	key := UnitRecordKeyFor(request)
	if key.NodeID != "exec-wi-1" || key.RoleSessionID != "role-exec" || key.Goal != "实现渲染件" || key.Role != "exec" {
		t.Fatalf("记录身份 = %+v", key)
	}
	if len(key.Plugins) != 1 || key.Plugins[0] != "cad" {
		t.Fatalf("插件集合应当原样搬运：%+v", key.Plugins)
	}
	request.Plugins[0] = "mutated"
	if key.Plugins[0] != "cad" {
		t.Fatal("插件集合必须拷贝（不能与载荷共享底层切片）")
	}
	if key.SystemPrompt != "" {
		t.Fatalf("SystemPrompt 由装配时刻补，不该在这里填：%q", key.SystemPrompt)
	}
}

// TestTeammateUnitRecordLinkWritesTheIdentity：写链上 teammate 那一环把 Kind/Role/Plugins/
// SystemPrompt 写进记录（其余字段原样）；身份块是拷贝（改 key 不影响已产出的 link 行为）。
func TestTeammateUnitRecordLinkWritesTheIdentity(t *testing.T) {
	key := UnitRecordKey{Role: " exec ", Plugins: []string{"cad"}, SystemPrompt: "系统提示"}
	link := TeammateUnitRecordLink(key)
	record := storeworkunit.Record{NodeID: "exec-wi-1", Status: UnitStatusRunning}
	got := link(record)
	if got.NodeID != "exec-wi-1" || got.Status != UnitStatusRunning {
		t.Fatalf("身份环不该动别的字段：%+v", got)
	}
	if got.Unit.Kind != string(workunit.KindTeammate) || got.Unit.Role != "exec" ||
		got.Unit.SystemPrompt != "系统提示" || len(got.Unit.Plugins) != 1 || got.Unit.Plugins[0] != "cad" {
		t.Fatalf("身份块 = %+v", got.Unit)
	}
	key.Plugins[0] = "mutated"
	if strings.Join(got.Unit.Plugins, ",") != "cad" {
		t.Fatal("身份块应当拷贝插件集合")
	}
}

// TestUnitRecordCarriesTheRoundFacts：记录形状只在域内拼一次——作用域、身份、状态、
// 摘要裁剪、阶段编码与现场四栏都必须落到同一条记录上。
func TestUnitRecordCarriesTheRoundFacts(t *testing.T) {
	key := UnitRecordKey{NodeID: " exec-wi-1 ", RoleSessionID: " role-1 ", Goal: " 目标 "}
	worktree := storeworkunit.WorktreeRecord{Path: "G:/tmp/wt", Branch: "b", MainBranch: "main", BaseCommit: "abc"}
	record := UnitRecord(key, " main-1 ", UnitStatusRunning, "  产出预览  ", "round_output", worktree)
	if record.SchemaVersion != storeworkunit.SchemaVersion || record.NodeID != "exec-wi-1" ||
		record.SessionID != "role-1" || record.MainSessionID != "main-1" ||
		record.Goal != "目标" || record.Status != UnitStatusRunning {
		t.Fatalf("记录身份/作用域不对：%+v", record)
	}
	if record.Summary != "产出预览" {
		t.Fatalf("摘要应当按 clip 口径裁剪：%q", record.Summary)
	}
	if !strings.Contains(string(record.StagesJSON), "round_output") {
		t.Fatalf("阶段必须编码进 StagesJSON：%q", record.StagesJSON)
	}
	if record.Worktree != worktree {
		t.Fatalf("现场四栏必须整份落进记录：%+v", record.Worktree)
	}
}
