package dto

import "time"

// ConversationKind identifies the fixed conversation window exposed by each
// agent layer. This does not unify their lifecycle management.
type ConversationKind string

const (
	ConversationMain     ConversationKind = "main"
	ConversationSubagent ConversationKind = "subagent"
	ConversationTeammate ConversationKind = "teammate"
)

// ConversationSessionRef identifies one conversation window. Main uses the
// main SessionID; subagent uses its own SessionID; teammate always carries both
// the WorkItemID and that WorkItem's SessionID.
type ConversationSessionRef struct {
	Kind          ConversationKind `json:"kind"`
	SessionID     string           `json:"session_id"`
	MainSessionID string           `json:"main_session_id,omitempty"`
	TeamID        string           `json:"team_id,omitempty"`
	Role          string           `json:"role,omitempty"`
	WorkItemID    string           `json:"work_item_id,omitempty"`
}

// ConversationRecordKind is the complete execution-record vocabulary. A
// provider assistant row with reasoning is split into assistant and reasoning
// records so clipping and pagination remain uniform.
type ConversationRecordKind string

const (
	ConversationRecordSystem       ConversationRecordKind = "system"
	ConversationRecordUser         ConversationRecordKind = "user"
	ConversationRecordAssistant    ConversationRecordKind = "assistant"
	ConversationRecordReasoning    ConversationRecordKind = "reasoning"
	ConversationRecordToolCall     ConversationRecordKind = "tool_call"
	ConversationRecordToolResult   ConversationRecordKind = "tool_result"
	ConversationRecordStage        ConversationRecordKind = "stage"
	ConversationRecordRecoveryNote ConversationRecordKind = "recovery_note"
)

// ConversationRecord is one independently displayable execution record.
type ConversationRecord struct {
	ID         string                 `json:"id,omitempty"`
	Kind       ConversationRecordKind `json:"kind"`
	Role       string                 `json:"role,omitempty"`
	Content    string                 `json:"content,omitempty"`
	Reasoning  string                 `json:"reasoning_content,omitempty"`
	ToolName   string                 `json:"tool_name,omitempty"`
	ToolCallID string                 `json:"tool_call_id,omitempty"`
	Status     string                 `json:"status,omitempty"`
	Error      string                 `json:"error,omitempty"`
	At         time.Time              `json:"at,omitempty"`
	Truncated  bool                   `json:"truncated,omitempty"`
}

// ConversationViewRequest is shared by all three adapters. Limit<=0 uses the
// adapter's default; Kinds empty means all record kinds.
type ConversationViewRequest struct {
	Offset   int                      `json:"offset,omitempty"`
	Limit    int                      `json:"limit,omitempty"`
	MaxRunes int                      `json:"max_runes,omitempty"`
	Kinds    []ConversationRecordKind `json:"kinds,omitempty"`
}

// SessionRecordPage is the only paginated execution-history payload used by
// generic conversation views, team_message reads, and WorkItem execution.
type SessionRecordPage struct {
	SessionID string               `json:"session_id,omitempty"`
	Status    string               `json:"status,omitempty"`
	Recovery  SessionRecoveryState `json:"recovery,omitempty"`
	Records   []ConversationRecord `json:"records"`
	Offset    int                  `json:"offset"`
	Limit     int                  `json:"limit"`
	Total     int                  `json:"total"`
	HasMore   bool                 `json:"has_more"`
	Truncated bool                 `json:"truncated"`
}

// SessionRecoveryState is the shared live -> cold -> interrupted -> missing
// recovery vocabulary.
type SessionRecoveryState string

const (
	RecoveryLive        SessionRecoveryState = "live"
	RecoveryCold        SessionRecoveryState = "cold"
	RecoveryInterrupted SessionRecoveryState = "interrupted"
	RecoveryMissing     SessionRecoveryState = "missing"
)

// SessionRecovery describes how the conversation window was resolved.
type SessionRecovery struct {
	State       SessionRecoveryState `json:"state"`
	SessionID   string               `json:"session_id,omitempty"`
	Durable     bool                 `json:"durable"`
	ResumeNote  string               `json:"resume_note,omitempty"`
	RecoveredAt time.Time            `json:"recovered_at,omitempty"`
}

// ConversationChatRequest is one user turn. WaitMS<=0 uses the adapter default.
type ConversationChatRequest struct {
	Message string `json:"message"`
	WaitMS  int    `json:"wait_ms,omitempty"`
}

// ChatReceipt is the bounded chat result. When TimedOut is true, JobHandle is
// the durable in-process handle for fetching the eventual reply.
type ChatReceipt struct {
	Accepted  bool                 `json:"accepted"`
	SessionID string               `json:"session_id,omitempty"`
	JobHandle string               `json:"job_handle,omitempty"`
	Reply     string               `json:"reply,omitempty"`
	TimedOut  bool                 `json:"timed_out,omitempty"`
	State     SessionRecoveryState `json:"state,omitempty"`
}
