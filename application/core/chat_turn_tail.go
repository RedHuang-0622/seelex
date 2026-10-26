package core

import (
	"context"

	applicationconversation "github.com/RedHuang-0622/seelex/application/conversation"
)

func newServiceConversationTurnTail(service *Service) *applicationconversation.TurnTailApplication {
	return applicationconversation.NewTurnTail(applicationconversation.TurnTailDeps{
		RebindWorkspaceWhenIdle: service.rebindViewWorkspaceWhenIdle,
		MarkPromotedConsumed:    service.components.sessions.QueueMarkConsumedInput,
		Publish:                 service.components.conversationTurnPublisher.Publish,
		StartNext: func(ctx context.Context, sessionID, requestID string, request applicationconversation.Request) {
			go service.runChat(ctx, sessionID, requestID, request)
		},
		ReconcileSessionRelease: service.reconcileSessionRelease,
		FlushToolRecord:         service.FlushToolRecord,
		Debug:                   runChatDebug,
	})
}
