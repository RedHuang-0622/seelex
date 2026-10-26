package core

import (
	applicationsessioncold "github.com/RedHuang-0622/seelex/application/session/cold"
	session_runtime "github.com/RedHuang-0622/seelex/backend/engine/session"
	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/contract/protocol"
)

func newServiceSessionColdActivation(service *Service) *applicationsessioncold.ActivationApplication {
	return applicationsessioncold.NewActivation(applicationsessioncold.ActivationDeps{
		ClearRestoring: service.clearRestoringLocked,
		ViewIntent:     service.sessionViewIntentLocked,
		TitleFromHistory: func(history []contract.EngineMessage) string {
			return session_runtime.SessionTitleFromHistory(history, displayUserInput)
		},
		SetActive:       service.sessions.SetActive,
		UnitFor:         service.sessionUnitLocked,
		ApplyStoredTier: service.applyStoredPermissionTier,
		SetTitle: func(sessionID string, title model.SessionTitle) {
			service.components.sessions.SetSessionTitleLocked(sessionID, title)
		},
	})
}
