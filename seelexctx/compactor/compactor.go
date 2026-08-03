// Package compactor 提供基于 token 预算的上下文快照压缩。
//
// 三级压缩策略：
//   - 全量（Budget ≥ 500 或已满足预算）
//   - 摘要（Budget 200~499）
//   - 极简（Budget < 200）
//
// Token 估算使用 seelectx.EstimateTokens。
package compactor

import (
	"errors"
	"fmt"
	"unicode/utf8"

	"github.com/RedHuang-0622/Seele/seelectx"
	"github.com/RedHuang-0622/seelex/seelexctx/snapshot"
)

type Compactor struct{}

func NewCompactor() *Compactor { return &Compactor{} }

var ErrBudgetTooSmall = errors.New("compactor: budget cannot preserve the minimum safe snapshot")

// Compact 压缩快照到目标 token 预算内。返回新快照，不修改原始快照。
func (c *Compactor) Compact(snap *snapshot.ContextSnapshot, budget int) (*snapshot.ContextSnapshot, error) {
	if snap == nil {
		return nil, fmt.Errorf("compactor: nil snapshot")
	}
	if budget < 0 {
		budget = 0
	}
	if estimateTokens(minimumSnapshot(snap)) > budget {
		return nil, fmt.Errorf("%w: need at least %d tokens, have %d", ErrBudgetTooSmall, estimateTokens(minimumSnapshot(snap)), budget)
	}

	if full := c.fullSnapshot(snap); estimateTokens(full) <= budget {
		return full, nil
	}
	if budget >= 200 {
		if summary := fitSnapshotFields(c.summarySnapshot(snap), snap, budget); estimateTokens(summary) <= budget {
			return summary, nil
		}
	}

	compacted := fitMinimalSnapshot(snap, budget)
	if estimateTokens(compacted) > budget {
		return nil, fmt.Errorf("%w: need at least %d tokens, have %d", ErrBudgetTooSmall, estimateTokens(compacted), budget)
	}
	return compacted, nil
}

// ── 三级压缩 ──────────────────────────────────────────────────

func (c *Compactor) fullSnapshot(snap *snapshot.ContextSnapshot) *snapshot.ContextSnapshot {
	return cloneSnapshot(snap)
}

func (c *Compactor) summarySnapshot(snap *snapshot.ContextSnapshot) *snapshot.ContextSnapshot {
	cp := minimumSnapshot(snap)
	if len(snap.Decisions) > 0 {
		cp.Decisions = []snapshot.Decision{{
			What: fmt.Sprintf("%d decisions", len(snap.Decisions)),
			Why:  "details compacted",
		}}
	}
	if len(snap.Findings) > 0 {
		cp.Findings = []string{fmt.Sprintf("%d findings", len(snap.Findings))}
	}
	if len(snap.Constraints) > 0 {
		cp.Constraints = []string{fmt.Sprintf("%d constraints", len(snap.Constraints))}
	}
	if len(snap.PendingWork) > 0 {
		cp.PendingWork = []string{fmt.Sprintf("%d pending items", len(snap.PendingWork))}
	}
	return cp
}

func (c *Compactor) minimalSnapshot(snap *snapshot.ContextSnapshot) *snapshot.ContextSnapshot {
	return minimumSnapshot(snap)
}

func minimumSnapshot(snap *snapshot.ContextSnapshot) *snapshot.ContextSnapshot {
	cp := &snapshot.ContextSnapshot{
		SourceSessionID: snap.SourceSessionID,
		ExportedAt:      snap.ExportedAt,
		MessageCount:    snap.MessageCount,
		TokenEstimate:   snap.TokenEstimate,
	}
	if snap.Escape != nil {
		cp.Escape = &snapshot.EscapeInfo{Iterations: snap.Escape.Iterations, ParentGoal: snap.Escape.ParentGoal}
	}
	return cp
}

func fitMinimalSnapshot(snap *snapshot.ContextSnapshot, budget int) *snapshot.ContextSnapshot {
	return fitSnapshotFields(minimumSnapshot(snap), snap, budget)
}

func fitSnapshotFields(cp, snap *snapshot.ContextSnapshot, budget int) *snapshot.ContextSnapshot {
	remaining := budget - estimateTokens(cp)
	if remaining <= 0 {
		return cp
	}
	cp.Goal = truncateForToken(snap.Goal, remaining)
	remaining = budget - estimateTokens(cp)
	if remaining <= 0 {
		return cp
	}
	cp.Progress = truncateForToken(snap.Progress, remaining)
	remaining = budget - estimateTokens(cp)
	if remaining <= 0 || cp.Escape == nil {
		return cp
	}
	cp.Escape.Reason = truncateForToken(snap.Escape.Reason, remaining)
	remaining = budget - estimateTokens(cp)
	if remaining > 0 {
		cp.Escape.Message = truncateForToken(snap.Escape.Message, remaining)
	}
	return cp
}

// ── Token 估算 ────────────────────────────────────────────────

func estimateTokens(snap *snapshot.ContextSnapshot) int {
	t := 20 // 元数据开销
	t += seelectx.EstimateTokens(snap.Goal)
	for _, d := range snap.Decisions {
		t += seelectx.EstimateTokens(d.What) + seelectx.EstimateTokens(d.Why)
		for _, a := range d.Alternatives {
			t += seelectx.EstimateTokens(a)
		}
		t += 4
	}
	for _, f := range snap.Findings {
		t += seelectx.EstimateTokens(f)
	}
	t += seelectx.EstimateTokens(snap.Progress)
	for _, c := range snap.Constraints {
		t += seelectx.EstimateTokens(c)
	}
	for _, w := range snap.PendingWork {
		t += seelectx.EstimateTokens(w)
	}
	if snap.Escape != nil {
		t += seelectx.EstimateTokens(snap.Escape.Reason) + seelectx.EstimateTokens(snap.Escape.Message) + seelectx.EstimateTokens(snap.Escape.ParentGoal) + 8
	}
	return t
}

func truncateForToken(s string, maxTokens int) string {
	if maxTokens <= 0 || s == "" {
		return ""
	}
	if seelectx.EstimateTokens(s) <= maxTokens {
		return s
	}
	const marker = "..."
	if seelectx.EstimateTokens(marker) > maxTokens {
		return ""
	}
	var out string
	for _, r := range s {
		next := out + string(r)
		if seelectx.EstimateTokens(next+marker) > maxTokens {
			break
		}
		out = next
	}
	if !utf8.ValidString(out) {
		return marker
	}
	return out + marker
}

func cloneSnapshot(snap *snapshot.ContextSnapshot) *snapshot.ContextSnapshot {
	cp := *snap
	cp.Decisions = append([]snapshot.Decision(nil), snap.Decisions...)
	for index := range cp.Decisions {
		cp.Decisions[index].Alternatives = append([]string(nil), snap.Decisions[index].Alternatives...)
	}
	cp.Findings = append([]string(nil), snap.Findings...)
	cp.Constraints = append([]string(nil), snap.Constraints...)
	cp.PendingWork = append([]string(nil), snap.PendingWork...)
	if snap.Escape != nil {
		escape := *snap.Escape
		cp.Escape = &escape
	}
	return &cp
}
