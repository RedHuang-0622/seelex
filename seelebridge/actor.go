package seelebridge

import (
	"strings"

	"github.com/RedHuang-0622/seelex/seelexctx"
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
type ParentEvidenceProjection struct {
	SessionID         string
	Goal              string
	ConversationCount int
}

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
	if strings.TrimSpace(projection.SessionID) == "" {
		r.parentEvidence.Store(nil)
		return
	}
	evidence := seelexctx.ExportSnapshotFromData(
		projection.SessionID,
		projection.Goal,
		projection.ConversationCount,
		r.Tracer(),
	)
	r.parentEvidence.Store(evidence)
}

// enqueueSubagentContext is the subagent-to-main nonblocking mailbox write.
// A full mailbox never blocks a subagent; the bounded drop count is observable
// for diagnostics and prevents a stalled main session from exhausting memory.
func (r *Runtime) enqueueSubagentContext(content string) {
	if r == nil || strings.TrimSpace(content) == "" {
		return
	}
	select {
	case r.subagentMailbox <- content:
	default:
		r.subagentDropped.Add(1)
	}
}

// DrainSubagentContexts returns all currently queued merge-back messages. It
// never waits for a producer or Application lock and is called before a main
// ChatStream starts.
func (r *Runtime) DrainSubagentContexts() []string {
	if r == nil {
		return nil
	}
	items := make([]string, 0, len(r.subagentMailbox))
	for {
		select {
		case content := <-r.subagentMailbox:
			items = append(items, content)
		default:
			return items
		}
	}
}

func (r *Runtime) subagentContextDropped() int64 {
	if r == nil {
		return 0
	}
	return r.subagentDropped.Load()
}

func cloneContextSnapshot(source *snapshot.ContextSnapshot) *snapshot.ContextSnapshot {
	if source == nil {
		return nil
	}
	copy := *source
	copy.Findings = append([]string(nil), source.Findings...)
	copy.Constraints = append([]string(nil), source.Constraints...)
	copy.PendingWork = append([]string(nil), source.PendingWork...)
	copy.Decisions = make([]snapshot.Decision, len(source.Decisions))
	for index, decision := range source.Decisions {
		copy.Decisions[index] = decision
		copy.Decisions[index].Alternatives = append([]string(nil), decision.Alternatives...)
	}
	if source.Escape != nil {
		escape := *source.Escape
		copy.Escape = &escape
	}
	return &copy
}
