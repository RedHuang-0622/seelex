package conversation

import (
	"fmt"

	view_state "github.com/RedHuang-0622/seelex/middle/view/state"
)

// TurnCleanupDeps supplies stream, route, context and projection callbacks.
type TurnCleanupDeps struct {
	StampRoute        func(sessionID, requestID string)
	DiscardMailbox    func(sessionID string)
	CollectProjection func(sessionID string) view_state.RuntimeStateProjection
	RemoveCheckpoints func(sessionID string) error
	RemoveRecovery    func(sessionID string) error
	RecordError       func(sessionID, requestID string, err error)
}

// TurnCleanupApplication owns pre-persistence cleanup.
type TurnCleanupApplication struct {
	deps TurnCleanupDeps
}

// NewTurnCleanup constructs the turn-cleanup application.
func NewTurnCleanup(deps TurnCleanupDeps) *TurnCleanupApplication {
	return &TurnCleanupApplication{deps: deps}
}

// Cleanup flushes, stamps route, discards mailbox, collects projection and
// removes transient recovery/checkpoint state.
func (application *TurnCleanupApplication) Cleanup(
	sessionID, requestID string,
	turnErr error,
	flush func() error,
	stopDraft func(),
) (view_state.RuntimeStateProjection, error) {
	if flushErr := flush(); flushErr != nil && turnErr == nil {
		turnErr = fmt.Errorf("flush streamed response: %w", flushErr)
	}
	application.deps.StampRoute(sessionID, requestID)
	application.deps.DiscardMailbox(sessionID)
	projection := application.deps.CollectProjection(sessionID)
	if cleanupErr := application.deps.RemoveCheckpoints(sessionID); cleanupErr != nil && turnErr == nil {
		turnErr = cleanupErr
	}
	if turnErr == nil {
		if cleanupErr := application.deps.RemoveRecovery(sessionID); cleanupErr != nil {
			turnErr = cleanupErr
		}
	}
	if turnErr != nil {
		application.deps.RecordError(sessionID, requestID, turnErr)
	}
	stopDraft()
	return projection, turnErr
}
