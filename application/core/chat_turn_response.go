package core

import (
	"context"

	applicationconversation "github.com/RedHuang-0622/seelex/application/conversation"
	task_context "github.com/RedHuang-0622/seelex/backend/engine/task"
	"github.com/RedHuang-0622/seelex/contract"
)

func newServiceConversationTurnResponse(service *Service) *applicationconversation.TurnResponseApplication {
	return applicationconversation.NewTurnResponse(applicationconversation.TurnResponseDeps{
		AttachReasoning: service.attachLatestReasoning,
		ObserveOutput: func(ctx context.Context, requestID, reply string, err error) error {
			return service.components.tasks.ObserveModelOutput(ctx, task_context.ModelOutput{
				RequestID: requestID, Reply: reply, Err: err,
			})
		},
		EnsureFinal: func(requestID, reply string) {
			service.components.tasks.EnsureFinalAssistantTranscript(requestID, reply)
		},
		HistoryFor: service.engineHistoryFor,
		BackfillReasoning: func(sessionID string, history []contract.EngineMessage) {
			service.components.tasks.BackfillAssistantReasoning(sessionID, history)
		},
	})
}
