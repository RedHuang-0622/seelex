package conversation

import (
	"context"
	"fmt"
)

// TurnRecoveryDeps supplies context recovery and finalization callbacks.
type TurnRecoveryDeps struct {
	TakeContextFailure func(requestID string) error
	Recover            func(ctx context.Context, turnErr error, originalInput string) (bool, error)
	Retryable          func(err error) bool
	Retry              func(ctx context.Context, requestID string, onChunk func(string)) error
	FinalizeBudget     func(ctx context.Context, requestID string, onChunk func(string)) error
	FinalizeTask       func(ctx context.Context, requestID, reply string) error
}

// TurnRecoveryApplication owns provider recovery and terminal finalization.
type TurnRecoveryApplication struct {
	deps TurnRecoveryDeps
}

// NewTurnRecovery constructs the turn-recovery application.
func NewTurnRecovery(deps TurnRecoveryDeps) *TurnRecoveryApplication {
	return &TurnRecoveryApplication{deps: deps}
}

// Finalize applies context failure, bounded retry, budget and task finalization.
func (application *TurnRecoveryApplication) Finalize(
	ctx context.Context,
	sessionID, requestID, originalInput, reply string,
	turnErr error,
	onChunk func(string),
) error {
	if contextErr := application.deps.TakeContextFailure(requestID); contextErr != nil {
		turnErr = contextErr
	}
	recovered, recoveryErr := application.deps.Recover(ctx, turnErr, originalInput)
	if recoveryErr != nil {
		turnErr = fmt.Errorf("%w; context recovery failed: %v", turnErr, recoveryErr)
	}
	if recovered && recoveryErr == nil && application.deps.Retryable(turnErr) {
		if retryErr := application.deps.Retry(ctx, requestID, onChunk); retryErr != nil {
			turnErr = fmt.Errorf("%w; bounded context recovery turn failed: %v", turnErr, retryErr)
		} else {
			turnErr = nil
		}
	}
	if turnErr == nil {
		turnErr = application.deps.FinalizeBudget(ctx, requestID, onChunk)
	}
	if turnErr == nil {
		turnErr = application.deps.FinalizeTask(ctx, requestID, reply)
	}
	return turnErr
}
