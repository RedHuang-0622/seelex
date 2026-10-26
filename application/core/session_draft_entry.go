package core

import (
	"time"

	applicationsession "github.com/RedHuang-0622/seelex/application/session"
	session_runtime "github.com/RedHuang-0622/seelex/backend/engine/session"
)

func newServiceSessionDraftEntry(service *Service) *applicationsession.DraftEntryApplication {
	return applicationsession.NewDraftEntry(applicationsession.DraftEntryDeps{
		Transition: service.transitionView,
		Snapshot: func() applicationsession.DraftEntryState {
			service.ViewMu.RLock()
			defer service.ViewMu.RUnlock()
			state := applicationsession.DraftEntryState{
				Closed:      service.closed,
				Draining:    service.draining,
				Draft:       service.Core.Snapshot.Session.Draft,
				SessionID:   service.Core.Snapshot.Session.ID,
				WorkspaceID: session_runtime.WorkspaceID(service.Core.Snapshot.CurrentWorkspace),
			}
			if unit := service.sessions.Unit(state.SessionID); unit != nil {
				state.CurrentRunning = unit.ChatState().Running
			}
			return state
		},
		HistoryLen: func(sessionID string) int {
			return len(service.engineHistoryFor(sessionID))
		},
		PersistCurrent: func(sessionID, workspaceID string) error {
			service.setWorkspaceWriteScope(workspaceID)
			location := service.components.sessions.LocateSession(sessionID)
			return service.components.sessions.PersistCurrentSession(location, sessionID)
		},
		ClearIdleHistory: service.clearEngineHistoryFor,
		ClearSkill: func() {
			service.promptStack.ClearKind("skill")
		},
		DetachIdleContext: func() {
			if store, ok := service.Deps.Sessions.(session_runtime.SessionContextPort); ok {
				store.DetachSessionContext()
			}
		},
		PrepareDraftSlot: func() applicationsession.DraftSlot {
			service.ViewMu.Lock()
			defer service.ViewMu.Unlock()
			slot := service.draft
			if slot == nil {
				slot = &draftSlot{ID: service.newDraftSessionIDLocked(), CreatedAt: time.Now()}
				service.draft = slot
			}
			slot.UpdatedAt = time.Now()
			result := applicationsession.DraftSlot{ID: slot.ID}
			if slot.Workspace != nil {
				workspace := *slot.Workspace
				result.Workspace = &workspace
			}
			return result
		},
		ReadTier: service.readStoredPermissionTier,
		ClearWorkspaceBindings: func() {
			if service.Deps.Runtime != nil {
				service.unbindGlobalProjectRoot()
			}
			service.setWorkspaceWriteScope("")
		},
		EnterShell: func(slot applicationsession.DraftSlot, storedTier string) uint64 {
			service.ViewMu.Lock()
			service.enterDraftShellLocked(draftShellOptions{
				sessionID:          slot.ID,
				workspace:          slot.Workspace,
				resetHistoryWindow: true,
				composerFromUnit:   true,
				resetTitle:         true,
				tier: func(sessionID string) {
					service.applyStoredPermissionTier(sessionID, storedTier)
				},
			})
			revision := service.bumpLocked()
			service.ViewMu.Unlock()
			return revision
		},
		PublishRuntimeProjections: service.publishRuntimeProjections,
		SwitchTasks: func(sessionID string) {
			service.Deps.Runtime.SwitchSessionTasks(sessionID, nil)
		},
		ClearSubagentTree: func() {
			_ = service.Deps.Runtime.ClearSubagentTree()
		},
		RefreshWorkTable: service.refreshWorkTableFromSources,
		PublishSnapshot: func(revision uint64, sessionID string) {
			service.publishSessionEvent(EventSnapshotChanged, revision, "", sessionID, nil)
		},
		DrainingError: ErrApplicationDraining,
	})
}
