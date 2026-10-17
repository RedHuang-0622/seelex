package dto

import "time"

// PlanEdge 是 Seelex 拥有的可序列化 Plan 边类型。
// JSON 格式与旧 serialize.PlanEdgeSpec 保持同形，PlanState.Edges 的 DTO
// 契约不变；backend/tool/plan 以 alias 复用本定义（单源）。
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

// SubagentPolicy 是 plan 执行策略（纯 DTO；行为方法在 backend/tool/plan 域以
// 自由函数提供：ValidatePolicyLoad / PolicyConcurrency）。
type SubagentPolicy struct {
	Effort              string
	MaxNodes            int
	RequireSerial       bool
	MaxForkConcurrency  int
	MaxNodeLoops        int
	MaxNodeOutputTokens int
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
