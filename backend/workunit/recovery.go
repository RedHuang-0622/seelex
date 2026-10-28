package workunit

// recovery.go 是**恢复链的判据面**（P21-16「域方法随类型走」）：一条持久记录该按哪一层恢复、
// 分派之后怎么跑、以及"恢复说明"的容器。
//
// 落点沿革：从 `backend/engine/assemble/{runtime_unit_recovery.go,resume_notes.go}` 归位——
// plan §2.7 把 `runtime_unit_recovery.go` 与 `seelebridge/resume_notes.go` 都指向 workunit 域
// （后者明确写着"与 `workunit.RecoveryNote` 正文同处"）。装配层只留**两层策略各自的恢复动作**
// （现场登记 / 详情数据面 / 子代理树——那是宿主组件），判据在这里。
//
// 为什么恢复侧必须是策略、写侧却是责任链：两条链**要读的东西、要恢复的内容确实不同**——
//   - subagent：恢复"崩溃遗留的节点"（详情数据面 + 子代理树节点 + 现场登记），跑完即清；
//   - teammate：恢复**团队现场**（Work Item 级 worktree + 角色会话"跑到哪"），它的记录是
//     运行期落盘的正本，**绝不是子代理树上的节点**。
//
// 只有一条链的代价是实打实的：两种记录共用同一个 `subagents/` 目录，于是 teammate 的单元记录
// 被当成"崩溃遗留的子代理节点"恢复——工作表格上因此长出一条
// `subagent:<role>-<itemID> interrupted` 的假行（它的真身是团队现场）。记录侧的身份
// （`NodeUnitRecord`）就是为了让这里有判据可用：**不猜形状，读记录自己写的身份**。

import (
	"strings"
	"sync"

	storageworkunit "github.com/RedHuang-0622/seelex/store/workunit"
)

// KindOf 读出这条记录属于哪一层（**唯一转换点**：快照里的 wire 串 → 契约枚举）。
//
// 两段判据，顺序是判据的一部分：
//  1. 快照里写明的身份（新记录一律写明；写侧见 `SubagentUnitRecordLink` 与 teammate 那一环）；
//  2. 加固之前写下的老记录没有身份那一格：按**团队事实**兜底——nodeID 落在本会话的团队现场
//     名单（计划 + 绑定账本，由调用方给）里，它就是一个 teammate 单元。这不是"猜 nodeID 的
//     形状"：那份名单是"这个 nodeID 属于谁"的正本。
//
// 第二返回值是**认不得的身份词**（非空 = 记录写了身份但本版本不认识）：调用方负责留痕，
// 归类仍按既有链（子代理）。之所以把留痕交给调用方：域包不做日志输出。
func KindOf(record storageworkunit.Record, teamScenes map[string]bool) (Kind, string) {
	switch kind := Kind(strings.TrimSpace(record.Unit.Kind)); kind {
	case KindSubagent, KindTeammate:
		return kind, ""
	}
	if word := strings.TrimSpace(record.Unit.Kind); word != "" {
		return KindSubagent, word
	}
	if teamScenes[strings.TrimSpace(record.NodeID)] {
		return KindTeammate, ""
	}
	return KindSubagent, ""
}

// RecoveryStrategy 是恢复链上的一格策略：认哪些记录、把它们恢复成什么。
type RecoveryStrategy interface {
	// Kind 报告本策略管哪一层（诊断读数与用例的判据）。
	Kind() Kind
	// Restore 恢复本策略**认得的那一批**记录（空批 = 幂等的无事路径）。
	Restore(records []storageworkunit.Record)
}

// RestoreDeps 是分派点的宿主读数与动作。
type RestoreDeps struct {
	// SceneNodes 返回本会话的**团队现场名单**（nodeID 列表；"这个 nodeID 属于谁"的正本）。
	// 只在老记录缺身份那一格时用得到；nil = 没有名单（老记录一律按子代理算）。
	SceneNodes func(sessionID string) []string
	// Strategies 是各层策略，**顺序固定**（子代理在前、teammate 在后）：它决定读数与日志的
	// 次序，调用方还依赖"全部策略跑完"这一事实。
	Strategies []RecoveryStrategy
	// Log 留痕（nil = 不输出）。
	Log func(format string, args ...any)
}

// RestoreUnits 是恢复链的**分派点**：先按身份把记录切开，再交给各自的策略。
func RestoreUnits(sessionID string, records []storageworkunit.Record, deps RestoreDeps) {
	if len(records) == 0 || len(deps.Strategies) == 0 {
		return
	}
	scenes := make(map[string]bool, 4)
	if deps.SceneNodes != nil {
		for _, nodeID := range deps.SceneNodes(sessionID) {
			scenes[nodeID] = true
		}
	}
	owned := make(map[Kind][]storageworkunit.Record, 2)
	for _, record := range records {
		kind, unknown := KindOf(record, scenes)
		if unknown != "" && deps.Log != nil {
			deps.Log("seelex: 记录 %q 的身份词未知（%q），按 subagent 那一层恢复", record.NodeID, unknown)
		}
		owned[kind] = append(owned[kind], record)
	}
	for _, strategy := range deps.Strategies {
		batch := owned[strategy.Kind()]
		if len(batch) == 0 {
			continue
		}
		strategy.Restore(batch)
		if deps.Log != nil {
			deps.Log("seelex: 恢复 %s 记录 %d 条（会话 %s）", strategy.Kind(), len(batch), sessionID)
		}
	}
}

// ResumeNotes 是恢复说明的容器：键 → 说明，**读完即消**（恢复说明是"这件事是中断后续跑的"
// 这一刻的事实，不是长期人格设定）。
//
// 容器只有这一份（此前 subagent 与 teammate 各写一遍同形实现）：**"键是什么"留在调用点**
// ——subagent 的键是节点 id（节点装配 PromptBlocks 时读）、teammate 的键是角色会话号
// （worker 回合装配系统提示时读）；键语义是各层的事实，不在这里猜。
// 正文（说明写什么）也只有一份：`RecoveryNote`。
//
// 零值可用（map 惰性建）。并发安全。
type ResumeNotes struct {
	mu    sync.Mutex
	notes map[string]string
}

// Set 记下某个键下一次装配要带的恢复说明（空键 / 空说明 = 不记，幂等）。
func (n *ResumeNotes) Set(key, note string) {
	if n == nil {
		return
	}
	key = strings.TrimSpace(key)
	note = strings.TrimSpace(note)
	if key == "" || note == "" {
		return
	}
	n.mu.Lock()
	if n.notes == nil {
		n.notes = map[string]string{}
	}
	n.notes[key] = note
	n.mu.Unlock()
}

// Take 取走某个键的恢复说明并**清掉**（一次装配读完即消）。
func (n *ResumeNotes) Take(key string) string {
	if n == nil {
		return ""
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return ""
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	note := n.notes[key]
	delete(n.notes, key)
	return note
}

// Peek 读回某个键的恢复说明但**不清掉**（非消费读：装配期间同一次提示可能被读多次）。
func (n *ResumeNotes) Peek(key string) string {
	if n == nil {
		return ""
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return ""
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.notes[key]
}

// Clear 丢掉某个键还没被读走的恢复说明（收口清会话内容时用：会话内容都不在了，
// 说明也没有落点）。
func (n *ResumeNotes) Clear(key string) {
	if n == nil {
		return
	}
	n.mu.Lock()
	delete(n.notes, strings.TrimSpace(key))
	n.mu.Unlock()
}
