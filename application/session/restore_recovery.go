package session

import "sync"

// RestoreRecoveryState is the locked fallback decision input.
type RestoreRecoveryState struct {
	StillActive bool
	Latest      bool
}

// RestoreRecoveryDeps supplies locked state, fallback and publication hooks.
type RestoreRecoveryDeps struct {
	Lock                      func() sync.Locker
	ClearRestoringLocked      func(sessionID string)
	SnapshotLocked            func(sessionID string, epoch uint64) RestoreRecoveryState
	HotAttach                 func(previousID string) error
	ResetDraft                func()
	PublishViewSessionChanged func()
}

// RestoreRecoveryApplication owns view fallback after a cold-restore failure.
type RestoreRecoveryApplication struct {
	deps RestoreRecoveryDeps
}

// NewRestoreRecovery constructs the restore recovery application.
func NewRestoreRecovery(deps RestoreRecoveryDeps) *RestoreRecoveryApplication {
	return &RestoreRecoveryApplication{deps: deps}
}

// Handle restores the previous session or resets to a draft when possible.
func (application *RestoreRecoveryApplication) Handle(sessionID, previousID string, epoch uint64, _ error) {
	lock := application.deps.Lock()
	lock.Lock()
	application.deps.ClearRestoringLocked(sessionID)
	state := application.deps.SnapshotLocked(sessionID, epoch)
	lock.Unlock()
	if !state.StillActive || !state.Latest {
		return
	}
	if previousID != "" {
		if err := application.deps.HotAttach(previousID); err == nil {
			application.deps.PublishViewSessionChanged()
			return
		}
	}
	application.deps.ResetDraft()
	application.deps.PublishViewSessionChanged()
}

// RestoreDraftResetDeps supplies draft-slot and publication hooks.
type RestoreDraftResetDeps struct {
	Lock                      func() sync.Locker
	EnsureSlotLocked          func() string
	EnterShellLocked          func(draftID string) uint64
	PublishRuntimeProjections func()
	PublishSnapshot           func(revision uint64, draftID string)
}

// RestoreDraftResetApplication resets the view to a fresh draft.
type RestoreDraftResetApplication struct {
	deps RestoreDraftResetDeps
}

// NewRestoreDraftReset constructs the restore draft reset application.
func NewRestoreDraftReset(deps RestoreDraftResetDeps) *RestoreDraftResetApplication {
	return &RestoreDraftResetApplication{deps: deps}
}

// Reset creates or reuses the draft slot and installs its shell.
func (application *RestoreDraftResetApplication) Reset() {
	lock := application.deps.Lock()
	lock.Lock()
	draftID := application.deps.EnsureSlotLocked()
	revision := application.deps.EnterShellLocked(draftID)
	lock.Unlock()
	application.deps.PublishRuntimeProjections()
	application.deps.PublishSnapshot(revision, draftID)
}
