package session

import "github.com/RedHuang-0622/seelex/seelexctx/snapshot"

// cloneContextSnapshot 返回快照的深拷贝（读侧导出与 parentEvidence 合并共用）。
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
