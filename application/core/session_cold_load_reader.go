package core

import (
	applicationsessioncold "github.com/RedHuang-0622/seelex/application/session/cold"
	session_runtime "github.com/RedHuang-0622/seelex/backend/engine/session"
	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/contract/protocol"
)

func newServiceSessionColdRead(service *Service) *applicationsessioncold.ReadApplication {
	return applicationsessioncold.NewRead(applicationsessioncold.ReadDeps{
		RecoverTail: service.recoverPendingMessageTailAtLoad,
		Locate: func(sessionID string) session_runtime.Location {
			return service.components.sessions.LocateSession(sessionID)
		},
		LoadRecord: func(location session_runtime.Location, sessionID string) (model.SessionRecord, int, bool, error) {
			return service.components.sessions.LoadSessionResumeRecord(location, sessionID)
		},
		LoadTranscript: func(location session_runtime.Location, sessionID string) ([]model.TranscriptEvent, error) {
			return service.components.sessions.LoadSessionTranscript(location, sessionID)
		},
		LoadHistory: func(location session_runtime.Location) ([]contract.EngineMessage, int, error) {
			return service.components.sessions.LoadHistoryTailWindow(location)
		},
	})
}
