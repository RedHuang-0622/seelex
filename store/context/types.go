package context

import (
	"encoding/json"
	"time"
)

const SessionContextSchemaVersion = 3

type EvidenceRef struct {
	Ref     string `json:"ref"`
	Summary string `json:"summary,omitempty"`
}

type PlanFrame struct {
	PlanID    string        `json:"plan_id"`
	Title     string        `json:"title"`
	Status    string        `json:"status"`
	Nodes     []NodeSummary `json:"nodes,omitempty"`
	EnteredAt time.Time     `json:"entered_at"`
	ClosedAt  *time.Time    `json:"closed_at,omitempty"`
}

type NodeSummary struct {
	ID     string `json:"id"`
	Label  string `json:"label,omitempty"`
	Status string `json:"status,omitempty"`
}

type TaskFrame struct {
	TaskID    string        `json:"task_id"`
	Objective string        `json:"objective"`
	Status    string        `json:"status"`
	Evidence  []EvidenceRef `json:"evidence,omitempty"`
	EnteredAt time.Time     `json:"entered_at,omitempty"`
}

type GoalBudget struct {
	MaxLoops  int `json:"max_loops,omitempty"`
	MaxTokens int `json:"max_tokens,omitempty"`
}

type GoalProgress struct {
	At      int64  `json:"at"`
	Kind    string `json:"kind"`
	Content string `json:"content"`
}

type GoalFrame struct {
	GoalID        string            `json:"goal_id"`
	Title         string            `json:"title"`
	Statement     string            `json:"statement,omitempty"`
	Acceptance    []string          `json:"acceptance,omitempty"`
	OutOfScope    []string          `json:"out_of_scope,omitempty"`
	Budget        GoalBudget        `json:"budget"`
	Status        string            `json:"status"`
	Progress      []GoalProgress    `json:"progress,omitempty"`
	Directives    []string          `json:"tl_directives,omitempty"`
	Meta          map[string]string `json:"meta,omitempty"`
	CreatedAt     int64             `json:"created_at,omitempty"`
	UpdatedAt     int64             `json:"updated_at,omitempty"`
	FinishedAt    int64             `json:"finished_at,omitempty"`
	EnteredAt     time.Time         `json:"entered_at,omitempty"`
	RoleName      string            `json:"role_name,omitempty"`
	RoleSessionID string            `json:"role_session_id,omitempty"`
}

type GoalAuditEntry struct {
	Seq           uint64 `json:"seq"`
	Kind          string `json:"kind"`
	GoalID        string `json:"goal_id"`
	Title         string `json:"title,omitempty"`
	Status        string `json:"status,omitempty"`
	At            int64  `json:"at,omitempty"`
	SourceSession string `json:"source_session,omitempty"`
	Reason        string `json:"reason,omitempty"`
	Result        string `json:"result,omitempty"`
	Detail        string `json:"detail,omitempty"`
}

type SkillFrame struct {
	SkillID     string    `json:"skill_id"`
	Name        string    `json:"name"`
	ActivatedAt time.Time `json:"activated_at,omitempty"`
}

type Revision struct {
	CommitID string `json:"commit_id,omitempty"`
	Number   uint64 `json:"number"`
}

type CompactFrame struct {
	SegmentID            string        `json:"segment_id"`
	From                 int           `json:"from"`
	To                   int           `json:"to"`
	RoundFrom            uint64        `json:"round_from,omitempty"`
	RoundTo              uint64        `json:"round_to,omitempty"`
	EventFrom            uint64        `json:"event_from,omitempty"`
	EventTo              uint64        `json:"event_to,omitempty"`
	MessageFrom          string        `json:"message_from,omitempty"`
	MessageTo            string        `json:"message_to,omitempty"`
	EventRevision        Revision      `json:"event_revision"`
	ConversationRevision Revision      `json:"conversation_revision"`
	Summary              string        `json:"summary"`
	Evidence             []EvidenceRef `json:"evidence,omitempty"`
	CompressedAt         time.Time     `json:"compressed_at"`
	RequestFrom          string        `json:"request_from,omitempty"`
	RequestTo            string        `json:"request_to,omitempty"`
	PrevSegmentID        string        `json:"prev_segment_id,omitempty"`
	PrevRequestFrom      string        `json:"prev_request_from,omitempty"`
	PrevRequestTo        string        `json:"prev_request_to,omitempty"`
	PrevSummaryOneLine   string        `json:"prev_summary_one_line,omitempty"`
	SummarySource        string        `json:"summary_source,omitempty"`
	AnchorSource         string        `json:"anchor_source,omitempty"`
}

type SessionContextRecord struct {
	SchemaVersion int              `json:"schema_version"`
	SystemPrompt  string           `json:"system_prompt,omitempty"`
	ForkedFrom    json.RawMessage  `json:"forked_from,omitempty"`
	PlanStack     []PlanFrame      `json:"plan_stack"`
	TaskStack     []TaskFrame      `json:"task_stack"`
	SkillStack    []SkillFrame     `json:"skill_stack"`
	CompactStack  []CompactFrame   `json:"compact_stack"`
	GoalStack     []GoalFrame      `json:"goal_stack"`
	GoalAudit     []GoalAuditEntry `json:"goal_audit"`
}

type CheckpointContent struct {
	SessionID string `json:"session_id"`
	Payload   []byte `json:"payload"`
}

type SystemPromptSnapshot struct {
	SessionID string    `json:"session_id"`
	Prompt    string    `json:"prompt"`
	Version   string    `json:"version,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}
