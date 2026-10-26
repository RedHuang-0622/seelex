package core

import applicationconversation "github.com/RedHuang-0622/seelex/application/conversation"

func newServiceConversationTurnProcess(service *Service) *applicationconversation.TurnProcessApplication {
	return applicationconversation.NewTurnProcess(applicationconversation.TurnProcessDeps{
		Tasks:          service.components.tasks,
		TurnState:      service.components.conversationTurnState,
		NextRequestID:  service.nextChatRequestIDLocked,
		EffortFor:      service.effortForSession,
		FallbackBudget: reactBudgetFor,
		MarkBusy:       service.markBusyLocked,
		MarkIdle:       service.markIdleLocked,
		AnyChatRunning: service.anyChatRunningLocked,
		SetSessionChat: service.setSessionChatLockedFor,
	})
}
