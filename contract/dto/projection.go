package dto

// RuntimeVisibilityProjection 是 application 到 runtime 的不可变可见性投影。
type RuntimeVisibilityProjection struct {
	GoalSkillActive bool
	// GoalGovernance 是当前会话 goal 治理只读视图（goal 激活/治理存在时非
	// nil）；Runtime 侧用于 goal 工具门控与调试，不参与模型上下文。
	GoalGovernance *GoalGovernanceView
}

// GoalGovernanceView 是会话 goal 治理的只读投影（§2.2）：由 goal 协调器
// 从 Controller 组装，经 SessionRuntime 下发给 GUI/TUI 面板。
// Active=false（或 nil）表示该会话无 goal 治理，前端隐藏面板。
//
// 2026-10-01（阶段三 W3）席位轮转退场：视图不再有"轮次 / 座次 / 断环"这类
// 循环概念（Round/RoundLimit/CurrentSeat/Broken/BreakReason/RoundError 一并删除）。
// 它只投影 goal **看板**（活动栈 + 状态 + 收口账本）；TL/ADVISOR 评审环已删除。
type GoalGovernanceView struct {
	Active bool       `json:"active"`
	GoalID string     `json:"goal_id,omitempty"`
	Title  string     `json:"title,omitempty"`
	Status GoalStatus `json:"status,omitempty"` // goal 状态（词表在 dto.GoalStatus）
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
	// Continuation 是**自动续跑的两层状态**读面（docs/arch/goal-auto-continue.md
	// §2.8.0/§2.8.1）：goal 状态（none/doing/done）× 子状态（continue/paused）
	// + stalled 时长。它随运行时投影进 Snapshot，GUI 与工具只读这一份，不在别处现算。
	// nil = 续跑能力未装配（关闭档）：读的人据此显示"没有这条横条"，而不是猜。
	Continuation *GoalContinuationView `json:"continuation,omitempty"`
}

// GoalContinuationView 是 stalled 横条的读面投影：两层状态 + 续跑进度 + 挂了多久。
type GoalContinuationView struct {
	GoalID   string          `json:"goal_id,omitempty"`
	Status   GoalRunStatus   `json:"status"`
	Substate GoalRunSubstate `json:"substate"`
	// Stalled 报告横条该不该出现（未收口 + 被硬中断 + 没停 + 没暂停）。
	Stalled bool `json:"stalled"`
	// Paused 是层二的低半位（⏸ 已按下）：与 Substate==paused 同源，便于前端只读一位。
	Paused bool `json:"paused,omitempty"`
	// Truncated 表示已达续跑上限（自动续跑停了，等人工 ⟳）。
	Truncated   bool `json:"truncated,omitempty"`
	Attempts    int  `json:"attempts,omitempty"`
	MaxAttempts int  `json:"max_attempts,omitempty"`
	// StalledForSec 是这条横条已经挂了多久（秒）；Stalled=false 时为 0。
	StalledForSec int64 `json:"stalled_for_sec,omitempty"`
	// NextAttemptAt 是退避期里的下次尝试时刻（unix 秒，0 = 无退避在等）。
	NextAttemptAt int64 `json:"next_attempt_at,omitempty"`
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

// GoalFrameProgressPreviewLimit is the compact-card progress window shared by
// live and recovered goal frame projections.
const GoalFrameProgressPreviewLimit = 3

// TailGoalProgressViews returns the newest limit progress entries.
func TailGoalProgressViews(items []GoalProgressView, limit int) []GoalProgressView {
	if limit <= 0 || len(items) <= limit {
		return items
	}
	return items[len(items)-limit:]
}

// ParentEvidenceProjection 是 application 到 Runtime 的最小父证据投影：
// Runtime 据此构造子代理可读的父证据快照（合并回传的起点）。
type ParentEvidenceProjection struct {
	SessionID         string
	Goal              string
	ConversationCount int
}
