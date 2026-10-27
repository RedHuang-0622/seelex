package assembly

import applicationconversation "github.com/RedHuang-0622/seelex/application/conversation"

type conversationTurnInput = applicationconversation.TurnInputApplication
type conversationTurnInputDeps = applicationconversation.TurnInputDeps

func newConversationTurnInput(deps conversationTurnInputDeps) *conversationTurnInput {
	return applicationconversation.NewTurnInput(deps)
}

type conversationTurnResponse = applicationconversation.TurnResponseApplication
type conversationTurnResponseDeps = applicationconversation.TurnResponseDeps

func newConversationTurnResponse(deps conversationTurnResponseDeps) *conversationTurnResponse {
	return applicationconversation.NewTurnResponse(deps)
}

type conversationTurnRecovery = applicationconversation.TurnRecoveryApplication
type conversationTurnRecoveryDeps = applicationconversation.TurnRecoveryDeps

func newConversationTurnRecovery(deps conversationTurnRecoveryDeps) *conversationTurnRecovery {
	return applicationconversation.NewTurnRecovery(deps)
}

type conversationTurnCleanup = applicationconversation.TurnCleanupApplication
type conversationTurnCleanupDeps = applicationconversation.TurnCleanupDeps

func newConversationTurnCleanup(deps conversationTurnCleanupDeps) *conversationTurnCleanup {
	return applicationconversation.NewTurnCleanup(deps)
}
