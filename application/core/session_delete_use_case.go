package core

import (
	applicationsession "github.com/RedHuang-0622/seelex/application/session"
	applicationworkspace "github.com/RedHuang-0622/seelex/application/workspace"
)

func newServiceSessionDelete(service *Service) *applicationsession.DeleteApplication {
	return applicationsession.NewDelete(applicationsession.DeleteDeps{
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
		WorkspaceProjection: func() applicationworkspace.SwitchProjection {
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
}
