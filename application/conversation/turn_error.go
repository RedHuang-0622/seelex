package conversation

import (
	"context"
	"errors"

	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/contract/protocol"
)

// TurnErrorDeps supplies task state and terminal mutation operations.
type TurnErrorDeps struct {
	TaskFor                 func(sessionID string) *model.TaskState
	ClassifyProviderFailure func(error) contract.ProviderFailureKind
	Interrupt               func(requestID, reason string)
	Fail                    func(requestID, reason string)
}

// TurnErrorApplication classifies an unhandled turn error against the
// authoritative Task state.
type TurnErrorApplication struct {
	deps TurnErrorDeps
}

// NewTurnErrorApplication constructs the turn-error application.
func NewTurnErrorApplication(deps TurnErrorDeps) *TurnErrorApplication {
	return &TurnErrorApplication{deps: deps}
}

// Record moves the active Task to the matching terminal state.
func (application *TurnErrorApplication) Record(sessionID, requestID string, err error) {
	task := application.deps.TaskFor(sessionID)
	if task == nil || task.RequestID != requestID || task.Status != model.TurnProgressing {
		return
	}
	if errors.Is(err, context.Canceled) {
		application.deps.Interrupt(requestID, "The task was canceled before completion.")
		return
	}
	if application.deps.ClassifyProviderFailure(err) != contract.ProviderFailureNone {
		application.deps.Interrupt(requestID, "The provider interrupted the task before completion.")
		return
	}
	application.deps.Fail(requestID, "The task stopped before a verified completion could be delivered.")
}
