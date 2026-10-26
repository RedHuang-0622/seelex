package core

import (
	applicationsessioncold "github.com/RedHuang-0622/seelex/application/session/cold"
	session_runtime "github.com/RedHuang-0622/seelex/application/session/runtime"
	"github.com/RedHuang-0622/seelex/contract"
)

func newServiceSessionColdRestore(service *Service) *applicationsessioncold.RestoreApplication {
	return applicationsessioncold.NewRestore(applicationsessioncold.RestoreDeps{
		SetWorkspace: service.Deps.Runtime.SetSessionWorkspace,
		ResumeEngine: func(sessionID string, history []contract.EngineMessage) (bool, error) {
			if enginePort, ok := service.Deps.Engine.(interface {
				ResumeSession(string, []contract.EngineMessage) error
			}); ok {
				return true, enginePort.ResumeSession(sessionID, history)
			}
			return false, nil
		},
		ReplaceHistory: service.Deps.Engine.ReplaceHistory,
		RecoverQueue: func(location session_runtime.Location, sessionID string) error {
			resent, lifecycleOK, err := service.components.sessions.QueueRecoverInputs(location, sessionID)
			if err != nil {
				return err
			}
			if lifecycleOK && len(resent) > 0 {
				service.reEnqueueRecoveredInputs(sessionID, resent)
			}
			return nil
		},
		WritePrompt: service.writeSessionSystemPrompt,
	})
}
