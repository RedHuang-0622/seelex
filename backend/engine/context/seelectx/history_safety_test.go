package seelexctx

import (
	"strings"
	"testing"

	"github.com/RedHuang-0622/Seele/types"
)

func TestPrepareReplaceHistoryRemovesControlMarkers(t *testing.T) {
	checkpoint := textMessage("user", checkpointMarker+"\n{\"version\":1}")
	compact := textMessage("user", compactContextMarker+"\n{\"segment_id\":\"c\"}")
	history := []types.Message{
		checkpoint,
		textMessage("user", "正常用户消息"),
		compact,
		textMessage("assistant", "正常回复"),
	}
	prepared := PrepareReplaceHistory(history)
	if len(prepared) != 2 {
		t.Fatalf("prepared length = %d, want 2 (control blocks removed)", len(prepared))
	}
	for _, message := range prepared {
		if message.Content != nil &&
			(strings.HasPrefix(*message.Content, checkpointMarker) ||
				strings.HasPrefix(*message.Content, compactContextMarker)) {
			t.Fatal("context control blocks must be removed before ReplaceHistory")
		}
	}
}

func TestRepairEmptyHistoryContentPairingRules(t *testing.T) {
	content := "有内容"
	history := []types.Message{
		// assistant + tool_calls 缺正文 → 工具调用配对文本。
		{Role: "assistant", ToolCalls: []types.ToolCall{{ID: "c1", Function: types.ToolCallFunction{Name: "read_file"}}}},
		// assistant 仅推理正文 → 缺失标记。
		{Role: "assistant", ReasoningContent: "推理过程"},
		// 普通空正文 → 缺失标记。
		textMessage("assistant", ""),
		textMessage("tool", ""),
		textMessage("user", ""),
		// 非空正文 → 原样保留。
		textMessage("user", content),
	}
	prepared := repairEmptyHistoryContent(history)
	if prepared[0].Content == nil || *prepared[0].Content != toolCallHistoryContent {
		t.Fatalf("assistant+toolcalls empty content = %v, want toolCallHistoryContent", prepared[0].Content)
	}
	if prepared[1].Content == nil || *prepared[1].Content != missingHistoryContent {
		t.Fatalf("assistant reasoning-only content = %v, want missingHistoryContent", prepared[1].Content)
	}
	for index := 2; index <= 4; index++ {
		if prepared[index].Content == nil || *prepared[index].Content != missingHistoryContent {
			t.Fatalf("empty %s content = %v, want missingHistoryContent", history[index].Role, prepared[index].Content)
		}
	}
	if prepared[5].Content == nil || *prepared[5].Content != content {
		t.Fatalf("non-empty content must be preserved, got %v", prepared[5].Content)
	}
}

func TestRepairPreservesToolCalls(t *testing.T) {
	history := []types.Message{{
		Role:      "assistant",
		ToolCalls: []types.ToolCall{{ID: "c1", Function: types.ToolCallFunction{Name: "bash", Arguments: "{}"}}},
	}}
	prepared := repairEmptyHistoryContent(history)
	if len(prepared[0].ToolCalls) != 1 {
		t.Fatal("tool calls must be retained during pairing repair")
	}
	if prepared[0].ToolCalls[0].ID != "c1" {
		t.Fatalf("tool call ID changed: %q", prepared[0].ToolCalls[0].ID)
	}
}

func TestIsProviderOnlyHistoryContent(t *testing.T) {
	if !IsProviderOnlyHistoryContent(missingHistoryContent) {
		t.Fatal("missingHistoryContent must be provider-only")
	}
	if !IsProviderOnlyHistoryContent(toolCallHistoryContent) {
		t.Fatal("toolCallHistoryContent must be provider-only")
	}
	if IsProviderOnlyHistoryContent("真实回复") {
		t.Fatal("real content must not be provider-only")
	}
}
