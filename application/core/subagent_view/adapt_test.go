package subagent_view

import (
	"strings"
	"testing"

	"github.com/RedHuang-0622/Seele/types"
	"github.com/RedHuang-0622/seelex/application/contract/dto"
	"github.com/RedHuang-0622/seelex/application/core/internal/state"
	"github.com/RedHuang-0622/seelex/application/model"
	"github.com/RedHuang-0622/seelex/seelexctx"
	"github.com/RedHuang-0622/seelex/seelexctx/snapshot"
)

func testCoordinator() *Coordinator {
	return NewCoordinator(Deps{
		Core: &state.Core{},
		Limits: func() seelexctx.Limits {
			return seelexctx.DefaultLimits()
		},
	})
}

// TestAdaptSubagentConversation 验证会话记录适配：截断（evidence_chars）、
// 条数上限、工具消息携带 Tool 摘要。
func TestAdaptSubagentConversation(t *testing.T) {
	c := testCoordinator()
	limits := c.limits()
	long := strings.Repeat("x", limits.EvidenceChars+100)
	messages := []types.Message{
		{Role: "user", Content: strPtr("goal")},
		{Role: "assistant", Content: strPtr(long)},
		{Role: "assistant", Content: strPtr("calling"), ToolCallID: "t1", Name: "read_file"},
		{Role: "tool", Content: strPtr("file content"), ToolCallID: "t1", Name: "read_file"},
	}
	adapted := c.adaptSubagentConversation(messages)
	if len(adapted) != 4 {
		t.Fatalf("adapted = %d messages, want 4", len(adapted))
	}
	if len(adapted[1].Content) > limits.EvidenceChars+3 {
		t.Fatalf("oversized message not truncated: %d", len(adapted[1].Content))
	}
	if adapted[2].Tool == nil || adapted[2].Tool.Name != "read_file" {
		t.Fatalf("tool message must carry tool summary: %+v", adapted[2])
	}
}

// TestAdaptSubagentContext 验证上下文快照适配：截断、条目上限、空快照 → nil。
func TestAdaptSubagentContext(t *testing.T) {
	c := testCoordinator()
	if adapted := c.adaptSubagentContext(nil); adapted != nil {
		t.Fatalf("nil snapshot must adapt to nil, got %+v", adapted)
	}
	limits := c.limits()
	long := strings.Repeat("x", limits.EvidenceChars+100)
	findings := make([]string, maxSubagentContextItems+5)
	for index := range findings {
		findings[index] = long
	}
	adapted := c.adaptSubagentContext(&snapshot.ContextSnapshot{
		Goal:          long,
		Progress:      "in progress",
		MessageCount:  42,
		TokenEstimate: 1234,
		Findings:      findings,
		Decisions:     []snapshot.Decision{{What: long, Why: long}},
		Constraints:   []string{long},
		PendingWork:   []string{"work"},
	})
	if adapted == nil {
		t.Fatal("adapt must not return nil for non-nil snapshot")
	}
	if len(adapted.Goal) > limits.EvidenceChars+3 || len(adapted.Findings) != maxSubagentContextItems {
		t.Fatalf("truncation/limit failed: goal=%d findings=%d", len(adapted.Goal), len(adapted.Findings))
	}
	if adapted.MessageCount != 42 || adapted.TokenEstimate != 1234 || adapted.Progress != "in progress" {
		t.Fatalf("scalar fields = %+v", adapted)
	}
	if len(adapted.Decisions) != 1 || len(adapted.Decisions[0].What) > limits.EvidenceChars+3 {
		t.Fatalf("decisions adaptation failed: %+v", adapted.Decisions)
	}
	if len(adapted.Constraints) != 1 || len(adapted.PendingWork) != 1 {
		t.Fatalf("list fields = constraints %d pending %d", len(adapted.Constraints), len(adapted.PendingWork))
	}
}

// TestNodeStatusMappingFallbacks 验证树状态/任务状态 → 详情状态映射。
func TestNodeStatusMappingFallbacks(t *testing.T) {
	tests := []struct {
		sub  dto.SubAgentNodeStatus
		task string
		want model.NodeStatus
	}{
		{dto.SubAgentQueued, "", model.NodeQueued},
		{dto.SubAgentRunning, "running", model.NodeRunning},
		{dto.SubAgentDone, "completed", model.NodeCompleted},
		{dto.SubAgentFailed, "failed", model.NodeFailed},
		{dto.SubAgentInterrupted, "", model.NodeFailed},
	}
	for _, test := range tests {
		if got := nodeStatusFromSubagentStatus(test.sub); got != test.want {
			t.Fatalf("subagent status %q -> %q, want %q", test.sub, got, test.want)
		}
	}
	if got := nodeStatusFromTaskStatus("doing"); got != model.NodeRunning {
		t.Fatalf("task doing -> %q, want running", got)
	}
	if got := nodeStatusFromTaskStatus("mystery"); got != model.NodeUnknown {
		t.Fatalf("task mystery -> %q, want empty", got)
	}
}

// TestFindSubagentTreeNodeNested 验证递归查找子代理树节点。
func TestFindSubagentTreeNodeNested(t *testing.T) {
	nodes := []dto.SubAgentTreeNode{
		{ID: "main", Children: []dto.SubAgentTreeNode{
			{ID: "a"},
			{ID: "b", Children: []dto.SubAgentTreeNode{{ID: "b1"}}},
		}},
	}
	if found := findSubagentTreeNode(nodes, "b1"); found == nil || found.ID != "b1" {
		t.Fatalf("nested find failed: %+v", found)
	}
	if found := findSubagentTreeNode(nodes, "missing"); found != nil {
		t.Fatalf("missing node must be nil, got %+v", found)
	}
}

func strPtr(value string) *string { return &value }
