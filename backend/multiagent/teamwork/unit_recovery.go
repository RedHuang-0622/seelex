package teamwork

// unit_recovery.go 是**中断判定**的判据面（P21-36「域方法随类型走」：从
// `backend/engine/assemble/workunit_team_records.go` 的 `RecoverTeamworkUnits` 搬进本域；
// plan §2.7 把 `workunit_*.go` 列为该目标）。
//
// 它只回答一个问题：**哪些单元"记录说在跑、而本进程里已经没有它的执行面"**——那正是中断
// （可重派 / 待人工处置）。判据两条来源、一次去重：
//   - 记录侧：`Progress.InFlight` 为真、且执行面不在（角色会话句柄与作业句柄都没有）；
//   - 账本侧：团队账本报"状态说在跑、句柄却不在册"的工作项——**记录缺席**时也不至于漏掉
//     这一件可重派的工作（有执行面的仍不算中断：它现在就在本进程里）。
//
// 顺带按记录生成恢复说明（键 = 角色会话号，正文来自 `workunit.RecoveryNote` 唯一构建器）：
// 恢复说明是"这件事是中断后续跑的"这一刻的事实，读一次即消，由宿主在下一次装配时读走。

import (
	"github.com/RedHuang-0622/seelex/backend/workunit"
	"github.com/RedHuang-0622/seelex/store"
)

// UnitRecovery 是一次中断判定的结果。
type UnitRecovery struct {
	// Interrupted 是中断单元的 nodeID（去重保序：先记录侧、后账本侧）。
	Interrupted []string
	// Notes 是"角色会话号 → 恢复说明"（只给中断且有记录的单元）。
	Notes map[string]string
}

// InterruptedUnits 判定中断单元并生成恢复说明。
//
// surfaceAlive 由宿主给（角色会话句柄还在册 / 作业句柄还在册）；nil = 没有执行面读数
// （按"都不在"处理——保守地判中断，调用方拿到的可重派清单只会多不会少，绝不会把在跑的
// 当成中断去重派）。
func InterruptedUnits(
	index []SceneEntry,
	progress map[string]workunit.Progress,
	stored map[string]store.NodeSessionRecord,
	ledgerInterrupted []string,
	surfaceAlive func(entry SceneEntry, roleSessionID string) bool,
) UnitRecovery {
	recovery := UnitRecovery{Notes: map[string]string{}}
	seen := make(map[string]bool, len(index))
	addInterrupted := func(nodeID string) {
		if nodeID == "" || seen[nodeID] {
			return
		}
		seen[nodeID] = true
		recovery.Interrupted = append(recovery.Interrupted, nodeID)
	}
	alive := func(entry SceneEntry, roleSessionID string) bool {
		return surfaceAlive != nil && surfaceAlive(entry, roleSessionID)
	}
	for _, entry := range index {
		unit, ok := progress[entry.NodeID]
		if !ok || !unit.InFlight {
			continue
		}
		if alive(entry, unit.SessionID) {
			continue
		}
		addInterrupted(entry.NodeID)
		// 恢复说明：事实来自记录本身（不编造），注入该角色会话的下一次装配。
		if record, found := stored[entry.NodeID]; found {
			recovery.Notes[unit.SessionID] = workunit.RecoveryNote(workunit.KindTeammate, record)
		}
	}
	// 账本侧的同一条事实（状态说在跑、句柄却不在册）也一并列出：**记录缺席**时不至于漏掉
	// 这一件可重派的工作。有执行面的（角色会话还在跑）不算中断——它现在就在本进程里。
	byItem := make(map[string]SceneEntry, len(index))
	for _, entry := range index {
		if entry.WorkItem != "" {
			byItem[entry.WorkItem] = entry
		}
	}
	for _, itemID := range ledgerInterrupted {
		entry, ok := byItem[itemID]
		if !ok {
			continue
		}
		if unit, has := progress[entry.NodeID]; has && alive(entry, unit.SessionID) {
			continue
		}
		addInterrupted(entry.NodeID)
	}
	if len(recovery.Interrupted) == 0 {
		recovery.Interrupted = nil
	}
	if len(recovery.Notes) == 0 {
		recovery.Notes = nil
	}
	return recovery
}
