package core

import applicationconversation "github.com/RedHuang-0622/seelex/application/conversation"

func newServiceConversationTurnOutcome(service *Service) *applicationconversation.TurnOutcomeApplication {
	return applicationconversation.NewTurnOutcome(applicationconversation.TurnOutcomeDeps{
		ClearError: service.components.conversationTurnState.ClearErrorLocked,
		SetError:   service.components.conversationTurnState.SetErrorLocked,
		AppendErrorLine: func(sessionID, visibleError string) {
			service.components.view.AppendMessageLockedFor(sessionID, "error", visibleError, nil)
		},
		ApplyRuntimeProjection: service.components.view.ApplyRuntimeProjectionForLocked,
		SetSessionChat:         service.components.view.SetSessionChatLockedFor,
		MirrorTask: func(sessionID string) {
			if task := service.components.tasks.VisibleTaskStateFor(sessionID); task != nil {
				service.Core.Snapshot.Task = task
			}
		},
		BumpRevision: service.components.view.BumpLocked,
	})
}
