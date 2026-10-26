package core

import (
	"context"

	"github.com/RedHuang-0622/seelex/contract/dto"
)

// WorkItemExecutionFor is the only Application read used by both WorkItem
// details and a teammate's current/history WorkItem progress page.
func (service *Service) WorkItemExecutionFor(ctx context.Context, mainSessionID, itemID string, page dto.ConversationViewRequest) (dto.WorkItemExecutionView, error) {
	return service.executionApplicationComponent().WorkItemExecution(ctx, mainSessionID, itemID, page)
}

// TeammateInspectionFor returns the safe teammate details without changing the
// team plan or WorkItem lifecycle.
func (service *Service) TeammateInspectionFor(ctx context.Context, mainSessionID, role string) (dto.TeammateInspectionView, error) {
	return service.executionApplicationComponent().TeammateInspection(ctx, mainSessionID, role)
}
