package core

import (
	"sync"
	"time"

	applicationsession "github.com/RedHuang-0622/seelex/application/session"
)

func newServiceSessionRestoreDraftReset(service *Service) *applicationsession.RestoreDraftResetApplication {
	return applicationsession.NewRestoreDraftReset(applicationsession.RestoreDraftResetDeps{
		Lock: func() sync.Locker { return &service.ViewMu },
		EnsureSlotLocked: func() string {
			slot := service.draft
			if slot == nil {
				slot = &draftSlot{ID: service.newDraftSessionIDLocked(), CreatedAt: time.Now()}
				service.draft = slot
			}
			slot.UpdatedAt = time.Now()
			return slot.ID
		},
		EnterShellLocked: func(draftID string) uint64 {
			service.enterDraftShellLocked(draftShellOptions{
				sessionID:     draftID,
				keepWorkspace: true,
				resetTitle:    true,
				tier: func(sessionID string) {
					service.syncViewPermissionTierLocked(sessionID)
				},
			})
			return service.components.view.BumpLocked()
		},
		PublishRuntimeProjections: service.publishRuntimeProjections,
		PublishSnapshot: func(revision uint64, draftID string) {
			service.publishSessionEvent(EventSnapshotChanged, revision, "", draftID, nil)
		},
	})
}
