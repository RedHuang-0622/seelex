package core

// workspace_environments.go 是**项目侧多环境 SSH 表**的应用层入口。
//
// 这些方法直接接收项目 ID：项目环境管理属于项目本身，不依赖当前视图会话是否已经
// 建立持久绑定。会话只在首次固定环境时引用项目环境名。
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

// workspaceEnvironmentPort 取指定项目的工作区环境端口（未装配报错）。
func (service *Service) workspaceEnvironmentPort(projectID string) (contract.WorkspaceEnvironmentPort, string, error) {
	if service == nil || service.Deps.Workspace == nil {
		return nil, "", errors.New("workspace environments: workspace port is not assembled")
	}
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return nil, "", errors.New("workspace environments: project id is required")
	}
	port, ok := service.Deps.Workspace.(contract.WorkspaceEnvironmentPort)
	if !ok {
		return nil, "", errors.New("workspace environments: workspace port does not expose the environment table")
	}
	return port, projectID, nil
}

// WorkspaceEnvironments 读指定项目的环境表与默认环境名。
func (service *Service) WorkspaceEnvironments(projectID string) ([]model.WorkspaceEnvironmentInfo, string, error) {
	port, workspaceID, err := service.workspaceEnvironmentPort(projectID)
	if err != nil {
		return nil, "", err
	}
	return port.WorkspaceEnvironments(workspaceID)
}

// SetWorkspaceEnvironment 写一个项目环境（口令三态；Delete=true 时删除）。
func (service *Service) SetWorkspaceEnvironment(projectID, name string, change model.WorkspaceEnvironmentChange) (model.WorkspaceEnvironmentInfo, error) {
	port, workspaceID, err := service.workspaceEnvironmentPort(projectID)
	if err != nil {
		return model.WorkspaceEnvironmentInfo{}, err
	}
	if strings.TrimSpace(name) == "" {
		return model.WorkspaceEnvironmentInfo{}, errors.New("workspace environments: environment name is required")
	}
	return port.SetWorkspaceEnvironment(workspaceID, strings.TrimSpace(name), change)
}

// DeleteWorkspaceEnvironment 删除一个项目环境（连同它的口令）。
func (service *Service) DeleteWorkspaceEnvironment(projectID, name string) error {
	port, workspaceID, err := service.workspaceEnvironmentPort(projectID)
	if err != nil {
		return err
	}
	return port.DeleteWorkspaceEnvironment(workspaceID, strings.TrimSpace(name))
}

// SetDefaultWorkspaceEnvironment 指定项目默认环境（空串 = 取消默认）。
func (service *Service) SetDefaultWorkspaceEnvironment(projectID, name string) error {
	port, workspaceID, err := service.workspaceEnvironmentPort(projectID)
	if err != nil {
		return err
	}
	return port.SetDefaultWorkspaceEnvironment(workspaceID, strings.TrimSpace(name))
}
