package core

// workspace_environments.go 是**项目侧多环境 SSH 表**的应用层入口。
//
// 为什么这些方法都带 sessionID：环境表挂在项目上，而"当前是哪个项目"由会话的绑定
// 决定（与会话名字/路径无关）。所以桥面拿当前视图会话的 id 进来，这里解析出项目再
// 转发给工作区端口——应用层不缓存"当前项目"这种会随切换失效的读数。
//
// 与 session_ssh.go 的分工：
//   - 本文件管**项目**（环境表：连接信息、凭证、默认环境）；
//   - session_ssh.go 管**会话**（限定哪个环境、放权哪些环境）。
//   两者之间只有名字这一条连接，"会话允许的环境是项目环境的子集"因此是一个可校验
//   的约束，而不是一句口头约定。

import (
	"errors"
	"strings"

	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/middle/protocol/model"
)

// workspaceEnvironmentPort 取会话所属项目的工作区环境端口（未装配报错）。
func (service *Service) workspaceEnvironmentPort(sessionID string) (contract.WorkspaceEnvironmentPort, string, error) {
	if service == nil || service.Deps.Workspace == nil {
		return nil, "", errors.New("workspace environments: workspace port is not assembled")
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, "", errors.New("workspace environments: session id is required")
	}
	bound, ok := service.Deps.Workspace.SessionWorkspace(sessionID)
	if !ok || strings.TrimSpace(bound.ID) == "" {
		return nil, "", errors.New("workspace environments: 会话没有绑定项目（环境表挂在项目上）")
	}
	port, ok := service.Deps.Workspace.(contract.WorkspaceEnvironmentPort)
	if !ok {
		return nil, "", errors.New("workspace environments: workspace port does not expose the environment table")
	}
	return port, bound.ID, nil
}

// WorkspaceEnvironments 读会话所属项目的环境表与默认环境名。
func (service *Service) WorkspaceEnvironments(sessionID string) ([]model.WorkspaceEnvironmentInfo, string, error) {
	port, workspaceID, err := service.workspaceEnvironmentPort(sessionID)
	if err != nil {
		return nil, "", err
	}
	return port.WorkspaceEnvironments(workspaceID)
}

// SetWorkspaceEnvironment 写一个项目环境（口令三态；Delete=true 时删除）。
func (service *Service) SetWorkspaceEnvironment(sessionID, name string, change model.WorkspaceEnvironmentChange) (model.WorkspaceEnvironmentInfo, error) {
	port, workspaceID, err := service.workspaceEnvironmentPort(sessionID)
	if err != nil {
		return model.WorkspaceEnvironmentInfo{}, err
	}
	if strings.TrimSpace(name) == "" {
		return model.WorkspaceEnvironmentInfo{}, errors.New("workspace environments: environment name is required")
	}
	return port.SetWorkspaceEnvironment(workspaceID, strings.TrimSpace(name), change)
}

// DeleteWorkspaceEnvironment 删除一个项目环境（连同它的口令）。
func (service *Service) DeleteWorkspaceEnvironment(sessionID, name string) error {
	port, workspaceID, err := service.workspaceEnvironmentPort(sessionID)
	if err != nil {
		return err
	}
	return port.DeleteWorkspaceEnvironment(workspaceID, strings.TrimSpace(name))
}

// SetDefaultWorkspaceEnvironment 指定项目默认环境（空串 = 取消默认）。
func (service *Service) SetDefaultWorkspaceEnvironment(sessionID, name string) error {
	port, workspaceID, err := service.workspaceEnvironmentPort(sessionID)
	if err != nil {
		return err
	}
	return port.SetDefaultWorkspaceEnvironment(workspaceID, strings.TrimSpace(name))
}
