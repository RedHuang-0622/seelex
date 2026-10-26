package core

import applicationconversation "github.com/RedHuang-0622/seelex/application/conversation"

func newServiceConversationTurnCompletion(service *Service) *applicationconversation.TurnCompletionApplication {
	releaseWorkingHistory := func(string) {}
	if releaser, ok := service.Deps.Engine.(interface{ ReleaseWorkingHistoryFor(string) }); ok {
		releaseWorkingHistory = releaser.ReleaseWorkingHistoryFor
	}
	return applicationconversation.NewTurnCompletion(applicationconversation.TurnCompletionDeps{
		LocateSession:          service.components.sessions.LocateSession,
		PersistSession:         service.components.sessions.PersistCurrentSession,
		QueueFailConsumedInput: service.components.sessions.QueueFailConsumedInput,
		QueueConfirmConsumed:   service.components.sessions.QueueConfirmConsumedInput,
		ReleaseWorkingHistory:  releaseWorkingHistory,
	})
}
