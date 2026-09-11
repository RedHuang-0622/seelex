package sessionstore

import (
	"strings"
	"testing"
	"time"
)

// derivedRowsFixture 复刻真实长会话的 message 行形状：一条 assistant 行里
// 可以带多个 tool_call，工具输出各自成行，回合末尾才有一条 llm 行。
func derivedRowsFixture() []Event {
	base := time.Date(2026, 9, 11, 13, 38, 0, 0, time.UTC)
	return []Event{
		{Seq: 1, MessageID: "message-1", Kind: EventKindUserInput, Role: "user", Content: "长会话恢复", CreatedAt: base},
		{Seq: 2, Kind: EventKindToolCall, Role: "assistant", ReasoningContent: "先看两个文件",
			ToolCalls: []EventToolCall{
				{ID: "call-a", Name: "read", Arguments: `{"path":"a.go"}`},
				{ID: "call-b", Name: "glob", Arguments: `{"pattern":"**/*.go"}`},
			}, CreatedAt: base},
		{Seq: 3, MessageID: "message-3", Kind: EventKindToolOutput, Role: "tool",
			ToolCallID: "call-a", Name: "read", Content: "package a", CreatedAt: base},
		{Seq: 4, MessageID: "message-4", Kind: EventKindToolOutput, Role: "tool",
			ToolCallID: "call-b", Name: "glob", Content: "a.go", CreatedAt: base},
		{Seq: 5, MessageID: "message-5", Kind: EventKindLLM, Role: "assistant", Content: "结论", CreatedAt: base},
	}
}

// TestDerivedConversationKeepsMessageOrder 是长会话恢复顺序的契约测试：派生
// 出来的可见会话必须按 message 行顺序展开，助手步骤（思考）→ 它发起的每个
// 工具调用 → 对应输出，而不是把行内多调用截断、把输出拆成两条。
func TestDerivedConversationKeepsMessageOrder(t *testing.T) {
	messages := derivedConversationMessages(derivedRowsFixture())

	shape := make([]string, 0, len(messages))
	for _, message := range messages {
		if message.Role == "tool_result" {
			// 结果消息按它回答的调用 ID 标注（配对键 = 框架 tool-call id）。
			shape = append(shape, "tool_result/"+message.Tool.ID)
			continue
		}
		if message.Role == "tool" {
			shape = append(shape, "tool/"+message.Tool.ID)
			continue
		}
		shape = append(shape, message.Role+"/"+message.ID)
	}
	want := []string{
		"user/message-1",
		"assistant/seq-2",
		"tool/call-a",
		"tool/call-b",
		"tool_result/call-a",
		"tool_result/call-b",
		"assistant/message-5",
	}
	if strings.Join(shape, " ") != strings.Join(want, " ") {
		t.Fatalf("conversation order =\n  %v\nwant\n  %v", shape, want)
	}

	// 行内每个 tool_call 都要保留参数，一个都不能丢。
	callArguments := map[string]string{}
	for _, message := range messages {
		if message.Role == "tool" && message.Tool != nil {
			callArguments[message.Tool.ID] = message.Tool.Arguments
		}
	}
	if callArguments["call-a"] != `{"path":"a.go"}` || callArguments["call-b"] != `{"pattern":"**/*.go"}` {
		t.Fatalf("tool call arguments = %#v，行内多调用必须逐条派生", callArguments)
	}

	// 助手步骤的思考内容必须随消息恢复，否则恢复后只剩工具痕迹。
	if messages[1].ReasoningContent != "先看两个文件" {
		t.Fatalf("assistant step reasoning = %q", messages[1].ReasoningContent)
	}

	// ID 唯一：同一行派生多条消息时不能复用行 ID（会撞前端 key）。
	seen := map[string]bool{}
	for _, message := range messages {
		if seen[message.ID] {
			t.Fatalf("duplicate derived message id %q in %#v", message.ID, shape)
		}
		seen[message.ID] = true
	}

	// 每条工具输出只派生一条结果消息，且结果正文进 Tool.Result。
	results := 0
	for _, message := range messages {
		if message.Role == "tool_result" {
			results++
			if message.Tool == nil || message.Tool.Result != message.Content {
				t.Fatalf("tool result = %#v", message)
			}
		}
	}
	if results != 2 {
		t.Fatalf("tool result count = %d, want 2", results)
	}
}
