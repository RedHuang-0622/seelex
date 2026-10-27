package assembly

import applicationconversation "github.com/RedHuang-0622/seelex/application/conversation"

type conversationTurnProcessDeps = applicationconversation.TurnProcessDeps

func newConversationTurnProcess(deps conversationTurnProcessDeps) *conversationTurnProcess {
	return applicationconversation.NewTurnProcess(deps)
}

type conversationTurnCompletionDeps = applicationconversation.TurnCompletionDeps

func newConversationTurnCompletion(deps conversationTurnCompletionDeps) *conversationTurnCompletion {
	return applicationconversation.NewTurnCompletion(deps)
}

type conversationTurnOutcomeDeps = applicationconversation.TurnOutcomeDeps

func newConversationTurnOutcome(deps conversationTurnOutcomeDeps) *conversationTurnOutcome {
	return applicationconversation.NewTurnOutcome(deps)
}

type conversationTurnPublisherDeps = applicationconversation.TurnPublisherDeps

func newConversationTurnPublisher(deps conversationTurnPublisherDeps) *conversationTurnPublisher {
	return applicationconversation.NewTurnPublisher(deps)
}

type conversationTurnTailDeps = applicationconversation.TurnTailDeps

func newConversationTurnTail(deps conversationTurnTailDeps) *conversationTurnTail {
	return applicationconversation.NewTurnTail(deps)
}

type conversationTurnCommitDeps = applicationconversation.TurnCommitDeps

func newConversationTurnCommit(deps conversationTurnCommitDeps) *conversationTurnCommit {
	return applicationconversation.NewTurnCommit(deps)
}

type conversationTurnFinalizeDeps = applicationconversation.TurnFinalizeDeps

func newConversationTurnFinalize(deps conversationTurnFinalizeDeps) *conversationTurnFinalize {
	return applicationconversation.NewTurnFinalize(deps)
}

type conversationTurnExecutionDeps = applicationconversation.TurnExecutionDeps

func newConversationTurnExecution(deps conversationTurnExecutionDeps) *conversationTurnExecution {
	return applicationconversation.NewTurnExecution(deps)
}
