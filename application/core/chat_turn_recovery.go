package core

import (
	"context"

	applicationconversation "github.com/RedHuang-0622/seelex/application/conversation"
	task_context "github.com/RedHuang-0622/seelex/application/execution/taskcontext"
)

func newServiceConversationTurnRecovery(service *Service) *applicationconversation.TurnRecoveryApplication {
	return applicationconversation.NewTurnRecovery(applicationconversation.TurnRecoveryDeps{
		TakeContextFailure: func(requestID string) error {
			return service.components.context.TakeContextControlFailure(requestID)
		},
		Recover:        service.recoverProviderFailureFor,
		Retryable:      retryableAfterRecovery,
		Retry:          service.retryContextRecovery,
		FinalizeBudget: service.finalizeReActBudgetWithSink,
		FinalizeTask: func(ctx context.Context, requestID, reply string) error {
			return service.components.tasks.FinalizeTask(ctx, task_context.ChatEndSummary{
				RequestID: requestID, Reply: reply,
			})
		},
	})
}
