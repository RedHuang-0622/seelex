package dto

// RuntimeVisibilityProjection 是 application 到 runtime 的不可变可见性投影。
type RuntimeVisibilityProjection struct {
	GoalSkillActive bool
	// GoalGovernance 是当前会话 goal 治理只读视图（goal 激活/治理存在时非
	// nil）；Runtime 侧用于 goal 工具门控与调试，不参与模型上下文。
	GoalGovernance *GoalGovernanceView
}

// GoalGovernanceView 是会话 goal 治理的只读投影（§2.2）：由 goal 协调器
// 组装（Controller + Supervisor + Governor 读面），经 SessionRuntime 下发给
// GUI/TUI 面板。Active=false（或 nil）表示该会话无 goal 治理，前端隐藏面板。
type GoalGovernanceView struct {
	Active        bool   `json:"active"`
	GoalID        string `json:"goal_id,omitempty"`
	Title         string `json:"title,omitempty"`
	Status        string `json:"status,omitempty"` // goal 状态
	Round         int    `json:"round"`            // 治理轮次
	CurrentSeat   string `json:"current_seat,omitempty"`
	PeerState     string `json:"peer_state,omitempty"`
	LastDirective string `json:"last_directive,omitempty"`
	Broken        bool   `json:"broken"`
	BreakReason   string `json:"break_reason,omitempty"`
	HeartbeatAt   int64  `json:"heartbeat_at,omitempty"`
	HeartbeatSeq  uint64 `json:"heartbeat_seq"`
}

// ParentEvidenceProjection 是 application 到 Runtime 的最小父证据投影：
// Runtime 据此构造子代理可读的父证据快照（合并回传的起点）。
type ParentEvidenceProjection struct {
	SessionID         string
	Goal              string
	ConversationCount int
}
