package dto

// RuntimeVisibilityProjection 是 application 到 runtime 的不可变可见性投影。
type RuntimeVisibilityProjection struct {
	GoalSkillActive bool
}

// ParentEvidenceProjection 是 application 到 Runtime 的最小父证据投影：
// Runtime 据此构造子代理可读的父证据快照（合并回传的起点）。
type ParentEvidenceProjection struct {
	SessionID         string
	Goal              string
	ConversationCount int
}
