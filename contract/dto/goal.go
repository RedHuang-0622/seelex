package dto

// Budget is the goal-level guardrail budget. Zero means unset/unbounded.
type Budget struct {
	MaxLoops  int `json:"max_loops,omitempty"`
	MaxTokens int `json:"max_tokens,omitempty"`
}

// ProgressKind classifies one goal progress entry.
type ProgressKind string

const (
	ProgressMilestone ProgressKind = "milestone"
	ProgressFinding   ProgressKind = "finding"
	ProgressDecision  ProgressKind = "decision"
	ProgressRisk      ProgressKind = "risk"
)

// Progress is one auditable goal update.
type Progress struct {
	At      int64        `json:"at"`
	Kind    ProgressKind `json:"kind"`
	Content string       `json:"content"`
}

// GoalRecord is the durable representation of one goal.
type GoalRecord struct {
	ID         string            `json:"id"`
	Title      string            `json:"title"`
	Statement  string            `json:"statement"`
	Acceptance []string          `json:"acceptance,omitempty"`
	OutOfScope []string          `json:"out_of_scope,omitempty"`
	Budget     Budget            `json:"budget"`
	Status     GoalStatus        `json:"status"`
	Progress   []Progress        `json:"progress,omitempty"`
	Directives []string          `json:"tl_directives,omitempty"`
	CreatedAt  int64             `json:"created_at"`
	UpdatedAt  int64             `json:"updated_at"`
	FinishedAt int64             `json:"finished_at,omitempty"`
	Meta       map[string]string `json:"meta,omitempty"`
}

// BeginGoalRequest is the goal_begin input.
type BeginGoalRequest struct {
	Title      string   `json:"title"`
	Statement  string   `json:"statement"`
	Acceptance []string `json:"acceptance,omitempty"`
	OutOfScope []string `json:"out_of_scope,omitempty"`
	Budget     *Budget  `json:"budget,omitempty"`
}

// UpdateGoalRequest is the goal_update input.
type UpdateGoalRequest struct {
	Title           *string      `json:"title,omitempty"`
	Statement       *string      `json:"statement,omitempty"`
	Acceptance      []string     `json:"acceptance,omitempty"`
	OutOfScope      []string     `json:"out_of_scope,omitempty"`
	ProgressKind    ProgressKind `json:"progress_kind,omitempty"`
	ProgressContent string       `json:"progress_content,omitempty"`
}

// FinishGoalRequest is the goal_finish / goal_abort input.
type FinishGoalRequest struct {
	Reason string `json:"reason,omitempty"`
	Result string `json:"result,omitempty"`
}

// GoalStatusView is the full goal_status view.
type GoalStatusView struct {
	Stack   []*GoalRecord `json:"stack"`
	Active  *GoalRecord   `json:"active,omitempty"`
	History int           `json:"history_count"`
}
