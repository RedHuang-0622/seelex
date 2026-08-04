package core

import (
	"strings"
	"testing"

	"github.com/RedHuang-0622/Seele/types"
)

// TestAdaptSubagentConversation 验证会话记录适配：截断（evidence_chars）、
// 条数上限、工具消息携带 Tool 摘要。
func TestAdaptSubagentConversation(t *testing.T) {
	long := strings.Repeat("x", Limits().EvidenceChars+100)
	messages := []types.Message{
		{Role: "user", Content: strPtr("goal")},
		{Role: "assistant", Content: strPtr(long)},
		{Role: "assistant", Content: strPtr("calling"), ToolCallID: "t1", Name: "read_file"},
		{Role: "tool", Content: strPtr("file content"), ToolCallID: "t1", Name: "read_file"},
	}
	adapted := adaptSubagentConversation(messages)
	if len(adapted) != 4 {
		t.Fatalf("adapted = %d messages, want 4", len(adapted))
	}
	// 超长单条截断到 evidence_chars（字节）+ 省略号（… 为 3 字节 UTF-8）。
	if len(adapted[1].Content) > Limits().EvidenceChars+3 {
		t.Fatalf("oversized message not truncated: %d", len(adapted[1].Content))
	}
	// 工具消息携带 Tool 摘要（详情弹窗可渲染工具名）。
	if adapted[2].Tool == nil || adapted[2].Tool.Name != "read_file" {
		t.Fatalf("tool message must carry tool summary: %+v", adapted[2])
	}
}

// TestSubagentSessionDetailMissingNode 验证无节点/无会话时的错误路径。
func TestSubagentSessionDetailMissingNode(t *testing.T) {
	svc := newTestService(t, &fakeEngine{})
	if _, err := svc.SubagentSessionDetail(""); err == nil {
		t.Fatal("empty node id must be rejected")
	}
	if _, err := svc.SubagentSessionDetail("missing"); err == nil {
		t.Fatal("unknown node must return an error")
	}
}

func strPtr(value string) *string { return &value }
