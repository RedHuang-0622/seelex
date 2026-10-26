// Package goal owns Goal application routing. Goal state machines remain in
// backend/multiagent/goal behind a narrow coordinator port.
package goal

import (
	"context"
	"errors"
	"fmt"
	"strings"

	goaldomain "github.com/RedHuang-0622/seelex/backend/multiagent/goal"
	"github.com/RedHuang-0622/seelex/contract/dto"
)

// ErrCoordinatorUnavailable reports that the session goal coordinator is not
// assembled.
var ErrCoordinatorUnavailable = errors.New("goal: 会话 goal 协调器未装配")

// CoordinatorPort is the application-facing surface of the session goal
// coordinator.
type CoordinatorPort interface {
	GoalGovernanceViewFor(sessionID string) *dto.GoalGovernanceView
	Begin(context.Context, string, goaldomain.BeginRequest) (*goaldomain.GoalRecord, error)
	Update(context.Context, string, goaldomain.UpdateRequest) (*goaldomain.GoalRecord, error)
	FinishDirect(context.Context, string, goaldomain.FinishRequest, bool) (*goaldomain.GoalRecord, error)
	StatusFor(sessionID string) goaldomain.StatusView
}

// Deps supplies the current coordinator and runtime projection refresh.
type Deps struct {
	Coordinator func() CoordinatorPort
	Refresh     func(sessionID string)
}

// Application owns goal use-case routing without owning the goal state machine.
type Application struct {
	deps Deps
}

// New constructs the Goal application.
func New(deps Deps) *Application {
	return &Application{deps: deps}
}

func (application *Application) coordinatorFor(sessionID string) (CoordinatorPort, error) {
	if application == nil || application.deps.Coordinator == nil {
		return nil, ErrCoordinatorUnavailable
	}
	coordinator := application.deps.Coordinator()
	if coordinator == nil {
		return nil, ErrCoordinatorUnavailable
	}
	if strings.TrimSpace(sessionID) == "" {
		return nil, fmt.Errorf("goal: session ID is required")
	}
	return coordinator, nil
}

// GovernanceViewFor returns the session's goal governance projection.
func (application *Application) GovernanceViewFor(sessionID string) *dto.GoalGovernanceView {
	if application == nil || application.deps.Coordinator == nil {
		return nil
	}
	coordinator := application.deps.Coordinator()
	if coordinator == nil {
		return nil
	}
	return coordinator.GoalGovernanceViewFor(sessionID)
}

// BeginFor begins a session goal.
func (application *Application) BeginFor(ctx context.Context, sessionID string, request goaldomain.BeginRequest) (*goaldomain.GoalRecord, error) {
	coordinator, err := application.coordinatorFor(sessionID)
	if err != nil {
		return nil, err
	}
	record, err := coordinator.Begin(ctx, sessionID, request)
	if err != nil {
		return nil, err
	}
	application.deps.Refresh(sessionID)
	return record, nil
}

// UpdateFor updates a session goal.
func (application *Application) UpdateFor(ctx context.Context, sessionID string, request goaldomain.UpdateRequest) (*goaldomain.GoalRecord, error) {
	coordinator, err := application.coordinatorFor(sessionID)
	if err != nil {
		return nil, err
	}
	record, err := coordinator.Update(ctx, sessionID, request)
	if err == nil {
		application.deps.Refresh(sessionID)
	}
	return record, err
}

// DoneFor finishes or aborts a session goal.
func (application *Application) DoneFor(ctx context.Context, sessionID string, request goaldomain.FinishRequest, abort bool) (*goaldomain.GoalRecord, error) {
	coordinator, err := application.coordinatorFor(sessionID)
	if err != nil {
		return nil, err
	}
	record, err := coordinator.FinishDirect(ctx, sessionID, request, abort)
	if err == nil {
		application.deps.Refresh(sessionID)
	}
	return record, err
}

// StatusFor returns the session goal status.
func (application *Application) StatusFor(sessionID string) (goaldomain.StatusView, error) {
	coordinator, err := application.coordinatorFor(sessionID)
	if err != nil {
		return goaldomain.StatusView{}, err
	}
	return coordinator.StatusFor(sessionID), nil
}
