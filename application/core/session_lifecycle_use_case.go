package core

import (
	applicationsession "github.com/RedHuang-0622/seelex/application/session"
	switching "github.com/RedHuang-0622/seelex/application/session/switchpolicy"
)

func newServiceSessionLifecycle(service *Service) *applicationsession.LifecycleApplication {
	return applicationsession.NewLifecycle(applicationsession.LifecycleDeps{
		Transition: service.transitionForSession,
		Gate: func(sessionID string) switching.UnloadGate {
			service.ViewMu.RLock()
			restoring := service.isRestoringLocked(sessionID)
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
			return service.releaseSessionMemory(sessionID, releaseFull)
		},
		RenewView: func() {
			service.ViewMu.Lock()
			service.switchViewToFreshDraftLocked()
			service.ViewMu.Unlock()
		},
		PublishSnapshot: func(sessionID string) {
			service.ViewMu.Lock()
			revision := service.bumpLocked()
			service.ViewMu.Unlock()
			service.publishSessionEvent(EventSnapshotChanged, revision, "", sessionID, nil)
		},
		RefreshCatalog: func() {
			service.components.sessions.RequestCatalogRefresh()
		},
	})
}
