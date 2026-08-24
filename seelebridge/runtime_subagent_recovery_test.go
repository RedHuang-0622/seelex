package seelebridge

import (
	"context"
	"path/filepath"
	"testing"

	frameworkSession "github.com/RedHuang-0622/Seele/session"
	"github.com/RedHuang-0622/Seele/types"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
	"github.com/RedHuang-0622/seelex/seelebridge/internal/model"
	"github.com/RedHuang-0622/seelex/sessionstore"
)

// recoveryTestCompleter 是最小 ChatCompleter（组件测试不发起真实模型调用）。
type recoveryTestCompleter struct{}

func (recoveryTestCompleter) Complete(context.Context, []types.Message, []types.Tool) (types.Message, error) {
	return types.Message{Role: "assistant"}, nil
}

func (recoveryTestCompleter) CompleteStream(context.Context, []types.Message, []types.Tool, func(string)) (string, string, []types.ToolCall, error) {
	return "", "", nil, nil
}

func (recoveryTestCompleter) CompleteStreamEvents(context.Context, []types.Message, []types.Tool, func(types.StreamEvent)) (string, string, []types.ToolCall, error) {
	return "", "", nil, nil
}

type recoveryTestAgent struct{ llm types.ChatCompleter }

func (a recoveryTestAgent) VisibleTools(context.Context) []types.Tool { return nil }
func (a recoveryTestAgent) Dispatch(context.Context, string, string) (string, error) {
	return "", nil
}
func (a recoveryTestAgent) LLM() types.ChatCompleter { return a.llm }

// TestSubagentConclusionFollowsMainAndAnchorsRebuild 验证生命周期闭环：
// 运行期记录落盘 → 结束结论写入主会话事件库 → 记录删除 → 重启后
// RestoreSubagentAnchors 从结论事件重建树（含 subSessionID，认领可回填）。
func TestSubagentConclusionFollowsMainAndAnchorsRebuild(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()

	router, err := sessionstore.NewRouter(filepath.Join(t.TempDir(), "session-storage.json"), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer router.Close()
	eventStore := sessionstore.NewEventStore(router)
	nodeStore := sessionstore.NewNodeSessionStore(router)

	runtime.AttachHistoryRouter(router)
	runtime.AttachSubSessionStore(nodeStore)
	runtime.SetEventPersister(eventStore.Append)
	if _, err := runtime.NewMainSessionWithID("sess_main", nil); err != nil {
		t.Fatalf("new main session: %v", err)
	}

	// 模拟一个子代理节点：注册 → 阶段 → 结果 → 终态 → 结束。
	sess, err := frameworkSession.NewSession(frameworkSession.SessionComponents{
		Agent:     recoveryTestAgent{llm: recoveryTestCompleter{}},
		SessionID: "node-abc",
	})
	if err != nil {
		t.Fatal(err)
	}
	runtime.subagentSessions.Register("n1", sess, "inspect")
	runtime.subagentSessions.RecordResult("n1", &model.NodeSemanticResult{
		SchemaVersion: model.NodeSemanticSchemaVersion,
		NodeID:        "n1", SessionID: "node-abc", Status: "completed",
		Summary: "done", Findings: []string{"f1"},
	})
	runtime.subagentSessions.NoteOutcome("n1", "done", "done", "")
	// Unregister 同步执行：结论回传 + 记录删除。
	runtime.subagentSessions.Unregister("n1")

	// 结论已入主会话事件库（seelex.subagent.result），记录文件已删除。
	events, err := eventStore.Load(context.Background(), "sess_main")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, event := range events {
		if event.Source == subagentConclusionEventSource && event.Scope.NodeID == "n1" {
			found = true
		}
	}
	if !found {
		t.Fatalf("conclusion event missing from main event rail: %+v", events)
	}
	if _, err := nodeStore.Load(router.Workspace(), "sess_main", "node-abc"); err == nil {
		t.Fatal("node session record must be deleted after finish")
	}

	// 重启后：清树 → RestoreSubagentAnchors → 树含节点（SessionID 可认领）。
	if err := runtime.ClearSubagentTree(); err != nil {
		t.Fatal(err)
	}
	if err := runtime.RestoreSubagentAnchors("sess_main"); err != nil {
		t.Fatalf("restore anchors: %v", err)
	}
	tree := runtime.SubAgentTree()
	node := findTreeNode(tree, "n1")
	if node == nil {
		t.Fatalf("restored tree missing node n1: %+v", tree)
	}
	if node.SessionID != "node-abc" || node.Status != dto.SubAgentDone {
		t.Fatalf("restored node = %+v, want sessionID=node-abc status=done", node)
	}
}

func findTreeNode(nodes []dto.SubAgentTreeNode, id string) *dto.SubAgentTreeNode {
	for index := range nodes {
		if nodes[index].ID == id {
			return &nodes[index]
		}
		if found := findTreeNode(nodes[index].Children, id); found != nil {
			return found
		}
	}
	return nil
}
