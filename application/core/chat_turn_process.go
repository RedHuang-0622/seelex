package core

import applicationconversation "github.com/RedHuang-0622/seelex/application/conversation"
import "github.com/RedHuang-0622/seelex/application/prompt"

func newServiceConversationTurnProcess(service *Service) *applicationconversation.TurnProcessApplication {
	return applicationconversation.NewTurnProcess(applicationconversation.TurnProcessDeps{
		Tasks:          service.components.tasks,
		TurnState:      service.components.conversationTurnState,
		NextRequestID:  service.nextChatRequestIDLocked,
		EffortFor:      service.effortForSession,
		FallbackBudget: prompt.ReActBudgetFor,
		MarkBusy:       service.markBusyLocked,
		MarkIdle:       service.markIdleLocked,
		AnyChatRunning: service.anyChatRunningLocked,
		SetSessionChat: service.components.view.SetSessionChatLockedFor,
	})
}
