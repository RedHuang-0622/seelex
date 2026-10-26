package cold

import (
	"fmt"

	session_runtime "github.com/RedHuang-0622/seelex/backend/engine/session"
	"github.com/RedHuang-0622/seelex/contract"
)

// RestoreDeps supplies engine and queue restoration.
type RestoreDeps struct {
	SetWorkspace   func(sessionID, workspaceID string)
	ResumeEngine   func(sessionID string, history []contract.EngineMessage) (bool, error)
	ReplaceHistory func(sessionID string, history []contract.EngineMessage) error
	RecoverQueue   func(location session_runtime.Location, sessionID string) error
	WritePrompt    func(sessionID string)
}

// RestoreApplication owns engine restore and durable queue rehydration.
type RestoreApplication struct {
	deps RestoreDeps
}

// NewRestore constructs the cold restore application.
func NewRestore(deps RestoreDeps) *RestoreApplication {
	return &RestoreApplication{deps: deps}
}

// Restore resumes or replaces engine history, recovers the queue and writes the
// system prompt.
func (application *RestoreApplication) Restore(sessionID string, location session_runtime.Location, history []contract.EngineMessage) error {
	application.deps.SetWorkspace(sessionID, location.WorkspaceID)
	resumed, err := application.deps.ResumeEngine(sessionID, history)
	if err != nil {
		return fmt.Errorf("resume engine session: %w", err)
	}
	if !resumed {
		if err := application.deps.ReplaceHistory(sessionID, history); err != nil {
			return fmt.Errorf("replace engine history: %w", err)
		}
	}
	if err := application.deps.RecoverQueue(location, sessionID); err != nil {
		return fmt.Errorf("lifecycle recover %q: %w", sessionID, err)
	}
	application.deps.WritePrompt(sessionID)
	return nil
}
