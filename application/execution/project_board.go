// Package execution owns read-only execution and project-board application
// projections. It does not own task execution, runtime state or persistence.
package execution

import (
	"strings"

	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/middle/protocol/model"
	"github.com/RedHuang-0622/seelex/middle/view/worktable"
)

// ProjectBoardDeps supplies the authoritative live inputs for the project
// board. WorkTable owns projection construction; the host owns all state.
type ProjectBoardDeps struct {
	WorkTableInputs func(sessionID string) (*model.PlanState, []dto.TaskRecord, []dto.SubAgentTreeNode, []dto.AsyncRunRecord)
	WorkTableLimits func() worktable.Limits
	ProjectRoot     func(sessionID string) string
	TeamPlan        func(sessionID string) *dto.TeamworkPlan
}

// ProjectBoardApplication coordinates the four project-board read entries.
type ProjectBoardApplication struct {
	deps ProjectBoardDeps
}

// NewProjectBoard constructs the project-board read application.
func NewProjectBoard(deps ProjectBoardDeps) *ProjectBoardApplication {
	return &ProjectBoardApplication{deps: deps}
}

// WorkTableRowsFor returns one session's live worktable projection.
func (application *ProjectBoardApplication) WorkTableRowsFor(sessionID string) []model.WorkItem {
	sessionID = strings.TrimSpace(sessionID)
	plan, tasks, tree, asyncRuns := application.deps.WorkTableInputs(sessionID)
	return worktable.BuildWorkTable(plan, tasks, tree, asyncRuns, application.deps.WorkTableLimits())
}

// ProjectRootFor returns the bound workspace root for the session.
func (application *ProjectBoardApplication) ProjectRootFor(sessionID string) string {
	return application.deps.ProjectRoot(sessionID)
}

// ProjectBoardTeamPlanFor returns the optional teamwork plan for the session.
func (application *ProjectBoardApplication) ProjectBoardTeamPlanFor(sessionID string) *dto.TeamworkPlan {
	return application.deps.TeamPlan(strings.TrimSpace(sessionID))
}

// ProjectBoardTeamStateFor extracts the run state from the authoritative plan.
func (application *ProjectBoardApplication) ProjectBoardTeamStateFor(sessionID string) *dto.TeamworkState {
	plan := application.ProjectBoardTeamPlanFor(sessionID)
	if plan == nil {
		return nil
	}
	state := plan.State
	return &state
}
