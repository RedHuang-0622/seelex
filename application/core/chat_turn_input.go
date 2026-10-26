package core

import applicationconversation "github.com/RedHuang-0622/seelex/application/conversation"

func newServiceConversationTurnInput(service *Service) *applicationconversation.TurnInputApplication {
	return applicationconversation.NewTurnInput(applicationconversation.TurnInputDeps{
		ApplyPrompt: func(sessionID, requestID string) {
			service.components.prompts.ApplyActiveTaskSystemPromptFor(sessionID, requestID)
		},
		PrepareContext: func(sessionID, requestID, modelInput string) (string, error) {
			return service.components.context.PrepareExecutionContextFor(sessionID, requestID, modelInput)
		},
		Normalize: nonEmptyProviderInput,
	})
}
