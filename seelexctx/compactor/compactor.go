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
	"fmt"
	"strings"

	"github.com/RedHuang-0622/Seele/seelectx"
	"github.com/RedHuang-0622/seelex/seelexctx/snapshot"
)

type Compactor struct{}

func NewCompactor() *Compactor { return &Compactor{} }

// Compact 压缩快照到目标 token 预算内。返回新快照，不修改原始快照。
func (c *Compactor) Compact(snap *snapshot.ContextSnapshot, budget int) (*snapshot.ContextSnapshot, error) {
	if snap == nil {
		return nil, fmt.Errorf("compactor: nil snapshot")
	}
	if budget < 0 {
		budget = 0
	}
	fullTokens := estimateTokens(snap)
	if budget >= 500 || fullTokens <= budget {
		return c.fullSnapshot(snap), nil
	}
	if budget >= 200 {
		return c.summarySnapshot(snap), nil
	}
	return c.minimalSnapshot(snap), nil
}

// ── 三级压缩 ──────────────────────────────────────────────────

func (c *Compactor) fullSnapshot(snap *snapshot.ContextSnapshot) *snapshot.ContextSnapshot {
	cp := *snap
	cp.Goal = truncateForToken(cp.Goal, 100)
	cp.Progress = truncateForToken(cp.Progress, 200)
	if len(cp.Findings) > 0 {
		cp.Findings = make([]string, len(snap.Findings))
		for i, f := range snap.Findings {
			cp.Findings[i] = truncateForToken(f, 100)
		}
	}
	return &cp
}

func (c *Compactor) summarySnapshot(snap *snapshot.ContextSnapshot) *snapshot.ContextSnapshot {
	cp := c.fullSnapshot(snap)
	if len(snap.Decisions) > 0 {
		tc := 0
		for _, d := range snap.Decisions {
			if strings.HasPrefix(d.What, "调用工具") {
				tc++
			}
		}
		cp.Decisions = []snapshot.Decision{{
			What: fmt.Sprintf("共 %d 项决策（含 %d 次工具调用）", len(snap.Decisions), tc),
			Why:  "摘要模式：详细决策已压缩",
		}}
	}
	if len(snap.Findings) > 0 {
		cp.Findings = []string{fmt.Sprintf("摘要模式：共 %d 项发现", len(snap.Findings))}
	}
	if len(snap.Constraints) > 0 {
		cp.Constraints = []string{fmt.Sprintf("摘要模式：共 %d 项约束", len(snap.Constraints))}
	}
	if len(snap.PendingWork) > 0 {
		cp.PendingWork = []string{fmt.Sprintf("摘要模式：共 %d 项待办", len(snap.PendingWork))}
	}
	return cp
}

func (c *Compactor) minimalSnapshot(snap *snapshot.ContextSnapshot) *snapshot.ContextSnapshot {
	return &snapshot.ContextSnapshot{
		SourceSessionID: snap.SourceSessionID,
		ExportedAt:      snap.ExportedAt,
		MessageCount:    snap.MessageCount,
		TokenEstimate:   snap.TokenEstimate,
		Escape:          snap.Escape,
		Goal:            truncateForToken(snap.Goal, 50),
		Progress:        truncateForToken(snap.Progress, 100),
	}
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
		t += seelectx.EstimateTokens(snap.Escape.Reason) + seelectx.EstimateTokens(snap.Escape.Message) + 8
	}
	return t
}

func truncateForToken(s string, maxTokens int) string {
	if seelectx.EstimateTokens(s) <= maxTokens {
		return s
	}
	return s[:min(maxTokens*3, len(s))] + "..."
}
