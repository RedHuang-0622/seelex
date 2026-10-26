package core

import applicationconversation "github.com/RedHuang-0622/seelex/application/conversation"

func newServiceConversationTurnFinalize(service *Service) *applicationconversation.TurnFinalizeApplication {
	return applicationconversation.NewTurnFinalize(applicationconversation.TurnFinalizeDeps{
		TailComplete: service.components.conversationTurnTail.Complete,
	})
}
