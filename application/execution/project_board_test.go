package execution

import (
	"testing"

	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/contract/protocol"
	worktable "github.com/RedHuang-0622/seelex/kernel/worktable"
)

func TestProjectBoardApplicationTrimsInputsAndBuildsLiveRows(t *testing.T) {
	var workSession string
	application := NewProjectBoard(ProjectBoardDeps{
		WorkTableInputs: func(sessionID string) (*model.PlanState, []dto.TaskRecord, []dto.SubAgentTreeNode, []dto.AsyncRunRecord) {
			workSession = sessionID
			return nil, []dto.TaskRecord{{
				ID: "task:1", Phase: "task", Task: "run", Status: dto.TaskRunning, Kind: "task",
			}}, nil, nil
		},
		WorkTableLimits: func() worktable.Limits {
			return worktable.Limits{WorkTableRows: 10, EvidenceChars: 800, PlanNodeEvents: 10}
		},
		ProjectRoot: func(sessionID string) string {
			if sessionID != " session-a " {
				t.Fatalf("ProjectRoot session = %q", sessionID)
			}
			return "/repo"
		},
		TeamPlan: func(sessionID string) *dto.TeamworkPlan {
			if sessionID != "session-a" {
				t.Fatalf("TeamPlan session = %q", sessionID)
			}
			return &dto.TeamworkPlan{
				TeamID: "team-a",
				State:  dto.TeamworkState{State: dto.TeamworkStateClosed},
			}
		},
	})
	rows := application.WorkTableRowsFor(" session-a ")
	if workSession != "session-a" || len(rows) == 0 || rows[0].ID != "task:1" {
		t.Fatalf("WorkTableRowsFor session=%q rows=%+v", workSession, rows)
	}
	if root := application.ProjectRootFor(" session-a "); root != "/repo" {
		t.Fatalf("ProjectRootFor = %q", root)
	}
	plan := application.ProjectBoardTeamPlanFor(" session-a ")
	if plan == nil || plan.TeamID != "team-a" {
		t.Fatalf("ProjectBoardTeamPlanFor = %+v", plan)
	}
	state := application.ProjectBoardTeamStateFor(" session-a ")
	if state == nil || state.State != dto.TeamworkStateClosed {
		t.Fatalf("ProjectBoardTeamStateFor = %+v", state)
	}
	plan.State.State = "mutated"
	if state.State != dto.TeamworkStateClosed {
		t.Fatalf("state aliases plan storage: %+v", state)
	}
}

func TestProjectBoardTeamStateUsesPlanOnly(t *testing.T) {
	application := NewProjectBoard(ProjectBoardDeps{
		WorkTableInputs: func(string) (*model.PlanState, []dto.TaskRecord, []dto.SubAgentTreeNode, []dto.AsyncRunRecord) {
			return nil, nil, nil, nil
		},
		WorkTableLimits: func() worktable.Limits { return worktable.Limits{} },
		TeamPlan:        func(string) *dto.TeamworkPlan { return nil },
	})
	if state := application.ProjectBoardTeamStateFor("session-a"); state != nil {
		t.Fatalf("state without plan = %+v", state)
	}
}
