package dto

import (
	"errors"
	"strings"
)

const (
	ProjectBoardSchema          = 1
	ProjectBoardMaxRows         = 512
	ProjectBoardReadMaxSessions = 32
	ProjectBoardWorkFile        = "worktable.jsonl"
	ProjectBoardTeamworkFile    = "teamwork.jsonl"
	ProjectBoardMarkdownFile    = "BOARD.md"

	ProjectBoardKindWork          = "work"
	ProjectBoardKindMilestone     = "milestone"
	ProjectBoardKindMilestoneItem = "milestone_item"
)

// SessionKey identifies one project-scoped session.
type SessionKey struct {
	ProjectID string
	SessionID string
}

// Complete reports whether both project and session identity are present.
func (key SessionKey) Complete() bool {
	return strings.TrimSpace(key.ProjectID) != "" && strings.TrimSpace(key.SessionID) != ""
}

// Validate checks the minimum session identity required by persistence.
func (key SessionKey) Validate() error {
	if strings.TrimSpace(key.SessionID) == "" {
		return errors.New("session storage: session ID is required")
	}
	return nil
}

// ProjectBoardScope is the session scope embedded in a projection row.
type ProjectBoardScope struct {
	Project string `json:"project,omitempty"`
	Session string `json:"session"`
}

// ProjectBoardRow is the append-only persisted project-board row envelope.
type ProjectBoardRow struct {
	Schema  int               `json:"schema"`
	Kind    string            `json:"kind"`
	Key     string            `json:"key"`
	Seq     uint64            `json:"seq"`
	At      int64             `json:"at"`
	Scope   ProjectBoardScope `json:"scope"`
	Payload map[string]any    `json:"payload,omitempty"`
}

// ProjectBoardReadOptions bounds a cross-session board read.
type ProjectBoardReadOptions struct {
	File           string
	ExcludeSession string
	MaxSessions    int
	MaxRows        int
}
