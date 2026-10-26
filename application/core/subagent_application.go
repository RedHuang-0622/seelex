package core

import applicationexecution "github.com/RedHuang-0622/seelex/application/execution"

func newServiceSubagentApplication(service *Service) *applicationexecution.SubagentApplication {
	return applicationexecution.NewSubagent(applicationexecution.SubagentDeps{
		Runtime: service.Deps.Runtime,
		Detail:  service.components.subagent.SubagentDetail,
		Refresh: service.RefreshRuntimeSnapshot,
	})
}

func (service *Service) subagentApplicationComponent() *applicationexecution.SubagentApplication {
	if service == nil {
		return nil
	}
	return service.components.subagentApplication
}
