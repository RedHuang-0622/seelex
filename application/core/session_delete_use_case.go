package core

import applicationsession "github.com/RedHuang-0622/seelex/application/session"

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
			return service.releaseSessionMemory(sessionID, releaseFull)
		},
		HasWorkspace: func() bool {
			return service.Deps.Workspace != nil
		},
		UnbindWorkspace: func(sessionID string) {
			service.Deps.Workspace.UnbindSession(sessionID)
		},
		WorkspaceProjection: service.components.workspace.Projection,
		ApplyWorkspace:      service.applyWorkspaceProjectionLocked,
		SwitchDraft:         service.switchViewToFreshDraftLocked,
		BumpRevision:        service.bumpLocked,
		PublishSnapshot: func(revision uint64, sessionID string) {
			service.publishSessionEvent(EventSnapshotChanged, revision, "", sessionID, nil)
		},
		RefreshCatalog: func() {
			service.components.sessions.RequestCatalogRefresh()
		},
	})
}
