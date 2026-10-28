package assemble

// runtime_unit_recovery.go — 恢复链的**两层策略本体**（装配面）。
//
// 判据（记录身份 → 哪一层、"认不得的词不静默归类"、分派次序、留痕）在契约包
// [`backend/workunit`](../../workunit/README.md) 的 `recovery.go`（P21-16 归位）；
// 本文件只剩两层策略各自**要对宿主做的那几件事**：恢复详情数据面 / 子代理树 / 现场登记。
// 策略差异只允许出现在本文件的装配与调用点上（AGENTS §8.5）。

import (
	"log"

	"github.com/RedHuang-0622/seelex/backend/workunit"
	storeworkunit "github.com/RedHuang-0622/seelex/store/workunit"
)

// unitRecoveryStrategy 是契约包里那一格策略接口的**同身份别名**（既有调用点与用例零改动）。
type unitRecoveryStrategy = workunit.RecoveryStrategy

// subagentRecovery 恢复子代理层：详情数据面 + 子代理树 + 现场登记。
//
// 树那一格仍带 `recordBelongsToCurrentMain` 谓词（本进程还活着的节点不许被记录快照覆盖成
// "中断"）；现场登记仍必须早于 `Prune`（顺序是判据的一部分，见调用方）。
type subagentRecovery struct {
	runtime   *Runtime
	sessionID string
}

func (s subagentRecovery) Kind() workunit.Kind { return workunit.KindSubagent }

func (s subagentRecovery) Restore(records []storeworkunit.Record) {
	if len(records) == 0 || s.runtime == nil {
		return
	}
	r := s.runtime
	if r.subagentSessions != nil {
		r.subagentSessions.Restore(records)
	}
	if r.subagentTree != nil {
		r.subagentTree.Restore(records, s.sessionID, r.recordBelongsToCurrentMain)
	}
	if r.worktreeMgr != nil {
		r.worktreeMgr.Restore(records)
	}
}

// teamUnitRecovery 恢复 teammate 层：**只有现场登记**——记录自带现场四栏（写侧写全，见
// `teamUnitWorktreeRecord`），所以这一层不再需要"计划 + 账本"那一路才能拿到收尾要用的两个
// 事实（`MainBranch`/`BaseCommit`）；后者仍然保留在恢复链上（记录可能已经不在了，而现场还在）。
//
// 刻意**不做**的两件事：
//   - 不进子代理树（它不是子代理节点——这正是本文件存在的理由）；
//   - 不恢复会话内容：角色会话是**进程内执行面**（见 `roleSessionLive`），重启即空，它的
//     "跑到哪"由记录 + 计划回答（`teamUnitReader` / `workunit.UnitReader`），下一次派发时
//     复用同一个角色会话号重建。
type teamUnitRecovery struct {
	runtime   *Runtime
	sessionID string
}

func (t teamUnitRecovery) Kind() workunit.Kind { return workunit.KindTeammate }

func (t teamUnitRecovery) Restore(records []storeworkunit.Record) {
	if len(records) == 0 || t.runtime == nil || t.runtime.worktreeMgr == nil {
		return
	}
	t.runtime.worktreeMgr.Restore(records)
}

// unitRecoveryStrategies 返回本会话的恢复策略。顺序固定（子代理在前、teammate 在后）：
// 它决定读数与日志的次序，也保证两种策略都在 `Prune` 之前跑完。
func (r *Runtime) unitRecoveryStrategies(sessionID string) []unitRecoveryStrategy {
	return []unitRecoveryStrategy{
		subagentRecovery{runtime: r, sessionID: sessionID},
		teamUnitRecovery{runtime: r, sessionID: sessionID},
	}
}

// restoreUnitRecords 是恢复链的分派点（装配侧接线）：读团队现场名单 → 交契约包按身份切开 →
// 各层策略跑自己的恢复动作。
func (r *Runtime) restoreUnitRecords(sessionID string, records []storeworkunit.Record) {
	if r == nil || len(records) == 0 {
		return
	}
	workunit.RestoreUnits(sessionID, records, workunit.RestoreDeps{
		// 名单投影只有一份（`teamwork.SceneNodeIDs`，经 `teamSceneNodeIDs` 转发）：
		// 这里曾按 SceneEntry 手写一遍同形循环，与认领现场那条路各算一次。
		SceneNodes: r.teamSceneNodeIDs,
		Strategies: r.unitRecoveryStrategies(sessionID),
		Log:        log.Printf,
	})
}
