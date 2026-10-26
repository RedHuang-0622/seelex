package session

import (
	"log"
	"sync"

	switchworkspace "github.com/RedHuang-0622/seelex/middle/switch/workspace"
)

// DeleteDeps supplies record deletion, release, workspace and publication
// callbacks.
type DeleteDeps struct {
	ViewMu              *sync.RWMutex
	DeleteRecord        func(sessionID string) error
	ReleaseAsync        func(sessionID string)
	IsViewSession       func(sessionID string) bool
	ReleaseMemory       func(sessionID string) error
	HasWorkspace        func() bool
	UnbindWorkspace     func(sessionID string)
	WorkspaceProjection func() switchworkspace.Projection
	ApplyWorkspace      func(projection switchworkspace.Projection)
	SwitchDraft         func()
	BumpRevision        func() uint64
	PublishSnapshot     func(revision uint64, sessionID string)
	RefreshCatalog      func()
}

// DeleteApplication owns ordered session record and memory deletion.
type DeleteApplication struct {
	deps DeleteDeps
}

// NewDelete constructs the session delete application.
func NewDelete(deps DeleteDeps) *DeleteApplication {
	return &DeleteApplication{deps: deps}
}

// Delete removes a session record and releases all process-local resources.
func (application *DeleteApplication) Delete(sessionID string) error {
	if err := application.deps.DeleteRecord(sessionID); err != nil {
		return err
	}
	application.deps.ReleaseAsync(sessionID)

	application.deps.ViewMu.RLock()
	viewed := application.deps.IsViewSession(sessionID)
	application.deps.ViewMu.RUnlock()

	if err := application.deps.ReleaseMemory(sessionID); err != nil {
		log.Printf("[session-release] delete %q: %v", sessionID, err)
	}
	if application.deps.HasWorkspace() {
		application.deps.UnbindWorkspace(sessionID)
		projection := application.deps.WorkspaceProjection()
		application.deps.ViewMu.Lock()
		if viewed {
			application.deps.SwitchDraft()
		}
		application.deps.ApplyWorkspace(projection)
		revision := application.deps.BumpRevision()
		application.deps.ViewMu.Unlock()
		application.deps.PublishSnapshot(revision, sessionID)
		application.deps.RefreshCatalog()
		return nil
	}
	if viewed {
		application.deps.ViewMu.Lock()
		application.deps.SwitchDraft()
		revision := application.deps.BumpRevision()
		application.deps.ViewMu.Unlock()
		application.deps.PublishSnapshot(revision, sessionID)
		application.deps.RefreshCatalog()
	}
	return nil
}
