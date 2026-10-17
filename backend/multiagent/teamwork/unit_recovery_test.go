package teamwork

// unit_recovery_test.go 是**中断判定**的域级用例（P21-36：判据从
// `backend/engine/assemble/workunit_team_records.go` 的 `RecoverTeamworkUnits` 搬进本域）。

import (
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/backend/workunit"
	"github.com/RedHuang-0622/seelex/store"
)

func recoveryRecord(status string) store.NodeSessionRecord {
	return store.NodeSessionRecord{NodeID: "exec-wi-1", SessionID: "role-exec", Status: status, Goal: "实现渲染件"}
}

// TestInterruptedUnitsJudgesInFlightWithoutSurface：记录说在跑 + 执行面不在 ⇒ 中断并生成恢复说明；
// 记录不在跑 / 执行面还在 ⇒ 不算中断、也不生成说明。
func TestInterruptedUnitsJudgesInFlightWithoutSurface(t *testing.T) {
	index := []SceneEntry{{NodeID: "exec-wi-1", Role: "exec", WorkItem: "wi-1"}}

	running := map[string]workunit.Progress{
		"exec-wi-1": workunit.ProgressOf(workunit.KindTeammate, recoveryRecord(workunit.StatusRunning.String())),
	}
	stored := map[string]store.NodeSessionRecord{"exec-wi-1": recoveryRecord(workunit.StatusRunning.String())}

	got := InterruptedUnits(index, running, stored, nil, func(SceneEntry, string) bool { return false })
	if len(got.Interrupted) != 1 || got.Interrupted[0] != "exec-wi-1" {
		t.Fatalf("在跑且无执行面 ⇒ 中断：%+v", got.Interrupted)
	}
	note, ok := got.Notes["role-exec"]
	if !ok || !strings.HasPrefix(note, workunit.RecoveryNotePrefix) {
		t.Fatalf("中断单元必须带恢复说明（契约前缀族）：%q", note)
	}

	// 执行面还在 ⇒ 不中断（不把在跑的重派）。
	alive := InterruptedUnits(index, running, stored, nil, func(SceneEntry, string) bool { return true })
	if len(alive.Interrupted) != 0 || len(alive.Notes) != 0 {
		t.Fatalf("有执行面时不该判中断：%+v", alive)
	}
	// 记录已终结（done）⇒ 不中断，即使没有执行面。
	done := map[string]workunit.Progress{
		"exec-wi-1": workunit.ProgressOf(workunit.KindTeammate, recoveryRecord(UnitStatusDone)),
	}
	if got := InterruptedUnits(index, done, stored, nil, nil); len(got.Interrupted) != 0 {
		t.Fatalf("终态记录不该判中断：%+v", got.Interrupted)
	}
}

// TestInterruptedUnitsCoversLedgerSideWithoutRecord：账本报"状态说在跑、句柄不在册"的工作项，
// 即使**记录缺席**也要列成中断（可重派清单只会多不会少）；有执行面的仍然排除。
func TestInterruptedUnitsCoversLedgerSideWithoutRecord(t *testing.T) {
	index := []SceneEntry{{NodeID: "exec-wi-2", Role: "exec", WorkItem: "wi-2"}}

	got := InterruptedUnits(index, nil, nil, []string{"wi-2"}, nil)
	if len(got.Interrupted) != 1 || got.Interrupted[0] != "exec-wi-2" {
		t.Fatalf("账本侧的中断必须被列出：%+v", got.Interrupted)
	}
	if len(got.Notes) != 0 {
		t.Fatalf("记录缺席时没有说明可生成：%+v", got.Notes)
	}

	// 账本说中断、但角色会话还在跑 ⇒ 不算中断。
	running := map[string]workunit.Progress{
		"exec-wi-2": workunit.ProgressOf(workunit.KindTeammate, recoveryRecord(workunit.StatusRunning.String())),
	}
	excluded := InterruptedUnits(index, running, nil, []string{"wi-2"}, func(SceneEntry, string) bool { return true })
	if len(excluded.Interrupted) != 0 {
		t.Fatalf("有执行面时不该按账本判中断：%+v", excluded.Interrupted)
	}
	// 账本里的工作项不在现场名单里 ⇒ 忽略（不猜 nodeID）。
	if got := InterruptedUnits(index, nil, nil, []string{"wi-9"}, nil); len(got.Interrupted) != 0 {
		t.Fatalf("名单外的工作项不该被列出：%+v", got.Interrupted)
	}
}

// TestInterruptedUnitsDedupesAcrossSources：记录侧与账本侧指向同一个单元 ⇒ 只列一次（保序：
// 记录侧在前）。
func TestInterruptedUnitsDedupesAcrossSources(t *testing.T) {
	index := []SceneEntry{{NodeID: "exec-wi-1", Role: "exec", WorkItem: "wi-1"}}
	running := map[string]workunit.Progress{
		"exec-wi-1": workunit.ProgressOf(workunit.KindTeammate, recoveryRecord(workunit.StatusRunning.String())),
	}
	got := InterruptedUnits(index, running, nil, []string{"wi-1"}, nil)
	if len(got.Interrupted) != 1 {
		t.Fatalf("两个来源同一单元只列一次：%+v", got.Interrupted)
	}
}
