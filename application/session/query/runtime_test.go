package query

import (
	"testing"

	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/middle/protocol/model"
)

func TestSessionRuntimeOfSelectsAndDetachesSessionFields(t *testing.T) {
	runtime := model.RuntimeState{
		Provider:       "process-only",
		Effort:         "high",
		FullAccess:     true,
		PermissionTier: "auto",
		ActiveSkills:   []string{"skill-a"},
		GoalGovernance: &dto.GoalGovernanceView{History: []dto.GoalHistoryView{{Title: "one"}}},
		WorkTable:      []model.WorkItem{{ID: "task-1"}},
	}
	sessionRuntime := SessionRuntimeOf(runtime)
	if sessionRuntime.Effort != "high" || !sessionRuntime.FullAccess || sessionRuntime.PermissionTier != "auto" {
		t.Fatalf("session runtime = %+v", sessionRuntime)
	}
	sessionRuntime.ActiveSkills[0] = "mutated"
	sessionRuntime.GoalGovernance.History[0].Title = "mutated"
	sessionRuntime.WorkTable[0].ID = "mutated"
	if runtime.ActiveSkills[0] != "skill-a" || runtime.GoalGovernance.History[0].Title != "one" || runtime.WorkTable[0].ID != "task-1" {
		t.Fatalf("session runtime shares mutable state: %+v", runtime)
	}
}

func TestCloneGoalGovernanceViewKeepsNil(t *testing.T) {
	if got := CloneGoalGovernanceView(nil); got != nil {
		t.Fatalf("nil clone = %+v", got)
	}
}
