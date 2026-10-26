package session

import (
	"fmt"
	"strings"
)

// ReleaseMode selects the strength of an in-memory session release.
type ReleaseMode int

const (
	// ReleaseIdle persists and releases execution state while retaining the
	// session unit, title and task projection for cold reload.
	ReleaseIdle ReleaseMode = iota
	// ReleaseFull additionally removes the session unit, task projection,
	// title and tool-record ledger.
	ReleaseFull
)

// ReleaseDeps supplies protected-set checks and the ordered release actions.
// The callbacks are owned by core adapters; this package owns the sequence.
type ReleaseDeps struct {
	Protected         func(sessionID string) bool
	Persist           func(sessionID string) error
	FlushToolRecord   func(sessionID string)
	UnloadEngine      func(sessionID string) error
	ReleaseBundle     func(sessionID string)
	ForgetGoal        func(sessionID string)
	ReleaseContent    func(sessionID string)
	ReleaseFull       func(sessionID string)
	ForgetOrders      func(sessionID string)
	ReleaseToolRecord func(sessionID string)
	RequestRefresh    func()
	ReconcileResident func()
	ReconcileContent  func()
}

// ReleaseApplication owns the release guard, flush and ordered cleanup
// sequence. It does not own the resident/content LRU or storage facts.
type ReleaseApplication struct {
	deps ReleaseDeps
}

// NewReleaseApplication constructs the release application.
func NewReleaseApplication(deps ReleaseDeps) *ReleaseApplication {
	return &ReleaseApplication{deps: deps}
}

// Release releases one session's in-memory state in the fixed order required
// by persistence and downstream cache ownership.
func (application *ReleaseApplication) Release(sessionID string, mode ReleaseMode) error {
	if application == nil {
		return nil
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil
	}

	if mode == ReleaseIdle && application.deps.Protected != nil && application.deps.Protected(sessionID) {
		return nil
	}
	if mode == ReleaseIdle && application.deps.Persist != nil {
		if err := application.deps.Persist(sessionID); err != nil {
			return fmt.Errorf("flush session %q before release: %w", sessionID, err)
		}
	}
	if application.deps.FlushToolRecord != nil {
		application.deps.FlushToolRecord(sessionID)
	}
	if application.deps.UnloadEngine != nil {
		if err := application.deps.UnloadEngine(sessionID); err != nil {
			return fmt.Errorf("unload engine %q: %w", sessionID, err)
		}
	}
	if application.deps.ReleaseBundle != nil {
		application.deps.ReleaseBundle(sessionID)
	}
	if application.deps.ForgetGoal != nil {
		application.deps.ForgetGoal(sessionID)
	}
	if application.deps.ReleaseContent != nil {
		application.deps.ReleaseContent(sessionID)
	}
	if mode == ReleaseFull && application.deps.ReleaseFull != nil {
		application.deps.ReleaseFull(sessionID)
	}
	if application.deps.ForgetOrders != nil {
		application.deps.ForgetOrders(sessionID)
	}
	if mode == ReleaseFull && application.deps.ReleaseToolRecord != nil {
		application.deps.ReleaseToolRecord(sessionID)
	}
	if application.deps.RequestRefresh != nil {
		application.deps.RequestRefresh()
	}
	return nil
}

// Reconcile runs both release layers after ownership or activity changes.
func (application *ReleaseApplication) Reconcile() {
	if application == nil {
		return
	}
	if application.deps.ReconcileResident != nil {
		application.deps.ReconcileResident()
	}
	if application.deps.ReconcileContent != nil {
		application.deps.ReconcileContent()
	}
}
