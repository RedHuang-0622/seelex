package core

import (
	"sync"

	applicationsession "github.com/RedHuang-0622/seelex/application/session"
)

func newServiceSessionRestoreRecovery(service *Service) *applicationsession.RestoreRecoveryApplication {
	return applicationsession.NewRestoreRecovery(applicationsession.RestoreRecoveryDeps{
		Lock:                 func() sync.Locker { return &service.ViewMu },
		ClearRestoringLocked: service.clearRestoringLocked,
		SnapshotLocked: func(sessionID string, epoch uint64) applicationsession.RestoreRecoveryState {
			return applicationsession.RestoreRecoveryState{
				StillActive: service.Core.Snapshot.Session.ID == sessionID,
				Latest:      service.sessionViewIntentLocked(sessionID) == epoch,
			}
		},
		HotAttach:                 service.hotAttachSession,
		ResetDraft:                service.resetViewToDraftAfterRestoreFailure,
		PublishViewSessionChanged: service.publishViewSessionChanged,
	})
}
