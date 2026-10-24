package adapters

// workspace_environment_port.go —— 项目侧工作环境表在装配侧的落地：
// 把 store/workspace 的环境表 API 翻成 contract.WorkspaceEnvironmentPort 的读面/写面
// （读面无口令，写面口令三态）。
//
// 为什么放在 WorkspacePort 上而不是新开一个结构：环境表与连接配置**同一份存储文件**
// （workspace_index.json + 同一个密文库），分成两个端口结构只会让人以为它们可以分别
// 装配；能力探测仍然走窄端口（`port.(contract.WorkspaceEnvironmentPort)`），
// 没有持久化的宿主照样只读。

import (
	"fmt"

	"github.com/RedHuang-0622/seelex/middle/protocol/model"
	"github.com/RedHuang-0622/seelex/store/workspace"
)

// WorkspaceEnvironments 实现 contract.WorkspaceEnvironmentPort：返回该项目全部环境
// （按名字升序）与默认环境名。
func (port WorkspacePort) WorkspaceEnvironments(id string) ([]model.WorkspaceEnvironmentInfo, string, error) {
	if port.Repo == nil {
		return nil, "", fmt.Errorf("workspace environments: repo is not assembled")
	}
	list, defaultEnv, err := port.Repo.Environments(id)
	if err != nil {
		return nil, "", err
	}
	out := make([]model.WorkspaceEnvironmentInfo, 0, len(list))
	for _, item := range list {
		out = append(out, adaptEnvironment(id, item))
	}
	return out, defaultEnv, nil
}

// SetWorkspaceEnvironment 实现写面：Delete=true 走删除；否则写入并（可选）设为默认。
func (port WorkspacePort) SetWorkspaceEnvironment(id, name string, change model.WorkspaceEnvironmentChange) (model.WorkspaceEnvironmentInfo, error) {
	if port.Repo == nil {
		return model.WorkspaceEnvironmentInfo{}, fmt.Errorf("workspace environments: repo is not assembled")
	}
	if change.Delete {
		if err := port.Repo.DeleteEnvironment(id, name); err != nil {
			return model.WorkspaceEnvironmentInfo{}, err
		}
		return model.WorkspaceEnvironmentInfo{WorkspaceID: id, Name: name}, nil
	}
	info, err := port.Repo.SetEnvironment(id, name, workspace.Environment{
		SSHEndpoint: workspace.SSHEndpoint{
			Host: change.Host, Port: change.Port, User: change.User, RemoteRoot: change.RemoteRoot,
		},
		Note: change.Note,
	}, change.Password)
	if err != nil {
		return model.WorkspaceEnvironmentInfo{}, err
	}
	if change.MakeDefault {
		if err := port.Repo.SetDefaultEnvironment(id, name); err != nil {
			return model.WorkspaceEnvironmentInfo{}, err
		}
		// 设了默认之后重新读一遍：返回值的 Default 必须是**落盘后**的事实，
		// 不能拿写之前的读数糊过去。
		list, defaultEnv, listErr := port.Repo.Environments(id)
		if listErr != nil {
			return model.WorkspaceEnvironmentInfo{}, listErr
		}
		for _, item := range list {
			if item.Name == name {
				adapted := adaptEnvironment(id, item)
				adapted.Default = item.Name == defaultEnv
				return adapted, nil
			}
		}
	}
	return adaptEnvironment(id, info), nil
}

// DeleteWorkspaceEnvironment 实现写面：删除环境（连同口令）。
func (port WorkspacePort) DeleteWorkspaceEnvironment(id, name string) error {
	if port.Repo == nil {
		return fmt.Errorf("workspace environments: repo is not assembled")
	}
	return port.Repo.DeleteEnvironment(id, name)
}

// SetDefaultWorkspaceEnvironment 实现写面：指定默认环境（空串 = 取消默认）。
func (port WorkspacePort) SetDefaultWorkspaceEnvironment(id, name string) error {
	if port.Repo == nil {
		return fmt.Errorf("workspace environments: repo is not assembled")
	}
	return port.Repo.SetDefaultEnvironment(id, name)
}

// adaptEnvironment 把存储层读面翻成跨层读面（字段逐个搬，不做任何推断）。
func adaptEnvironment(workspaceID string, item workspace.EnvironmentInfo) model.WorkspaceEnvironmentInfo {
	return model.WorkspaceEnvironmentInfo{
		WorkspaceID: workspaceID,
		Name:        item.Name,
		Host:        item.Host,
		Port:        item.Port,
		User:        item.User,
		RemoteRoot:  item.RemoteRoot,
		Note:        item.Note,
		PasswordSet: item.PasswordSet,
		Default:     item.Default,
	}
}
