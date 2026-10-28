package integration

import (
	"sync"

	"github.com/RedHuang-0622/seelex/contract"
)

type gatedHistorySessions struct {
	*scopedSessions
	entered     chan struct{}
	release     chan struct{}
	releaseOnce sync.Once
	once        sync.Once
	armMu       sync.Mutex
	armed       bool
}

func (sessions *gatedHistorySessions) releaseNow() {
	sessions.releaseOnce.Do(func() { close(sessions.release) })
}

func (sessions *gatedHistorySessions) LoadHistory(sessionID string) ([]contract.EngineMessage, error) {
	sessions.gateEnabled(sessionID)
	return sessions.scopedSessions.LoadHistory(sessionID)
}

func (sessions *gatedHistorySessions) LoadHistoryRange(sessionID string, offset, limit int) ([]contract.EngineMessage, int, error) {
	sessions.gateEnabled(sessionID)
	return sessions.scopedSessions.LoadHistoryRange(sessionID, offset, limit)
}

func (sessions *gatedHistorySessions) gateEnabled(sessionID string) bool {
	sessions.armMu.Lock()
	armed := sessions.armed
	sessions.armMu.Unlock()
	if !armed || sessionID != "sess-cold" {
		return false
	}
	sessions.once.Do(func() { close(sessions.entered) })
	<-sessions.release
	return true
}
