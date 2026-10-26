package observability

import (
	"testing"

	model "github.com/RedHuang-0622/seelex/contract/protocol"
)

func TestPerfStatsCountsConversationAndArchive(t *testing.T) {
	snapshot := model.Snapshot{
		Revision: 7,
		Conversation: []model.Message{
			{Role: "user", Content: "hello"},
			{Role: "tool", Content: "x", Tool: &model.ToolCall{Arguments: "a", Result: "rr", Truncated: true}},
		},
	}
	refs := []model.ToolResultRef{{Size: 100}, {Size: 23}}

	stats := PerfStats(snapshot, 42, refs)
	if stats.ConversationMessages != 2 {
		t.Errorf("ConversationMessages = %d, want 2", stats.ConversationMessages)
	}
	// 5 ("hello") + 1 + 1 + 2 ("x" + 参数 + 结果)
	if stats.ConversationChars != 9 {
		t.Errorf("ConversationChars = %d, want 9", stats.ConversationChars)
	}
	if stats.LargestMessageChars != 5 {
		t.Errorf("LargestMessageChars = %d, want 5", stats.LargestMessageChars)
	}
	if stats.TruncatedOutputs != 1 {
		t.Errorf("TruncatedOutputs = %d, want 1", stats.TruncatedOutputs)
	}
	if stats.HistoryWindow != 42 {
		t.Errorf("HistoryWindow = %d, want 42 (host-supplied)", stats.HistoryWindow)
	}
	if stats.Revision != 7 {
		t.Errorf("Revision = %d, want 7", stats.Revision)
	}
	if stats.ToolResults != 2 || stats.ArchivedBytes != 123 {
		t.Errorf("archive readings = (%d, %d), want (2, 123)", stats.ToolResults, stats.ArchivedBytes)
	}
	if stats.SnapshotBytes <= 0 {
		t.Errorf("SnapshotBytes = %d, want > 0 (JSON estimate)", stats.SnapshotBytes)
	}
}
