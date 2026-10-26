package core

import protocol "github.com/RedHuang-0622/seelex/contract/protocol"

// ForkSession and ForkSessionLatest are test-only compatibility shims.
func (service *Service) ForkSession(parentID string, request protocol.ForkRequest) (string, error) {
	return service.components.forkApplication.Fork(parentID, request)
}

func (service *Service) ForkSessionLatest(parentID string) (string, error) {
	return service.components.forkApplication.ForkLatest(parentID)
}
