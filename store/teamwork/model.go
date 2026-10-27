// Package teamwork owns the persisted team plan, audit-event and Work Item
// binding model and its pure invariants.
package teamwork

import (
	"strings"
	"time"

	"github.com/RedHuang-0622/seelex/contract/dto"
)

type Plan = dto.TeamworkPlan
type Member = dto.TeamworkMember
type Milestone = dto.TeamworkMilestone
type WorkItem = dto.TeamworkWorkItem
type State = dto.TeamworkState

const (
	RoleMain = dto.TeamworkRoleMain
	RoleUser = dto.TeamworkRoleUser

	MilestonePending = dto.TeamworkMilestonePending
	MilestoneActive  = dto.TeamworkMilestoneActive
	MilestoneDone    = dto.TeamworkMilestoneDone

	ItemPending = dto.TeamworkItemPending
	ItemRunning = dto.TeamworkItemRunning
	ItemReview  = dto.TeamworkItemReview
	ItemDone    = dto.TeamworkItemDone
	ItemFailed  = dto.TeamworkItemFailed

	StateClosed = dto.TeamworkStateClosed
)

const (
	EventPlan      = "plan"
	EventDispatch  = "dispatch"
	EventJoin      = "join"
	EventMilestone = "milestone"
	EventRetire    = "retire"
	EventClose     = "close"
	EventItem      = "item"
	EventAccept    = "accept"
	EventFail      = "fail"
	EventSettle    = "settle"
	EventRecover   = "recover"
	EventMessage   = "message"
)

// Event is one append-only teamwork audit row.
type Event struct {
	At            time.Time `json:"at"`
	Kind          string    `json:"kind"`
	TeamID        string    `json:"team_id,omitempty"`
	Role          string    `json:"role,omitempty"`
	Handle        string    `json:"handle,omitempty"`
	Node          string    `json:"node,omitempty"`
	Milestone     string    `json:"milestone,omitempty"`
	WorkItem      string    `json:"work_item,omitempty"`
	RoleSessionID string    `json:"role_session_id,omitempty"`
	Detail        string    `json:"detail,omitempty"`
}

// Binding is one append-only Work Item binding fact.
type Binding struct {
	WorkItem      string `json:"work_item"`
	Milestone     string `json:"milestone,omitempty"`
	Role          string `json:"role"`
	SessionID     string `json:"session_id"`
	Worktree      string `json:"worktree,omitempty"`
	WorkspaceMode string `json:"workspace_mode,omitempty"`
	At            int64  `json:"at"`
	Released      bool   `json:"released,omitempty"`
	Reason        string `json:"reason,omitempty"`
}

// PlanIsEmpty reports whether no team has been declared.
func PlanIsEmpty(plan Plan) bool {
	return dto.TeamworkPlanIsEmpty(plan)
}

// WorkItemStatusOrPending maps the empty status to pending.
func WorkItemStatusOrPending(item WorkItem) string {
	return dto.TeamworkWorkItemStatusOrPending(item)
}

// WorkItemTerminal reports whether an item has a terminal status.
func WorkItemTerminal(item WorkItem) bool {
	return dto.TeamworkWorkItemTerminal(item)
}

// Bindings folds append-only binding rows by Work Item; the last row wins.
// Released rows remove the item from the live map.
func Bindings(rows []Binding) map[string]Binding {
	current := make(map[string]Binding, len(rows))
	for _, row := range rows {
		item := strings.TrimSpace(row.WorkItem)
		if item == "" {
			continue
		}
		if row.Released {
			delete(current, item)
			continue
		}
		current[item] = row
	}
	return current
}
