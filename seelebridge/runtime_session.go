package seelebridge

import (
	"strings"

	"github.com/RedHuang-0622/seelex/seelexctx/snapshot"
)

// 主会话与子代理 merge-back 的会话侧内部方法。公开会话端口见 ports.go。

// enqueueSubagentContext is the subagent-to-main merge-back write. It is
// delegated to the subagentContext actor: the command channel is bounded and
// never blocks the producer for long, and the actor queue preserves every
// message until Drain（2026-08-10 A/B 修复：mailbox 满不再静默丢弃）。
func (r *Runtime) enqueueSubagentContext(content string) {
	if r == nil || strings.TrimSpace(content) == "" {
		return
	}
	r.subagentContext.Enqueue(content)
}

// DrainSubagentContexts returns all currently queued merge-back messages. It
// never waits for a producer or Application lock and is called before a main
// ChatStream starts.
func (r *Runtime) DrainSubagentContexts() []string {
	if r == nil {
		return nil
	}
	return r.subagentContext.Drain()
}

// subagentContextDropped 返回 merge-back 队列溢出计数（诊断面）。
func (r *Runtime) subagentContextDropped() int64 {
	if r == nil {
		return 0
	}
	return r.subagentContext.Overflow()
}

// mergeBackIntoParent 把子代理快照合并进 Runtime 持有的父证据，并把合并
// 结果原子写回 parentEvidence（后续子代理/嵌套 fork 因此能看到先前子代理
// 的 Findings/Decisions——合并累积）。合并由 subagentContext actor 串行执行，
// 防止并行子代理各自基于旧快照合并后互相覆盖（2026-08-10 B 修复）。
func (r *Runtime) mergeBackIntoParent(child *snapshot.ContextSnapshot) *snapshot.ContextSnapshot {
	if r == nil || child == nil {
		return nil
	}
	return r.subagentContext.MergeBackIntoParent(child)
}
