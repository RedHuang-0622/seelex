package table

import (
	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/contract/protocol"
	worktable "github.com/RedHuang-0622/seelex/middle/view/worktable"
)

type Limits = worktable.Limits
type WorkTableUpdate = worktable.WorkTableUpdate

func CloneSubAgentTreeForSync(nodes []dto.SubAgentTreeNode) []dto.SubAgentTreeNode {
	return worktable.CloneSubAgentTreeForSync(nodes)
}

func TruncateWorkEvidence(value string, limit int) string {
	return worktable.TruncateWorkEvidence(value, limit)
}

func ClonePlanForSync(plan *model.PlanState) *model.PlanState {
	return worktable.ClonePlanForSync(plan)
}

func MergeTaskRecords(
	primary, secondary []dto.TaskRecord,
	secondarySessionID string,
	identity func(dto.TaskRecord) string,
) []dto.TaskRecord {
	return worktable.MergeTaskRecords(primary, secondary, secondarySessionID, identity)
}

func BuildWorkTable(
	plan *model.PlanState,
	tasks []dto.TaskRecord,
	subagentTree []dto.SubAgentTreeNode,
	asyncRuns []dto.AsyncRunRecord,
	limits Limits,
) []model.WorkItem {
	return worktable.BuildWorkTable(plan, tasks, subagentTree, asyncRuns, limits)
}

func BuildWorkTableBatches(rows []model.WorkItem) []model.WorkTableBatch {
	return worktable.BuildWorkTableBatches(rows)
}

func TaskStatusForNode(status model.NodeStatus) dto.TaskStatus {
	return worktable.TaskStatusForNode(status)
}

func TaskStatusForSubagent(status dto.SubAgentNodeStatus) dto.TaskStatus {
	return worktable.TaskStatusForSubagent(status)
}
