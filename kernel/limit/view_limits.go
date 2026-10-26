package limit

import "github.com/RedHuang-0622/seelex/contract/dto"

// ViewLimits 把完整运行时上限投影为中间层只读视图需要的窄契约。
//
// 这是 limit.Limits 到 dto.ViewLimits 的唯一投影点，消费方不得再各自
// 拼字段或依赖完整 Limits。
func (limits Limits) ViewLimits() dto.ViewLimits {
	return dto.ViewLimits{
		HistoryWindow:  limits.HistoryWindow,
		PlanNodeEvents: limits.PlanNodeEvents,
		EvidenceChars:  limits.EvidenceChars,
	}
}
