package session

import (
	"errors"
	"fmt"
	"sync"

	"github.com/RedHuang-0622/seelex/contract/protocol"
)

// DraftEntryState describes the current view before entering a new draft.
type DraftEntryState struct {
	Closed         bool
	Draining       bool
	Draft          bool
	SessionID      string
	WorkspaceID    string
	CurrentRunning bool
}

// DraftSlot is the pre-assigned draft identity and optional workspace.
type DraftSlot struct {
	ID        string
	Workspace *model.WorkspaceInfo
}

// DraftEntryDeps supplies draft entry persistence, cleanup and publication.
type DraftEntryDeps struct {
	Transition                func() sync.Locker
	Snapshot                  func() DraftEntryState
	HistoryLen                func(sessionID string) int
	PersistCurrent            func(sessionID, workspaceID string) error
	ClearIdleHistory          func(sessionID string)
	ClearSkill                func()
	DetachIdleContext         func()
	PrepareDraftSlot          func() DraftSlot
	ReadTier                  func(sessionID string) string
	ClearWorkspaceBindings    func()
	EnterShell                func(slot DraftSlot, storedTier string) uint64
	PublishRuntimeProjections func()
	SwitchTasks               func(sessionID string)
	ClearSubagentTree         func()
	RefreshWorkTable          func()
	PublishSnapshot           func(revision uint64, sessionID string)
	DrainingError             error
}

// DraftEntryApplication owns BeginNewSession ordering.
type DraftEntryApplication struct {
	deps DraftEntryDeps
}

// NewDraftEntry constructs the draft-entry application.
func NewDraftEntry(deps DraftEntryDeps) *DraftEntryApplication {
	return &DraftEntryApplication{deps: deps}
}

// Begin switches the current view into a new draft shell.
func (application *DraftEntryApplication) Begin() error {
	transition := application.deps.Transition()
	transition.Lock()
	defer transition.Unlock()

	state := application.deps.Snapshot()
	if state.Closed {
		return errors.New("application is shut down")
	}
	if state.Draining {
		return application.deps.DrainingError
	}
	if state.Draft {
		return nil
	}
	if !state.CurrentRunning && application.deps.HistoryLen(state.SessionID) > 0 {
		if err := application.deps.PersistCurrent(state.SessionID, state.WorkspaceID); err != nil {
			return fmt.Errorf("save current session before drafting a new one: %w", err)
		}
	}
	if !state.CurrentRunning {
		application.deps.ClearIdleHistory(state.SessionID)
	}
	application.deps.ClearSkill()
	if !state.CurrentRunning {
		application.deps.DetachIdleContext()
	}

	slot := application.deps.PrepareDraftSlot()
	storedTier := application.deps.ReadTier(slot.ID)
	if slot.Workspace == nil {
		application.deps.ClearWorkspaceBindings()
	}
	revision := application.deps.EnterShell(slot, storedTier)
	application.deps.PublishRuntimeProjections()
	application.deps.SwitchTasks(slot.ID)
	application.deps.ClearSubagentTree()
	application.deps.RefreshWorkTable()
	application.deps.PublishSnapshot(revision, slot.ID)
	return nil
}
