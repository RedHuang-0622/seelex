package conversation

import (
	"log"

	view_state "github.com/RedHuang-0622/seelex/application/session/viewstate"
	"github.com/RedHuang-0622/seelex/contract/protocol"
	viewerrors "github.com/RedHuang-0622/seelex/middle/view/errors"
)

// TurnOutcomeDeps supplies error/projection/snapshot callbacks.
type TurnOutcomeDeps struct {
	ClearError             func(unit ChatStateUnit)
	SetError               func(unit ChatStateUnit, visibleError string)
	AppendErrorLine        func(sessionID, visibleError string)
	ApplyRuntimeProjection func(sessionID string, projection view_state.RuntimeStateProjection)
	SetSessionChat         func(sessionID string, chat model.ChatState)
	MirrorTask             func(sessionID string)
	BumpRevision           func() uint64
}

// TurnOutcomeApplication applies terminal visible result state.
type TurnOutcomeApplication struct {
	deps TurnOutcomeDeps
}

// NewTurnOutcome constructs the turn-outcome application.
func NewTurnOutcome(deps TurnOutcomeDeps) *TurnOutcomeApplication {
	return &TurnOutcomeApplication{deps: deps}
}

// ApplyLocked applies the current outcome or rejects a superseded request.
func (application *TurnOutcomeApplication) ApplyLocked(
	sessionID, requestID string,
	unit ChatStateUnit,
	projection view_state.RuntimeStateProjection,
	turnErr error,
) (string, bool) {
	if unit.ChatState().RequestID != requestID {
		return "", true
	}
	application.deps.ClearError(unit)
	visibleError := ""
	if turnErr != nil {
		if viewerrors.IsUnclassified(turnErr) {
			log.Printf("[runChat] request_id=%s unclassified_error=%v", requestID, turnErr)
		}
		visibleError = turnErr.Error()
		application.deps.SetError(unit, visibleError)
		application.deps.AppendErrorLine(sessionID, visibleError)
	}
	application.deps.ApplyRuntimeProjection(sessionID, projection)
	return visibleError, false
}

// MirrorLocked mirrors ChatState and active task/revision.
func (application *TurnOutcomeApplication) MirrorLocked(sessionID string, unit ChatStateUnit, active bool) uint64 {
	application.deps.SetSessionChat(sessionID, unit.ChatState())
	if !active {
		return 0
	}
	application.deps.MirrorTask(sessionID)
	return application.deps.BumpRevision()
}
