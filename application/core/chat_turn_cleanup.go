package core

import (
	"context"

	applicationconversation "github.com/RedHuang-0622/seelex/application/conversation"
	view_state "github.com/RedHuang-0622/seelex/application/session/viewstate"
)

func newServiceConversationTurnCleanup(service *Service) *applicationconversation.TurnCleanupApplication {
	return applicationconversation.NewTurnCleanup(applicationconversation.TurnCleanupDeps{
		StampRoute:     service.stampServedRoute,
		DiscardMailbox: service.discardPendingSubagentContextsFor,
		CollectProjection: func(sessionID string) view_state.RuntimeStateProjection {
			return service.components.view.CollectRuntimeProjectionFor(context.Background(), sessionID)
		},
		RemoveCheckpoints: func(sessionID string) error {
			return service.components.context.RemoveTaskContextCheckpointsFor(sessionID)
		},
		RemoveRecovery: service.removeProviderContextRecoveryFor,
		RecordError: func(sessionID, requestID string, err error) {
			service.ViewMu.Lock()
			service.recordUnhandledTaskErrorLocked(sessionID, requestID, err)
			service.ViewMu.Unlock()
		},
	})
}
