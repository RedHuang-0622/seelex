package integration

import (
	assembly "github.com/RedHuang-0622/seelex/application/assembly"
	applicationworkspace "github.com/RedHuang-0622/seelex/application/workspace"
	switchworkspace "github.com/RedHuang-0622/seelex/application/workspace/switch"
)

func workspaceUseCase(service *assembly.Service) *switchworkspace.UseCase {
	return assembly.ComponentsFor(service).Workspace.Switch
}

func workspaceApplication(service *assembly.Service) *applicationworkspace.Application {
	return assembly.ComponentsFor(service).Workspace.Projects
}
