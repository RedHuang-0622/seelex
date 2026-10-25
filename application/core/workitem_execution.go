package core

import (
	"context"
	"errors"

	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/contract/dto"
)

// WorkItemExecutionFor is the only Application read used by both WorkItem
// details and a teammate's current/history WorkItem progress page.
func (service *Service) WorkItemExecutionFor(ctx context.Context, mainSessionID, itemID string, page dto.ConversationViewRequest) (dto.WorkItemExecutionView, error) {
	if service == nil || service.Deps.Runtime == nil {
		return dto.WorkItemExecutionView{}, errors.New("workitem execution: runtime is not configured")
	}
	port, ok := service.Deps.Runtime.(contract.WorkItemExecutionPort)
	if !ok {
		return dto.WorkItemExecutionView{}, errors.New("workitem execution: adapter is not configured")
	}
	return port.WorkItemExecution(ctx, mainSessionID, itemID, page)
}

// TeammateInspectionFor returns the safe teammate details without changing the
// team plan or WorkItem lifecycle.
func (service *Service) TeammateInspectionFor(ctx context.Context, mainSessionID, role string) (dto.TeammateInspectionView, error) {
	if service == nil || service.Deps.Runtime == nil {
		return dto.TeammateInspectionView{}, errors.New("teammate inspection: runtime is not configured")
	}
	port, ok := service.Deps.Runtime.(contract.TeammateInspectionPort)
	if !ok {
		return dto.TeammateInspectionView{}, errors.New("teammate inspection: adapter is not configured")
	}
	return port.TeammateInspection(ctx, mainSessionID, role)
}
