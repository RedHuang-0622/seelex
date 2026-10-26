package core

import (
	"context"

	applicationtoolrecord "github.com/RedHuang-0622/seelex/application/execution/toolrecord"
	applicationinput "github.com/RedHuang-0622/seelex/application/input"
	assemble "github.com/RedHuang-0622/seelex/backend/engine/assemble"
	seelsession "github.com/RedHuang-0622/seelex/backend/multiagent/subagent"
	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/contract/protocol"
	goaldomain "github.com/RedHuang-0622/seelex/kernel/goal"
	"github.com/RedHuang-0622/seelex/middle/protocol/event"
)

// The migrated frontend entries live in application/compat in production.
// Core tests use this local forwarding surface so package-internal fixtures can
// still exercise the assembled host without importing compat (which would form
// a test import cycle).

func (frontend *Frontend) testComponents() serviceComponents {
	if frontend == nil || frontend.Service == nil {
		return serviceComponents{}
	}
	return frontend.Service.components
}

func (frontend *Frontend) AddAccount(ctx context.Context, draft dto.AccountDraft) (dto.AccountAddResult, error) {
	return frontend.testComponents().adminApplication.AddAccount(ctx, draft)
}

func (frontend *Frontend) AccountDetails(name string) (dto.AccountDetails, error) {
	return frontend.testComponents().adminApplication.AccountDetails(name)
}

func (frontend *Frontend) UpdateAccount(update dto.AccountUpdate) (dto.AccountDetails, error) {
	return frontend.testComponents().adminApplication.UpdateAccount(update)
}

func (frontend *Frontend) DeleteAccount(name string) (dto.AccountDetails, error) {
	return frontend.testComponents().adminApplication.DeleteAccount(name)
}

func (frontend *Frontend) AccountBalance(ctx context.Context, name string) (dto.AccountBalance, error) {
	return frontend.testComponents().adminApplication.AccountBalance(ctx, name)
}

func (frontend *Frontend) SetAccountWindow(_ context.Context, name string, window int) (int, error) {
	return frontend.testComponents().adminApplication.SetAccountWindow(context.Background(), name, window)
}

func (frontend *Frontend) Suggestions(input string) []applicationinput.Suggestion {
	return frontend.testComponents().completionApplication.Suggestions(input)
}

func (frontend *Frontend) SelectAccount(ctx context.Context, name string) error {
	return frontend.testComponents().adminApplication.SelectAccount(context.Background(), name)
}

func (frontend *Frontend) SwitchEffort(_ context.Context, level string) error {
	return frontend.testComponents().policyApplication.SwitchEffort(level)
}

func (frontend *Frontend) SwitchPlugin(ctx context.Context, name string) error {
	return frontend.testComponents().policyApplication.SwitchPlugin(ctx, name)
}

func (frontend *Frontend) LoadMoreHistory(limit int) error {
	return frontend.testComponents().conversationHistoryQuery.LoadMoreHistory(limit)
}

func (frontend *Frontend) LoadLatestHistory() error {
	return frontend.testComponents().conversationHistoryQuery.LoadLatestHistory()
}

func (frontend *Frontend) ReorderQueuedInput(sessionID string, from, to int) error {
	return frontend.testComponents().conversationQueue.ReorderQueuedInput(sessionID, from, to)
}

func (frontend *Frontend) RecallQueuedInput(sessionID string, index int) (string, error) {
	return frontend.testComponents().conversationQueue.RecallQueuedInput(sessionID, index)
}

func (frontend *Frontend) ListSessions() []model.SessionInfo {
	return frontend.testComponents().sessionQuery.ListSessions()
}

func (frontend *Frontend) WaitCatalogRefresh(ctx context.Context) error {
	return frontend.testComponents().sessionQuery.WaitCatalogRefresh(ctx)
}

func (frontend *Frontend) GetSessionTranscript(sessionID string, fromSeq, toSeq uint64) ([]model.TranscriptEvent, error) {
	return frontend.testComponents().sessionQuery.GetSessionTranscript(sessionID, fromSeq, toSeq)
}

func (frontend *Frontend) SessionInputIndex(sessionID string) (model.SessionInputIndex, error) {
	return frontend.testComponents().sessionQuery.SessionInputIndex(sessionID)
}

func (frontend *Frontend) SnapshotOf(sessionID string) (model.SessionSnapshot, error) {
	return frontend.testComponents().sessionQuery.SnapshotOf(sessionID)
}

func (frontend *Frontend) SessionMeta(sessionID string) (model.SessionMeta, error) {
	return frontend.testComponents().sessionMetaApplication.SessionMeta(sessionID)
}

func (frontend *Frontend) PerfStats() model.PerfStats {
	return frontend.testComponents().perfStatsApplication.PerfStats()
}

func (frontend *Frontend) SearchHistory(ctx context.Context, query string, limit int) (dto.SearchResult, error) {
	return frontend.testComponents().historySearchApplication.SearchHistory(ctx, query, limit)
}

func (frontend *Frontend) ProjectRootFor(sessionID string) string {
	return frontend.testComponents().projectBoardQuery.ProjectRootFor(sessionID)
}

func (frontend *Frontend) WorkTableRowsFor(sessionID string) []model.WorkItem {
	return frontend.testComponents().projectBoardQuery.WorkTableRowsFor(sessionID)
}

func (frontend *Frontend) TeammateSessionLiveFor(sessionID string) dto.TeammateSessionLiveView {
	return frontend.testComponents().executionApplication.SessionLive(sessionID)
}

func (frontend *Frontend) TeammateSessionLivePageFor(sessionID string, offset, limit int) dto.TeammateSessionLiveView {
	return frontend.testComponents().executionApplication.SessionPage(sessionID, offset, limit)
}

func (frontend *Frontend) WorkItemExecutionFor(ctx context.Context, mainSessionID, itemID string, page dto.ConversationViewRequest) (dto.WorkItemExecutionView, error) {
	return frontend.testComponents().executionApplication.WorkItemExecution(ctx, mainSessionID, itemID, page)
}

func (frontend *Frontend) TeammateInspectionFor(ctx context.Context, mainSessionID, role string) (dto.TeammateInspectionView, error) {
	return frontend.testComponents().executionApplication.TeammateInspection(ctx, mainSessionID, role)
}

func (frontend *Frontend) ToolResultContent(_ context.Context, resultRef string, offset, limit int) (model.ToolResultPage, error) {
	return frontend.testComponents().referenceApplication.ToolResultContent(resultRef, offset, limit)
}

func (frontend *Frontend) CompactContextNow(ctx context.Context) (ContextCompactionResult, error) {
	return frontend.testComponents().contextCompactionApplication.CompactContextNow(ctx)
}

func (frontend *Frontend) CompactContextHandler(ctx context.Context, argsJSON string) (string, error) {
	return frontend.testComponents().contextCompactionApplication.CompactContextHandler(ctx, argsJSON)
}

func (frontend *Frontend) ReadToolResultHandler(_ context.Context, argsJSON string) (string, error) {
	return frontend.testComponents().referenceApplication.ReadToolResultHandler(argsJSON)
}

func (frontend *Frontend) ReadPlanHandler(_ context.Context, argsJSON string) (string, error) {
	return frontend.testComponents().referenceApplication.ReadPlanHandler(argsJSON)
}

func (frontend *Frontend) ReadCompressedTurnHandler(_ context.Context, argsJSON string) (string, error) {
	return frontend.testComponents().referenceApplication.ReadCompressedTurnHandler(argsJSON)
}

func (frontend *Frontend) RequestFileReview(ctx context.Context, files []dto.ReviewFileRef) (dto.ReviewRequest, error) {
	return frontend.testComponents().reviewApplication.RequestFileReview(ctx, files)
}

func (frontend *Frontend) GoalGovernanceViewFor(sessionID string) *dto.GoalGovernanceView {
	return frontend.testComponents().goalApplication.GovernanceViewFor(sessionID)
}

func (frontend *Frontend) GoalBeginFor(ctx context.Context, sessionID string, request goaldomain.BeginRequest) (*goaldomain.GoalRecord, error) {
	return frontend.testComponents().goalApplication.BeginFor(ctx, sessionID, request)
}

func (frontend *Frontend) GoalUpdateFor(ctx context.Context, sessionID string, request goaldomain.UpdateRequest) (*goaldomain.GoalRecord, error) {
	return frontend.testComponents().goalApplication.UpdateFor(ctx, sessionID, request)
}

func (frontend *Frontend) GoalDoneFor(ctx context.Context, sessionID string, request goaldomain.FinishRequest, abort bool) (*goaldomain.GoalRecord, error) {
	return frontend.testComponents().goalApplication.DoneFor(ctx, sessionID, request, abort)
}

func (frontend *Frontend) GoalStatusFor(sessionID string) (goaldomain.StatusView, error) {
	return frontend.testComponents().goalApplication.StatusFor(sessionID)
}

func (frontend *Frontend) ScheduleTask(ctx context.Context, spec assemble.ScheduledTaskSpec) (*assemble.ScheduledTaskStatus, error) {
	return frontend.testComponents().schedulerApplication.ScheduleTask(ctx, spec)
}

func (frontend *Frontend) UpdateScheduledTask(ctx context.Context, id string, spec assemble.ScheduledTaskSpec) (*assemble.ScheduledTaskStatus, error) {
	return frontend.testComponents().schedulerApplication.UpdateScheduledTask(ctx, id, spec)
}

func (frontend *Frontend) CancelScheduledTask(id string) error {
	return frontend.testComponents().schedulerApplication.CancelScheduledTask(id)
}

func (frontend *Frontend) AssembleScheduledRun(ctx context.Context, sessionID string, task assemble.ScheduledTaskSpec) (context.Context, error) {
	return frontend.testComponents().scheduledRunApplication.AssembleScheduledRun(ctx, sessionID, task)
}

func (frontend *Frontend) StartScheduledSession(ctx context.Context, task assemble.ScheduledTaskSpec) (string, error) {
	return frontend.testComponents().scheduledSessionApplication.StartScheduledSession(ctx, task)
}

func (frontend *Frontend) RefreshRuntimeSnapshot() {
	frontend.testComponents().runtimeApplication.Refresh()
}

func (frontend *Frontend) AnyChatRunning() bool {
	return frontend.testComponents().lifecycleApplication.AnyChatRunning()
}

func (frontend *Frontend) WaitForIdle(ctx context.Context) error {
	return frontend.testComponents().lifecycleApplication.WaitForIdle(ctx)
}

func (frontend *Frontend) ArchiveSession(sessionID string) error {
	return frontend.testComponents().sessionArchive.Archive(sessionID)
}

func (frontend *Frontend) BeginNewSession() error {
	return frontend.testComponents().sessionDraftEntry.Begin()
}

func (frontend *Frontend) UnloadSession(sessionID string) error {
	return frontend.testComponents().sessionLifecycle.Unload(sessionID)
}

func (frontend *Frontend) ResumeSession(sessionID string) error {
	return frontend.Service.components.sessionResume.Resume(sessionID)
}

func (frontend *Frontend) ActivateSession(sessionID string) error {
	return frontend.Service.components.sessionResume.Resume(sessionID)
}

func (frontend *Frontend) SetSessionMeta(sessionID string, meta model.SessionMeta) error {
	return frontend.testComponents().sessionMetaApplication.SetSessionMeta(sessionID, meta)
}

func (frontend *Frontend) EditUserMessageAndFork(ctx context.Context, parentID string, request model.EditUserMessageForkRequest) (model.EditUserMessageForkResult, error) {
	return frontend.testComponents().editForkApplication.EditUserMessageAndFork(ctx, parentID, request)
}

func (frontend *Frontend) SessionSSHState(sessionID string) (model.SessionSSHInfo, error) {
	return frontend.testComponents().sessionSSHApplication.State(sessionID)
}

func (frontend *Frontend) SetSessionSSHAllowed(sessionID string, envs []string) (model.SessionSSHInfo, error) {
	return frontend.testComponents().sessionSSHApplication.SetAllowed(sessionID, envs)
}

func (frontend *Frontend) SetSessionSSHEnv(sessionID, projectID, env string) (model.SessionSSHInfo, error) {
	return frontend.testComponents().sessionSSHApplication.SetEnv(sessionID, projectID, env)
}

func (frontend *Frontend) ActivateSkill(name string) (model.SkillInfo, error) {
	return frontend.testComponents().policyApplication.ActivateSkill(name)
}

func (frontend *Frontend) UpdateWorkItemStatus(id, status string) error {
	return frontend.testComponents().executionApplication.UpdateWorkItemStatus(id, status)
}

func (frontend *Frontend) ListSubagentRecovery(sessionID string) ([]dto.SubagentRecoveryView, error) {
	return frontend.testComponents().subagentApplication.ListRecovery(sessionID)
}

func (frontend *Frontend) ResumeInterruptedSubagents(ctx context.Context, sessionID string) (dto.SubagentResumeReport, error) {
	return frontend.testComponents().subagentApplication.ResumeInterrupted(ctx, sessionID)
}

func (frontend *Frontend) ResumeSubagent(ctx context.Context, sessionID, nodeID string) (dto.SubagentResumeResult, error) {
	return frontend.testComponents().subagentApplication.Resume(ctx, sessionID, nodeID)
}

func (frontend *Frontend) ForkSubagents(ctx context.Context, sessionID string, specs []dto.SubagentForkSpec) (string, error) {
	return frontend.testComponents().subagentApplication.Fork(ctx, sessionID, specs)
}

func (frontend *Frontend) SubagentSessionDetail(nodeID string) (*model.SubagentDetail, error) {
	return frontend.testComponents().subagentApplication.Detail(nodeID)
}

func (frontend *Frontend) ClearSubagentTree() error {
	return frontend.testComponents().subagentApplication.ClearTree()
}

func (frontend *Frontend) SearchHistoryHandler(_ context.Context, argsJSON string) (string, error) {
	return frontend.testComponents().historySearchApplication.SearchHistoryHandler(argsJSON)
}

func (frontend *Frontend) BeginGracefulShutdown() {
	frontend.testComponents().lifecycleApplication.BeginGracefulShutdown()
}

func (frontend *Frontend) CancelAllChats() {
	frontend.testComponents().lifecycleApplication.CancelAllChats()
}

func (frontend *Frontend) CancelChat(requestID string) bool {
	return frontend.testComponents().lifecycleApplication.CancelChat(requestID)
}

func (frontend *Frontend) Shutdown() {
	frontend.testComponents().lifecycleApplication.Shutdown()
}

func (frontend *Frontend) SetFullAccess(on bool) bool {
	return frontend.testComponents().policyApplication.SetFullAccess(on)
}

func (frontend *Frontend) SetPermissionTier(tier string) (string, error) {
	return frontend.testComponents().policyApplication.SetPermissionTier(tier)
}

func (frontend *Frontend) SetLoopLimit(_ context.Context, limit int) error {
	return frontend.testComponents().policyApplication.SetLoopLimit(limit)
}

func (frontend *Frontend) SetReasoningEffort(_ context.Context, effort string) error {
	return frontend.testComponents().policyApplication.SetReasoningEffort(effort)
}

func (frontend *Frontend) SessionReleaseWindow() int {
	return frontend.testComponents().policyApplication.SessionReleaseWindow()
}

func (frontend *Frontend) SetSessionReleaseWindow(_ context.Context, window int) (int, error) {
	return frontend.testComponents().policyApplication.SetSessionReleaseWindow(window)
}

func (frontend *Frontend) Subscribe(buffer int) event.Subscription {
	return frontend.testComponents().view.Subscribe(buffer)
}

func (frontend *Frontend) GoalSkillActive() bool {
	return frontend.testComponents().tasks.GoalSkillActive()
}

func (frontend *Frontend) HandleSubagentToolEventFor(ownerSessionID string, toolEvent seelsession.SubagentToolEvent) {
	frontend.testComponents().subagent.HandleSubagentToolEventFor(ownerSessionID, toolEvent)
}

func (frontend *Frontend) AddNotice(notice string) {
	frontend.Service.components.view.AddNotice(notice)
}

func (frontend *Frontend) PublishRuntimeProjections() {
	frontend.Service.publishRuntimeProjections()
}

func (frontend *Frontend) CloseRoleToolRecordUnit(mainSessionID, roleSessionID string) {
	frontend.Service.closeUnitToolRecord(mainSessionID, applicationtoolrecord.RoleUnitID(roleSessionID, ""))
}

func (frontend *Frontend) RoleDefaultVector(sessionID, role string) ([]string, bool, error) {
	return frontend.testComponents().roleApplication.RoleDefaultVector(sessionID, role)
}

func (frontend *Frontend) SetRoleDefaultVector(sessionID, role string, vector []string) error {
	return frontend.testComponents().roleApplication.SetRoleDefaultVector(sessionID, role, vector)
}

func (frontend *Frontend) RoleAccount(sessionID, role string) (string, bool, error) {
	return frontend.testComponents().roleApplication.RoleAccount(sessionID, role)
}

func (frontend *Frontend) SetRoleAccount(sessionID, role, account string) error {
	return frontend.testComponents().roleApplication.SetRoleAccount(sessionID, role, account)
}

func (frontend *Frontend) RoleInheritMainAgent(sessionID, role string) (bool, error) {
	return frontend.testComponents().roleApplication.RoleInheritMainAgent(sessionID, role)
}

func (frontend *Frontend) SetRoleInheritMainAgent(sessionID, role string, inherit bool) error {
	return frontend.testComponents().roleApplication.SetRoleInheritMainAgent(sessionID, role, inherit)
}

func (frontend *Frontend) RoleSSHUserSpaces(sessionID, role string) ([]string, error) {
	return frontend.testComponents().roleApplication.RoleSSHUserSpaces(sessionID, role)
}

func (frontend *Frontend) RoleDefaultSSHUserSpaces(sessionID, role string) ([]string, error) {
	return frontend.testComponents().roleApplication.RoleDefaultSSHUserSpaces(sessionID, role)
}

func (frontend *Frontend) SetRoleSSHUserSpaces(sessionID, role string, paths []string) error {
	return frontend.testComponents().roleApplication.SetRoleSSHUserSpaces(sessionID, role, paths)
}

func (frontend *Frontend) SetRoleSessionSSHUserSpaces(sessionID, role string, paths []string) error {
	return frontend.testComponents().roleApplication.SetRoleSessionSSHUserSpaces(sessionID, role, paths)
}

func (frontend *Frontend) RolePermissionVector(sessionID, role string) ([]string, error) {
	return frontend.testComponents().roleApplication.RolePermissionVector(sessionID, role)
}

func (frontend *Frontend) SetRolePermissionVector(sessionID, role string, vector []string) error {
	return frontend.testComponents().roleApplication.SetRolePermissionVector(sessionID, role, vector)
}

func (frontend *Frontend) CreateRoleSession(mainSessionID, roleName, roleSessionID string, joinSeq uint64) (dto.RoleSessionInfo, error) {
	return frontend.testComponents().roleSessions.CreateRoleSession(mainSessionID, roleName, roleSessionID, joinSeq)
}

func (frontend *Frontend) AppendRoleDraft(mainSessionID, roleName, roleSessionID string, rows []dto.RoleDraftRow) error {
	return frontend.testComponents().roleSessions.AppendRoleDraft(mainSessionID, roleName, roleSessionID, rows)
}

func (frontend *Frontend) ReadRoleDraft(mainSessionID, roleName, roleSessionID string) ([]dto.RoleDraftRow, error) {
	return frontend.testComponents().roleSessions.ReadRoleDraft(mainSessionID, roleName, roleSessionID)
}

func (frontend *Frontend) SyncRoleDraft(mainSessionID, roleName, roleSessionID string, order []string) (dto.RoleDraftSyncResult, error) {
	return frontend.testComponents().roleSessions.SyncRoleDraft(mainSessionID, roleName, roleSessionID, order)
}

func (frontend *Frontend) AppendRoleSessionRows(mainSessionID, roleName, roleSessionID string, rows []dto.RoleRow) error {
	return frontend.testComponents().roleSessions.AppendRoleSessionRows(mainSessionID, roleName, roleSessionID, rows)
}

func (frontend *Frontend) ReadRoleSessionRows(mainSessionID, roleName, roleSessionID string) ([]dto.RoleRow, error) {
	return frontend.testComponents().roleSessions.ReadRoleSessionRows(mainSessionID, roleName, roleSessionID)
}

func (frontend *Frontend) RoleSnapshot(mainSessionID, roleName, roleSessionID string) (dto.RoleSnapshot, error) {
	return frontend.testComponents().roleSessions.RoleSnapshot(mainSessionID, roleName, roleSessionID)
}

func (frontend *Frontend) AssembleRoleWire(mainSessionID, roleName, roleSessionID string, budget, k int) (dto.RoleWireSnapshot, error) {
	return frontend.testComponents().roleSessions.AssembleRoleWire(mainSessionID, roleName, roleSessionID, budget, k)
}

func (frontend *Frontend) SetLifecycleOrder(sessionID, policy string, roles []string) error {
	return frontend.testComponents().roleSessions.SetLifecycleOrder(sessionID, policy, roles)
}

func (frontend *Frontend) SetRoleLifecycle(mainSessionID, roleName, roleSessionID string, joinSeq uint64, ref *dto.CompactFrameRef) error {
	return frontend.testComponents().roleSessions.SetRoleLifecycle(mainSessionID, roleName, roleSessionID, joinSeq, ref)
}

func (frontend *Frontend) ListRoleSessions(mainSessionID string) ([]string, error) {
	return frontend.testComponents().roleSessions.ListRoleSessions(mainSessionID)
}

func (frontend *Frontend) ScheduleRegister(sessionID string, payload dto.ScheduleEventPayload) error {
	return frontend.testComponents().roleSessions.ScheduleRegister(sessionID, payload)
}

func (frontend *Frontend) ScheduleCancel(sessionID string, payload dto.ScheduleEventPayload) error {
	return frontend.testComponents().roleSessions.ScheduleCancel(sessionID, payload)
}

func (frontend *Frontend) ScheduleFire(sessionID string, payload dto.ScheduleEventPayload) error {
	return frontend.testComponents().roleSessions.ScheduleFire(sessionID, payload)
}

func (frontend *Frontend) WorkspaceEnvironments(projectID string) ([]model.WorkspaceEnvironmentInfo, string, error) {
	return frontend.testComponents().workspaceApplication.WorkspaceEnvironments(projectID)
}

func (frontend *Frontend) SetWorkspaceEnvironment(projectID, name string, change model.WorkspaceEnvironmentChange) (model.WorkspaceEnvironmentInfo, error) {
	return frontend.testComponents().workspaceApplication.SetWorkspaceEnvironment(projectID, name, change)
}

func (frontend *Frontend) DeleteWorkspaceEnvironment(projectID, name string) error {
	return frontend.testComponents().workspaceApplication.DeleteWorkspaceEnvironment(projectID, name)
}

func (frontend *Frontend) SetDefaultWorkspaceEnvironment(projectID, name string) error {
	return frontend.testComponents().workspaceApplication.SetDefaultWorkspaceEnvironment(projectID, name)
}

func (frontend *Frontend) WorkspaceGitRepos() (dto.GitReposResult, error) {
	return frontend.testComponents().workspaceApplication.WorkspaceGitRepos()
}

func (frontend *Frontend) DeleteSession(sessionID string) error {
	return frontend.testComponents().sessionDelete.Delete(sessionID)
}

func (frontend *Frontend) CreateWorkspace(name, rootPath, gitRemote string) error {
	return frontend.testComponents().workspace.CreateWorkspace(name, rootPath, gitRemote)
}

func (frontend *Frontend) CreateWorkspaces(requests []dto.WorkspaceCreateRequest) (dto.WorkspaceCreateBatchResult, error) {
	return frontend.testComponents().workspaceApplication.CreateWorkspaces(requests)
}

func (frontend *Frontend) BindWorkspace(workspaceID string) error {
	return frontend.testComponents().workspace.BindWorkspace(workspaceID)
}

func (frontend *Frontend) UnbindWorkspace() {
	frontend.testComponents().workspace.UnbindWorkspace()
}

func (frontend *Frontend) WorkspaceTree(relPath string, depth int) (dto.TreeListing, error) {
	return frontend.testComponents().workspace.Tree(relPath, depth)
}

func (frontend *Frontend) WorkspaceFileCount() (dto.TreeCount, error) {
	return frontend.testComponents().workspace.FileCount()
}

func (frontend *Frontend) WorkspaceGitLog(repo string, limit int) (dto.GitLogResult, error) {
	return frontend.testComponents().workspaceApplication.GitLog(repo, limit)
}

func (frontend *Frontend) WorkspaceChanges(repo string, limit int) (dto.WorkspaceChangesResult, error) {
	return frontend.testComponents().workspaceApplication.Changes(repo, limit)
}

func (frontend *Frontend) WorkspaceGitCommitDetail(repo, hash string, limit int) (dto.GitCommitDetail, error) {
	return frontend.testComponents().workspaceApplication.CommitDetail(repo, hash, limit)
}

func (frontend *Frontend) WorkspaceFileContent(relPath string, limit int64) (dto.FileContent, error) {
	return frontend.testComponents().workspace.FileContent(relPath, limit)
}

func (frontend *Frontend) WorkspaceGitCommitFileContent(repo, hash, relPath string, limit int64) (dto.FileContent, error) {
	return frontend.testComponents().workspaceApplication.CommitFileContent(repo, hash, relPath, limit)
}

func (frontend *Frontend) WorkspaceWriteFile(relPath, content string) (dto.FileWriteResult, error) {
	return frontend.testComponents().workspace.WriteFile(relPath, content)
}
