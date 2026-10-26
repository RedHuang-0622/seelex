package core

import (
	"context"

	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/middle/protocol/model"
)

// The migrated frontend entries live in application/compat in production.
// Core tests use this local forwarding surface so package-internal fixtures can
// still exercise the assembled host without importing compat (which would form
// a test import cycle).

func (frontend *Frontend) AddAccount(ctx context.Context, draft dto.AccountDraft) (dto.AccountAddResult, error) {
	return frontend.Service.adminApplicationComponent().AddAccount(ctx, draft)
}

func (frontend *Frontend) AccountDetails(name string) (dto.AccountDetails, error) {
	return frontend.Service.adminApplicationComponent().AccountDetails(name)
}

func (frontend *Frontend) UpdateAccount(update dto.AccountUpdate) (dto.AccountDetails, error) {
	return frontend.Service.adminApplicationComponent().UpdateAccount(update)
}

func (frontend *Frontend) DeleteAccount(name string) (dto.AccountDetails, error) {
	return frontend.Service.adminApplicationComponent().DeleteAccount(name)
}

func (frontend *Frontend) AccountBalance(ctx context.Context, name string) (dto.AccountBalance, error) {
	return frontend.Service.adminApplicationComponent().AccountBalance(ctx, name)
}

func (frontend *Frontend) SetAccountWindow(_ context.Context, name string, window int) (int, error) {
	return frontend.Service.adminApplicationComponent().SetAccountWindow(context.Background(), name, window)
}

func (frontend *Frontend) Suggestions(input string) []Suggestion {
	return frontend.Service.completionApplicationComponent().Suggestions(input)
}

func (frontend *Frontend) SelectAccount(ctx context.Context, name string) error {
	return frontend.Service.adminApplicationComponent().SelectAccount(context.Background(), name)
}

func (frontend *Frontend) SwitchEffort(_ context.Context, level string) error {
	return frontend.Service.components.policyApplication.SwitchEffort(level)
}

func (frontend *Frontend) SwitchPlugin(ctx context.Context, name string) error {
	return frontend.Service.components.policyApplication.SwitchPlugin(ctx, name)
}

func (frontend *Frontend) LoadMoreHistory(limit int) error {
	return frontend.Service.components.conversationHistoryQuery.LoadMoreHistory(limit)
}

func (frontend *Frontend) LoadLatestHistory() error {
	return frontend.Service.components.conversationHistoryQuery.LoadLatestHistory()
}

func (frontend *Frontend) ReorderQueuedInput(sessionID string, from, to int) error {
	return frontend.Service.queueComponent().ReorderQueuedInput(sessionID, from, to)
}

func (frontend *Frontend) RecallQueuedInput(sessionID string, index int) (string, error) {
	return frontend.Service.queueComponent().RecallQueuedInput(sessionID, index)
}

func (frontend *Frontend) RoleDefaultVector(sessionID, role string) ([]string, bool, error) {
	return frontend.Service.roleApplicationComponent().RoleDefaultVector(sessionID, role)
}

func (frontend *Frontend) SetRoleDefaultVector(sessionID, role string, vector []string) error {
	return frontend.Service.roleApplicationComponent().SetRoleDefaultVector(sessionID, role, vector)
}

func (frontend *Frontend) RoleAccount(sessionID, role string) (string, bool, error) {
	return frontend.Service.roleApplicationComponent().RoleAccount(sessionID, role)
}

func (frontend *Frontend) SetRoleAccount(sessionID, role, account string) error {
	return frontend.Service.roleApplicationComponent().SetRoleAccount(sessionID, role, account)
}

func (frontend *Frontend) RoleInheritMainAgent(sessionID, role string) (bool, error) {
	return frontend.Service.roleApplicationComponent().RoleInheritMainAgent(sessionID, role)
}

func (frontend *Frontend) SetRoleInheritMainAgent(sessionID, role string, inherit bool) error {
	return frontend.Service.roleApplicationComponent().SetRoleInheritMainAgent(sessionID, role, inherit)
}

func (frontend *Frontend) RoleSSHUserSpaces(sessionID, role string) ([]string, error) {
	return frontend.Service.roleApplicationComponent().RoleSSHUserSpaces(sessionID, role)
}

func (frontend *Frontend) RoleDefaultSSHUserSpaces(sessionID, role string) ([]string, error) {
	return frontend.Service.roleApplicationComponent().RoleDefaultSSHUserSpaces(sessionID, role)
}

func (frontend *Frontend) SetRoleSSHUserSpaces(sessionID, role string, paths []string) error {
	return frontend.Service.roleApplicationComponent().SetRoleSSHUserSpaces(sessionID, role, paths)
}

func (frontend *Frontend) SetRoleSessionSSHUserSpaces(sessionID, role string, paths []string) error {
	return frontend.Service.roleApplicationComponent().SetRoleSessionSSHUserSpaces(sessionID, role, paths)
}

func (frontend *Frontend) RolePermissionVector(sessionID, role string) ([]string, error) {
	return frontend.Service.roleApplicationComponent().RolePermissionVector(sessionID, role)
}

func (frontend *Frontend) SetRolePermissionVector(sessionID, role string, vector []string) error {
	return frontend.Service.roleApplicationComponent().SetRolePermissionVector(sessionID, role, vector)
}

func (frontend *Frontend) CreateRoleSession(mainSessionID, roleName, roleSessionID string, joinSeq uint64) (dto.RoleSessionInfo, error) {
	return frontend.Service.roleSessions().CreateRoleSession(mainSessionID, roleName, roleSessionID, joinSeq)
}

func (frontend *Frontend) AppendRoleDraft(mainSessionID, roleName, roleSessionID string, rows []dto.RoleDraftRow) error {
	return frontend.Service.roleSessions().AppendRoleDraft(mainSessionID, roleName, roleSessionID, rows)
}

func (frontend *Frontend) ReadRoleDraft(mainSessionID, roleName, roleSessionID string) ([]dto.RoleDraftRow, error) {
	return frontend.Service.roleSessions().ReadRoleDraft(mainSessionID, roleName, roleSessionID)
}

func (frontend *Frontend) SyncRoleDraft(mainSessionID, roleName, roleSessionID string, order []string) (dto.RoleDraftSyncResult, error) {
	return frontend.Service.roleSessions().SyncRoleDraft(mainSessionID, roleName, roleSessionID, order)
}

func (frontend *Frontend) AppendRoleSessionRows(mainSessionID, roleName, roleSessionID string, rows []dto.RoleRow) error {
	return frontend.Service.roleSessions().AppendRoleSessionRows(mainSessionID, roleName, roleSessionID, rows)
}

func (frontend *Frontend) ReadRoleSessionRows(mainSessionID, roleName, roleSessionID string) ([]dto.RoleRow, error) {
	return frontend.Service.roleSessions().ReadRoleSessionRows(mainSessionID, roleName, roleSessionID)
}

func (frontend *Frontend) RoleSnapshot(mainSessionID, roleName, roleSessionID string) (dto.RoleSnapshot, error) {
	return frontend.Service.roleSessions().RoleSnapshot(mainSessionID, roleName, roleSessionID)
}

func (frontend *Frontend) AssembleRoleWire(mainSessionID, roleName, roleSessionID string, budget, k int) (dto.RoleWireSnapshot, error) {
	return frontend.Service.roleSessions().AssembleRoleWire(mainSessionID, roleName, roleSessionID, budget, k)
}

func (frontend *Frontend) SetLifecycleOrder(sessionID, policy string, roles []string) error {
	return frontend.Service.roleSessions().SetLifecycleOrder(sessionID, policy, roles)
}

func (frontend *Frontend) SetRoleLifecycle(mainSessionID, roleName, roleSessionID string, joinSeq uint64, ref *dto.CompactFrameRef) error {
	return frontend.Service.roleSessions().SetRoleLifecycle(mainSessionID, roleName, roleSessionID, joinSeq, ref)
}

func (frontend *Frontend) ListRoleSessions(mainSessionID string) ([]string, error) {
	return frontend.Service.roleSessions().ListRoleSessions(mainSessionID)
}

func (frontend *Frontend) ScheduleRegister(sessionID string, payload dto.ScheduleEventPayload) error {
	return frontend.Service.roleSessions().ScheduleRegister(sessionID, payload)
}

func (frontend *Frontend) ScheduleCancel(sessionID string, payload dto.ScheduleEventPayload) error {
	return frontend.Service.roleSessions().ScheduleCancel(sessionID, payload)
}

func (frontend *Frontend) ScheduleFire(sessionID string, payload dto.ScheduleEventPayload) error {
	return frontend.Service.roleSessions().ScheduleFire(sessionID, payload)
}

func (frontend *Frontend) WorkspaceEnvironments(projectID string) ([]model.WorkspaceEnvironmentInfo, string, error) {
	return frontend.Service.workspaceApplicationComponent().WorkspaceEnvironments(projectID)
}

func (frontend *Frontend) SetWorkspaceEnvironment(projectID, name string, change model.WorkspaceEnvironmentChange) (model.WorkspaceEnvironmentInfo, error) {
	return frontend.Service.workspaceApplicationComponent().SetWorkspaceEnvironment(projectID, name, change)
}

func (frontend *Frontend) DeleteWorkspaceEnvironment(projectID, name string) error {
	return frontend.Service.workspaceApplicationComponent().DeleteWorkspaceEnvironment(projectID, name)
}

func (frontend *Frontend) SetDefaultWorkspaceEnvironment(projectID, name string) error {
	return frontend.Service.workspaceApplicationComponent().SetDefaultWorkspaceEnvironment(projectID, name)
}

func (frontend *Frontend) WorkspaceGitRepos() (dto.GitReposResult, error) {
	return frontend.Service.workspaceApplicationComponent().WorkspaceGitRepos()
}

func (frontend *Frontend) DeleteSession(sessionID string) error {
	return frontend.Service.components.sessionDelete.Delete(sessionID)
}

func (frontend *Frontend) CreateWorkspace(name, rootPath, gitRemote string) error {
	return frontend.Service.components.workspace.CreateWorkspace(name, rootPath, gitRemote)
}

func (frontend *Frontend) CreateWorkspaces(requests []dto.WorkspaceCreateRequest) (dto.WorkspaceCreateBatchResult, error) {
	return frontend.Service.workspaceApplicationComponent().CreateWorkspaces(requests)
}

func (frontend *Frontend) BindWorkspace(workspaceID string) error {
	return frontend.Service.components.workspace.BindWorkspace(workspaceID)
}

func (frontend *Frontend) UnbindWorkspace() {
	frontend.Service.components.workspace.UnbindWorkspace()
}

func (frontend *Frontend) WorkspaceTree(relPath string, depth int) (dto.TreeListing, error) {
	return frontend.Service.components.workspace.Tree(relPath, depth)
}

func (frontend *Frontend) WorkspaceFileCount() (dto.TreeCount, error) {
	return frontend.Service.components.workspace.FileCount()
}

func (frontend *Frontend) WorkspaceGitLog(repo string, limit int) (dto.GitLogResult, error) {
	return frontend.Service.workspaceApplicationComponent().GitLog(repo, limit)
}

func (frontend *Frontend) WorkspaceChanges(repo string, limit int) (dto.WorkspaceChangesResult, error) {
	return frontend.Service.workspaceApplicationComponent().Changes(repo, limit)
}

func (frontend *Frontend) WorkspaceGitCommitDetail(repo, hash string, limit int) (dto.GitCommitDetail, error) {
	return frontend.Service.workspaceApplicationComponent().CommitDetail(repo, hash, limit)
}

func (frontend *Frontend) WorkspaceFileContent(relPath string, limit int64) (dto.FileContent, error) {
	return frontend.Service.components.workspace.FileContent(relPath, limit)
}

func (frontend *Frontend) WorkspaceGitCommitFileContent(repo, hash, relPath string, limit int64) (dto.FileContent, error) {
	return frontend.Service.workspaceApplicationComponent().CommitFileContent(repo, hash, relPath, limit)
}

func (frontend *Frontend) WorkspaceWriteFile(relPath, content string) (dto.FileWriteResult, error) {
	return frontend.Service.components.workspace.WriteFile(relPath, content)
}
