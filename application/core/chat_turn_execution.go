package core

import applicationconversation "github.com/RedHuang-0622/seelex/application/conversation"

func newServiceConversationTurnExecution(service *Service) *applicationconversation.TurnExecutionApplication {
	return applicationconversation.NewTurnExecution(applicationconversation.TurnExecutionDeps{
		BindProjectRoot:     service.bindSessionProjectRoot,
		SyncSubagentPolicy:  service.syncSubagentPolicyFor,
		SyncReasoningEffort: service.syncSessionReasoningEffortFor,
		SyncFullAccess:      service.syncFullAccessFor,
	})
}

func newServiceConversationTurnRunner(service *Service) *applicationconversation.TurnRunnerApplication {
	return applicationconversation.NewTurnRunner(applicationconversation.TurnRunnerDeps{
		WithSessionID:      withSessionID,
		StartDraftSnapshot: service.startDraftSnapshotLoop,
		PrepareExecution:   service.components.conversationTurnExecution.Prepare,
		ClearReActBudget:   service.components.tasks.ClearReActBudget,
		Debug:              runChatDebug,
		NewBatchedDeltaSink: func(requestID string) (applicationconversation.StreamBatcher, func(string)) {
			batcher, onChunk := service.newBatchedDeltaSink(requestID)
			return batcher, onChunk
		},
		Stream:   service.chatStream,
		Input:    service.components.conversationTurnInput,
		Response: service.components.conversationTurnResponse,
		Recovery: service.components.conversationTurnRecovery,
		Cleanup:  service.components.conversationTurnCleanup,
		Commit:   service.components.conversationTurnCommit,
		Finalize: service.components.conversationTurnFinalize,
	})
}
