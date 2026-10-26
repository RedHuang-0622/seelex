package core

import (
	applicationexecution "github.com/RedHuang-0622/seelex/application/execution"
	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/middle/protocol/model"
	"github.com/RedHuang-0622/seelex/middle/view/board"
	"github.com/RedHuang-0622/seelex/middle/view/worktable"
	"github.com/RedHuang-0622/seelex/store"
)

// WorkTableRowsFor returns the live worktable projection for one session.
func (service *Service) WorkTableRowsFor(sessionID string) []model.WorkItem {
	return service.projectBoardQueryComponent().WorkTableRowsFor(sessionID)
}

// ProjectRootFor returns the bound workspace root for the session.
func (service *Service) ProjectRootFor(sessionID string) string {
	return service.projectBoardQueryComponent().ProjectRootFor(sessionID)
}

// ProjectBoardTeamPlanFor returns the optional teamwork plan for the session.
func (service *Service) ProjectBoardTeamPlanFor(sessionID string) *store.TeamworkPlan {
	return service.projectBoardQueryComponent().ProjectBoardTeamPlanFor(sessionID)
}

// ProjectBoardTeamStateFor returns the plan's authoritative run state.
func (service *Service) ProjectBoardTeamStateFor(sessionID string) *store.TeamworkState {
	return service.projectBoardQueryComponent().ProjectBoardTeamStateFor(sessionID)
}

func (service *Service) projectBoardQueryComponent() *applicationexecution.ProjectBoardApplication {
	if service == nil {
		return nil
	}
	return service.components.projectBoardQuery
}

func newServiceProjectBoardQuery(service *Service) *applicationexecution.ProjectBoardApplication {
	return applicationexecution.NewProjectBoard(applicationexecution.ProjectBoardDeps{
		WorkTableInputs: func(sessionID string) (*model.PlanState, []dto.TaskRecord, []dto.SubAgentTreeNode, []dto.AsyncRunRecord) {
			service.ViewMu.RLock()
			viewSessionID := service.Core.Snapshot.Session.ID
			plan := service.sessionActivePlanLocked(sessionID)
			tree := worktable.CloneSubAgentTreeForSync(service.Core.Snapshot.Runtime.SubAgentTree)
			service.ViewMu.RUnlock()
			return plan, service.taskRecordsForSession(sessionID, viewSessionID), tree, service.asyncRunsForTable()
		},
		WorkTableLimits: workTableBounds,
		ProjectRoot: func(sessionID string) string {
			return service.components.sessionQuery.ProjectRootFor(sessionID)
		},
		TeamPlan: func(sessionID string) *store.TeamworkPlan {
			if service.Deps.Runtime == nil || sessionID == "" {
				return nil
			}
			source, ok := service.Deps.Runtime.(board.TeamworkPlanSource)
			if !ok {
				return nil
			}
			plan, ok := source.TeamworkPlanFor(sessionID)
			if !ok {
				return nil
			}
			return &plan
		},
	})
}

// taskRecordsForSession selects the live task registry or the session scope.
// WorkTable reads and trace reads share this single predicate.
func (state *serviceState) taskRecordsForSession(sessionID, viewSessionID string) []dto.TaskRecord {
	if state == nil || state.Deps.Runtime == nil {
		return nil
	}
	forTasks, ok := state.Deps.Runtime.(interface{ TaskSnapshotFor(string) []dto.TaskRecord })
	if !ok {
		return state.Deps.Runtime.TaskSnapshot()
	}
	scope := sessionID
	if scope == "" || scope == viewSessionID {
		scope = ""
	}
	return forTasks.TaskSnapshotFor(scope)
}
