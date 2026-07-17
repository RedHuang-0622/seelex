// Package merger 提供双向上下文合并（MergeBack）能力。
//
// 合并策略：
//   - Findings / Decisions       → append
//   - Progress / PendingWork     → 替换
//   - Constraints                → append + 去重
//   - TokenEstimate / MessageCount → 累加
//   - Escape                     → 子代理非空则替换
//
// 并发安全：所有合并操作使用 copy-on-write。
package merger

import (
	"fmt"
	"slices"
	"sync"

	"github.com/RedHuang-0622/seelex/seelexctx/snapshot"
)

type Merger struct{ mu sync.Mutex }

func NewMerger() *Merger { return &Merger{} }

// MergeBack 将子代理快照合并到父代理快照。不修改原始切片。
func (m *Merger) MergeBack(parent, child *snapshot.ContextSnapshot) error {
	if parent == nil {
		return fmt.Errorf("merger: nil parent")
	}
	if child == nil {
		return fmt.Errorf("merger: nil child")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// copy-on-write
	parent.Decisions = copyDecisions(parent.Decisions)
	parent.Findings = copyStrings(parent.Findings)
	parent.Constraints = copyStrings(parent.Constraints)

	// append
	parent.Findings = append(parent.Findings, child.Findings...)
	parent.Decisions = append(parent.Decisions, child.Decisions...)

	// replace
	if child.Progress != "" {
		parent.Progress = child.Progress
	}
	if len(child.PendingWork) > 0 {
		parent.PendingWork = copyStrings(child.PendingWork)
	}

	// append + dedup
	for _, cc := range child.Constraints {
		if !slices.Contains(parent.Constraints, cc) {
			parent.Constraints = append(parent.Constraints, cc)
		}
	}

	// accumulate
	parent.TokenEstimate += child.TokenEstimate
	parent.MessageCount += child.MessageCount

	// escape replace
	if child.Escape != nil {
		parent.Escape = child.Escape
	}
	return nil
}

// ── 辅助 ──────────────────────────────────────────────────────

func copyDecisions(src []snapshot.Decision) []snapshot.Decision {
	if src == nil {
		return nil
	}
	dst := make([]snapshot.Decision, len(src))
	for i, d := range src {
		dst[i] = snapshot.Decision{What: d.What, Why: d.Why}
		if len(d.Alternatives) > 0 {
			dst[i].Alternatives = make([]string, len(d.Alternatives))
			copy(dst[i].Alternatives, d.Alternatives)
		}
	}
	return dst
}

func copyStrings(src []string) []string {
	if src == nil {
		return nil
	}
	dst := make([]string, len(src))
	copy(dst, src)
	return dst
}
