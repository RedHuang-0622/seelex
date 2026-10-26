package core

import applicationexecution "github.com/RedHuang-0622/seelex/application/execution"

func newServiceExecutionApplication(service *Service, reader applicationexecution.BoardReader) *applicationexecution.WorkItemApplication {
	return applicationexecution.NewWorkItem(applicationexecution.WorkItemDeps{
		Runtime: service.Deps.Runtime, BoardView: reader.BoardViewFor,
		SessionLive: reader.SessionLiveFor, SessionPage: reader.SessionLivePageFor,
		RefreshTodo: service.refreshRuntimeAfterTodoChange,
	})
}
