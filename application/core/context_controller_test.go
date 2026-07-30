package core

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTruncateToolResultsPreservesPairingAndUTF8(t *testing.T) {
	history := []EngineMessage{
		{Role: "assistant", ToolCalls: []EngineToolCall{{ID: "call-1", Name: "read_file", Arguments: `{"path":"中文.txt"}`}}},
		{Role: "tool", ToolCallID: "call-1", Name: "read_file", Content: strings.Repeat("中🙂", 300), ContentSet: true},
	}

	truncated, changed := truncateToolResults(history, 200)
	if !changed {
		t.Fatal("expected oversized tool result to be truncated")
	}
	if got := truncated[1]; got.ToolCallID != "call-1" || got.Name != "read_file" || len(got.Content) > 200 || !utf8.ValidString(got.Content) {
		t.Fatalf("truncated tool result = %#v", got)
	}
	if got := truncated[0].ToolCalls[0]; got.ID != "call-1" || got.Arguments != `{"path":"中文.txt"}` {
		t.Fatalf("tool call protocol changed: %#v", got)
	}
}
