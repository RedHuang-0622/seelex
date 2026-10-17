package model

import "time"

// ForkRequest 是会话 fork 的切断点入参（RequestID 与 EventSeq 二选一；
// 两者都为空时按 fork 起点（EventSeq=0）处理）。
type ForkRequest struct {
	// RequestID 指定按轮次关联的请求 ID 切断：解析为该轮完整单元的最后
	// 一个 EventSeq。requestID 只是关联字段，不承担截断/检索键。
	RequestID string `json:"request_id,omitempty"`
	// EventSeq 显式指定继承事件流的最后一个 EventSeq（含端点；0 = 空 fork）。
	// 必须落在完整段落（轮次单元）边界，否则拒绝。
	EventSeq uint64 `json:"event_seq,omitempty"`
}

// SessionForkRef 是会话 fork 血缘（子会话侧事实源；父会话删除/解绑后仍
// 保留）。一期 fork 强制同项目，ParentWorkspaceID 恒等于子会话项目；
// 显式记录保证父被删除后血缘仍可展示与追溯。
type SessionForkRef struct {
	// ParentSessionID 是父会话 ID（fork 来源）。
	ParentSessionID string `json:"parent_session_id"`
	// ParentWorkspaceID 是父会话所在项目 ID（一期恒等于子会话项目）。
	ParentWorkspaceID string `json:"parent_workspace_id,omitempty"`
	// ParentGeneration 是 fork 所基于的父会话已发布 generation（不可变
	// 快照版本绑定；禁止引用父的最新内存状态或后续提交）。
	ParentGeneration string `json:"parent_generation,omitempty"`
	// ForkPoint 是切断点（继承前缀的末端，EventSeq 含端点）。
	ForkPoint ForkPoint `json:"fork_point"`
	// ForkedAt 是 fork 创建时间。
	ForkedAt time.Time `json:"forked_at"`
}

// ForkPoint 是 fork 切断点（继承前缀的末端）。EventSeq 是唯一锚点；
// 其余字段是派生/关联信息，不承担截断语义。
type ForkPoint struct {
	// EventSeq 是继承事件流的最后一个 EventSeq（含端点；0 = 无继承事件）。
	EventSeq uint64 `json:"event_seq"`
	// EventCount 是继承事件数（诊断/展示）。
	EventCount uint64 `json:"event_count,omitempty"`
	// Round 是继承的完整轮次序号（1-based；RoundNo 体系启用前由事件流
	// 完整单元推导）。
	Round uint64 `json:"round,omitempty"`
	// RequestID 是切断轮次关联的请求 ID（仅关联字段）。
	RequestID string `json:"request_id,omitempty"`
	// MessageID 是继承的最后一个 UI 消息定位键。
	MessageID string `json:"message_id,omitempty"`
	// MessageCount 是继承的可见会话消息数。
	MessageCount int `json:"message_count,omitempty"`
}
