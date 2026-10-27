package context

import (
	"encoding/json"
	"time"
)

type StackKind string

const (
	StackKindPlan     StackKind = "plan"
	StackKindTask     StackKind = "task"
	StackKindGoal     StackKind = "goal"
	StackKindSubagent StackKind = "subagent"
)

const (
	StackStatusPlanClosed    = "closed"
	StackStatusTaskCompleted = "completed"
	StackStatusTaskFailed    = "failed"
	StackStatusGoalAborted   = "aborted"
	StackStatusSubagentDone  = "done"
	StackStatusArchived      = "archived"
)

var terminalStackStatus = map[string]bool{
	StackStatusPlanClosed:    true,
	StackStatusTaskCompleted: true,
	StackStatusTaskFailed:    true,
	StackStatusGoalAborted:   true,
	StackStatusSubagentDone:  true,
	StackStatusArchived:      true,
}

func ValidStackKind(kind StackKind) bool {
	return kind == StackKindPlan || kind == StackKindTask ||
		kind == StackKindGoal || kind == StackKindSubagent
}

func StackKindIndex(kind StackKind) int {
	switch kind {
	case StackKindTask:
		return 1
	case StackKindGoal:
		return 2
	case StackKindSubagent:
		return 3
	default:
		return 0
	}
}

func IsTerminalStackStatus(status string) bool { return terminalStackStatus[status] }

type StackItemInput struct {
	ItemID        string
	Kind          StackKind
	Status        string
	Payload       json.RawMessage
	RoleName      string
	RoleSessionID string
	EnteredAt     time.Time
}

type StackItemRecord struct {
	ItemID            string          `json:"item_id"`
	BatchID           string          `json:"batch_id"`
	StackID           string          `json:"stack_id"`
	Kind              StackKind       `json:"kind"`
	Seq               uint64          `json:"seq"`
	Status            string          `json:"status"`
	Payload           json.RawMessage `json:"payload,omitempty"`
	RoleName          string          `json:"role_name,omitempty"`
	RoleSessionID     string          `json:"role_session_id,omitempty"`
	Revision          uint64          `json:"revision"`
	ItemMessageID     string          `json:"item_message_id,omitempty"`
	ItemMessageSeq    uint64          `json:"item_message_seq,omitempty"`
	BatchMessageFrom  string          `json:"batch_message_from,omitempty"`
	BatchMessageTo    string          `json:"batch_message_to,omitempty"`
	BatchMessageToSeq uint64          `json:"batch_message_to_seq,omitempty"`
	EnteredAt         time.Time       `json:"entered_at"`
	ClosedAt          time.Time       `json:"closed_at,omitempty"`
}

type StackWatermark struct {
	HeadSeq      uint64 `json:"head_seq"`
	ActiveCount  int    `json:"active_count"`
	HistoryCount uint64 `json:"history_count"`
	HistoryBytes uint64 `json:"history_bytes,omitempty"`
	LastCommitID string `json:"last_commit_id,omitempty"`
}

type StackModuleHead struct {
	SessionID string                       `json:"session_id"`
	HeadSeq   uint64                       `json:"head_seq"`
	Kinds     map[StackKind]StackWatermark `json:"kinds,omitempty"`
}

type StackMutation struct {
	Revision  uint64
	Pushed    []StackItemRecord
	Updated   []StackItemRecord
	Archived  []StackItemRecord
	BatchID   string
	BatchDone bool
}
