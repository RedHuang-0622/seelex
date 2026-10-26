package core

import applicationconversation "github.com/RedHuang-0622/seelex/application/conversation"

func newServiceConversationTurnOutcome(service *Service) *applicationconversation.TurnOutcomeApplication {
	return applicationconversation.NewTurnOutcome(applicationconversation.TurnOutcomeDeps{
		ClearError: service.components.conversationTurnState.ClearErrorLocked,
		SetError:   service.components.conversationTurnState.SetErrorLocked,
		AppendErrorLine: func(sessionID, visibleError string) {
			service.appendSessionMessageLocked(sessionID, "error", visibleError, nil)
		},
		ApplyRuntimeProjection: service.applyRuntimeProjectionForLocked,
		SetSessionChat:         service.setSessionChatLockedFor,
		MirrorTask: func(sessionID string) {
			if task := service.components.tasks.VisibleTaskStateFor(sessionID); task != nil {
				service.Core.Snapshot.Task = task
			}
		},
		BumpRevision: service.bumpLocked,
	})
}
