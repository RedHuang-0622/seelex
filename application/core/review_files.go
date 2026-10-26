package core

import (
	task_context "github.com/RedHuang-0622/seelex/application/execution/taskcontext"
	applicationreview "github.com/RedHuang-0622/seelex/application/review"
	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/contract/dto"
)

func (service *Service) reviewApplicationComponent() *applicationreview.Application {
	if service == nil {
		return nil
	}
	return service.components.reviewApplication
}

func newServiceReviewApplication(service *Service) *applicationreview.Application {
	return applicationreview.New(applicationreview.Deps{
		WorkspaceFile: func() (contract.WorkspaceFilePort, string, error) {
			return service.components.workspace.FilePort()
		},
		SessionIDFromContext: task_context.SessionIDFromContext,
		ViewSessionID:        service.currentViewSessionID,
		SetReviewRequest: func(sessionID string, request *dto.ReviewRequest) uint64 {
			service.ViewMu.Lock()
			defer service.ViewMu.Unlock()
			service.components.view.SetReviewRequestForLocked(sessionID, request)
			return service.bumpLocked()
		},
		ClearReviewRequest: func(sessionID string) {
			service.ViewMu.Lock()
			defer service.ViewMu.Unlock()
			if service.components.view.ReviewRequestForLocked(sessionID) != nil {
				service.components.view.ClearReviewRequestForLocked(sessionID)
				service.bumpLocked()
			}
		},
		PublishSnapshotChanged: func(sessionID string, revision uint64) {
			service.publishSessionEvent(EventSnapshotChanged, revision, "", sessionID, nil)
		},
	})
}
