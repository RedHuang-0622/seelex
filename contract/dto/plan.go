package dto

import "time"

// PlanEdge 是 Seelex 拥有的可序列化 Plan 边类型。
// JSON 格式与旧 serialize.PlanEdgeSpec 保持同形，PlanState.Edges 的 DTO
// 契约不变；seelebridge/plan 以 alias 复用本定义（单源）。
type PlanEdge struct {
	From      string `json:"from"`
	To        string `json:"to"`
	Label     string `json:"label,omitempty"`
	Condition string `json:"condition,omitempty"`
}

// AccountRole 表示账号承担的任务类别（agent/subagent/goalplan）。
type AccountRole string

const (
	RoleAgent    AccountRole = "agent"
	RoleSubAgent AccountRole = "subagent"
	RoleGoalPlan AccountRole = "goalplan"
)

// PlanPolicy 是 plan 执行策略（纯 DTO；行为方法在 seelebridge/plan 域以
// 自由函数提供：ValidatePolicyLoad / PolicyConcurrency）。
type PlanPolicy struct {
	Effort              string
	MaxNodes            int
	RequireSerial       bool
	MaxForkConcurrency  int
	MaxNodeLoops        int
	MaxNodeOutputTokens int
}

// PlanBranchBinding 冻结 plan_run 请求级绑定值。
type PlanBranchBinding struct {
	SessionID   string
	WorkspaceID string
	PlanID      string
	EntryNodeID string
	AccountID   string
	PrimaryRole AccountRole
	TraceID     string
}

// PlanNodeEvent 是 plan 节点执行事件的投影（前端 plan 可视化数据源）。
type PlanNodeEvent struct {
	// SessionID 是事件归属会话（plan 执行绑定所在会话；空 = 全局/未知，
	// 会话域收口后必填）。前端按 session_id 过滤，后台会话 plan 事件不得
	// 污染当前快照（P6 收口）。
	SessionID string
	PlanID    string
	RunID     string
	NodeID    string
	Kind      string // 展示用 kind（approve 由前端映射为 manual）
	Status    string
	Output    string
	Elapsed   string
	At        time.Time
}

// PlanPreflight 是隔离规划回合的输出（Arguments/Result）。
type PlanPreflight struct {
	Arguments string
	Result    string
}

// ReplanRequest 是提供给隔离恢复规划回合的有界可审计上下文（只含执行
// 事实，不含无界聊天转写）。
type ReplanRequest struct {
	// SessionID 是本次重规划归属会话（G1/M5：额度按会话建槽；空 = 无
	// sid legacy 路径，回退默认槽）。
	SessionID      string
	IdempotencyKey string
	Objective      string
	PreviousPlan   string
	Failure        string
	Evidence       string
}

// ReplanMetrics 是重规划保护（replan guard）的窗口统计快照。
type ReplanMetrics struct {
	InFlight               int       `json:"in_flight"`
	ConcurrentLimit        int       `json:"concurrent_limit"`
	WindowAttempts         int       `json:"window_attempts"`
	WindowLimit            int       `json:"window_limit"`
	WindowStartedAt        time.Time `json:"window_started_at,omitempty"`
	Accepted               uint64    `json:"accepted"`
	Succeeded              uint64    `json:"succeeded"`
	Failed                 uint64    `json:"failed"`
	Rejected               uint64    `json:"rejected"`
	DuplicateRejected      uint64    `json:"duplicate_rejected"`
	ProviderRequests       uint64    `json:"provider_requests"`
	ProviderWindowRequests int       `json:"provider_window_requests"`
	ProviderWindowLimit    int       `json:"provider_window_limit"`
}
