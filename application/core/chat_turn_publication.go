package core

import (
	applicationconversation "github.com/RedHuang-0622/seelex/application/conversation"
	"github.com/RedHuang-0622/seelex/application/event"
)

func newServiceConversationTurnPublisher(service *Service) *applicationconversation.TurnPublisherApplication {
	return applicationconversation.NewTurnPublisher(applicationconversation.TurnPublisherDeps{
		PublishChatState: service.publishChatStateFor,
		PublishEvent: func(kind event.EventKind, revision uint64, requestID, sessionID string, payload any) {
			service.publishSessionEvent(kind, revision, requestID, sessionID, payload)
		},
		SetCurrentTaskBatch:       service.Deps.Runtime.SetCurrentTaskBatch,
		PublishRuntimeProjections: service.publishRuntimeProjections,
	})
}
