package main

import (
	"reflect"
	"testing"

	"github.com/RedHuang-0622/Seele/types"

	"github.com/RedHuang-0622/seelex/seelebridge"
)

func TestEngineMessageRoundTripPreservesResumeContext(t *testing.T) {
	t.Parallel()
	empty := ""
	toolResult := "done"
	original := []seelebridge.Message{
		{
			Role: "assistant", ReasoningContent: "reasoning", Content: &empty,
			ToolCalls: []types.ToolCall{{
				ID: "call-1", Type: "function",
				Function: types.ToolCallFunction{Name: "read", Arguments: `{"path":"README.md"}`},
			}},
		},
		{Role: "tool", Content: &toolResult, ToolCallID: "call-1", Name: "read"},
	}

	restored := restoreMessages(adaptMessages(original))
	if !reflect.DeepEqual(restored, original) {
		t.Fatalf("restored history differs\n got: %#v\nwant: %#v", restored, original)
	}
}
