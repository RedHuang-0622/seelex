package seelexctx

import (
	"strings"
	"testing"

	"github.com/RedHuang-0622/Seele/types"
)

// interruptedChatTurn 构造残缺工具链 working history（与 sessionstore/task_context
// 探针同构）：user 任务 → assistant 请求 t1/t2 → 只记录 t1 结果。
func interruptedChatTurn(withSuffixText, atTail bool) []types.Message {
	messages := []types.Message{
		textMessage("user", "任务1：重构并验证"),
		{Role: "assistant", ToolCalls: []types.ToolCall{
			{ID: "t1", Function: types.ToolCallFunction{Name: "read_file"}},
			{ID: "t2", Function: types.ToolCallFunction{Name: "grep_search"}},
		}},
		{Role: "tool", ToolCallID: "t1", Name: "read_file", Content: stringPtr("file body")},
	}
	if withSuffixText {
		messages = append(messages, textMessage("assistant", "已完成第一步，继续"))
	}
	if !atTail {
		messages = append(messages,
			textMessage("user", "任务2：继续"),
			textMessage("assistant", "任务2完成"),
		)
	}
	return messages
}

// TestChatUnitsKeepsSuffixTextOfInterruptedChain：残缺工具链后的同轮文本与
// 下一轮不得被跳转丢弃（压缩溢出单元统计与投影依据）。
func TestChatUnitsKeepsSuffixTextOfInterruptedChain(t *testing.T) {
	history := interruptedChatTurn(true, false)
	units := chatUnits(history)
	if len(units) != 2 {
		t.Fatalf("units = %d, want 2: %+v", len(units), units)
	}
	first := units[0]
	if len(first.messages) != 4 || first.messages[0].Role != "user" || first.messages[3].Role != "assistant" {
		t.Fatalf("interrupted turn with suffix text must be one retained unit: %+v", first.messages)
	}
	if !strings.HasPrefix(*units[1].messages[0].Content, "任务2") {
		t.Fatalf("next user turn must survive: %+v", units[1].messages)
	}
}

// TestChatUnitsKeepsTailInterruptedChainAsOpenUnit：以残缺工具链收尾且前面
// 无文本终止点时，整轮（user + 已记录工具部分）必须构成单元，窗口溢出压缩
// 时不会被静默丢弃（可见会话仍显示该轮）。
func TestChatUnitsKeepsTailInterruptedChainAsOpenUnit(t *testing.T) {
	history := interruptedChatTurn(false, true)
	units := chatUnits(history)
	if len(units) != 1 {
		t.Fatalf("units = %d, want 1 open tail unit: %+v", len(units), units)
	}
	if len(units[0].messages) != 3 || units[0].messages[0].Role != "user" {
		t.Fatalf("tail unit must keep user+tc+partial result: %+v", units[0].messages)
	}
}

// TestPrepareReplaceHistoryRepairsInterruptedChain：装配 seam（ReplaceHistory
// 前）对残缺工具链补齐合成 tool 占位，再补空正文 —— 窗口历史替换后协议合法。
func TestPrepareReplaceHistoryRepairsInterruptedChain(t *testing.T) {
	history := []types.Message{
		textMessage("user", "任务1：重构并验证"),
		{Role: "assistant", ToolCalls: []types.ToolCall{
			{ID: "t1", Function: types.ToolCallFunction{Name: "read_file"}},
			{ID: "t2", Function: types.ToolCallFunction{Name: "grep_search"}},
		}},
		{Role: "tool", ToolCallID: "t1", Name: "read_file", Content: stringPtr("file body")},
		textMessage("assistant", "已完成第一步，继续"),
	}
	prepared := PrepareReplaceHistory(history)
	if len(prepared) != 5 {
		t.Fatalf("prepared length = %d, want 5 (user, tc, t1, t2 recovery, text)", len(prepared))
	}
	filled := prepared[3]
	if filled.Role != "tool" || filled.ToolCallID != "t2" || filled.Name != "grep_search" {
		t.Fatalf("missing result must be filled as tool t2: %+v", filled)
	}
	if filled.Content == nil || !strings.HasPrefix(*filled.Content, interruptedToolResultPrefix) {
		t.Fatalf("filled result must carry recovery prefix: %+v", filled)
	}
	if !IsProviderOnlyHistoryContent(*filled.Content) {
		t.Fatal("filled result must be recognized as provider-only")
	}
	if prepared[4].Role != "assistant" {
		t.Fatalf("suffix text must stay after the filled result: %+v", prepared[4])
	}
}
