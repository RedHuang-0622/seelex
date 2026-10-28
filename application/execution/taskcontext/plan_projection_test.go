package task_context_test

import (
	"testing"

	task_context "github.com/RedHuang-0622/seelex/application/execution/taskcontext"
	"github.com/RedHuang-0622/seelex/contract/protocol"
)

func TestPlanProjectionApplicationUsesCurrentSnapshotOrBackendProjection(t *testing.T) {
	current := &model.PlanState{Name: "view"}
	background := &model.PlanState{Name: "background"}
	var mirrored *model.PlanState
	application := task_context.NewPlanProjection(task_context.PlanProjectionDeps{
		CurrentSessionID: func() string { return "session-a" },
		CurrentPlan:      func() *model.PlanState { return current },
		Fallback: func(string) *model.PlanState {
			return &model.PlanState{Name: "fallback"}
		},
		ProjectionFor: func(sessionID string, fallback func() *model.PlanState) *model.PlanState {
			if sessionID != "session-b" {
				t.Fatalf("sessionID = %q", sessionID)
			}
			if fallback().Name != "fallback" {
				t.Fatal("background projection did not consume fallback")
			}
			return background
		},
		ProjectionCopy: func(sessionID string) *model.PlanState {
			if sessionID == "session-b" {
				return background
			}
			return nil
		},
		Ensure: func(sessionID string, plan *model.PlanState) {
			if sessionID != "session-a" || plan.Name != "fallback" {
				t.Fatalf("Ensure(%q,%v)", sessionID, plan)
			}
		},
		Mirror: func(plan *model.PlanState) { mirrored = plan },
	})

	if got := application.PlanLocked("session-a"); got != current {
		t.Fatalf("current plan = %v", got)
	}
	if got := application.PlanLocked("session-b"); got != background {
		t.Fatalf("background plan = %v", got)
	}
	application.MirrorLocked("session-a")
	if mirrored == nil || mirrored.Name != "fallback" {
		t.Fatalf("mirrored = %v", mirrored)
	}
}
