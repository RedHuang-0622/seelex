package store

// Compatibility aliases for the canonical teamwork vocabulary. Persistence and
// validation live in store/teamwork and are injected through Stores.Teamwork.

import storeteamwork "github.com/RedHuang-0622/seelex/store/teamwork"

const (
	TeamworkRoleMain = storeteamwork.RoleMain
	TeamworkRoleUser = storeteamwork.RoleUser

	TeamworkMilestonePending = storeteamwork.MilestonePending
	TeamworkMilestoneActive  = storeteamwork.MilestoneActive
	TeamworkMilestoneDone    = storeteamwork.MilestoneDone

	TeamworkEventPlan      = storeteamwork.EventPlan
	TeamworkEventDispatch  = storeteamwork.EventDispatch
	TeamworkEventJoin      = storeteamwork.EventJoin
	TeamworkEventMilestone = storeteamwork.EventMilestone
	TeamworkEventRetire    = storeteamwork.EventRetire
	TeamworkEventClose     = storeteamwork.EventClose
	TeamworkEventItem      = storeteamwork.EventItem
	TeamworkEventAccept    = storeteamwork.EventAccept
	TeamworkEventFail      = storeteamwork.EventFail
	TeamworkEventSettle    = storeteamwork.EventSettle
	TeamworkEventRecover   = storeteamwork.EventRecover
	TeamworkEventMessage   = storeteamwork.EventMessage

	TeamworkStateClosed = storeteamwork.StateClosed

	TeamworkItemPending = storeteamwork.ItemPending
	TeamworkItemRunning = storeteamwork.ItemRunning
	TeamworkItemReview  = storeteamwork.ItemReview
	TeamworkItemDone    = storeteamwork.ItemDone
	TeamworkItemFailed  = storeteamwork.ItemFailed
)

type TeamworkPlan = storeteamwork.Plan
type TeamworkMember = storeteamwork.Member
type TeamworkMilestone = storeteamwork.Milestone
type TeamworkState = storeteamwork.State
type TeamworkEvent = storeteamwork.Event
type TeamworkWorkItem = storeteamwork.WorkItem
type TeamworkBinding = storeteamwork.Binding

func TeamworkPlanIsEmpty(plan TeamworkPlan) bool {
	return storeteamwork.PlanIsEmpty(plan)
}

func ValidateTeamworkPlan(plan TeamworkPlan, maxTeammates int) error {
	return storeteamwork.ValidatePlan(plan, maxTeammates)
}

func TeamworkWorkItemStatusOrPending(item TeamworkWorkItem) string {
	return storeteamwork.WorkItemStatusOrPending(item)
}

func TeamworkWorkItemTerminal(item TeamworkWorkItem) bool {
	return storeteamwork.WorkItemTerminal(item)
}

func TeamworkBindings(rows []TeamworkBinding) map[string]TeamworkBinding {
	return storeteamwork.Bindings(rows)
}
