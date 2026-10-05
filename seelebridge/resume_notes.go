package seelebridge

// resume_notes.go — 「恢复说明」的**唯一一份容器**（两层共用）。
//
// 恢复说明这件事此前写了两遍，形状一模一样、语义一模一样（源码注释里两边都自称"与另一份
// 同形"）：`runtime_subagent_resume.go` 的 `subagentResumeState.notes` 与
// `workunit_team_records.go` 的 `teamResumeState.notes`——都是 `map[string]string` + 一把锁 +
// set/取走即消/清三个动作，各自实现。
//
// 收口之后：**容器只有这一份**，"键是什么"这件事留在调用点（subagent 的键是节点 id、在节点
// 装配 PromptBlocks 时读；teammate 的键是角色会话号、在 worker 回合装配系统提示时读）——
// 键语义是各层的事实，不在这里猜。
//
// 正文（说明写什么）也只有一份：`workunit.RecoveryNote`。本文件只管"放了、取走即消、丢掉"。

import (
	"strings"
	"sync"
)

// resumeNotes 是恢复说明的容器：键 → 说明，**读完即消**（恢复说明是"这件事是中断后续跑的"
// 这一刻的事实，不是长期人格设定）。
//
// 零值可用（map 惰性建）。并发安全。
type resumeNotes struct {
	mu    sync.Mutex
	notes map[string]string
}

// set 记下某个键下一次装配要带的恢复说明（空键 / 空说明 = 不记，幂等）。
func (n *resumeNotes) set(key, note string) {
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

// take 取走某个键的恢复说明并**清掉**（一次装配读完即消）。
func (n *resumeNotes) take(key string) string {
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

// peek 读回某个键的恢复说明但**不清掉**（非消费读：装配期间同一次提示可能被读多次）。
func (n *resumeNotes) peek(key string) string {
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

// clear 丢掉某个键还没被读走的恢复说明（收口清会话内容时用：会话内容都不在了，
// 说明也没有落点）。
func (n *resumeNotes) clear(key string) {
	if n == nil {
		return
	}
	n.mu.Lock()
	delete(n.notes, strings.TrimSpace(key))
	n.mu.Unlock()
}
