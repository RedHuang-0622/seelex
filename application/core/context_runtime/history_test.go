package context_runtime

import (
	"reflect"
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

func TestRetainedSystemHistoryKeepsStablePrefixAndSettledContext(t *testing.T) {
	history := []contract.EngineMessage{
		{Role: "system", Content: "product instruction", ContentSet: true},
		{Role: "user", Content: "settled request", ContentSet: true},
		{Role: "assistant", Content: "settled answer", ContentSet: true},
		{Role: "user", Content: planContextPrefix + "\n{}", ContentSet: true},
	}
	retained := RetainedSystemHistory(history)
	// 稳定前缀（system）+ 已定稿轮次保留；动态尾部（plan 消息）剔除。
	if got := retainedContents(retained); !reflect.DeepEqual(got, []string{"product instruction", "settled request", "settled answer"}) {
		t.Fatalf("retained history = %v, want stable prefix + settled context", got)
	}
	systemOnly := RetainedSystemOnly(history)
	if got := retainedContents(systemOnly); !reflect.DeepEqual(got, []string{"product instruction"}) {
		t.Fatalf("recovery retention = %v, want first system instruction only", got)
	}
}

func retainedContents(history []contract.EngineMessage) []string {
	contents := make([]string, 0, len(history))
	for _, message := range history {
		contents = append(contents, message.Content)
	}
	return contents
}
