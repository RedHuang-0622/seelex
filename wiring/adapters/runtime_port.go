package adapters

import (
	"context"

	assemble "github.com/RedHuang-0622/seelex/backend/engine/assemble"
	seelexctxsearch "github.com/RedHuang-0622/seelex/backend/engine/context/seelectx/search"
	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/middle/protocol/model"
)

type RuntimePort struct{ Runtime *assemble.Runtime }

func (port RuntimePort) Model() string         { return port.Runtime.Model() }
func (port RuntimePort) Provider() string      { return port.Runtime.Provider() }
func (port RuntimePort) ContextWindow() int    { return port.Runtime.ContextWindow() }
func (port RuntimePort) MaxOutputTokens() int  { return port.Runtime.MaxOutputTokens() }
func (port RuntimePort) ActivePlugin() string  { return port.Runtime.ActivePlugin() }
func (port RuntimePort) FullAccess() bool      { return port.Runtime.FullAccess() }
func (port RuntimePort) SetFullAccess(on bool) { port.Runtime.SetFullAccess(on) }
func (port RuntimePort) SetFullAccessFor(sessionID string, on bool) {
	port.Runtime.SetFullAccessFor(sessionID, on)
}
func (port RuntimePort) PermissionTier() string { return port.Runtime.PermissionTier() }
func (port RuntimePort) SetPermissionTierFor(sessionID, tier string) error {
	return port.Runtime.SetPermissionTierFor(sessionID, tier)
}
func (port RuntimePort) PerSessionExecution() bool {
	return port.Runtime.PerSessionExecution()
}
func (port RuntimePort) ForkInFlight(sessionID string) bool {
	return port.Runtime.ForkInFlight(sessionID)
}
func (port RuntimePort) SetRuntimeVisibilityProjection(projection assemble.RuntimeVisibilityProjection) {
	port.Runtime.SetRuntimeVisibilityProjection(projection)
}
func (port RuntimePort) SetParentEvidenceProjection(projection assemble.ParentEvidenceProjection) {
	port.Runtime.SetParentEvidenceProjection(projection)
}
func (port RuntimePort) DrainSubagentContexts() []string { return port.Runtime.DrainSubagentContexts() }
func (port RuntimePort) SetSubagentPolicy(policy dto.SubagentPolicy) {
	port.Runtime.SetSubagentPolicy(policy)
}
func (port RuntimePort) SetSubagentPolicyFor(sessionID string, policy dto.SubagentPolicy) {
	port.Runtime.SetSubagentPolicyFor(sessionID, policy)
}
func (port RuntimePort) BindProjectRoot(rootPath string) error {
	return port.Runtime.BindProjectRoot(rootPath)
}
func (port RuntimePort) UnbindProjectRoot() { port.Runtime.UnbindProjectRoot() }
func (port RuntimePort) BindProjectRootFor(sessionID, rootPath string) error {
	return port.Runtime.BindProjectRootFor(sessionID, rootPath)
}
func (port RuntimePort) UnbindProjectRootFor(sessionID string) {
	port.Runtime.UnbindProjectRootFor(sessionID)
}
func (port RuntimePort) SetCurrentTaskBatch(sessionID, batchID string) {
	port.Runtime.SetCurrentTaskBatch(sessionID, batchID)
}
func (port RuntimePort) TodoSnapshot() []dto.TodoItem {
	return port.Runtime.TodoSnapshot()
}
func (port RuntimePort) SetTodoStatus(index int, status dto.TodoItemStatus) error {
	return port.Runtime.SetTodoStatus(index, status)
}
func (port RuntimePort) TaskSnapshot() []dto.TaskRecord {
	return port.Runtime.TaskSnapshot()
}
func (port RuntimePort) TaskSnapshotFor(sessionID string) []dto.TaskRecord {
	return port.Runtime.TaskSnapshotFor(sessionID)
}
func (port RuntimePort) ReleaseSessionAsync(sessionID string) int {
	return port.Runtime.ReleaseSessionAsync(sessionID)
}
func (port RuntimePort) AsyncPendingFor(sessionID string) int {
	return port.Runtime.AsyncPendingFor(sessionID)
}
func (port RuntimePort) AsyncRunsSnapshot() []dto.AsyncRunRecord {
	return port.Runtime.AsyncRunsSnapshot()
}
func (port RuntimePort) AsyncRunEvents() <-chan struct{} {
	return port.Runtime.AsyncRunEvents()
}
func (port RuntimePort) TaskAdd(spec dto.TaskSpec) (dto.TaskRecord, bool, error) {
	return port.Runtime.TaskAdd(spec)
}
func (port RuntimePort) TaskAddFor(sessionID string, spec dto.TaskSpec) (dto.TaskRecord, bool, error) {
	return port.Runtime.TaskAddFor(sessionID, spec)
}
func (port RuntimePort) ResolveTaskByKey(key string) (dto.TaskRecord, bool, error) {
	return port.Runtime.ResolveTaskByKey(key)
}
func (port RuntimePort) ResolveTaskByKeyFor(sessionID, key string) (dto.TaskRecord, bool, error) {
	return port.Runtime.ResolveTaskByKeyFor(sessionID, key)
}
func (port RuntimePort) TaskSetStatus(id string, status dto.TaskStatus, evidence string) (dto.TaskRecord, error) {
	return port.Runtime.TaskSetStatus(id, status, evidence)
}
func (port RuntimePort) TaskSetStatusFor(sessionID, id string, status dto.TaskStatus, evidence string) (dto.TaskRecord, error) {
	return port.Runtime.TaskSetStatusFor(sessionID, id, status, evidence)
}
func (port RuntimePort) TaskAttachParticipant(id, participant string) (dto.TaskRecord, error) {
	return port.Runtime.TaskAttachParticipant(id, participant)
}
func (port RuntimePort) TaskChangedChannel() <-chan dto.TaskRecord {
	return port.Runtime.TaskChangedChannel()
}
func (port RuntimePort) SubagentTreeEvents() <-chan struct{} {
	return port.Runtime.SubagentTreeEvents()
}
func (port RuntimePort) TakeServedRoute(sessionID string) (dto.ServedRoute, bool) {
	return port.Runtime.TakeServedRoute(sessionID)
}
func (port RuntimePort) SwitchSessionTasks(sessionID string, records []dto.TaskRecord) {
	port.Runtime.SwitchSessionTasks(sessionID, records)
}
func (port RuntimePort) SetSessionWorkspace(sessionID, workspaceID string) {
	port.Runtime.SetSessionWorkspace(sessionID, workspaceID)
}
func (port RuntimePort) ScheduledCommands() []assemble.ScheduledCommandInfo {
	return port.Runtime.ScheduledCommands()
}
func (port RuntimePort) ScheduledTasksSnapshot() []assemble.ScheduledTaskStatus {
	return port.Runtime.ScheduledTasksSnapshot()
}
func (port RuntimePort) ScheduleTask(ctx context.Context, spec assemble.ScheduledTaskSpec) (*assemble.ScheduledTaskStatus, error) {
	return port.Runtime.ScheduleTask(ctx, spec)
}
func (port RuntimePort) UpdateScheduledTask(ctx context.Context, id string, spec assemble.ScheduledTaskSpec) (*assemble.ScheduledTaskStatus, error) {
	return port.Runtime.UpdateScheduledTask(ctx, id, spec)
}
func (port RuntimePort) CancelScheduledTask(id string) error {
	return port.Runtime.CancelScheduledTask(id)
}
func (port RuntimePort) ClearSubagentTree() error {
	return port.Runtime.ClearSubagentTree()
}
func (port RuntimePort) RestoreSubagentAnchors(sessionID string) error {
	return port.Runtime.RestoreSubagentAnchors(sessionID)
}
func (port RuntimePort) ListSubagentRecovery(sessionID string) ([]dto.SubagentRecoveryView, error) {
	return port.Runtime.ListSubagentRecovery(sessionID)
}
func (port RuntimePort) ResumeInterruptedSubagents(ctx context.Context, sessionID string) (dto.SubagentResumeReport, error) {
	return port.Runtime.ResumeInterruptedSubagents(ctx, sessionID)
}
func (port RuntimePort) ResumeSubagent(ctx context.Context, sessionID, nodeID string) (dto.SubagentResumeResult, error) {
	return port.Runtime.ResumeSubagent(ctx, sessionID, nodeID)
}
func (port RuntimePort) ForkSubagents(ctx context.Context, sessionID string, specs []dto.SubagentForkSpec) (string, error) {
	return port.Runtime.ForkSubagents(ctx, sessionID, specs)
}
func (port RuntimePort) SetSubagentParentRepairer(fn func(sessionID string) error) {
	port.Runtime.SetSubagentParentRepairer(fn)
}
func (port RuntimePort) SearchHistory(ctx context.Context, query string, limit int) (seelexctxsearch.Result, error) {
	return port.Runtime.SearchHistory(ctx, query, limit)
}
func (port RuntimePort) SelectAccount(name string) bool { return port.Runtime.SelectAccount(name) }
func (port RuntimePort) VisibleTools(ctx context.Context) []model.Tool {
	tools := port.Runtime.VisibleTools(ctx)
	result := make([]model.Tool, 0, len(tools))
	for _, tool := range tools {
		result = append(result, model.Tool{Name: tool.Name, Description: tool.Description})
	}
	return result
}
func (port RuntimePort) Accounts() []model.AccountInfo {
	accounts := port.Runtime.Accounts()
	result := make([]model.AccountInfo, 0, len(accounts))
	for _, account := range accounts {
		result = append(result, model.AccountInfo{
			Name: account.Name, Provider: account.Provider, Model: account.Model, Disabled: account.Disabled,
			Role: account.Role, APIKeySet: account.APIKeySet, Fallback: account.Fallback,
			// 逐账号读各自的生效窗口（账号级覆盖 > 全局 > 账号档）：界面按账号显示
			// 挡位时需要它，拿当前账号的值顶替会让别的行显示成假的。
			ContextWindow: port.Runtime.ContextWindowFor(account.Name),
		})
	}
	return result
}
