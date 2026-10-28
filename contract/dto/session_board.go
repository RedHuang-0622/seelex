package dto

import "encoding/json"

// Session-board archive is a derived recovery snapshot. These values are shared
// by the board owner and its consumers; the board package owns validation and
// physical publication.
const (
	BoardKindGoal = "goal"
	BoardKindTeam = "team"

	BoardStateActive = "active"
	BoardStateClosed = "closed"

	BoardCloseGoalFinish = "goal.finish"
	BoardCloseGoalAbort  = "goal.abort"
	BoardCloseTeamReplan = "team.replan"
	BoardCloseTeamClose  = "team.close"
)

type BoardLifecycle struct {
	Kind         string `json:"board_kind"`
	State        string `json:"state"`
	Seq          uint64 `json:"seq"`
	OpenedAt     int64  `json:"opened_at,omitempty"`
	UpdatedAt    int64  `json:"updated_at,omitempty"`
	ClosedAt     int64  `json:"closed_at,omitempty"`
	ClosedReason string `json:"closed_reason,omitempty"`
	Fingerprint  string `json:"fingerprint,omitempty"`
}

func (l BoardLifecycle) Closed() bool { return l.State == BoardStateClosed }

type BoardGoalProgress struct {
	At      int64  `json:"at"`
	Kind    string `json:"kind"`
	Content string `json:"content"`
}

type GoalBoardActive struct {
	GoalID     string              `json:"goal_id"`
	Title      string              `json:"title,omitempty"`
	Statement  string              `json:"statement,omitempty"`
	Status     string              `json:"status,omitempty"`
	Acceptance []string            `json:"acceptance,omitempty"`
	OutOfScope []string            `json:"out_of_scope,omitempty"`
	Progress   []BoardGoalProgress `json:"progress,omitempty"`
	CreatedAt  int64               `json:"created_at,omitempty"`
	UpdatedAt  int64               `json:"updated_at,omitempty"`
}

type GoalBoardHistory struct {
	GoalID        string `json:"goal_id"`
	Title         string `json:"title,omitempty"`
	Status        string `json:"status,omitempty"`
	ClosedAt      int64  `json:"closed_at,omitempty"`
	ClosedReason  string `json:"closed_reason,omitempty"`
	ProgressCount int    `json:"progress_count,omitempty"`
}

type GoalBoardMeta struct {
	BoardLifecycle
	Active  *GoalBoardActive   `json:"active,omitempty"`
	History []GoalBoardHistory `json:"history,omitempty"`
}

type TeamBoardMeta struct {
	BoardLifecycle
	TeamID   string          `json:"team_id,omitempty"`
	Version  int             `json:"version,omitempty"`
	Snapshot json.RawMessage `json:"snapshot,omitempty"`
}
