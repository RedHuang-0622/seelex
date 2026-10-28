package task_context

import "github.com/RedHuang-0622/seelex/contract/protocol"

// PlanProjectionDeps supplies current-session reads and the backend projection
// owner.
type PlanProjectionDeps struct {
	CurrentSessionID func() string
	CurrentPlan      func() *model.PlanState
	Fallback         func(sessionID string) *model.PlanState
	ProjectionFor    func(sessionID string, fallback func() *model.PlanState) *model.PlanState
	ProjectionCopy   func(sessionID string) *model.PlanState
	Ensure           func(sessionID string, plan *model.PlanState)
	Mirror           func(plan *model.PlanState)
}

// PlanProjectionApplication owns selection and mirroring of historical Plan
// projections.
type PlanProjectionApplication struct {
	deps PlanProjectionDeps
}

// NewPlanProjection constructs the Plan projection application.
func NewPlanProjection(deps PlanProjectionDeps) *PlanProjectionApplication {
	return &PlanProjectionApplication{deps: deps}
}

// PlanLocked returns the current view plan or the backend projection for a
// background session. The caller owns the shared view lock.
func (application *PlanProjectionApplication) PlanLocked(sessionID string) *model.PlanState {
	if sessionID == "" || sessionID == application.deps.CurrentSessionID() {
		return application.deps.CurrentPlan()
	}
	return application.deps.ProjectionFor(sessionID, func() *model.PlanState {
		return application.deps.Fallback(sessionID)
	})
}

// MirrorLocked refreshes the active snapshot plan, seeding the projection when
// it is absent. The caller owns the shared view lock.
func (application *PlanProjectionApplication) MirrorLocked(sessionID string) {
	application.MirrorWithFallbackLocked(sessionID, func() *model.PlanState {
		return application.deps.Fallback(sessionID)
	})
}

// MirrorWithFallbackLocked is MirrorLocked with a path-specific baseline.
func (application *PlanProjectionApplication) MirrorWithFallbackLocked(
	sessionID string,
	fallback func() *model.PlanState,
) {
	plan := application.deps.ProjectionCopy(sessionID)
	if plan == nil {
		plan = fallback()
		application.deps.Ensure(sessionID, plan)
	}
	application.deps.Mirror(plan)
}
