package assembly

import "github.com/RedHuang-0622/seelex/contract/dto"

const (
	asyncStateDone   = dto.AsyncStateDone
	asyncStateFailed = dto.AsyncStateFailed
)

// sessionIdleForAsyncTrigger reports whether a session can start a completion
// turn. A missing unit is deliberately treated as unavailable.
func (service *Service) sessionIdleForAsyncTrigger(sessionID string) bool {
	service.ViewMu.RLock()
	defer service.ViewMu.RUnlock()
	if service.closed || service.draining {
		return false
	}
	unit := service.sessions.Unit(sessionID)
	if unit == nil {
		return false
	}
	return !unit.ChatState().Running
}
