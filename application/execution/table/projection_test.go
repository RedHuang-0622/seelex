package table

import (
	"testing"

	"github.com/RedHuang-0622/seelex/contract/dto"
	worktable "github.com/RedHuang-0622/seelex/kernel/worktable"
)

func TestBuildRowsDelegatesRowAndBatchProjection(t *testing.T) {
	rows, batches := BuildRows(RowInputs{
		Tasks: []dto.TaskRecord{{
			ID: "todo:1", Kind: "todo", Task: "write tests", Status: dto.TaskPending,
		}},
		Limits: worktable.Limits{WorkTableRows: 10, EvidenceChars: 200},
	})
	if len(rows) != 1 || rows[0].ID != "todo:1" {
		t.Fatalf("rows = %+v", rows)
	}
	if len(batches) == 0 {
		t.Fatal("batch projection returned no sheets")
	}
}

func TestWorkItemForRecordWithoutPlanLeavesDependencies(t *testing.T) {
	item := WorkItemForRecord(dto.TaskRecord{
		ID: "todo:1", Kind: "todo", Task: "write tests", Status: dto.TaskPending,
		Dependencies: []string{"todo:0"},
	}, nil, worktable.Limits{EvidenceChars: 200})
	if item.ID != "todo:1" || len(item.Dependencies) != 1 || item.Dependencies[0] != "todo:0" {
		t.Fatalf("work item = %+v", item)
	}
}
