package seelebridge

import (
	"strings"

	"github.com/RedHuang-0622/seelex/seelebridge/internal/model"
	"github.com/RedHuang-0622/seelex/seelexctx/snapshot"
)

// ── Actor 模型消息边界 ────────────────────────────────────────
// seelex 的并发语义：每个会话是独立 actor（自己的锁 = 自己的状态），actor
// 之间不共享状态、不竞争锁，只交换消息（值对象）。
//
// RuntimeVisibilityProjection is an immutable application-to-runtime message.
// Runtime owns the received value and never calls back into Application to read
// visibility state while a tool hook is executing.
type RuntimeVisibilityProjection struct {
	GoalSkillActive bool
}

// ParentEvidenceProjection is the minimal application-owned data needed for a
// Runtime-local parent evidence snapshot. Runtime adds its own telemetry and
// stores the resulting immutable snapshot for subagents to read.
type ParentEvidenceProjection = model.ParentEvidenceProjection

// The parent/child boundary is intentionally one-way in both directions:
// Application publishes immutable parent evidence to Runtime; Runtime owns a
// bounded merge-back mailbox that Application drains outside service.mu.
//
// 死锁教训（2026-08-02 冒烟实测）：plan_run 作为主代理的工具调用在
// Session.ChatStream 内同步执行（主会话锁全程持有），任何跨 actor 直接访问
// 主会话状态（History/AppendHistory）都会形成循环等待（持锁者等待自己的
// 工作）。因此本通道的实现必须遵守"状态私有、消息进出"。
// SetRuntimeVisibilityProjection publishes a value copy from Application. It
// has no synchronous reverse callback and is safe from Runtime tool hooks.
func (r *Runtime) SetRuntimeVisibilityProjection(projection RuntimeVisibilityProjection) {
	if r == nil {
		return
	}
	copy := projection
	r.visibilityProjection.Store(&copy)
}

// SetParentEvidenceProjection turns the application projection into a
// Runtime-owned immutable snapshot. A blank session clears stale evidence.
func (r *Runtime) SetParentEvidenceProjection(projection ParentEvidenceProjection) {
	if r == nil {
		return
	}
	r.subagentContext.SetParentEvidenceProjection(projection)
}

// enqueueSubagentContext is the subagent-to-main merge-back write. It is
// delegated to the subagentContext actor: the command channel is bounded and
// never blocks the producer for long, and the actor queue preserves every
// message until Drain (2026-08-10 A/B 修复：mailbox 满不再静默丢弃）。
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

func (r *Runtime) subagentContextDropped() int64 {
	if r == nil {
		return 0
	}
	return r.subagentContext.Overflow()
}

// mergeBackIntoParent 把子代理快照合并进 Runtime 持有的父证据，并把合并
// 结果原子写回 parentEvidence（后续子代理/嵌套 fork 因此能看到先前子代理
// 的 Findings/Decisions——合并累积）。合并由 subagentContext actor 串行执行，
// 防止并发子代理各自基于旧快照合并后互相覆盖（2026-08-10 B 修复）。
// 返回合并后的快照（调用方可 Format 后入 mailbox 供主会话注入）。
func (r *Runtime) mergeBackIntoParent(child *snapshot.ContextSnapshot) *snapshot.ContextSnapshot {
	if r == nil || child == nil {
		return nil
	}
	return r.subagentContext.MergeBackIntoParent(child)
}
