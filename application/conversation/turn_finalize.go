package conversation

import (
	"context"

	session_runtime "github.com/RedHuang-0622/seelex/kernel/session"
)

// TurnFinalizeDeps supplies the tail completion callback.
type TurnFinalizeDeps struct {
	TailComplete func(location session_runtime.Location, publication Publication, nextContext context.Context)
}

// TurnFinalizeApplication maps commit results to tail publications.
type TurnFinalizeApplication struct {
	deps TurnFinalizeDeps
}

// NewTurnFinalize constructs the finalize application.
func NewTurnFinalize(deps TurnFinalizeDeps) *TurnFinalizeApplication {
	return &TurnFinalizeApplication{deps: deps}
}

// Finalize ignores stale results and starts the tail for current results.
func (application *TurnFinalizeApplication) Finalize(committed CommitResult) {
	if committed.Stale {
		return
	}
	application.deps.TailComplete(committed.Location, Publication{
		SessionID: committed.SessionID, RequestID: committed.RequestID,
		Revision: committed.Revision, VisibleError: committed.VisibleError,
		TurnErr: committed.TurnErr, Promotion: committed.Promotion,
		NextUser: committed.NextUser, NextAssistant: committed.NextAssistant,
	}, committed.Promotion.Context)
}
