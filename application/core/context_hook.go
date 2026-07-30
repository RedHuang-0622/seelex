package core

import "fmt"

// recordContextControlFailure transfers a hook failure to runChat. LoopHooks
// can stop the upstream ReAct loop but cannot return an error themselves.
func (service *Service) recordContextControlFailure(requestID string, err error) {
	if err == nil {
		return
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	if service.snapshot.Chat.RequestID != requestID {
		return
	}
	service.contextControlFailure = fmt.Errorf("context control: %w", err)
	service.contextControlRequestID = requestID
}

func (service *Service) takeContextControlFailure(requestID string) error {
	service.mu.Lock()
	defer service.mu.Unlock()
	if service.contextControlRequestID != requestID {
		return nil
	}
	err := service.contextControlFailure
	service.contextControlFailure = nil
	service.contextControlRequestID = ""
	return err
}
