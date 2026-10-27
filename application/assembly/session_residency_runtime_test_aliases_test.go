package assembly

func (service *Service) ensureSessionContent(sessionID string) error {
	return service.components.residencyRuntime.EnsureContent(sessionID)
}

func (service *Service) sessionContentUnloaded(sessionID string) bool {
	return service.components.residencyRuntime.ContentUnloaded(sessionID)
}
