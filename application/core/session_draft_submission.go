package core

import applicationsession "github.com/RedHuang-0622/seelex/application/session"

func newServiceSessionDraftSubmission(service *Service) *applicationsession.DraftSubmissionApplication {
	return applicationsession.NewDraftSubmission(applicationsession.DraftSubmissionDeps{
		IsDraftTarget: service.isUnmaterializedDraftTarget,
		Transition:    service.transitionView,
		IsInView: func(sessionID string) bool {
			service.ViewMu.RLock()
			defer service.ViewMu.RUnlock()
			return service.draftTargetLocked().InView(sessionID)
		},
		Materialize: service.materializeDraftSession,
		NotInView:   ErrDraftNotInView,
	})
}
