package integration

import (
	"errors"
	"sync"
	"testing"

	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/contract/protocol"
)

type contextAwareSessions struct {
	archiveSessions
	mu        sync.Mutex
	attached  []string
	detached  int
	attachErr error
}

func (sessions *contextAwareSessions) AttachSessionContext(workspaceID, sessionID string) error {
	sessions.mu.Lock()
	defer sessions.mu.Unlock()
	sessions.attached = append(sessions.attached, workspaceID+":"+sessionID)
	return sessions.attachErr
}

func (sessions *contextAwareSessions) DetachSessionContext() {
	sessions.mu.Lock()
	defer sessions.mu.Unlock()
	sessions.detached++
}

func compactionRecordsOf(t *testing.T, service *legacyTestService) []model.ContextCompaction {
	t.Helper()
	service.ViewMu.RLock()
	defer service.ViewMu.RUnlock()
	state := service.components.tasks.CurrentTaskExecution()
	if state == nil {
		return nil
	}
	return append([]model.ContextCompaction(nil), state.ContextCompactions...)
}

type archiveSessions struct {
	fakeSessions

	mu         sync.Mutex
	history    []contract.EngineMessage
	historyErr error
	record     model.SessionRecord
	transcript []model.TranscriptEvent
	saved      int
	tailBudget int
	tailUnits  int
	sessionID  string
}

func (sessions *archiveSessions) SessionsOf(projectID string) []model.SessionInfo {
	sessions.mu.Lock()
	defer sessions.mu.Unlock()
	if projectID != "" || sessions.sessionID == "" {
		return nil
	}
	return []model.SessionInfo{{ID: sessions.sessionID}}
}

func (sessions *archiveSessions) setSessionID(sessionID string) {
	sessions.mu.Lock()
	defer sessions.mu.Unlock()
	sessions.sessionID = sessionID
}

func (sessions *archiveSessions) SaveCurrent(string) error {
	sessions.saved++
	return nil
}

func (sessions *archiveSessions) LoadHistory(string) ([]contract.EngineMessage, error) {
	return append([]contract.EngineMessage(nil), sessions.history...), sessions.historyErr
}

func (sessions *archiveSessions) SaveSessionRecord(_ string, record model.SessionRecord) error {
	sessions.record = record
	return nil
}

func (sessions *archiveSessions) SaveSessionRecordWorkspace(_ string, _ string, record model.SessionRecord) error {
	sessions.record = record
	return nil
}

func (sessions *archiveSessions) LoadSessionRecord(string) (model.SessionRecord, error) {
	return sessions.record, nil
}

func (sessions *archiveSessions) LoadSessionRecordWorkspace(string, string) (model.SessionRecord, error) {
	return sessions.record, nil
}

func (sessions *archiveSessions) LoadTranscriptTailWorkspace(_, _ string, tokenBudget, maxUnits int) ([]model.TranscriptEvent, error) {
	sessions.tailBudget = tokenBudget
	sessions.tailUnits = maxUnits
	return append([]model.TranscriptEvent(nil), sessions.transcript...), nil
}

func (sessions *archiveSessions) LoadToolResultWorkspace(string, string, string) (model.StoredToolResult, error) {
	return model.StoredToolResult{}, errors.New("tool result unavailable")
}
