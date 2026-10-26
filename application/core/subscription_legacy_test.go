package core

import protocol_event "github.com/RedHuang-0622/seelex/application/event"

// The public subscription facade now belongs to application/compat and
// application/session/viewstate. These test-only shims keep legacy core
// integration tests focused on behavior instead of forcing every fixture to
// construct the frontend adapter.
func (service *Service) SubscribeSession(sessionID string, buffer int) (protocol_event.Subscription, error) {
	return service.components.view.SubscribeSession(sessionID, buffer)
}

func (service *Service) SubscribeSessionWithReplay(sessionID string, buffer, replayWindow int) (protocol_event.Subscription, error) {
	return service.components.view.SubscribeSessionWithReplay(sessionID, buffer, replayWindow)
}
