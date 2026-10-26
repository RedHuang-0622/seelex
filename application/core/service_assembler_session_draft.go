package core

import (
	"time"

	applicationsession "github.com/RedHuang-0622/seelex/application/session"
	session_runtime "github.com/RedHuang-0622/seelex/application/session/runtime"
	"github.com/RedHuang-0622/seelex/contract/protocol"
)

func (service *Service) assembleSessionDraftComponents() {
	service.components.sessionDraftSubmission = applicationsession.NewDraftSubmission(applicationsession.DraftSubmissionDeps{
		IsDraftTarget: service.isUnmaterializedDraftTarget,
		Transition:    service.transitionView,
		IsInView: func(sessionID string) bool {
			service.ViewMu.RLock()
			defer service.ViewMu.RUnlock()
			return service.draftTargetLocked().InView(sessionID)
		},
		Materialize: func(firstInput string) error {
			return service.components.sessionDraftMaterialization.Materialize(firstInput)
		},
		NotInView: ErrDraftNotInView,
	})

	service.components.sessionDraftMaterialization = applicationsession.NewDraftMaterialization(applicationsession.DraftMaterializationDeps{
		Snapshot: func() applicationsession.DraftMaterializationSource {
			service.ViewMu.RLock()
			defer service.ViewMu.RUnlock()
			source := applicationsession.DraftMaterializationSource{
				Draft:   service.Core.Snapshot.Session.Draft,
				DraftID: service.Core.Snapshot.Session.ID,
			}
			if service.draft != nil && service.draft.ID != "" {
				source.DraftID = service.draft.ID
			}
			if service.Core.Snapshot.CurrentWorkspace != nil {
				workspace := *service.Core.Snapshot.CurrentWorkspace
				source.Workspace = &workspace
			}
			return source
		},
		BindWorkspace: func(workspace *model.WorkspaceInfo) error {
			if workspace != nil {
				if err := service.bindGlobalProjectRoot(workspace.RootPath); err != nil {
					return err
				}
				service.setWorkspaceWriteScope(workspace.ID)
				return nil
			}
			service.unbindGlobalProjectRoot()
			service.setWorkspaceWriteScope("")
			return nil
		},
		OpenEngine: service.openSessionEngine,
		Title: func(firstQuestion string) model.SessionTitle {
			return model.SessionTitle{
				Value:       session_runtime.SessionTitle(firstQuestion),
				Source:      "first_request",
				FinalizedAt: time.Now(),
			}
		},
		Commit: func(newID string, title model.SessionTitle) uint64 {
			workspaceProjection := service.components.workspace.Projection()
			service.ViewMu.Lock()
			service.draft = nil
			service.Core.Snapshot.Session = model.SessionState{ID: newID, Name: title.Value}
			service.sessions.SetActive(newID)
			service.components.sessions.SetSessionTitleLocked(newID, title)
			service.components.tasks.ResetPlanStateLocked()
			service.applyWorkspaceProjectionLocked(workspaceProjection)
			revision := service.components.view.BumpLocked()
			service.ViewMu.Unlock()
			return revision
		},
		PublishRuntimeProjections: service.publishRuntimeProjections,
		PublishSnapshot: func(revision uint64, sessionID string) {
			service.publishSessionEvent(EventSnapshotChanged, revision, "", sessionID, nil)
		},
		RefreshCatalog: func() {
			service.components.sessions.RequestCatalogRefresh()
		},
		FlushToolRecord: service.flushToolRecord,
		TouchResident:   service.touchResident,
	})

	service.components.sessionDraftEntry = applicationsession.NewDraftEntry(applicationsession.DraftEntryDeps{
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
			revision := service.components.view.BumpLocked()
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
		RefreshWorkTable: func() {
			if service.components.workTableRuntime != nil {
				service.components.workTableRuntime.RefreshFromSources()
			}
		},
		PublishSnapshot: func(revision uint64, sessionID string) {
			service.publishSessionEvent(EventSnapshotChanged, revision, "", sessionID, nil)
		},
		DrainingError: ErrApplicationDraining,
	})
}
