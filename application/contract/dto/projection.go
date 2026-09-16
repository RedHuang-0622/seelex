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
	Active bool   `json:"active"`
	GoalID string `json:"goal_id,omitempty"`
	Title  string `json:"title,omitempty"`
	Status string `json:"status,omitempty"` // goal 状态
	Round  int    `json:"round"`            // 治理轮次
	// RoundLimit 是本会话治理循环的轮次上限（逃生路径：到达即收束；
	// 0 = 显式不设上限）。前端据此显示"轮次 n/limit"与接近上限的提示。
	RoundLimit    int    `json:"round_limit"`
	CurrentSeat   string `json:"current_seat,omitempty"`
	PeerState     string `json:"peer_state,omitempty"`
	LastDirective string `json:"last_directive,omitempty"`
	Broken        bool   `json:"broken"`
	BreakReason   string `json:"break_reason,omitempty"`
	HeartbeatAt   int64  `json:"heartbeat_at,omitempty"`
	HeartbeatSeq  uint64 `json:"heartbeat_seq"`
	// InFlight / InFlightChars 是**当前 b（ADVISOR）回合进行中**的正文近端。
	//
	// 为什么存在于治理视图：b 回合是同步跑完的（回合结束才推一次状态），旧实现里
	// "评审在写什么"因此完全不可见（渲染不及时）。这里把进行中正文作为只读快照暴露，
	// 前端在 peer_state=evaluating 期间轮询快照即可看到；回合结束即清空（权威正文是
	// tl_directive 行）。**只有后端 → 前端的单向投影**。
	InFlight      string `json:"in_flight,omitempty"`
	InFlightChars int    `json:"in_flight_chars,omitempty"`
	// Stack 是 goal **活动栈**的逐帧只读投影（栈底→栈顶；末元素 = Active 那一帧）。
	//
	// 为什么要有它：governance 视图原来只有栈顶一帧，会话里嵌套压栈（新 goal 压栈、
	// 栈下目标转 paused）之后，工作台看不到"栈上还有什么"。这里按帧给出内容，工作台
	// 据此分块展示；事实仍是 Controller 的 LIFO 栈一份，这里是只读投影。
	Stack []GoalFrameView `json:"stack,omitempty"`
}

// GoalFrameView 是 goal 活动栈里**一帧**的只读投影（工作台按帧分块查看）。
type GoalFrameView struct {
	ID         string   `json:"id"`
	Title      string   `json:"title"`
	Statement  string   `json:"statement,omitempty"`
	Status     string   `json:"status"`
	Active     bool     `json:"active,omitempty"` // 栈顶帧 = 当前目标
	Acceptance []string `json:"acceptance,omitempty"`
	// Progress 是这一帧的最近若干条进度（时间升序；完整进度仍以 goal_status 为准）。
	Progress  []GoalProgressView `json:"progress,omitempty"`
	UpdatedAt int64              `json:"updated_at,omitempty"`
}

// GoalProgressView 是逐帧进度条目的只读投影。
type GoalProgressView struct {
	At      int64  `json:"at,omitempty"`
	Kind    string `json:"kind,omitempty"`
	Content string `json:"content"`
}

// ParentEvidenceProjection 是 application 到 Runtime 的最小父证据投影：
// Runtime 据此构造子代理可读的父证据快照（合并回传的起点）。
type ParentEvidenceProjection struct {
	SessionID         string
	Goal              string
	ConversationCount int
}
