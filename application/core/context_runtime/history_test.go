package context_runtime

import (
	"testing"

	"github.com/RedHuang-0622/seelex/application/contract"
)

func TestRepairEmptyHistoryContentRepairsToolCallAssistantContent(t *testing.T) {
	history := []contract.EngineMessage{
		{Role: "assistant", ToolCalls: []contract.EngineToolCall{{ID: "call-1", Name: "read_file", Arguments: `{"path":"a.go"}`}}},
		{Role: "tool", ToolCallID: "call-1", Name: "read_file", Content: ""},
		{Role: "assistant", Content: ""},
	}
	prepared, repaired := RepairEmptyHistoryContent(history)
	if !repaired {
		t.Fatal("expected empty non-protocol messages to be repaired")
	}
	if prepared[0].Content != ToolCallHistoryContent || !prepared[0].ContentSet {
		t.Fatalf("tool-call assistant was not repaired: %+v", prepared[0])
	}
	if len(prepared[0].ToolCalls) != 1 || prepared[0].ToolCalls[0].ID != "call-1" {
		t.Fatalf("tool-call assistant lost its protocol data: %+v", prepared[0])
	}
	for _, index := range []int{1, 2} {
		if prepared[index].Content != MissingHistoryContent || !prepared[index].ContentSet {
			t.Fatalf("message %d was not repaired: %+v", index, prepared[index])
		}
	}
}
