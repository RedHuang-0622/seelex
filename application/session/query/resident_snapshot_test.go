package query

import (
	"testing"

	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/contract/protocol"
	worktable "github.com/RedHuang-0622/seelex/kernel/worktable"
)

func TestBuildResidentSnapshotDetachesAndEnriches(t *testing.T) {
	input := ResidentSnapshotInput{
		ProtocolVersion: 3,
		Revision:        7,
		Session:         model.SessionState{ID: "s1", Name: "session"},
		Conversation:    []model.Message{{ID: "m1", Role: "assistant"}},
		Chat:            model.ChatState{Running: true},
		ReadFiles:       []model.ReadFileRef{{Path: "a.go"}},
		Approvals:       []model.Interaction{{ID: "approval-1"}},
		Plan:            &model.PlanState{Nodes: []model.PlanNode{{ID: "node-1"}}},
		Task:            &model.TaskState{ContextCompactions: []model.ContextCompaction{{Version: 1}}},
		TaskRecords:     []dto.TaskRecord{{ID: "todo:1", Kind: "todo", Task: "task", Status: dto.TaskPending}},
		Limits:          worktable.Limits{WorkTableRows: 10, EvidenceChars: 200},
		AnnotateConversation: func(messages []model.Message, running bool) []model.Message {
			if running {
				messages[0].Editable = true
			}
			return messages
		},
	}
	snapshot := BuildResidentSnapshot(input)
	if !snapshot.Resident || snapshot.Revision != 7 || !snapshot.Conversation[0].Editable {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	if len(snapshot.Runtime.WorkTable) != 1 || snapshot.Runtime.WorkTable[0].ID != "todo:1" {
		t.Fatalf("worktable = %+v", snapshot.Runtime.WorkTable)
	}
	input.ReadFiles[0].Path = "mutated"
	input.Task.ContextCompactions[0].Version = 99
	if snapshot.ReadFiles[0].Path != "a.go" || snapshot.Task.ContextCompactions[0].Version != 1 {
		t.Fatalf("snapshot aliases input: %+v", snapshot)
	}
}
