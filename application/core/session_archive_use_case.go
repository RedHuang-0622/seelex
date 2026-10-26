package core

import (
	"errors"

	applicationsession "github.com/RedHuang-0622/seelex/application/session"
	"github.com/RedHuang-0622/seelex/middle/protocol/model"
	"github.com/RedHuang-0622/seelex/middle/residency"
)

func newServiceSessionArchive(service *Service) *applicationsession.ArchiveApplication {
	engine, routed := service.Deps.Engine.(residentEngine)
	return applicationsession.NewArchive(applicationsession.ArchiveDeps{
		Transition: service.transitionForKey,
		Gate: func(sessionID string) residency.ArchiveGate {
			service.ViewMu.RLock()
			gate := residency.ArchiveGate{
				Closed:    service.closed,
				Draining:  service.draining,
				Busy:      service.sessionBusy(service.sessions.Unit(sessionID)),
				Restoring: service.isRestoringLocked(sessionID),
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
			service.forgetResidentLocked(sessionID)
			service.ViewMu.Unlock()
		},
		ReleaseSession: service.releaseSessionAsync,
	})
}
