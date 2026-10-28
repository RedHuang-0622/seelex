package integration

import "github.com/RedHuang-0622/seelex/contract/protocol"

type environmentTraceWorkspace struct {
	*fakeWorkspace
	environments []model.WorkspaceEnvironmentInfo
	defaultEnv   string
}

func (workspace *environmentTraceWorkspace) WorkspaceEnvironments(string) ([]model.WorkspaceEnvironmentInfo, string, error) {
	return append([]model.WorkspaceEnvironmentInfo(nil), workspace.environments...), workspace.defaultEnv, nil
}

func (*environmentTraceWorkspace) SetWorkspaceEnvironment(string, string, model.WorkspaceEnvironmentChange) (model.WorkspaceEnvironmentInfo, error) {
	return model.WorkspaceEnvironmentInfo{}, nil
}

func (*environmentTraceWorkspace) DeleteWorkspaceEnvironment(string, string) error { return nil }

func (*environmentTraceWorkspace) SetDefaultWorkspaceEnvironment(string, string) error {
	return nil
}

type environmentTraceSessions struct {
	fakeSessions
	state model.SessionSSHInfo
}

func (sessions *environmentTraceSessions) SessionSSHState(sessionID string) (model.SessionSSHInfo, error) {
	sessions.state.SessionID = sessionID
	return sessions.state, nil
}

func (sessions *environmentTraceSessions) SetSessionSSHState(_ string, state model.SessionSSHInfo) error {
	sessions.state = state
	return nil
}
