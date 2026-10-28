package dto

// session_record.go —— 会话粒度存储的**跨层形状**（会话记录 / 绑定 / 上下文四栈 /
// 目录摘要 / 工具结果引用）。
//
// 形状放契约层的原因：这些类型同时被 store（持久化）、session 域（SessionUnit
// 挂载点）、application 与前端读面消费；此前形状定义在 store，session 域为了
// 引用它们被迫 import store（阻碍 `session/` 迁入中间层）。facts 仍由 store 写，
// 这里只统一形状与词表。

import "time"

// SessionRecordKind 是会话类型（主会话 / 子代理）。
type SessionRecordKind string

const (
	SessionRecordKindMain     SessionRecordKind = "main"
	SessionRecordKindSubagent SessionRecordKind = "subagent"
)

// SessionRecordStatus 是持久记录里的会话状态词（string wire；可见面的枚举是 dto.SessionStatus）。
type SessionRecordStatus string

const (
	SessionRecordStatusDraft            SessionRecordStatus = "draft"
	SessionRecordStatusActive           SessionRecordStatus = "active"
	SessionRecordStatusIdle             SessionRecordStatus = "idle"
	SessionRecordStatusRunning          SessionRecordStatus = "running"
	SessionRecordStatusQueued           SessionRecordStatus = "queued"
	SessionRecordStatusAwaitingApproval SessionRecordStatus = "awaiting_approval"
	SessionRecordStatusArchived         SessionRecordStatus = "archived"
)

// String 返回持久状态词。
func (s SessionRecordStatus) String() string { return string(s) }

// SessionIndexRecord 是项目目录与存储索引使用的会话摘要记录。
type SessionIndexRecord struct {
	ID         string              `json:"id"`
	Kind       SessionRecordKind   `json:"kind"`
	ParentID   string              `json:"parent_id,omitempty"`
	Title      string              `json:"title,omitempty"`
	Status     SessionRecordStatus `json:"status"`
	Checkpoint string              `json:"checkpoint,omitempty"`
	UpdatedAt  time.Time           `json:"updated_at,omitempty"`
	Binding    SessionBinding      `json:"binding,omitempty"`
}

// SessionRecord 是迁移期兼容名；新代码应使用 SessionIndexRecord，避免与
// protocol.SessionRecord（完整会话聚合）同名混淆。
type SessionRecord = SessionIndexRecord

// SessionBinding 是会话绑定（B_i：workspaceID / parent / kind）。
type SessionBinding struct {
	WorkspaceID string            `json:"workspace_id,omitempty"`
	ParentID    string            `json:"parent_id,omitempty"`
	Kind        SessionRecordKind `json:"kind,omitempty"`
}

// SessionToolResultRef 是工具结果归档引用（session:<id>:toolresults）。
type SessionToolResultRef struct {
	Ref  string `json:"ref"`
	Name string `json:"name"`
	Size int64  `json:"size"`
}

// SessionContextStack 是会话上下文四栈（session:<id>:context；C_i）。
type SessionContextStack struct {
	Plan    []string `json:"plan,omitempty"`
	Task    []string `json:"task,omitempty"`
	Skill   []string `json:"skill,omitempty"`
	Compact []string `json:"compact,omitempty"`
}

// SessionInfo 是项目索引中的会话摘要（project:<p>:sessions）。
type SessionInfo struct {
	ID       string              `json:"id"`
	Title    string              `json:"title,omitempty"`
	Kind     SessionRecordKind   `json:"kind"`
	Status   SessionRecordStatus `json:"status"`
	ParentID string              `json:"parent_id,omitempty"`
	// UpdatedAt / TokenCount 取自会话 manifest（每次提交即刷新的权威时间与计费面）。
	UpdatedAt  time.Time `json:"updated_at,omitempty"`
	TokenCount int       `json:"token_count,omitempty"`
}
