package core

import (
	applicationexecution "github.com/RedHuang-0622/seelex/application/execution"
	"github.com/RedHuang-0622/seelex/backend/multiagent/teamwork"
)

func newServiceExecutionApplication(service *Service) *applicationexecution.WorkItemApplication {
	reader := teamwork.NewBoardReader(service.Deps.Runtime)
	return applicationexecution.NewWorkItem(applicationexecution.WorkItemDeps{
		Runtime:     service.Deps.Runtime,
		BoardView:   reader.BoardViewFor,
		SessionLive: reader.SessionLiveFor,
		SessionPage: reader.SessionLivePageFor,
		RefreshTodo: service.refreshRuntimeAfterTodoChange,
	})
}

func (service *Service) executionApplicationComponent() *applicationexecution.WorkItemApplication {
	if service == nil {
		return nil
	}
	return service.components.executionApplication
}
