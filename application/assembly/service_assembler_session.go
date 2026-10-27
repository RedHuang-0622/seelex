package assembly

import (
	"errors"
	"sync"
	"time"

	applicationsession "github.com/RedHuang-0622/seelex/application/session"
	sessionstate "github.com/RedHuang-0622/seelex/application/session/state"
	switchworkspace "github.com/RedHuang-0622/seelex/application/workspace/switch"
	"github.com/RedHuang-0622/seelex/contract/protocol"
	switching "github.com/RedHuang-0622/seelex/kernel/sessionpolicy"
)

var ErrSessionSSHUnavailable = applicationsession.ErrSessionSSHUnavailable

func (service *Service) assembleSessionSSHApplication() {
	service.components.sessionSSHApplication = applicationsession.NewSSHApplication(applicationsession.SSHDeps{
		Sessions: func() applicationsession.SessionSSHPort {
			if service == nil || service.Deps.Sessions == nil {
				return nil
			}
			port, _ := service.Deps.Sessions.(applicationsession.SessionSSHPort)
			return port
		},
		Workspace: func() applicationsession.WorkspacePort {
			if service == nil || service.Deps.Workspace == nil {
				return nil
			}
			return service.Deps.Workspace
		},
	})
}

func (service *Service) assembleSessionMetaApplication() {
	service.components.sessionMetaApplication = applicationsession.NewMetaApplication(applicationsession.MetaDeps{
		Meta: service.metaDeps,
		ReadMeta: func(sessionID string) (model.SessionMeta, error) {
			return service.components.sessionQuery.SessionMeta(sessionID)
		},
	})
}

func (service *Service) metaDeps() switching.MetaDeps {
	if service == nil {
		return switching.MetaDeps{}
	}
	return switching.MetaDeps{
		Store: service.metaPort(),
		Wake:  service.wakeCatalogRefresh,
	}
}

func (service *Service) metaPort() sessionstate.SessionMetaPort {
	if service == nil || service.Deps.Sessions == nil {
		return nil
	}
	port, ok := service.Deps.Sessions.(sessionstate.SessionMetaPort)
	if !ok {
		return nil
	}
	return port
}

func (service *Service) wakeCatalogRefresh() {
	if service == nil || service.components.sessions == nil {
		return
	}
	service.components.sessions.RequestCatalogRefresh()
}

func (service *Service) assembleSessionReleaseApplication() {
	service.components.sessionReleaseApplication = applicationsession.NewReleaseApplication(applicationsession.ReleaseDeps{
		Protected: func(sessionID string) bool {
			service.ViewMu.RLock()
			defer service.ViewMu.RUnlock()
			return service.components.residencyRuntime.ProtectedSessionLocked(sessionID)
		},
		Persist: func(sessionID string) error {
			location := service.components.sessions.LocateSession(sessionID)
			return service.components.sessions.PersistCurrentSession(location, sessionID)
		},
		FlushToolRecord: func(sessionID string) {
			service.toolRecords.Flush(sessionID)
		},
		UnloadEngine: func(sessionID string) error {
			if engine, ok := service.Deps.Engine.(residentEngine); ok {
				return engine.UnloadSession(sessionID)
			}
			return nil
		},
		ReleaseBundle: func(sessionID string) {
			if releaser, ok := service.Deps.Engine.(interface{ ReleaseSessionBundle(string) bool }); ok {
				releaser.ReleaseSessionBundle(sessionID)
			}
		},
		ForgetGoal: func(sessionID string) {
			service.components.goal.ForgetSession(sessionID)
		},
		ReleaseContent: func(sessionID string) {
			service.components.residencyRuntime.ReleaseContentMemory(sessionID)
		},
		ReleaseFull: func(sessionID string) {
			service.ViewMu.Lock()
			service.sessions.Remove(sessionID)
			service.components.tasks.DropPlanProjection(sessionID)
			service.components.tasks.UnloadSessionState(sessionID)
			service.components.sessions.UnloadSessionTitle(sessionID)
			service.ViewMu.Unlock()
		},
		ForgetOrders: func(sessionID string) {
			service.ViewMu.Lock()
			service.components.residencyRuntime.ForgetResidentLocked(sessionID)
			service.components.residencyRuntime.ForgetContentLocked(sessionID)
			service.ViewMu.Unlock()
		},
		ReleaseToolRecord: func(sessionID string) {
			service.toolRecords.ReleaseSession(sessionID)
		},
		RequestRefresh: func() {
			service.components.sessions.RequestCatalogRefresh()
		},
		ReconcileResident: func() {
			service.components.residencyRuntime.ReconcileResident()
		},
		ReconcileContent: func() {
			service.components.residencyRuntime.ReconcileContent("")
		},
	})
}

func (service *Service) assembleSessionUseCaseApplications(restore sessionRestoreComponents) {
	service.components.sessionLifecycle = applicationsession.NewLifecycle(applicationsession.LifecycleDeps{
		Transition: service.transitionForSession,
		Gate: func(sessionID string) switching.UnloadGate {
			service.ViewMu.RLock()
			restoring := service.components.view.IsRestoringLocked(sessionID)
			running := false
			if unit := service.sessions.Unit(sessionID); unit != nil {
				running = unit.ChatState().Running
			}
			service.ViewMu.RUnlock()
			return switching.UnloadGate{Restoring: restoring, Running: running, ErrBusy: ErrChatRunning}
		},
		IsViewSession: func(sessionID string) bool {
			service.ViewMu.RLock()
			defer service.ViewMu.RUnlock()
			return sessionID == service.Core.Snapshot.Session.ID
		},
		Persist: func(sessionID string) error {
			location := service.components.sessions.LocateSession(sessionID)
			return service.components.sessions.PersistCurrentSession(location, sessionID)
		},
		Release: func(sessionID string) error {
			return service.components.sessionReleaseApplication.Release(sessionID, releaseFull)
		},
		RenewView: func() {
			service.ViewMu.Lock()
			service.switchViewToFreshDraftLocked()
			service.ViewMu.Unlock()
		},
		PublishSnapshot: func(sessionID string) {
			service.ViewMu.Lock()
			revision := service.components.view.BumpLocked()
			service.ViewMu.Unlock()
			service.publishSessionEvent(EventSnapshotChanged, revision, "", sessionID, nil)
		},
		RefreshCatalog: func() {
			service.components.sessions.RequestCatalogRefresh()
		},
	})

	engine, routed := service.Deps.Engine.(residentEngine)
	service.components.sessionArchive = applicationsession.NewArchive(applicationsession.ArchiveDeps{
		Residency:  service.components.residency,
		Transition: service.transitionForKey,
		Gate: func(sessionID string) applicationsession.ArchiveGate {
			service.ViewMu.RLock()
			gate := applicationsession.ArchiveGate{
				Closed:    service.closed,
				Draining:  service.draining,
				Busy:      service.components.residencyRuntime.SessionBusy(service.sessions.Unit(sessionID)),
				Restoring: service.components.view.IsRestoringLocked(sessionID),
			}
			service.ViewMu.RUnlock()
			if gate.Closed {
				gate.ErrClosed = errors.New("application is shut down")
			}
			if gate.Draining {
				gate.ErrDraining = ErrApplicationDraining
			}
			if gate.Busy || gate.Restoring {
				gate.ErrBusy = ErrChatRunning
			}
			return gate
		},
		Load: func(sessionID string) (model.SessionRecord, bool, error) {
			return service.components.sessions.LoadSessionRecord(service.components.sessions.LocateSession(sessionID), sessionID)
		},
		Flush: func(sessionID string) error {
			return service.components.sessions.PersistCurrentSession(service.components.sessions.LocateSession(sessionID), sessionID)
		},
		MarkArchived: func(sessionID string) error {
			return service.components.sessions.MarkSessionArchived(service.components.sessions.LocateSession(sessionID), sessionID)
		},
		RefreshProject: func(sessionID string) {
			service.components.sessions.RequestCatalogRefreshProject(service.components.sessions.LocateSession(sessionID).WorkspaceID)
		},
		EngineResident: func(sessionID string) bool {
			service.ViewMu.RLock()
			defer service.ViewMu.RUnlock()
			unit := service.sessions.Unit(sessionID)
			return unit != nil && unit.Resident()
		},
		EngineLoaded: func(sessionID string) bool {
			return routed && engine.HasSession(sessionID)
		},
		EngineUnload: func(sessionID string) error {
			return engine.UnloadSession(sessionID)
		},
		ForgetResident: func(sessionID string) {
			service.ViewMu.Lock()
			service.components.residencyRuntime.ForgetResidentLocked(sessionID)
			service.ViewMu.Unlock()
		},
		ReleaseSession: service.releaseSessionAsync,
	})

	service.components.sessionDelete = applicationsession.NewDelete(applicationsession.DeleteDeps{
		ViewMu: &service.ViewMu,
		DeleteRecord: func(sessionID string) error {
			return service.Deps.Sessions.Delete(sessionID)
		},
		ReleaseAsync: service.releaseSessionAsync,
		IsViewSession: func(sessionID string) bool {
			return service.Core.Snapshot.Session.ID == sessionID
		},
		ReleaseMemory: func(sessionID string) error {
			return service.components.sessionReleaseApplication.Release(sessionID, releaseFull)
		},
		HasWorkspace: func() bool {
			return service.Deps.Workspace != nil
		},
		UnbindWorkspace: func(sessionID string) {
			service.Deps.Workspace.UnbindSession(sessionID)
		},
		WorkspaceProjection: func() switchworkspace.Projection {
			return service.components.workspace.Projection()
		},
		ApplyWorkspace: service.applyWorkspaceProjectionLocked,
		SwitchDraft:    service.switchViewToFreshDraftLocked,
		BumpRevision: func() uint64 {
			return service.components.view.BumpLocked()
		},
		PublishSnapshot: func(revision uint64, sessionID string) {
			service.publishSessionEvent(EventSnapshotChanged, revision, "", sessionID, nil)
		},
		RefreshCatalog: func() {
			service.components.sessions.RequestCatalogRefresh()
		},
	})

	service.components.sessionResume = applicationsession.NewResume(applicationsession.ResumeDeps{
		TransitionForSession: service.transitionForSession,
		Snapshot: func(sessionID string) applicationsession.ResumeState {
			service.ViewMu.RLock()
			defer service.ViewMu.RUnlock()
			return applicationsession.ResumeState{
				Hot:       service.sessionLoaded(sessionID),
				Running:   service.anyChatRunningLocked(),
				Restoring: service.components.view.IsRestoringLocked(sessionID),
				Previous:  service.Core.Snapshot.Session.ID,
			}
		},
		PointViewAtRestoringShell: func(sessionID string) bool {
			return service.components.sessionHistoryRuntime.PointViewAtRestoringShell(sessionID)
		},
		HotAttach: service.hotAttachSession,
		MarkViewIntent: func(sessionID string) {
			service.components.view.MarkSessionViewIntent(sessionID)
		},
		BeginAsyncRestore: func(sessionID string) (uint64, error) {
			return service.components.sessionHistoryRuntime.BeginAsyncRestore(sessionID)
		},
		ResumeColdInBackground: func(sessionID, previousID string, epoch uint64) {
			go service.components.sessionHistoryRuntime.ResumeSessionColdInBackground(sessionID, previousID, epoch)
		},
		ResumeCold: func(sessionID string, epoch uint64) error {
			return service.components.sessionHistoryRuntime.ResumeSessionCold(sessionID, epoch)
		},
		HandleFailure: func(sessionID, previousID string, epoch uint64, cause error) {
			restore.recovery.Handle(sessionID, previousID, epoch, cause)
		},
	})

}

type sessionRestoreComponents struct {
	shell      *applicationsession.RestoringShellApplication
	recovery   *applicationsession.RestoreRecoveryApplication
	draftReset *applicationsession.RestoreDraftResetApplication
}

func (service *Service) assembleSessionRestoreComponents() sessionRestoreComponents {
	draftReset := applicationsession.NewRestoreDraftReset(applicationsession.RestoreDraftResetDeps{
		Lock: func() sync.Locker { return &service.ViewMu },
		EnsureSlotLocked: func() string {
			slot := service.draft
			if slot == nil {
				slot = &sessionstate.DraftSlot{ID: service.newDraftSessionIDLocked(), CreatedAt: time.Now()}
				service.draft = slot
			}
			slot.UpdatedAt = time.Now()
			return slot.ID
		},
		EnterShellLocked: func(draftID string) uint64 {
			service.enterDraftShellLocked(draftShellOptions{
				sessionID:     draftID,
				keepWorkspace: true,
				resetTitle:    true,
				tier: func(sessionID string) {
					service.syncViewPermissionTierLocked(sessionID)
				},
			})
			return service.components.view.BumpLocked()
		},
		PublishRuntimeProjections: service.publishRuntimeProjections,
		PublishSnapshot: func(revision uint64, draftID string) {
			service.publishSessionEvent(EventSnapshotChanged, revision, "", draftID, nil)
		},
	})

	recovery := applicationsession.NewRestoreRecovery(applicationsession.RestoreRecoveryDeps{
		Lock: func() sync.Locker { return &service.ViewMu },
		ClearRestoringLocked: func(sessionID string) {
			service.components.view.ClearRestoringLocked(sessionID)
		},
		SnapshotLocked: func(sessionID string, epoch uint64) applicationsession.RestoreRecoveryState {
			return applicationsession.RestoreRecoveryState{
				StillActive: service.Core.Snapshot.Session.ID == sessionID,
				Latest:      service.components.view.SessionViewIntentLocked(sessionID) == epoch,
			}
		},
		HotAttach:                 service.hotAttachSession,
		ResetDraft:                draftReset.Reset,
		PublishViewSessionChanged: service.publishViewSessionChanged,
	})

	shell := applicationsession.NewRestoringShell(applicationsession.RestoringShellDeps{
		Title:     func(sessionID string) string { return service.components.sessions.SessionTitleFor(sessionID).Value },
		SetActive: service.sessions.SetActive,
		SetSessionChat: func(sessionID string, chat model.ChatState) {
			service.components.view.SetSessionChatLockedFor(sessionID, chat)
		},
		MirrorActiveView: func() {
			service.components.view.MirrorActiveViewLocked()
		},
		BumpRevision: func() uint64 {
			return service.components.view.BumpLocked()
		},
	})
	return sessionRestoreComponents{shell: shell, recovery: recovery, draftReset: draftReset}
}
