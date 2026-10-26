package session

import (
	"errors"
	"strings"
	"sync"
)

// ResumeState describes the current session-selection branch.
type ResumeState struct {
	Hot       bool
	Running   bool
	Restoring bool
	Previous  string
}

// ResumeDeps supplies transition, hot/cold and recovery callbacks.
type ResumeDeps struct {
	TransitionForSession      func(sessionID string) sync.Locker
	Snapshot                  func(sessionID string) ResumeState
	PointViewAtRestoringShell func(sessionID string) bool
	HotAttach                 func(sessionID string) error
	MarkViewIntent            func(sessionID string)
	BeginAsyncRestore         func(sessionID string) (uint64, error)
	ResumeColdInBackground    func(sessionID, previousID string, epoch uint64)
	ResumeCold                func(sessionID string, epoch uint64) error
	HandleFailure             func(sessionID, previousID string, epoch uint64, cause error)
}

// ResumeApplication owns the top-level session-selection branch.
type ResumeApplication struct {
	deps ResumeDeps
}

// NewResume constructs the session resume application.
func NewResume(deps ResumeDeps) *ResumeApplication {
	return &ResumeApplication{deps: deps}
}

// Resume selects hot, restoring or cold loading for a session.
func (application *ResumeApplication) Resume(sessionID string) error {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return errors.New("session ID is required")
	}
	transition := application.deps.TransitionForSession(sessionID)
	transition.Lock()
	state := application.deps.Snapshot(sessionID)

	if state.Restoring {
		transition.Unlock()
		if application.deps.PointViewAtRestoringShell(sessionID) {
			return nil
		}
		return application.deps.HotAttach(sessionID)
	}
	if state.Hot {
		application.deps.MarkViewIntent(sessionID)
		transition.Unlock()
		return application.deps.HotAttach(sessionID)
	}
	epoch, err := application.deps.BeginAsyncRestore(sessionID)
	transition.Unlock()
	if err != nil {
		return err
	}
	if state.Running {
		application.deps.ResumeColdInBackground(sessionID, state.Previous, epoch)
		return nil
	}
	if err := application.deps.ResumeCold(sessionID, epoch); err != nil {
		application.deps.HandleFailure(sessionID, state.Previous, epoch, err)
		return err
	}
	return nil
}
