package core

import (
	"context"

	applicationtoolrecord "github.com/RedHuang-0622/seelex/application/execution/toolrecord"
	assemble "github.com/RedHuang-0622/seelex/backend/engine/assemble"
	goaldomain "github.com/RedHuang-0622/seelex/backend/multiagent/goal"
	seelsession "github.com/RedHuang-0622/seelex/backend/multiagent/subagent"
	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/middle/protocol/event"
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

func (frontend *Frontend) ListSessions() []model.SessionInfo {
	return frontend.Service.components.sessionQuery.ListSessions()
}

func (frontend *Frontend) WaitCatalogRefresh(ctx context.Context) error {
	return frontend.Service.components.sessionQuery.WaitCatalogRefresh(ctx)
}

func (frontend *Frontend) GetSessionTranscript(sessionID string, fromSeq, toSeq uint64) ([]model.TranscriptEvent, error) {
	return frontend.Service.components.sessionQuery.GetSessionTranscript(sessionID, fromSeq, toSeq)
}

func (frontend *Frontend) SessionInputIndex(sessionID string) (model.SessionInputIndex, error) {
	return frontend.Service.components.sessionQuery.SessionInputIndex(sessionID)
}

func (frontend *Frontend) SnapshotOf(sessionID string) (model.SessionSnapshot, error) {
	return frontend.Service.components.sessionQuery.SnapshotOf(sessionID)
}

func (frontend *Frontend) SessionMeta(sessionID string) (model.SessionMeta, error) {
	return frontend.Service.sessionMetaApplicationComponent().SessionMeta(sessionID)
}

func (frontend *Frontend) PerfStats() model.PerfStats {
	return frontend.Service.perfStatsApplicationComponent().PerfStats()
}

func (frontend *Frontend) SearchHistory(ctx context.Context, query string, limit int) (dto.SearchResult, error) {
	return frontend.Service.historySearchApplicationComponent().SearchHistory(ctx, query, limit)
}

func (frontend *Frontend) ProjectRootFor(sessionID string) string {
	return frontend.Service.projectBoardQueryComponent().ProjectRootFor(sessionID)
}

func (frontend *Frontend) WorkTableRowsFor(sessionID string) []model.WorkItem {
	return frontend.Service.projectBoardQueryComponent().WorkTableRowsFor(sessionID)
}

func (frontend *Frontend) TeammateSessionLiveFor(sessionID string) dto.TeammateSessionLiveView {
	return frontend.Service.executionApplicationComponent().SessionLive(sessionID)
}

func (frontend *Frontend) TeammateSessionLivePageFor(sessionID string, offset, limit int) dto.TeammateSessionLiveView {
	return frontend.Service.executionApplicationComponent().SessionPage(sessionID, offset, limit)
}

func (frontend *Frontend) WorkItemExecutionFor(ctx context.Context, mainSessionID, itemID string, page dto.ConversationViewRequest) (dto.WorkItemExecutionView, error) {
	return frontend.Service.executionApplicationComponent().WorkItemExecution(ctx, mainSessionID, itemID, page)
}

func (frontend *Frontend) TeammateInspectionFor(ctx context.Context, mainSessionID, role string) (dto.TeammateInspectionView, error) {
	return frontend.Service.executionApplicationComponent().TeammateInspection(ctx, mainSessionID, role)
}

func (frontend *Frontend) ToolResultContent(_ context.Context, resultRef string, offset, limit int) (model.ToolResultPage, error) {
	return frontend.Service.referenceApplicationComponent().ToolResultContent(resultRef, offset, limit)
}

func (frontend *Frontend) CompactContextNow(ctx context.Context) (ContextCompactionResult, error) {
	return frontend.Service.contextCompactionApplicationComponent().CompactContextNow(ctx)
}

func (frontend *Frontend) CompactContextHandler(ctx context.Context, argsJSON string) (string, error) {
	return frontend.Service.contextCompactionApplicationComponent().CompactContextHandler(ctx, argsJSON)
}

func (frontend *Frontend) ReadToolResultHandler(_ context.Context, argsJSON string) (string, error) {
	return frontend.Service.referenceApplicationComponent().ReadToolResultHandler(argsJSON)
}

func (frontend *Frontend) ReadPlanHandler(_ context.Context, argsJSON string) (string, error) {
	return frontend.Service.referenceApplicationComponent().ReadPlanHandler(argsJSON)
}

func (frontend *Frontend) ReadCompressedTurnHandler(_ context.Context, argsJSON string) (string, error) {
	return frontend.Service.referenceApplicationComponent().ReadCompressedTurnHandler(argsJSON)
}

func (frontend *Frontend) RequestFileReview(ctx context.Context, files []dto.ReviewFileRef) (dto.ReviewRequest, error) {
	return frontend.Service.reviewApplicationComponent().RequestFileReview(ctx, files)
}

func (frontend *Frontend) GoalGovernanceViewFor(sessionID string) *dto.GoalGovernanceView {
	return frontend.Service.goalApplicationComponent().GovernanceViewFor(sessionID)
}

func (frontend *Frontend) GoalBeginFor(ctx context.Context, sessionID string, request goaldomain.BeginRequest) (*goaldomain.GoalRecord, error) {
	return frontend.Service.goalApplicationComponent().BeginFor(ctx, sessionID, request)
}

func (frontend *Frontend) GoalUpdateFor(ctx context.Context, sessionID string, request goaldomain.UpdateRequest) (*goaldomain.GoalRecord, error) {
	return frontend.Service.goalApplicationComponent().UpdateFor(ctx, sessionID, request)
}

func (frontend *Frontend) GoalDoneFor(ctx context.Context, sessionID string, request goaldomain.FinishRequest, abort bool) (*goaldomain.GoalRecord, error) {
	return frontend.Service.goalApplicationComponent().DoneFor(ctx, sessionID, request, abort)
}

func (frontend *Frontend) GoalStatusFor(sessionID string) (goaldomain.StatusView, error) {
	return frontend.Service.goalApplicationComponent().StatusFor(sessionID)
}

func (frontend *Frontend) ScheduleTask(ctx context.Context, spec assemble.ScheduledTaskSpec) (*assemble.ScheduledTaskStatus, error) {
	return frontend.Service.schedulerApplicationComponent().ScheduleTask(ctx, spec)
}

func (frontend *Frontend) UpdateScheduledTask(ctx context.Context, id string, spec assemble.ScheduledTaskSpec) (*assemble.ScheduledTaskStatus, error) {
	return frontend.Service.schedulerApplicationComponent().UpdateScheduledTask(ctx, id, spec)
}

func (frontend *Frontend) CancelScheduledTask(id string) error {
	return frontend.Service.schedulerApplicationComponent().CancelScheduledTask(id)
}

func (frontend *Frontend) AssembleScheduledRun(ctx context.Context, sessionID string, task assemble.ScheduledTaskSpec) (context.Context, error) {
	return frontend.Service.scheduledRunApplicationComponent().AssembleScheduledRun(ctx, sessionID, task)
}

func (frontend *Frontend) StartScheduledSession(ctx context.Context, task assemble.ScheduledTaskSpec) (string, error) {
	return frontend.Service.scheduledSessionApplicationComponent().StartScheduledSession(ctx, task)
}

func (frontend *Frontend) RefreshRuntimeSnapshot() {
	frontend.Service.runtimeApplicationComponent().Refresh()
}

func (frontend *Frontend) AnyChatRunning() bool {
	return frontend.Service.lifecycleApplicationComponent().AnyChatRunning()
}

func (frontend *Frontend) WaitForIdle(ctx context.Context) error {
	return frontend.Service.lifecycleApplicationComponent().WaitForIdle(ctx)
}

func (frontend *Frontend) ArchiveSession(sessionID string) error {
	return frontend.Service.components.sessionArchive.Archive(sessionID)
}

func (frontend *Frontend) BeginNewSession() error {
	return frontend.Service.components.sessionDraftEntry.Begin()
}

func (frontend *Frontend) UnloadSession(sessionID string) error {
	return frontend.Service.components.sessionLifecycle.Unload(sessionID)
}

func (frontend *Frontend) ResumeSession(sessionID string) error {
	return frontend.Service.resumeSession(sessionID)
}

func (frontend *Frontend) ActivateSession(sessionID string) error {
	return frontend.Service.resumeSession(sessionID)
}

func (frontend *Frontend) SetSessionMeta(sessionID string, meta model.SessionMeta) error {
	return frontend.Service.sessionMetaApplicationComponent().SetSessionMeta(sessionID, meta)
}

func (frontend *Frontend) EditUserMessageAndFork(ctx context.Context, parentID string, request model.EditUserMessageForkRequest) (model.EditUserMessageForkResult, error) {
	return frontend.Service.editForkApplicationComponent().EditUserMessageAndFork(ctx, parentID, request)
}

func (frontend *Frontend) SessionSSHState(sessionID string) (model.SessionSSHInfo, error) {
	return frontend.Service.sessionSSHApplicationComponent().State(sessionID)
}

func (frontend *Frontend) SetSessionSSHAllowed(sessionID string, envs []string) (model.SessionSSHInfo, error) {
	return frontend.Service.sessionSSHApplicationComponent().SetAllowed(sessionID, envs)
}

func (frontend *Frontend) SetSessionSSHEnv(sessionID, projectID, env string) (model.SessionSSHInfo, error) {
	return frontend.Service.sessionSSHApplicationComponent().SetEnv(sessionID, projectID, env)
}

func (frontend *Frontend) ActivateSkill(name string) (model.SkillInfo, error) {
	return frontend.Service.policyApplicationComponent().ActivateSkill(name)
}

func (frontend *Frontend) UpdateWorkItemStatus(id, status string) error {
	return frontend.Service.executionApplicationComponent().UpdateWorkItemStatus(id, status)
}

func (frontend *Frontend) ListSubagentRecovery(sessionID string) ([]dto.SubagentRecoveryView, error) {
	return frontend.Service.subagentApplicationComponent().ListRecovery(sessionID)
}

func (frontend *Frontend) ResumeInterruptedSubagents(ctx context.Context, sessionID string) (dto.SubagentResumeReport, error) {
	return frontend.Service.subagentApplicationComponent().ResumeInterrupted(ctx, sessionID)
}

func (frontend *Frontend) ResumeSubagent(ctx context.Context, sessionID, nodeID string) (dto.SubagentResumeResult, error) {
	return frontend.Service.subagentApplicationComponent().Resume(ctx, sessionID, nodeID)
}

func (frontend *Frontend) ForkSubagents(ctx context.Context, sessionID string, specs []dto.SubagentForkSpec) (string, error) {
	return frontend.Service.subagentApplicationComponent().Fork(ctx, sessionID, specs)
}

func (frontend *Frontend) SubagentSessionDetail(nodeID string) (*model.SubagentDetail, error) {
	return frontend.Service.subagentApplicationComponent().Detail(nodeID)
}

func (frontend *Frontend) ClearSubagentTree() error {
	return frontend.Service.subagentApplicationComponent().ClearTree()
}

func (frontend *Frontend) SearchHistoryHandler(_ context.Context, argsJSON string) (string, error) {
	return frontend.Service.historySearchApplicationComponent().SearchHistoryHandler(argsJSON)
}

func (frontend *Frontend) BeginGracefulShutdown() {
	frontend.Service.lifecycleApplicationComponent().BeginGracefulShutdown()
}

func (frontend *Frontend) CancelAllChats() {
	frontend.Service.lifecycleApplicationComponent().CancelAllChats()
}

func (frontend *Frontend) CancelChat(requestID string) bool {
	return frontend.Service.lifecycleApplicationComponent().CancelChat(requestID)
}

func (frontend *Frontend) Shutdown() {
	frontend.Service.lifecycleApplicationComponent().Shutdown()
}

func (frontend *Frontend) SetFullAccess(on bool) bool {
	return frontend.Service.policyApplicationComponent().SetFullAccess(on)
}

func (frontend *Frontend) SetPermissionTier(tier string) (string, error) {
	return frontend.Service.policyApplicationComponent().SetPermissionTier(tier)
}

func (frontend *Frontend) SetLoopLimit(_ context.Context, limit int) error {
	return frontend.Service.policyApplicationComponent().SetLoopLimit(limit)
}

func (frontend *Frontend) SetReasoningEffort(_ context.Context, effort string) error {
	return frontend.Service.policyApplicationComponent().SetReasoningEffort(effort)
}

func (frontend *Frontend) SessionReleaseWindow() int {
	return frontend.Service.policyApplicationComponent().SessionReleaseWindow()
}

func (frontend *Frontend) SetSessionReleaseWindow(_ context.Context, window int) (int, error) {
	return frontend.Service.policyApplicationComponent().SetSessionReleaseWindow(window)
}

func (frontend *Frontend) Subscribe(buffer int) event.Subscription {
	return frontend.Service.components.view.Subscribe(buffer)
}

func (frontend *Frontend) GoalSkillActive() bool {
	return frontend.Service.components.tasks.GoalSkillActive()
}

func (frontend *Frontend) HandleSubagentToolEventFor(ownerSessionID string, toolEvent seelsession.SubagentToolEvent) {
	frontend.Service.components.subagent.HandleSubagentToolEventFor(ownerSessionID, toolEvent)
}

func (frontend *Frontend) AddNotice(notice string) {
	frontend.Service.addNotice(notice)
}

func (frontend *Frontend) PublishRuntimeProjections() {
	frontend.Service.publishRuntimeProjections()
}

func (frontend *Frontend) CloseRoleToolRecordUnit(mainSessionID, roleSessionID string) {
	frontend.Service.closeUnitToolRecord(mainSessionID, applicationtoolrecord.RoleUnitID(roleSessionID, ""))
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
	return frontend.Service.components.roleSessions.CreateRoleSession(mainSessionID, roleName, roleSessionID, joinSeq)
}

func (frontend *Frontend) AppendRoleDraft(mainSessionID, roleName, roleSessionID string, rows []dto.RoleDraftRow) error {
	return frontend.Service.components.roleSessions.AppendRoleDraft(mainSessionID, roleName, roleSessionID, rows)
}

func (frontend *Frontend) ReadRoleDraft(mainSessionID, roleName, roleSessionID string) ([]dto.RoleDraftRow, error) {
	return frontend.Service.components.roleSessions.ReadRoleDraft(mainSessionID, roleName, roleSessionID)
}

func (frontend *Frontend) SyncRoleDraft(mainSessionID, roleName, roleSessionID string, order []string) (dto.RoleDraftSyncResult, error) {
	return frontend.Service.components.roleSessions.SyncRoleDraft(mainSessionID, roleName, roleSessionID, order)
}

func (frontend *Frontend) AppendRoleSessionRows(mainSessionID, roleName, roleSessionID string, rows []dto.RoleRow) error {
	return frontend.Service.components.roleSessions.AppendRoleSessionRows(mainSessionID, roleName, roleSessionID, rows)
}

func (frontend *Frontend) ReadRoleSessionRows(mainSessionID, roleName, roleSessionID string) ([]dto.RoleRow, error) {
	return frontend.Service.components.roleSessions.ReadRoleSessionRows(mainSessionID, roleName, roleSessionID)
}

func (frontend *Frontend) RoleSnapshot(mainSessionID, roleName, roleSessionID string) (dto.RoleSnapshot, error) {
	return frontend.Service.components.roleSessions.RoleSnapshot(mainSessionID, roleName, roleSessionID)
}

func (frontend *Frontend) AssembleRoleWire(mainSessionID, roleName, roleSessionID string, budget, k int) (dto.RoleWireSnapshot, error) {
	return frontend.Service.components.roleSessions.AssembleRoleWire(mainSessionID, roleName, roleSessionID, budget, k)
}

func (frontend *Frontend) SetLifecycleOrder(sessionID, policy string, roles []string) error {
	return frontend.Service.components.roleSessions.SetLifecycleOrder(sessionID, policy, roles)
}

func (frontend *Frontend) SetRoleLifecycle(mainSessionID, roleName, roleSessionID string, joinSeq uint64, ref *dto.CompactFrameRef) error {
	return frontend.Service.components.roleSessions.SetRoleLifecycle(mainSessionID, roleName, roleSessionID, joinSeq, ref)
}

func (frontend *Frontend) ListRoleSessions(mainSessionID string) ([]string, error) {
	return frontend.Service.components.roleSessions.ListRoleSessions(mainSessionID)
}

func (frontend *Frontend) ScheduleRegister(sessionID string, payload dto.ScheduleEventPayload) error {
	return frontend.Service.components.roleSessions.ScheduleRegister(sessionID, payload)
}

func (frontend *Frontend) ScheduleCancel(sessionID string, payload dto.ScheduleEventPayload) error {
	return frontend.Service.components.roleSessions.ScheduleCancel(sessionID, payload)
}

func (frontend *Frontend) ScheduleFire(sessionID string, payload dto.ScheduleEventPayload) error {
	return frontend.Service.components.roleSessions.ScheduleFire(sessionID, payload)
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
