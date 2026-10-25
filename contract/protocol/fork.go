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
	// SourceEventSeq / SourceMessageID 是本次 fork 的产品来源（例如用户编辑的
	// 原消息），只用于血缘展示，不参与切断点解析。
	SourceEventSeq  uint64 `json:"source_event_seq,omitempty"`
	SourceMessageID string `json:"source_message_id,omitempty"`
	// ExpectedEventSeq 是可选 CAS：父会话事件流最大 Seq 必须仍等于该值，
	// 防止读校验与落盘之间父会话推进。
	ExpectedEventSeq uint64 `json:"expected_event_seq,omitempty"`
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
	// SourceEventSeq / SourceMessageID 是触发本次 fork 的原父消息（编辑 fork
	// 时为目标用户消息；普通会话 fork 为空）。它们不参与截断。
	SourceEventSeq  uint64 `json:"source_event_seq,omitempty"`
	SourceMessageID string `json:"source_message_id,omitempty"`
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

// EditUserMessageForkRequest 是“编辑用户消息并从其上一完整段落边界分叉”的
// 原子应用请求。
type EditUserMessageForkRequest struct {
	// TargetEventSeq 是用户要编辑的原始 user 事件序号。
	TargetEventSeq uint64 `json:"target_event_seq"`
	// NewText 是子会话要提交的新用户输入（非空）。
	NewText string `json:"new_text"`
	// ExpectedRevision 是打开编辑器时的父会话快照 revision（CAS 防陈旧编辑）。
	ExpectedRevision uint64 `json:"expected_revision"`
	// OperationID 是一次编辑提交的幂等键；双击/传输重试必须复用同一个值。
	OperationID string `json:"operation_id,omitempty"`
}

// EditUserMessageForkResult 是原子编辑分叉的稳定回执。
type EditUserMessageForkResult struct {
	ChildSessionID string `json:"child_session_id"`
	ForkPoint      uint64 `json:"fork_point"`
}
