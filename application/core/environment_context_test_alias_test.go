package core

import (
	"github.com/RedHuang-0622/seelex/application/prompt"
	protocol "github.com/RedHuang-0622/seelex/contract/protocol"
)

const (
	environmentTraceMarkerOpen  = prompt.EnvironmentTraceMarkerOpen
	environmentTraceMarkerClose = prompt.EnvironmentTraceMarkerClose
)

func renderEnvironmentTraceBlock(projectRoot, defaultEnv, currentEnv string, environments []protocol.WorkspaceEnvironmentInfo) string {
	return prompt.RenderEnvironmentTraceBlock(projectRoot, defaultEnv, currentEnv, environments)
}

func (service *Service) environmentTraceBlockFor(sessionID string) string {
	return prompt.EnvironmentTraceBlockFor(prompt.EnvironmentTraceDeps{
		ProjectEnvironments: service.components.sessionSSHApplication.ProjectEnvironments,
		CurrentEnvironment: func(sessionID string) (string, error) {
			state, err := service.components.sessionSSHApplication.State(sessionID)
			return state.CurrentEnv, err
		},
		ProjectRoot: func(sessionID string) string {
			if service.Deps.Workspace == nil {
				return ""
			}
			workspace, _ := service.Deps.Workspace.SessionWorkspace(sessionID)
			return workspace.RootPath
		},
	}, sessionID)
}
