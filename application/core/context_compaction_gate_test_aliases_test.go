package core

import "context"

func (service *Service) isCompactingLocked(sessionID string) bool {
	return service.components.contextCompactionGate.IsCompactingLocked(sessionID)
}

func (service *Service) releaseCompactionRound(sessionID string) {
	service.components.contextCompactionGate.Release(sessionID)
}

func (service *Service) awaitCompactionRound(ctx context.Context, sessionID string) error {
	return service.components.contextCompactionGate.Await(ctx, sessionID)
}
