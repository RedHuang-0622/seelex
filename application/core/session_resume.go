package core

import applicationsession "github.com/RedHuang-0622/seelex/application/session"

func newServiceSessionResume(service *Service) *applicationsession.ResumeApplication {
	return applicationsession.NewResume(applicationsession.ResumeDeps{
		TransitionForSession: service.transitionForSession,
		Snapshot: func(sessionID string) applicationsession.ResumeState {
			service.ViewMu.RLock()
			defer service.ViewMu.RUnlock()
			return applicationsession.ResumeState{
				Hot:       service.sessionLoaded(sessionID),
				Running:   service.anyChatRunningLocked(),
				Restoring: service.isRestoringLocked(sessionID),
				Previous:  service.Core.Snapshot.Session.ID,
			}
		},
		PointViewAtRestoringShell: service.pointViewAtRestoringShell,
		HotAttach:                 service.hotAttachSession,
		MarkViewIntent:            service.markSessionViewIntent,
		BeginAsyncRestore:         service.beginAsyncRestore,
		ResumeColdInBackground: func(sessionID, previousID string, epoch uint64) {
			go service.resumeSessionColdInBackground(sessionID, previousID, epoch)
		},
		ResumeCold:    service.resumeSessionCold,
		HandleFailure: service.handleColdRestoreFailure,
	})
}
