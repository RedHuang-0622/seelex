package dto

// RuntimeVisibilityProjection 是 application 到 runtime 的不可变可见性投影。
type RuntimeVisibilityProjection struct {
	GoalSkillActive bool
	// GoalGovernance 是当前会话 goal 治理只读视图（goal 激活/治理存在时非
	// nil）；Runtime 侧用于 goal 工具门控与调试，不参与模型上下文。
	GoalGovernance *GoalGovernanceView
}

// GoalGovernanceView 是会话 goal 治理的只读投影（§2.2）：由 goal 协调器
// 组装（Controller + Supervisor 读面），经 SessionRuntime 下发给 GUI/TUI 面板。
// Active=false（或 nil）表示该会话无 goal 治理，前端隐藏面板。
//
// 2026-10-01（阶段三 W3）席位轮转退场：视图不再有"轮次 / 座次 / 断环"这类
// 循环概念（Round/RoundLimit/CurrentSeat/Broken/BreakReason/RoundError 一并删除）。
// 它现在只投影 goal **看板**（活动栈 + 状态 + 最近裁决）与终态 gate / 审批预筛
// 期间短暂有值的评审过程。
type GoalGovernanceView struct {
	Active bool       `json:"active"`
	GoalID string     `json:"goal_id,omitempty"`
	Title  string     `json:"title,omitempty"`
	Status GoalStatus `json:"status,omitempty"` // goal 状态（词表在 dto.GoalStatus）
	// PeerState 是评审者（b）的状态：只在终态 gate / 审批预筛跑真实 TL 回合时
	// 短暂进入 evaluating / advisory_pending，其余时间为稳态（词表在 dto.PeerState）。
	PeerState     PeerState `json:"peer_state,omitempty"`
	LastDirective string    `json:"last_directive,omitempty"`
	// InFlight / InFlightChars 是**当前 b（ADVISOR）回合进行中**的正文近端。
	//
	// 为什么存在于治理视图：b 回合是同步跑完的（回合结束才推一次状态），"评审在
	// 写什么"因此完全不可见（渲染不及时）。这里把进行中正文作为只读快照暴露，
	// 前端在 peer_state=evaluating 期间轮询快照即可看到；回合结束即清空（权威正文是
	// tl_directive 行）。**只有后端 → 前端的单向投影**。
	InFlight      string `json:"in_flight,omitempty"`
	InFlightChars int    `json:"in_flight_chars,omitempty"`
	// RoundSteps 是**本轮/最近一轮** ADVISOR 评审的过程步骤（工具调用 + 返回）。
	//
	// 为什么存在于治理视图：b 回合是一次**带只读工具的**评审（读文件/搜索），但那些
	// 工具调用只活在进程内的角色会话里，回合结束即消失——前端因此只看得到终局裁决
	// （tl_directive 行），看不到"评审者核对了什么"。这里把过程作为只读投影暴露，
	// 前端据此渲染"评审过程"时间线（进行中与刚结束都可看）。**单向投影**。
	RoundSteps []GoalStepView `json:"round_steps,omitempty"`
	// Stack 是 goal **活动栈**的逐帧只读投影（栈底→栈顶；末元素 = Active 那一帧）。
	//
	// 为什么要有它：governance 视图原来只有栈顶一帧，会话里嵌套压栈（新 goal 压栈、
	// 栈下目标转 paused）之后，工作台看不到"栈上还有什么"。这里按帧给出内容，工作台
	// 据此分块展示；事实仍是 Controller 的 LIFO 栈一份，这里是只读投影。
	Stack []GoalFrameView `json:"stack,omitempty"`
	// Recovered 标记这一帧**来自看板存档快照**而不是活体栈（§9 的读侧兜底）：
	// 活体栈为空、存档里 state=active 时用它把看板重建出来。前端据此可标注
	// "从上次会话恢复"。活体可用时恒为 false。
	Recovered bool `json:"recovered,omitempty"`
	// History 是**已收口目标的账本**（只追加，来自看板存档的 history）。
	//
	// 为什么它不是兜底专属：账本与活动栈是两个正交的面——活体栈可用时它照样
	// 存在（"这台会话收口过哪些目标"），所以活体可用时也要下发；反之它**不会**
	// 混进 Stack，也不代表 active 那一帧（活动栈的事实只有 Controller 一份）。
	History []GoalHistoryView `json:"history,omitempty"`
}

// GoalHistoryView 是**已收口目标**的一条只读账本条目（看板存档 history 的投影；
// 只追加、不重写，按 goal_id 去重）。
type GoalHistoryView struct {
	GoalID       string `json:"goal_id"`
	Title        string `json:"title,omitempty"`
	Status       string `json:"status,omitempty"` // completed | aborted（收口终态）
	ClosedAt     int64  `json:"closed_at,omitempty"`
	ClosedReason string `json:"closed_reason,omitempty"`
	// ProgressCount 是该目标收口时的打点条数（判定得出时才有值：终态审计条目
	// 自己不承载条数，取不到就是 0，不猜）。
	ProgressCount int `json:"progress_count,omitempty"`
}

// GoalStepView 是 ADVISOR 评审过程里的**一步**只读投影（前端"评审过程"时间线）。
//
// 两步一条工具调用：Kind="tool"（Name/Args 有效）与 Kind="tool_result"
// （Result/Err 有效）——与轨迹视图的请求/响应对齐，前端可配对渲染。
type GoalStepView struct {
	Kind   string `json:"kind,omitempty"`
	Turn   int    `json:"turn,omitempty"`
	Name   string `json:"name,omitempty"`
	Args   string `json:"args,omitempty"`
	Result string `json:"result,omitempty"`
	Err    string `json:"err,omitempty"`
	At     int64  `json:"at,omitempty"`
}

// GoalFrameView 是 goal 活动栈里**一帧**的只读投影（看板一行 + 详情一份属性表）。
type GoalFrameView struct {
	ID         string   `json:"id"`
	Title      string   `json:"title"`
	Statement  string   `json:"statement,omitempty"`
	Status     string   `json:"status"`
	Active     bool     `json:"active,omitempty"` // 栈顶帧 = 当前目标
	Acceptance []string `json:"acceptance,omitempty"`
	// OutOfScope 是这一帧声明的非目标范围（可选）。它进投影是为了**详情面**能回答
	// "这个目标明确不做什么"——只有看板卡片时，这一条没有别的可见处。
	OutOfScope []string `json:"out_of_scope,omitempty"`
	// Progress 是这一帧的**最近若干条**进度（时间升序），供看板卡片写一行摘要。
	Progress []GoalProgressView `json:"progress,omitempty"`
	// ProgressAll 是这一帧保留的**全部**打点（时间升序，含 Progress 那几条），
	// 供"点开看详情"渲染完整打点流水。有界：goal 域自身的环形保留上限
	// （goal.MaxProgressItems = 32）就是它的上界，因此不需要再截一刀——
	// 截了详情面就只能看到最后几条，等于没有详情。
	ProgressAll []GoalProgressView `json:"progress_all,omitempty"`
	CreatedAt   int64              `json:"created_at,omitempty"`
	UpdatedAt   int64              `json:"updated_at,omitempty"`
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
