package core

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestToolCompleteTruncatesSnapshotOutput 验证实时截断链端到端：大工具
// 输出经 handleToolComplete 进入快照时被截断为预览 + result_ref，
// ToolResultContent 能读回全文；小输出原样保留。
func TestToolCompleteTruncatesSnapshotOutput(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	limit := snapshotToolOutputLimit()
	big := strings.Repeat("data-line\n", 4000) // ~44KB > 8KB

	service.handleToolStart(context.Background(), "bash", "tool-1", "{}")
	service.handleToolComplete("bash", "tool-1", big, nil, 1500*time.Millisecond)

	snapshot := service.Snapshot()
	var toolMessage *Message
	for index := range snapshot.Conversation {
		message := snapshot.Conversation[index]
		if message.Role == "tool_result" && message.Tool != nil && message.Tool.ID == "tool-1" {
			toolMessage = &snapshot.Conversation[index]
			break
		}
	}
	if toolMessage == nil {
		t.Fatal("tool message not found in snapshot")
	}
	tool := toolMessage.Tool
	if !tool.Truncated {
		t.Fatal("large output must be flagged truncated")
	}
	if tool.ResultRef == "" {
		t.Fatal("large output must carry result_ref")
	}
	if tool.TotalChars == 0 {
		t.Fatal("large output must carry total chars")
	}
	if len(tool.Result) > limit+128 {
		t.Fatalf("snapshot result too large: %d bytes (limit %d)", len(tool.Result), limit)
	}
	if toolMessage.Content != tool.Result {
		t.Fatal("message content must be the snapshot preview")
	}

	// 完整内容仍可读回（pending 通道，无需落盘）。
	page, err := service.ToolResultContent(t.Context(), tool.ResultRef, 0, 128)
	if err != nil {
		t.Fatalf("read back full content: %v", err)
	}
	if !strings.Contains(page.Content, "data-line") {
		t.Fatalf("page must contain original content, got %q", page.Content[:16])
	}

	// 小输出原样保留（无截断标记、无引用）。
	service.handleToolStart(context.Background(), "read_file", "tool-2", "{}")
	service.handleToolComplete("read_file", "tool-2", "tiny output", nil, 0)
	snapshot = service.Snapshot()
	for index := range snapshot.Conversation {
		message := snapshot.Conversation[index]
		if message.Role == "tool_result" && message.Tool != nil && message.Tool.ID == "tool-2" {
			tool = message.Tool
			break
		}
	}
	if tool.Truncated || tool.ResultRef != "" {
		t.Fatalf("small output must not be truncated: truncated=%v ref=%q", tool.Truncated, tool.ResultRef)
	}
	if tool.Result != "tiny output" {
		t.Fatalf("small output must pass through, got %q", tool.Result)
	}
}

// TestAppendHistoryLockedTruncatesRestoredOutput 验证会话恢复路径：恢复的
// 历史 tool 消息（大输出）同样只进预览 + 引用，避免重启后渲染进程吞全文。
func TestAppendHistoryLockedTruncatesRestoredOutput(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	limit := snapshotToolOutputLimit()
	big := strings.Repeat("restore-line-", 2000) // ~24KB
	history := []EngineMessage{
		{Role: "assistant", Content: "let me check"},
		{Role: "tool", ToolCallID: "call-restore-1", Name: "bash", Content: big},
	}

	service.Mu.Lock()
	service.appendHistoryLocked(history)
	service.Mu.Unlock()

	snapshot := service.Snapshot()
	for index := range snapshot.Conversation {
		message := snapshot.Conversation[index]
		if message.Role == "tool_result" && message.Tool != nil && message.Tool.ID == "call-restore-1" {
			if !message.Tool.Truncated || message.Tool.ResultRef == "" {
				t.Fatalf("restored large output must be truncated with ref: %+v", message.Tool)
			}
			if len(message.Tool.Result) > limit+128 {
				t.Fatalf("restored preview too large: %d bytes", len(message.Tool.Result))
			}
			return
		}
	}
	t.Fatal("restored tool_result message not found")
}
