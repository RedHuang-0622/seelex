package table

import (
	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/contract/protocol"
	"github.com/RedHuang-0622/seelex/middle/view/worktable"
)

// RowInputs is the immutable input for one WorkTable row projection.
type RowInputs struct {
	Plan         *model.PlanState
	Tasks        []dto.TaskRecord
	SubAgentTree []dto.SubAgentTreeNode
	AsyncRuns    []dto.AsyncRunRecord
	Limits       worktable.Limits
}

// BuildRows delegates to the unique row/batch projection implementation.
func BuildRows(input RowInputs) ([]model.WorkItem, []model.WorkTableBatch) {
	rows := worktable.BuildWorkTable(
		input.Plan,
		input.Tasks,
		input.SubAgentTree,
		input.AsyncRuns,
		input.Limits,
	)
	return rows, worktable.BuildWorkTableBatches(rows)
}

// WorkItemForRecord adds Plan adjacency dependencies to one task-change row.
func WorkItemForRecord(record dto.TaskRecord, plan *model.PlanState, limits worktable.Limits) model.WorkItem {
	item := worktable.TaskRecordToWorkItem(record, limits)
	if record.Kind != "plan" {
		return item
	}
	item.Dependencies = worktable.MergeWorkDependencies(
		item.Dependencies,
		worktable.PlanDependencies(plan, worktable.PlanNodeIDFor(record)),
	)
	return item
}
