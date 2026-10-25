package dto

// ViewLimits 是中间层只读投影需要的运行时上限窄面。
//
// 它刻意不包含完整 limit.Limits：middle/view 只消费展示截断与窗口读数，
// 不能为了几个整数反向依赖 backend/limit。
type ViewLimits struct {
	HistoryWindow  int
	PlanNodeEvents int
	EvidenceChars  int
}
