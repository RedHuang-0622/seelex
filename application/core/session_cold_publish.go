package core

import (
	applicationsessioncold "github.com/RedHuang-0622/seelex/application/session/cold"
	session_runtime "github.com/RedHuang-0622/seelex/application/session/runtime"
	"github.com/RedHuang-0622/seelex/contract/dto"
)

func newServiceSessionColdPublish(service *Service) *applicationsessioncold.PublishApplication {
	return applicationsessioncold.NewPublish(applicationsessioncold.PublishDeps{
		SetGlobalPrompt: func(prompt string) {
			if _, ok := service.Deps.Engine.(interface{ SetSystemPromptFor(string, string) }); !ok {
				service.Deps.Engine.SetSystemPrompt(prompt)
			}
		},
		AttachContext: func(location session_runtime.Location, sessionID string) error {
			return service.attachSessionContextFor(location.WorkspaceID, sessionID)
		},
		SwitchTasks: func(sessionID string, tasks []dto.TaskRecord) {
			service.Deps.Runtime.SwitchSessionTasks(sessionID, tasks)
		},
		ClearSubagentTree: func() {
			_ = service.Deps.Runtime.ClearSubagentTree()
		},
		RestoreAnchors: func(sessionID string) {
			_ = service.Deps.Runtime.RestoreSubagentAnchors(sessionID)
		},
		RefreshRuntime: service.refreshRuntimeProjectionForSession,
		PublishSnapshot: func(revision uint64, sessionID string) {
			service.publishSessionEvent(EventSnapshotChanged, revision, "", sessionID, nil)
		},
		PublishRuntime: service.publishRuntimeProjections,
		RefreshCatalog: func() { service.components.sessions.RequestCatalogRefresh() },
		TouchResident:  service.touchResident,
		TouchContent:   service.touchContent,
	})
}
