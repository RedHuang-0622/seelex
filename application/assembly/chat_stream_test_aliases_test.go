package assembly

func (service *Service) appendDelta(requestID, chunk string) {
	service.components.conversationStreamRuntime.AppendDelta(requestID, chunk)
}

func (service *Service) attachLatestReasoning(sessionID, requestID string) {
	service.components.conversationStreamRuntime.AttachLatestReasoning(sessionID, requestID)
}

func (service *Service) stampServedRoute(sessionID, requestID string) {
	service.components.conversationStreamRuntime.StampServedRoute(sessionID, requestID)
}

func (service *Service) streamedAssistantTextLocked(sessionID string) string {
	return service.components.conversationStreamRuntime.StreamedAssistantTextLocked(sessionID)
}
