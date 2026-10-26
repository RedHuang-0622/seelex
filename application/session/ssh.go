package session

import (
	"errors"
	"strings"

	"github.com/RedHuang-0622/seelex/middle/protocol/model"
)

// ErrSessionSSHUnavailable reports that the session port does not expose SSH
// state. It is an explicit assembly failure rather than an empty default.
var ErrSessionSSHUnavailable = errors.New("session ssh: session port does not expose the session ssh state")

// SessionSSHPort is the optional session-state capability required by the SSH
// use case. The concrete session adapter implements it structurally.
type SessionSSHPort interface {
	SessionSSHState(sessionID string) (model.SessionSSHInfo, error)
	SetSessionSSHState(sessionID string, state model.SessionSSHInfo) error
}

// WorkspacePort exposes the project binding needed to read a session's
// environment table. It intentionally does not include workspace writes.
type WorkspacePort interface {
	SessionWorkspace(sessionID string) (model.WorkspaceInfo, bool)
}

// WorkspaceEnvironmentPort is the optional project environment table reader.
type WorkspaceEnvironmentPort interface {
	WorkspaceEnvironments(projectID string) ([]model.WorkspaceEnvironmentInfo, string, error)
}

// SSHDeps supplies the two narrow host capabilities used by SSHApplication.
type SSHDeps struct {
	Sessions  func() SessionSSHPort
	Workspace func() WorkspacePort
}

// SSHApplication owns session SSH application rules: environment-table
// membership and the one-way fixed environment.
type SSHApplication struct {
	deps SSHDeps
}

// NewSSHApplication constructs the session SSH application.
func NewSSHApplication(deps SSHDeps) *SSHApplication {
	return &SSHApplication{deps: deps}
}

// State reads the current SSH state for a session.
func (application *SSHApplication) State(sessionID string) (model.SessionSSHInfo, error) {
	port := application.sessions()
	if port == nil {
		return model.SessionSSHInfo{}, ErrSessionSSHUnavailable
	}
	return port.SessionSSHState(strings.TrimSpace(sessionID))
}

// ProjectEnvironments reads the environment table for the session's bound
// project. It is used by the request-tail environment trace and does not
// change session state.
func (application *SSHApplication) ProjectEnvironments(sessionID string) ([]model.WorkspaceEnvironmentInfo, string, error) {
	return application.projectEnvironments(sessionID)
}

// SetAllowed replaces the session's allowed environment set after checking
// that every name still exists in the bound project's environment table.
func (application *SSHApplication) SetAllowed(sessionID string, envs []string) (model.SessionSSHInfo, error) {
	port := application.sessions()
	if port == nil {
		return model.SessionSSHInfo{}, ErrSessionSSHUnavailable
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return model.SessionSSHInfo{}, errors.New("session ssh: session id is required")
	}
	list, _, err := application.projectEnvironments(sessionID)
	if err != nil {
		return model.SessionSSHInfo{}, err
	}
	known := make(map[string]struct{}, len(list))
	for _, item := range list {
		known[item.Name] = struct{}{}
	}
	allowed, err := ValidateAllowedEnvs(known, envs)
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

// SetEnv fixes a session's environment once. Repeating the same value is
// idempotent; changing or clearing it is rejected.
func (application *SSHApplication) SetEnv(sessionID, projectID, env string) (model.SessionSSHInfo, error) {
	port := application.sessions()
	if port == nil {
		return model.SessionSSHInfo{}, ErrSessionSSHUnavailable
	}
	sessionID, projectID, env = strings.TrimSpace(sessionID), strings.TrimSpace(projectID), strings.TrimSpace(env)
	if sessionID == "" {
		return model.SessionSSHInfo{}, errors.New("session ssh: session id is required")
	}
	if projectID == "" {
		return model.SessionSSHInfo{}, errors.New("session ssh: project id is required")
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
	list, _, err := application.projectEnvironmentsFor(projectID)
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

func (application *SSHApplication) projectEnvironments(sessionID string) ([]model.WorkspaceEnvironmentInfo, string, error) {
	workspace := application.workspace()
	if workspace == nil {
		return nil, "", errors.New("session ssh: workspace port is not assembled")
	}
	bound, ok := workspace.SessionWorkspace(sessionID)
	if !ok || strings.TrimSpace(bound.ID) == "" {
		return nil, "", errors.New("session ssh: 会话没有绑定项目，SSH 环境表挂在项目上")
	}
	return application.projectEnvironmentsFor(bound.ID)
}

func (application *SSHApplication) projectEnvironmentsFor(projectID string) ([]model.WorkspaceEnvironmentInfo, string, error) {
	workspace := application.workspace()
	if workspace == nil {
		return nil, "", errors.New("session ssh: workspace port is not assembled")
	}
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return nil, "", errors.New("session ssh: project id is required")
	}
	port, ok := workspace.(WorkspaceEnvironmentPort)
	if !ok {
		return nil, "", errors.New("session ssh: workspace port does not expose the environment table")
	}
	return port.WorkspaceEnvironments(projectID)
}

func (application *SSHApplication) sessions() SessionSSHPort {
	if application == nil || application.deps.Sessions == nil {
		return nil
	}
	return application.deps.Sessions()
}

func (application *SSHApplication) workspace() WorkspacePort {
	if application == nil || application.deps.Workspace == nil {
		return nil
	}
	return application.deps.Workspace()
}

// ValidateAllowedEnvs checks that a requested environment set is a subset of
// the project's environment table and normalizes blank and duplicate values.
func ValidateAllowedEnvs(known map[string]struct{}, envs []string) ([]string, error) {
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
		if _, duplicate := seen[name]; duplicate {
			continue
		}
		seen[name] = struct{}{}
		allowed = append(allowed, name)
	}
	return allowed, nil
}
