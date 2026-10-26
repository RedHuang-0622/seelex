package core

import (
	applicationsession "github.com/RedHuang-0622/seelex/application/session"
	session_runtime "github.com/RedHuang-0622/seelex/backend/engine/session"
)

func newServiceSessionForkLineage(service *Service) *applicationsession.ForkLineageApplication {
	var loadPayload func(session_runtime.Location, string) ([]byte, error)
	if port, ok := service.Deps.Sessions.(session_runtime.SessionForkPort); ok {
		loadPayload = func(location session_runtime.Location, sessionID string) ([]byte, error) {
			return port.LoadContextStateWorkspace(location.WorkspaceID, sessionID)
		}
	}
	return applicationsession.NewForkLineage(applicationsession.ForkLineageDeps{
		LoadPayload: loadPayload,
	})
}
