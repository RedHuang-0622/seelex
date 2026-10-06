package seelebridge

// runtime_unit_recovery.go — 恢复链的**策略面**：一条持久记录该按哪一层恢复，由记录快照里的
// **身份**（`sessionstore.NodeUnitRecord.Kind`）决定，而不是由 nodeID 的拼法去猜。
//
// 为什么恢复侧必须是策略、写侧却是责任链：两条链**要读的东西、要恢复的内容确实不同**——
//   - subagent：恢复"崩溃遗留的节点"（详情数据面 + 子代理树节点 + 现场登记），跑完即清；
//   - teammate：恢复**团队现场**（Work Item 级 worktree + 角色会话"跑到哪"），它的记录是
//     运行期落盘的正本，**绝不是子代理树上的节点**。
//
// 只有一条链的代价是实打实的：两种记录共用同一个 `subagents/` 目录，于是 teammate 的单元记录
// 被当成"崩溃遗留的子代理节点"恢复——工作表格上因此长出一条 `subagent:<role>-<itemID>
// interrupted` 的假行（它的真身是团队现场）。记录侧的身份（`NodeUnitRecord`）就是为了让这里
// 有判据可用：**不猜形状，读记录自己写的身份**。

import (
	"log"
	"strings"

	"github.com/RedHuang-0622/seelex/seelebridge/workunit"
	"github.com/RedHuang-0622/seelex/sessionstore"
)

// unitRecoveryStrategy 是恢复链上的一格策略：认哪些记录、把它们恢复成什么。
type unitRecoveryStrategy interface {
	// Kind 报告本策略管哪一层（诊断读数与用例的判据）。
	Kind() workunit.Kind
	// Restore 恢复本策略**认得的那一批**记录（空批 = 幂等的无事路径）。
	Restore(records []sessionstore.NodeSessionRecord)
}

// subagentRecovery 恢复子代理层：详情数据面 + 子代理树 + 现场登记。
//
// 树那一格仍带 `recordBelongsToCurrentMain` 谓词（本进程还活着的节点不许被记录快照覆盖成
// "中断"）；现场登记仍必须早于 `Prune`（顺序是判据的一部分，见调用方）。
type subagentRecovery struct {
	runtime   *Runtime
	sessionID string
}

func (s subagentRecovery) Kind() workunit.Kind { return workunit.KindSubagent }

func (s subagentRecovery) Restore(records []sessionstore.NodeSessionRecord) {
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

func (t teamUnitRecovery) Restore(records []sessionstore.NodeSessionRecord) {
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

// unitKindOf 读出这条记录属于哪一层（**唯一转换点**：快照里的 wire 串 → 契约枚举）。
//
// 两段判据，顺序是判据的一部分：
//  1. 快照里写明的身份（新记录一律写明；写侧见 `SubagentUnitRecordLink` 与 teammate 那一环）；
//  2. 加固之前写下的老记录没有身份那一格：按**团队事实**兜底——nodeID 落在本会话的团队现场
//     名单（计划 + 绑定账本，见 `teamSceneIndex`）里，它就是一个 teammate 单元。这不是"猜
//     nodeID 的形状"：那份名单是"这个 nodeID 属于谁"的正本。
//
// 认不得的词（将来加了新层而这里没跟上）不静默归类：按既有链（子代理）恢复并留痕。
func (r *Runtime) unitKindOf(record sessionstore.NodeSessionRecord, teamScenes map[string]bool) workunit.Kind {
	switch kind := workunit.Kind(strings.TrimSpace(record.Unit.Kind)); kind {
	case workunit.KindSubagent, workunit.KindTeammate:
		return kind
	}
	if word := strings.TrimSpace(record.Unit.Kind); word != "" {
		log.Printf("seelebridge: 记录 %q 的身份词未知（%q），按 subagent 那一层恢复", record.NodeID, word)
		return workunit.KindSubagent
	}
	if teamScenes[strings.TrimSpace(record.NodeID)] {
		return workunit.KindTeammate
	}
	return workunit.KindSubagent
}

// restoreUnitRecords 是恢复链的**分派点**：先按身份把记录切开，再交给各自的策略。
func (r *Runtime) restoreUnitRecords(sessionID string, records []sessionstore.NodeSessionRecord) {
	if r == nil || len(records) == 0 {
		return
	}
	scenes := make(map[string]bool, 4)
	for _, entry := range r.teamSceneIndex(sessionID) {
		scenes[entry.NodeID] = true
	}
	owned := make(map[workunit.Kind][]sessionstore.NodeSessionRecord, 2)
	for _, record := range records {
		kind := r.unitKindOf(record, scenes)
		owned[kind] = append(owned[kind], record)
	}
	for _, strategy := range r.unitRecoveryStrategies(sessionID) {
		batch := owned[strategy.Kind()]
		if len(batch) == 0 {
			continue
		}
		strategy.Restore(batch)
		log.Printf("seelebridge: 恢复 %s 记录 %d 条（会话 %s）", strategy.Kind(), len(batch), sessionID)
	}
}
