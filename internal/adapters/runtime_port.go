package adapters

import (
	"context"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
	"github.com/RedHuang-0622/seelex/application/model"
	"github.com/RedHuang-0622/seelex/seelebridge"
	seelexctxsearch "github.com/RedHuang-0622/seelex/seelexctx/search"
)

type RuntimePort struct{ Runtime *seelebridge.Runtime }

func (port RuntimePort) Model() string         { return port.Runtime.Model() }
func (port RuntimePort) Provider() string      { return port.Runtime.Provider() }
func (port RuntimePort) ContextWindow() int    { return port.Runtime.ContextWindow() }
func (port RuntimePort) MaxOutputTokens() int  { return port.Runtime.MaxOutputTokens() }
func (port RuntimePort) ActivePlugin() string  { return port.Runtime.ActivePlugin() }
func (port RuntimePort) FullAccess() bool      { return port.Runtime.FullAccess() }
func (port RuntimePort) SetFullAccess(on bool) { port.Runtime.SetFullAccess(on) }
func (port RuntimePort) SetRuntimeVisibilityProjection(projection seelebridge.RuntimeVisibilityProjection) {
	port.Runtime.SetRuntimeVisibilityProjection(projection)
}
func (port RuntimePort) SetParentEvidenceProjection(projection seelebridge.ParentEvidenceProjection) {
	port.Runtime.SetParentEvidenceProjection(projection)
}
func (port RuntimePort) DrainSubagentContexts() []string { return port.Runtime.DrainSubagentContexts() }
func (port RuntimePort) SetPlanPolicy(policy dto.PlanPolicy) {
	port.Runtime.SetPlanPolicy(policy)
}
func (port RuntimePort) PrepareReplan(ctx context.Context, request dto.ReplanRequest) (dto.PlanPreflight, error) {
	return port.Runtime.PrepareReplan(ctx, request)
}
func (port RuntimePort) ReplanMetrics() dto.ReplanMetrics {
	return port.Runtime.ReplanMetrics()
}
func (port RuntimePort) SetPlanBranchBinding(binding dto.PlanBranchBinding) {
	port.Runtime.SetPlanBranchBinding(binding)
}
func (port RuntimePort) RestorePlan(ctx context.Context, arguments string) error {
	return port.Runtime.RestorePlan(ctx, arguments)
}
func (port RuntimePort) BindProjectRoot(rootPath string) error {
	return port.Runtime.BindProjectRoot(rootPath)
}
func (port RuntimePort) UnbindProjectRoot() { port.Runtime.UnbindProjectRoot() }
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
func (port RuntimePort) TaskAdd(spec dto.TaskSpec) (dto.TaskRecord, bool, error) {
	return port.Runtime.TaskAdd(spec)
}
func (port RuntimePort) ResolveTaskByKey(key string) (dto.TaskRecord, bool, error) {
	return port.Runtime.ResolveTaskByKey(key)
}
func (port RuntimePort) TaskSetStatus(id string, status dto.TaskStatus, evidence string) (dto.TaskRecord, error) {
	return port.Runtime.TaskSetStatus(id, status, evidence)
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
func (port RuntimePort) PlanNodeEventChannel() <-chan dto.PlanNodeEvent {
	return port.Runtime.PlanNodeEventChannel()
}
func (port RuntimePort) SwitchSessionTasks(sessionID string, records []dto.TaskRecord) {
	port.Runtime.SwitchSessionTasks(sessionID, records)
}
func (port RuntimePort) SetSessionWorkspace(sessionID, workspaceID string) {
	port.Runtime.SetSessionWorkspace(sessionID, workspaceID)
}
func (port RuntimePort) ScheduledCommands() []seelebridge.ScheduledCommandInfo {
	return port.Runtime.ScheduledCommands()
}
func (port RuntimePort) ScheduledTasksSnapshot() []seelebridge.ScheduledTaskStatus {
	return port.Runtime.ScheduledTasksSnapshot()
}
func (port RuntimePort) ScheduleTask(ctx context.Context, spec seelebridge.ScheduledTaskSpec) (*seelebridge.ScheduledTaskStatus, error) {
	return port.Runtime.ScheduleTask(ctx, spec)
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
		result = append(result, model.AccountInfo{Name: account.Name, Provider: account.Provider, Model: account.Model, Disabled: account.Disabled})
	}
	return result
}
