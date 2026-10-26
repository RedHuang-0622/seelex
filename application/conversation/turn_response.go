package conversation

import (
	"context"

	"github.com/RedHuang-0622/seelex/contract"
)

// TurnResponseDeps supplies reasoning/output/transcript callbacks.
type TurnResponseDeps struct {
	AttachReasoning   func(sessionID, requestID string)
	ObserveOutput     func(ctx context.Context, requestID, reply string, err error) error
	EnsureFinal       func(requestID, reply string)
	HistoryFor        func(sessionID string) []contract.EngineMessage
	BackfillReasoning func(sessionID string, history []contract.EngineMessage)
}

// TurnResponseApplication owns post-stream response finalization.
type TurnResponseApplication struct {
	deps TurnResponseDeps
}

// NewTurnResponse constructs the turn-response application.
func NewTurnResponse(deps TurnResponseDeps) *TurnResponseApplication {
	return &TurnResponseApplication{deps: deps}
}

// Finalize observes model output and backfills assistant reasoning.
func (application *TurnResponseApplication) Finalize(
	ctx context.Context,
	sessionID, requestID, reply string,
	turnErr error,
) {
	if turnErr == nil {
		application.deps.AttachReasoning(sessionID, requestID)
	}
	_ = application.deps.ObserveOutput(ctx, requestID, reply, turnErr)
	if reply != "" {
		application.deps.EnsureFinal(requestID, reply)
	}
	application.deps.BackfillReasoning(sessionID, application.deps.HistoryFor(sessionID))
}
