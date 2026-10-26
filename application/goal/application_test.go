package goal

import (
	"context"
	"errors"
	"strings"
	"testing"

	goaldomain "github.com/RedHuang-0622/seelex/backend/multiagent/goal"
	"github.com/RedHuang-0622/seelex/contract/dto"
)

type coordinatorFake struct {
	beginErr  error
	updateErr error
	finishErr error
	refreshed []string
}

func (coordinator *coordinatorFake) GoalGovernanceViewFor(sessionID string) *dto.GoalGovernanceView {
	return &dto.GoalGovernanceView{GoalID: sessionID}
}
func (coordinator *coordinatorFake) Begin(context.Context, string, goaldomain.BeginRequest) (*goaldomain.GoalRecord, error) {
	return &goaldomain.GoalRecord{ID: "goal-a"}, coordinator.beginErr
}
func (coordinator *coordinatorFake) Update(context.Context, string, goaldomain.UpdateRequest) (*goaldomain.GoalRecord, error) {
	return &goaldomain.GoalRecord{ID: "goal-a"}, coordinator.updateErr
}
func (coordinator *coordinatorFake) FinishDirect(context.Context, string, goaldomain.FinishRequest, bool) (*goaldomain.GoalRecord, error) {
	return &goaldomain.GoalRecord{ID: "goal-a"}, coordinator.finishErr
}
func (coordinator *coordinatorFake) StatusFor(string) goaldomain.StatusView {
	return goaldomain.StatusView{Active: &dto.GoalRecord{ID: "goal-a"}}
}

func TestGoalApplicationRoutesAndRefreshesOnSuccess(t *testing.T) {
	coordinator := &coordinatorFake{}
	application := New(Deps{
		Coordinator: func() CoordinatorPort { return coordinator },
		Refresh: func(sessionID string) {
			coordinator.refreshed = append(coordinator.refreshed, sessionID)
		},
		WriteSessionID: func(context.Context) string { return "session-a" },
		ReadSessionID:  func(context.Context) string { return "session-a" },
	})
	record, err := application.BeginFor(context.Background(), "session-a", goaldomain.BeginRequest{})
	if err != nil || record.ID != "goal-a" {
		t.Fatalf("BeginFor = %+v, %v", record, err)
	}
	if _, err := application.UpdateFor(context.Background(), "session-a", goaldomain.UpdateRequest{}); err != nil {
		t.Fatal(err)
	}
	if _, err := application.DoneFor(context.Background(), "session-a", goaldomain.FinishRequest{}, false); err != nil {
		t.Fatal(err)
	}
	if strings.Join(coordinator.refreshed, ",") != "session-a,session-a,session-a" {
		t.Fatalf("refreshes = %+v", coordinator.refreshed)
	}
	if view := application.GovernanceViewFor("session-a"); view == nil || view.GoalID != "session-a" {
		t.Fatalf("GovernanceViewFor = %+v", view)
	}
	if status, err := application.StatusFor("session-a"); err != nil || status.Active == nil ||
		status.Active.ID != "goal-a" {
		t.Fatalf("StatusFor = %+v, %v", status, err)
	}
	if _, err := application.BeginHandler(context.Background(), `{"title":"goal"}`); err != nil {
		t.Fatalf("BeginHandler: %v", err)
	}
	if _, err := application.UpdateHandler(context.Background(), `{"progress_content":"done"}`); err != nil {
		t.Fatalf("UpdateHandler: %v", err)
	}
	if _, err := application.StatusHandler(context.Background(), `{}`); err != nil {
		t.Fatalf("StatusHandler: %v", err)
	}
	if _, err := application.DoneHandler(context.Background(), `{"action":"abort","reason":"test"}`); err != nil {
		t.Fatalf("DoneHandler: %v", err)
	}
}

func TestGoalHandlersRejectMalformedInput(t *testing.T) {
	application := New(Deps{
		Coordinator:    func() CoordinatorPort { return &coordinatorFake{} },
		Refresh:        func(string) {},
		WriteSessionID: func(context.Context) string { return "session-a" },
		ReadSessionID:  func(context.Context) string { return "session-a" },
	})
	if _, err := application.BeginHandler(context.Background(), `{`); err == nil {
		t.Fatal("BeginHandler accepted malformed JSON")
	}
	if _, err := application.DoneHandler(context.Background(), `{"action":"complete"}`); err == nil {
		t.Fatal("DoneHandler accepted unsupported action")
	}
}

func TestGoalApplicationDoesNotRefreshOnErrors(t *testing.T) {
	updateErr := errors.New("update")
	coordinator := &coordinatorFake{updateErr: updateErr}
	application := New(Deps{
		Coordinator: func() CoordinatorPort { return coordinator },
		Refresh: func(sessionID string) {
			t.Fatalf("refresh after error: %s", sessionID)
		},
	})
	if _, err := application.UpdateFor(context.Background(), "session-a", goaldomain.UpdateRequest{}); !errors.Is(err, updateErr) {
		t.Fatalf("UpdateFor error = %v", err)
	}
}

func TestGoalApplicationRequiresCoordinatorAndSession(t *testing.T) {
	application := New(Deps{Coordinator: func() CoordinatorPort { return nil }})
	if _, err := application.BeginFor(context.Background(), "session-a", goaldomain.BeginRequest{}); !errors.Is(err, ErrCoordinatorUnavailable) {
		t.Fatalf("missing coordinator error = %v", err)
	}
	coordinator := &coordinatorFake{}
	application = New(Deps{Coordinator: func() CoordinatorPort { return coordinator }})
	if _, err := application.BeginFor(context.Background(), " ", goaldomain.BeginRequest{}); err == nil ||
		!strings.Contains(err.Error(), "session ID is required") {
		t.Fatalf("empty session error = %v", err)
	}
}
