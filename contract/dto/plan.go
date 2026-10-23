package dto

import "time"

// PlanEdge 是历史 Plan 的可序列化边类型（只读展示用；执行器已退役）。
// JSON 格式与旧 serialize.PlanEdgeSpec 保持同形，PlanState.Edges 的 DTO
// 契约不变。
type PlanEdge struct {
	From      string `json:"from"`
	To        string `json:"to"`
	Label     string `json:"label,omitempty"`
	Condition string `json:"condition,omitempty"`
}

// AccountRole 表示账号承担的任务类别（agent/subagent/goalplan/teammate）。
//
// teammate 是 2026-10-23 新增的第四类：team 里被派活的成员（一个 Work Item 一个
// teammate 会话）走自己这一类的账号档，于是"队友用哪个供应商/模型"成了一处可配的
// 事实，而不是从 agent/subagent 里蹭一个。没配 teammate 档时按 FallbackRoles 回退
// agent，单账号安装不受影响。
type AccountRole string

const (
	RoleAgent    AccountRole = "agent"
	RoleSubAgent AccountRole = "subagent"
	RoleGoalPlan AccountRole = "goalplan"
	RoleTeammate AccountRole = "teammate"
)

// SubagentPolicy 是子代理执行策略（fork 数量/并发/节点循环上限）。
// 唯一消费者是 fork/workunit 的装配与执行路径；Plan 执行器已退役。
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
