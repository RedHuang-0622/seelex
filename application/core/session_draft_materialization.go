package core

import (
	"time"

	applicationsession "github.com/RedHuang-0622/seelex/application/session"
	session_runtime "github.com/RedHuang-0622/seelex/application/session/runtime"
	"github.com/RedHuang-0622/seelex/contract/protocol"
)

func newServiceSessionDraftMaterialization(service *Service) *applicationsession.DraftMaterializationApplication {
	return applicationsession.NewDraftMaterialization(applicationsession.DraftMaterializationDeps{
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
}
