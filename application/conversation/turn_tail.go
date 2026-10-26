package conversation

import (
	"context"

	session_runtime "github.com/RedHuang-0622/seelex/backend/engine/session"
)

// TurnTailDeps supplies post-lock completion callbacks.
type TurnTailDeps struct {
	RebindWorkspaceWhenIdle func()
	MarkPromotedConsumed    func(location session_runtime.Location, sessionID, requestID string) error
	Publish                 func(publication Publication)
	StartNext               func(ctx context.Context, sessionID, requestID string, request Request)
	ReconcileSessionRelease func()
	FlushToolRecord         func(sessionID string)
	Debug                   func(format string, args ...any)
}

// TurnTailApplication owns the asynchronous post-lock tail.
type TurnTailApplication struct {
	deps TurnTailDeps
}

// NewTurnTail constructs the tail application.
func NewTurnTail(deps TurnTailDeps) *TurnTailApplication {
	return &TurnTailApplication{deps: deps}
}

// Complete settles promoted input, publishes, starts the next turn and flushes.
func (application *TurnTailApplication) Complete(location session_runtime.Location, publication Publication, nextContext context.Context) {
	application.deps.RebindWorkspaceWhenIdle()
	if application.deps.Debug != nil {
		application.deps.Debug(
			"runChat tail session=%s request=%s err=%v processQueue=%v nextRequest=%q",
			publication.SessionID, publication.RequestID, publication.TurnErr,
			publication.Promotion.Promoted, publication.Promotion.RequestID,
		)
	}
	if publication.Promotion.Promoted && publication.Promotion.RequestID != "" {
		if err := application.deps.MarkPromotedConsumed(location, publication.SessionID, publication.Promotion.RequestID); err != nil &&
			application.deps.Debug != nil {
			application.deps.Debug(
				"durable queue mark consumed session=%s turn=%s err=%v",
				publication.SessionID, publication.Promotion.RequestID, err,
			)
		}
	}
	application.deps.Publish(publication)
	if publication.Promotion.Promoted {
		application.deps.StartNext(nextContext, publication.SessionID, publication.Promotion.RequestID, publication.Promotion.Request)
	}
	application.deps.ReconcileSessionRelease()
	application.deps.FlushToolRecord(publication.SessionID)
}
