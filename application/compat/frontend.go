// Package compat hosts temporary frontend compatibility adapters that forward
// to already-extracted application components without owning state.
package compat

import (
	"context"

	applicationadmin "github.com/RedHuang-0622/seelex/application/admin"
	applicationconversation "github.com/RedHuang-0622/seelex/application/conversation"
	"github.com/RedHuang-0622/seelex/application/core"
	applicationhistory "github.com/RedHuang-0622/seelex/application/history"
	applicationinput "github.com/RedHuang-0622/seelex/application/input"
	applicationpolicy "github.com/RedHuang-0622/seelex/application/policy"
	applicationsession "github.com/RedHuang-0622/seelex/application/session"
	applicationworkspace "github.com/RedHuang-0622/seelex/application/workspace"
	"github.com/RedHuang-0622/seelex/backend/multiagent/team"
	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/middle/protocol/model"
	switchworkspace "github.com/RedHuang-0622/seelex/middle/switch/workspace"
)

// Frontend keeps the existing frontend method surface while the consumers
// migrate to narrow capability interfaces. State and lifecycle stay in core.
type Frontend struct {
	*core.Frontend
	ports core.FrontendPorts
}

// New builds the compatibility adapter from an assembled core service.
func New(service *core.Service) *Frontend {
	frontend := &Frontend{Frontend: &core.Frontend{Service: service}}
	if service != nil {
		frontend.ports = core.NewFrontendPorts(service)
	}
	return frontend
}

func (frontend *Frontend) admin() *applicationadmin.AccountApplication {
	if frontend == nil {
		return nil
	}
	return frontend.ports.Admin
}

func (frontend *Frontend) completion() *applicationinput.Application {
	if frontend == nil {
		return nil
	}
	return frontend.ports.Completion
}

func (frontend *Frontend) conversationQueue() *applicationconversation.QueueApplication {
	if frontend == nil {
		return nil
	}
	return frontend.ports.ConversationQueue
}

func (frontend *Frontend) history() *applicationhistory.Application {
	if frontend == nil {
		return nil
	}
	return frontend.ports.History
}

func (frontend *Frontend) policy() *applicationpolicy.SwitchApplication {
	if frontend == nil {
		return nil
	}
	return frontend.ports.Policy
}

func (frontend *Frontend) role() *applicationpolicy.RoleApplication {
	if frontend == nil {
		return nil
	}
	return frontend.ports.Role
}

func (frontend *Frontend) roleSessions() team.RoleSessions {
	if frontend == nil {
		return team.RoleSessions{}
	}
	return frontend.ports.RoleSessions
}

func (frontend *Frontend) workspace() *applicationworkspace.Application {
	if frontend == nil {
		return nil
	}
	return frontend.ports.Workspace
}

func (frontend *Frontend) workspaceUseCase() *switchworkspace.UseCase {
	if frontend == nil {
		return nil
	}
	return frontend.ports.WorkspaceUseCase
}

func (frontend *Frontend) sessionDelete() *applicationsession.DeleteApplication {
	if frontend == nil {
		return nil
	}
	return frontend.ports.SessionDelete
}

// AddAccount forwards to the account application.
func (frontend *Frontend) AddAccount(ctx context.Context, draft dto.AccountDraft) (dto.AccountAddResult, error) {
	return frontend.admin().AddAccount(ctx, draft)
}

// AccountDetails forwards to the account application.
func (frontend *Frontend) AccountDetails(name string) (dto.AccountDetails, error) {
	return frontend.admin().AccountDetails(name)
}

// UpdateAccount forwards to the account application.
func (frontend *Frontend) UpdateAccount(update dto.AccountUpdate) (dto.AccountDetails, error) {
	return frontend.admin().UpdateAccount(update)
}

// DeleteAccount forwards to the account application.
func (frontend *Frontend) DeleteAccount(name string) (dto.AccountDetails, error) {
	return frontend.admin().DeleteAccount(name)
}

// AccountBalance forwards to the account application.
func (frontend *Frontend) AccountBalance(ctx context.Context, name string) (dto.AccountBalance, error) {
	return frontend.admin().AccountBalance(ctx, name)
}

// SetAccountWindow forwards to the account application.
func (frontend *Frontend) SetAccountWindow(_ context.Context, name string, window int) (int, error) {
	return frontend.admin().SetAccountWindow(context.Background(), name, window)
}

// Suggestions returns command, skill, and plugin suggestions.
func (frontend *Frontend) Suggestions(input string) []applicationinput.Suggestion {
	return frontend.completion().Suggestions(input)
}

// SelectAccount switches the active account.
func (frontend *Frontend) SelectAccount(ctx context.Context, name string) error {
	return frontend.admin().SelectAccount(context.Background(), name)
}

// SwitchEffort changes the current effort level.
func (frontend *Frontend) SwitchEffort(_ context.Context, level string) error {
	return frontend.policy().SwitchEffort(level)
}

// SwitchPlugin activates or disables a plugin.
func (frontend *Frontend) SwitchPlugin(ctx context.Context, name string) error {
	return frontend.policy().SwitchPlugin(ctx, name)
}

// LoadMoreHistory prepends an older history window.
func (frontend *Frontend) LoadMoreHistory(limit int) error {
	return frontend.history().LoadMoreHistory(limit)
}

// LoadLatestHistory returns the visible window to the tail.
func (frontend *Frontend) LoadLatestHistory() error {
	return frontend.history().LoadLatestHistory()
}

// ReorderQueuedInput moves one queued input.
func (frontend *Frontend) ReorderQueuedInput(sessionID string, from, to int) error {
	return frontend.conversationQueue().ReorderQueuedInput(sessionID, from, to)
}

// RecallQueuedInput removes one queued input and returns its text.
func (frontend *Frontend) RecallQueuedInput(sessionID string, index int) (string, error) {
	return frontend.conversationQueue().RecallQueuedInput(sessionID, index)
}

// RoleDefaultVector reads a role's explicit default permission vector.
func (frontend *Frontend) RoleDefaultVector(sessionID, role string) ([]string, bool, error) {
	return frontend.role().RoleDefaultVector(sessionID, role)
}

// SetRoleDefaultVector writes a role's default permission vector.
func (frontend *Frontend) SetRoleDefaultVector(sessionID, role string, vector []string) error {
	return frontend.role().SetRoleDefaultVector(sessionID, role, vector)
}

// RoleAccount reads a role's explicit default account.
func (frontend *Frontend) RoleAccount(sessionID, role string) (string, bool, error) {
	return frontend.role().RoleAccount(sessionID, role)
}

// SetRoleAccount applies and persists a role's default account.
func (frontend *Frontend) SetRoleAccount(sessionID, role, account string) error {
	return frontend.role().SetRoleAccount(sessionID, role, account)
}

// RoleInheritMainAgent reports whether a role inherits main-agent settings.
func (frontend *Frontend) RoleInheritMainAgent(sessionID, role string) (bool, error) {
	return frontend.role().RoleInheritMainAgent(sessionID, role)
}

// SetRoleInheritMainAgent updates role inheritance.
func (frontend *Frontend) SetRoleInheritMainAgent(sessionID, role string, inherit bool) error {
	return frontend.role().SetRoleInheritMainAgent(sessionID, role, inherit)
}

// RoleSSHUserSpaces reads a role's effective SSH path scope.
func (frontend *Frontend) RoleSSHUserSpaces(sessionID, role string) ([]string, error) {
	return frontend.role().RoleSSHUserSpaces(sessionID, role)
}

// RoleDefaultSSHUserSpaces reads a role's default SSH path scope.
func (frontend *Frontend) RoleDefaultSSHUserSpaces(sessionID, role string) ([]string, error) {
	return frontend.role().RoleDefaultSSHUserSpaces(sessionID, role)
}

// SetRoleSSHUserSpaces writes a role's default SSH path scope.
func (frontend *Frontend) SetRoleSSHUserSpaces(sessionID, role string, paths []string) error {
	return frontend.role().SetRoleSSHUserSpaces(sessionID, role, paths)
}

// SetRoleSessionSSHUserSpaces writes a session-local SSH path scope.
func (frontend *Frontend) SetRoleSessionSSHUserSpaces(sessionID, role string, paths []string) error {
	return frontend.role().SetRoleSessionSSHUserSpaces(sessionID, role, paths)
}

// RolePermissionVector reads one effective role permission vector.
func (frontend *Frontend) RolePermissionVector(sessionID, role string) ([]string, error) {
	return frontend.role().RolePermissionVector(sessionID, role)
}

// SetRolePermissionVector writes a session-local role permission vector.
func (frontend *Frontend) SetRolePermissionVector(sessionID, role string, vector []string) error {
	return frontend.role().SetRolePermissionVector(sessionID, role, vector)
}

func (frontend *Frontend) CreateRoleSession(mainSessionID, roleName, roleSessionID string, joinSeq uint64) (dto.RoleSessionInfo, error) {
	return frontend.roleSessions().CreateRoleSession(mainSessionID, roleName, roleSessionID, joinSeq)
}

func (frontend *Frontend) AppendRoleDraft(mainSessionID, roleName, roleSessionID string, rows []dto.RoleDraftRow) error {
	return frontend.roleSessions().AppendRoleDraft(mainSessionID, roleName, roleSessionID, rows)
}

func (frontend *Frontend) ReadRoleDraft(mainSessionID, roleName, roleSessionID string) ([]dto.RoleDraftRow, error) {
	return frontend.roleSessions().ReadRoleDraft(mainSessionID, roleName, roleSessionID)
}

func (frontend *Frontend) SyncRoleDraft(mainSessionID, roleName, roleSessionID string, order []string) (dto.RoleDraftSyncResult, error) {
	return frontend.roleSessions().SyncRoleDraft(mainSessionID, roleName, roleSessionID, order)
}

func (frontend *Frontend) AppendRoleSessionRows(mainSessionID, roleName, roleSessionID string, rows []dto.RoleRow) error {
	return frontend.roleSessions().AppendRoleSessionRows(mainSessionID, roleName, roleSessionID, rows)
}

func (frontend *Frontend) ReadRoleSessionRows(mainSessionID, roleName, roleSessionID string) ([]dto.RoleRow, error) {
	return frontend.roleSessions().ReadRoleSessionRows(mainSessionID, roleName, roleSessionID)
}

func (frontend *Frontend) RoleSnapshot(mainSessionID, roleName, roleSessionID string) (dto.RoleSnapshot, error) {
	return frontend.roleSessions().RoleSnapshot(mainSessionID, roleName, roleSessionID)
}

func (frontend *Frontend) AssembleRoleWire(mainSessionID, roleName, roleSessionID string, budget, k int) (dto.RoleWireSnapshot, error) {
	return frontend.roleSessions().AssembleRoleWire(mainSessionID, roleName, roleSessionID, budget, k)
}

func (frontend *Frontend) SetLifecycleOrder(sessionID, policy string, roles []string) error {
	return frontend.roleSessions().SetLifecycleOrder(sessionID, policy, roles)
}

func (frontend *Frontend) SetRoleLifecycle(mainSessionID, roleName, roleSessionID string, joinSeq uint64, ref *dto.CompactFrameRef) error {
	return frontend.roleSessions().SetRoleLifecycle(mainSessionID, roleName, roleSessionID, joinSeq, ref)
}

func (frontend *Frontend) ListRoleSessions(mainSessionID string) ([]string, error) {
	return frontend.roleSessions().ListRoleSessions(mainSessionID)
}

func (frontend *Frontend) ScheduleRegister(sessionID string, payload dto.ScheduleEventPayload) error {
	return frontend.roleSessions().ScheduleRegister(sessionID, payload)
}

func (frontend *Frontend) ScheduleCancel(sessionID string, payload dto.ScheduleEventPayload) error {
	return frontend.roleSessions().ScheduleCancel(sessionID, payload)
}

func (frontend *Frontend) ScheduleFire(sessionID string, payload dto.ScheduleEventPayload) error {
	return frontend.roleSessions().ScheduleFire(sessionID, payload)
}

// WorkspaceEnvironments reads a project's environment table and default name.
func (frontend *Frontend) WorkspaceEnvironments(projectID string) ([]model.WorkspaceEnvironmentInfo, string, error) {
	return frontend.workspace().WorkspaceEnvironments(projectID)
}

// SetWorkspaceEnvironment writes one project environment.
func (frontend *Frontend) SetWorkspaceEnvironment(projectID, name string, change model.WorkspaceEnvironmentChange) (model.WorkspaceEnvironmentInfo, error) {
	return frontend.workspace().SetWorkspaceEnvironment(projectID, name, change)
}

// DeleteWorkspaceEnvironment removes one project environment.
func (frontend *Frontend) DeleteWorkspaceEnvironment(projectID, name string) error {
	return frontend.workspace().DeleteWorkspaceEnvironment(projectID, name)
}

// SetDefaultWorkspaceEnvironment selects or clears a project's default.
func (frontend *Frontend) SetDefaultWorkspaceEnvironment(projectID, name string) error {
	return frontend.workspace().SetDefaultWorkspaceEnvironment(projectID, name)
}

// WorkspaceGitRepos discovers git repositories under the current workspace.
func (frontend *Frontend) WorkspaceGitRepos() (dto.GitReposResult, error) {
	return frontend.workspace().WorkspaceGitRepos()
}

// DeleteSession deletes a session record and runs release.
func (frontend *Frontend) DeleteSession(sessionID string) error {
	return frontend.sessionDelete().Delete(sessionID)
}

// CreateWorkspace creates a workspace and binds it to the current view.
func (frontend *Frontend) CreateWorkspace(name, rootPath, gitRemote string) error {
	return frontend.workspaceUseCase().CreateWorkspace(name, rootPath, gitRemote)
}

// CreateWorkspaces registers projects without binding the current session.
func (frontend *Frontend) CreateWorkspaces(requests []dto.WorkspaceCreateRequest) (dto.WorkspaceCreateBatchResult, error) {
	return frontend.workspace().CreateWorkspaces(requests)
}

// BindWorkspace binds a workspace to the current view session or draft.
func (frontend *Frontend) BindWorkspace(workspaceID string) error {
	return frontend.workspaceUseCase().BindWorkspace(workspaceID)
}

// UnbindWorkspace removes the current view session's workspace binding.
func (frontend *Frontend) UnbindWorkspace() {
	frontend.workspaceUseCase().UnbindWorkspace()
}

// WorkspaceTree lists children of one directory.
func (frontend *Frontend) WorkspaceTree(relPath string, depth int) (dto.TreeListing, error) {
	return frontend.workspaceUseCase().Tree(relPath, depth)
}

// WorkspaceFileCount counts visible files and directories.
func (frontend *Frontend) WorkspaceFileCount() (dto.TreeCount, error) {
	return frontend.workspaceUseCase().FileCount()
}

// WorkspaceGitLog returns recent commits for one repository.
func (frontend *Frontend) WorkspaceGitLog(repo string, limit int) (dto.GitLogResult, error) {
	return frontend.workspace().GitLog(repo, limit)
}

// WorkspaceChanges returns uncommitted changes for one repository.
func (frontend *Frontend) WorkspaceChanges(repo string, limit int) (dto.WorkspaceChangesResult, error) {
	return frontend.workspace().Changes(repo, limit)
}

// WorkspaceGitCommitDetail returns the changed-file summary of one commit.
func (frontend *Frontend) WorkspaceGitCommitDetail(repo, hash string, limit int) (dto.GitCommitDetail, error) {
	return frontend.workspace().CommitDetail(repo, hash, limit)
}

// WorkspaceFileContent reads controlled bytes from the current workspace.
func (frontend *Frontend) WorkspaceFileContent(relPath string, limit int64) (dto.FileContent, error) {
	return frontend.workspaceUseCase().FileContent(relPath, limit)
}

// WorkspaceGitCommitFileContent reads one file from a commit.
func (frontend *Frontend) WorkspaceGitCommitFileContent(repo, hash, relPath string, limit int64) (dto.FileContent, error) {
	return frontend.workspace().CommitFileContent(repo, hash, relPath, limit)
}

// WorkspaceWriteFile overwrites one workspace file.
func (frontend *Frontend) WorkspaceWriteFile(relPath, content string) (dto.FileWriteResult, error) {
	return frontend.workspaceUseCase().WriteFile(relPath, content)
}
