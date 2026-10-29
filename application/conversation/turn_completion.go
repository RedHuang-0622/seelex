package conversation

import (
	"context"
	"fmt"

	viewerrors "github.com/RedHuang-0622/seelex/application/errorpresentation"
	"github.com/RedHuang-0622/seelex/contract"
	session_runtime "github.com/RedHuang-0622/seelex/kernel/session"
)

// TurnCompletionDeps supplies persistence and consumed-queue settlement.
type TurnCompletionDeps struct {
	LocateSession          func(sessionID string) session_runtime.Location
	PersistSession         func(location session_runtime.Location, sessionID string) error
	QueueFailConsumedInput func(location session_runtime.Location, sessionID, requestID string) error
	QueueConfirmConsumed   func(location session_runtime.Location, sessionID, requestID string) error
	// CommitContextDraft 在会话持久化前等待 draft actor 的 durable ack；失败时
	// draft 保持 uncommitted，不能被当作已提交历史。
	CommitContextDraft func(ctx context.Context, sessionID string) error
}

// TurnCompletionApplication owns the durable turn completion transaction.
type TurnCompletionApplication struct {
	deps TurnCompletionDeps
}

// NewTurnCompletion constructs the turn-completion application.
func NewTurnCompletion(deps TurnCompletionDeps) *TurnCompletionApplication {
	return &TurnCompletionApplication{deps: deps}
}

// Persist settles consumed input according to persistence success.
func (application *TurnCompletionApplication) Persist(sessionID, requestID string, turnErr error) (session_runtime.Location, error) {
	location := application.deps.LocateSession(sessionID)
	if application.deps.CommitContextDraft != nil {
		if err := application.deps.CommitContextDraft(context.Background(), sessionID); err != nil {
			_ = application.deps.QueueFailConsumedInput(location, sessionID, requestID)
			return location, viewerrors.Wrap(
				fmt.Errorf("context draft commit failed: %w", err),
				contract.ErrorCodePersistenceFailed,
			)
		}
	}
	saveErr := application.deps.PersistSession(location, sessionID)
	if saveErr != nil {
		_ = application.deps.QueueFailConsumedInput(location, sessionID, requestID)
		if turnErr != nil {
			return location, viewerrors.Wrap(
				fmt.Errorf("%w; persistence failed and recovery is not guaranteed: %v", turnErr, saveErr),
				contract.ErrorCodePersistenceFailed,
			)
		}
		return location, viewerrors.Wrap(
			fmt.Errorf("persistence failed and recovery is not guaranteed: %w", saveErr),
			contract.ErrorCodePersistenceFailed,
		)
	}
	_ = application.deps.QueueConfirmConsumed(location, sessionID, requestID)
	return location, turnErr
}
