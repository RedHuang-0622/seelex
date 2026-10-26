package core

import "context"

// ResolveInteraction is a test-only compatibility shim.
func (service *Service) ResolveInteraction(ctx context.Context, id, optionID string) error {
	return service.components.interactionApplication.Resolve(ctx, id, optionID)
}
