package core

// session_ssh.go 是「会话级 SSH」的应用层：读状态、设当前环境、设放权集合。
//
// 三条不变式在这里强制（执行侧判定再兜一次，两处都必须有，方向只能是收紧）：
//
//	1. AllowedEnvs ⊆ 项目环境表——放权集合是**项目环境表的子集**，不许放一个
//	   项目里不存在的名字（否则环境被删掉之后那条放权会变成"永远命不中的僵尸权限"，
//	   更糟的是将来同名环境重建时它悄悄复活）。
//	2. CurrentEnv 是**会话固定环境**：首次由用户绑定，之后 agent 与工具入参都不能改；
//	   要换环境必须新开会话（与 VS Code 的 remote workspace 同口径）。
//	3. 缺省 CurrentEnv 为空 = 不绑定命名环境，环境感知工具在本地项目根执行。
//
// 环境表的事实源只有一处：项目（contract.WorkspaceEnvironmentPort）。会话侧只存名字。
// 这条边界决定了"热更新"是免费的——项目改表之后，会话下一次读就看见新表，不需要
// 任何失效协议，也没有第二份缓存要同步。

import (
	"errors"
	"strings"

	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/middle/protocol/model"
)

// ErrSessionSSHUnavailable 表示会话端口没有暴露「会话 SSH 状态」的读写能力（装配缺口）。
var ErrSessionSSHUnavailable = errors.New("session ssh: session port does not expose the session ssh state")

// sessionSSHPort 是会话端口上的**可选**能力：会话级 SSH 状态（键 = sessionID）。
//
// 接口定义在消费方（本包），装配处实现会话端口时顺带实现——与 rolePermissionPort
// 同款发现方式（类型断言，缺失即不可用）。
type sessionSSHPort interface {
	SessionSSHState(sessionID string) (model.SessionSSHInfo, error)
	SetSessionSSHState(sessionID string, state model.SessionSSHInfo) error
}

// sessionSSHStore 取会话 SSH 状态端口（未装配返回 nil）。
func (service *Service) sessionSSHStore() sessionSSHPort {
	if service == nil || service.Deps.Sessions == nil {
		return nil
	}
	port, _ := service.Deps.Sessions.(sessionSSHPort)
	return port
}

// projectEnvironments 读**该会话所属项目**的环境表（会话不持有连接信息，只认名字）。
//
// 没绑定项目 = 没有环境表可用：显式报错而不是回落到"空表就放行"。
func (service *Service) projectEnvironments(sessionID string) ([]model.WorkspaceEnvironmentInfo, string, error) {
	if service == nil || service.Deps.Workspace == nil {
		return nil, "", errors.New("session ssh: workspace port is not assembled")
	}
	bound, ok := service.Deps.Workspace.SessionWorkspace(sessionID)
	if !ok || strings.TrimSpace(bound.ID) == "" {
		return nil, "", errors.New("session ssh: 会话没有绑定项目，SSH 环境表挂在项目上")
	}
	port, ok := service.Deps.Workspace.(contract.WorkspaceEnvironmentPort)
	if !ok {
		return nil, "", errors.New("session ssh: workspace port does not expose the environment table")
	}
	return port.WorkspaceEnvironments(bound.ID)
}

// SessionSSHState 读一个会话的 SSH 状态（缺省 = 没环境、没放权）。
func (service *Service) SessionSSHState(sessionID string) (model.SessionSSHInfo, error) {
	port := service.sessionSSHStore()
	if port == nil {
		return model.SessionSSHInfo{}, ErrSessionSSHUnavailable
	}
	return port.SessionSSHState(strings.TrimSpace(sessionID))
}

// SetSessionSSHAllowed 是旧版放权集合的兼容写面，并返回落盘后读回的状态。
//
// 校验口径：
//   - 每个名字都必须在项目环境表里（AllowedEnvs ⊆ 项目环境表）；
//   - 当前会话已经固定环境时，不能从兼容集合中撤销该环境；换环境必须新开会话。
func (service *Service) SetSessionSSHAllowed(sessionID string, envs []string) (model.SessionSSHInfo, error) {
	port := service.sessionSSHStore()
	if port == nil {
		return model.SessionSSHInfo{}, ErrSessionSSHUnavailable
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return model.SessionSSHInfo{}, errors.New("session ssh: session id is required")
	}
	list, _, err := service.projectEnvironments(sessionID)
	if err != nil {
		return model.SessionSSHInfo{}, err
	}
	known := make(map[string]struct{}, len(list))
	for _, item := range list {
		known[item.Name] = struct{}{}
	}
	allowed, err := validateAllowedEnvs(known, envs)
	if err != nil {
		return model.SessionSSHInfo{}, err
	}
	state, err := port.SessionSSHState(sessionID)
	if err != nil {
		return model.SessionSSHInfo{}, err
	}
	if current := strings.TrimSpace(state.CurrentEnv); current != "" {
		found := false
		for _, name := range allowed {
			if name == current {
				found = true
				break
			}
		}
		if !found {
			return model.SessionSSHInfo{}, errors.New("session ssh: 固定环境 " + current + " 不能从会话放权集合中撤销；请新建会话")
		}
	}
	state.AllowedEnvs = allowed
	if err := port.SetSessionSSHState(sessionID, state); err != nil {
		return model.SessionSSHInfo{}, err
	}
	return port.SessionSSHState(sessionID)
}

// SetSessionSSHEnv 首次为用户会话绑定固定环境。绑定后不可修改或清空：环境是
// 会话身份的一部分，工具与 agent 都不获得改写它的入口。
func (service *Service) SetSessionSSHEnv(sessionID, env string) (model.SessionSSHInfo, error) {
	port := service.sessionSSHStore()
	if port == nil {
		return model.SessionSSHInfo{}, ErrSessionSSHUnavailable
	}
	sessionID, env = strings.TrimSpace(sessionID), strings.TrimSpace(env)
	if sessionID == "" {
		return model.SessionSSHInfo{}, errors.New("session ssh: session id is required")
	}
	state, err := port.SessionSSHState(sessionID)
	if err != nil {
		return model.SessionSSHInfo{}, err
	}
	if current := strings.TrimSpace(state.CurrentEnv); current != "" {
		if current == env {
			return state, nil
		}
		return model.SessionSSHInfo{}, errors.New("session ssh: 固定环境 " + current + " 不可更改；请新建会话选择其它环境")
	}
	if env == "" {
		return model.SessionSSHInfo{}, errors.New("session ssh: 请选择一个项目工作环境")
	}
	// 项目环境表可能已经把这个名字删掉了（项目侧改了表）：这里兜一次，避免把
	// 一个已经不存在于项目里的名字写进会话状态。
	list, _, err := service.projectEnvironments(sessionID)
	if err != nil {
		return model.SessionSSHInfo{}, err
	}
	exists := false
	for _, item := range list {
		if item.Name == env {
			exists = true
			break
		}
	}
	if !exists {
		return model.SessionSSHInfo{}, errors.New("session ssh: 环境 " + env + " 已经不在项目环境表里（项目侧可能刚删掉它）")
	}
	state.CurrentEnv = env
	if err := port.SetSessionSSHState(sessionID, state); err != nil {
		return model.SessionSSHInfo{}, err
	}
	return port.SessionSSHState(sessionID)
}

// validateAllowedEnvs 校验放权集合是**项目环境表的子集**，并把集合规整成
// 去空白 / 去重 / 保序的形态。
//
// 为什么必须卡子集：放权集合是会话里的人为选择，而环境表是项目的事实。允许写进
// 一个项目里不存在的名字，后果是那条放权永远命不中；更麻烦的是将来同名环境被重建
// 时它会悄悄复活——一个用户早已忘记的旧授权重新生效。
func validateAllowedEnvs(known map[string]struct{}, envs []string) ([]string, error) {
	allowed := make([]string, 0, len(envs))
	seen := make(map[string]struct{}, len(envs))
	for _, raw := range envs {
		name := strings.TrimSpace(raw)
		if name == "" {
			continue
		}
		if _, exists := known[name]; !exists {
			return nil, errors.New("session ssh: 环境 " + name + " 不在项目环境表里（放权集合必须是项目环境的子集）")
		}
		if _, dup := seen[name]; dup {
			continue
		}
		seen[name] = struct{}{}
		allowed = append(allowed, name)
	}
	return allowed, nil
}
