package session

import (
	"testing"
	"time"

	frameworkSession "github.com/RedHuang-0622/Seele/session"
	"github.com/RedHuang-0622/seelex/seelebridge/fork"
	"github.com/RedHuang-0622/seelex/seelebridge/internal/model"
	"github.com/RedHuang-0622/seelex/seelexctx/provider"
	"github.com/RedHuang-0622/seelex/seelexctx/snapshot"
)

// TestProjectionExportDoesNotHoldTreeLock 是 2026-09-07 死锁的确定性回归：
// 旧实现 Projection 持 SubagentTree.mu 时对运行中会话 ExportSnapshot，
// 而节点 ChatStream 持子会话锁执行首次装配 MarkStarted → MarkRunning 又
// 等树锁 → 锁序死锁（headless 真实 API 复现：子代理 running 后无任何
// llm/tool 事件）。本测试把实时导出替换为“导出中再取树锁”的钩子：
// 修复后导出发生在树锁之外，MarkRunning 能立即完成；若投影仍持锁则
// 必然死锁（由超时判定失败）。
func TestProjectionExportDoesNotHoldTreeLock(t *testing.T) {
	tree := NewSubagentTree(nil)
	tree.RegisterFork(model.MainAgentNodeID, []fork.SubagentSpec{
		{ID: "live-node", Goal: "live"},
	})
	// 运行中会话只需要一个非 nil 指针；实时导出已被下方钩子替换。
	live := frameworkSession.New(nil)
	tree.NoteSession("live-node", live)

	previous := exportSubagentSnapshot
	defer func() { exportSubagentSnapshot = previous }()
	exportStarted := make(chan struct{}, 1)
	exportSubagentSnapshot = func(_ provider.SessionSource, _ provider.TraceSource, _ string) *snapshot.ContextSnapshot {
		// 导出进行中尝试获取树锁：修复后此时树锁已释放，能立即成功；
		// 若投影仍持树锁（旧实现）这里会永久阻塞。
		exportStarted <- struct{}{}
		tree.MarkRunning("live-node")
		return nil
	}

	done := make(chan struct{}, 1)
	go func() {
		tree.Projection()
		done <- struct{}{}
	}()
	select {
	case <-exportStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("projection did not reach the export hook (deadlock before export)")
	}
	select {
	case <-done:
		// 投影完成；MarkRunning 已在导出钩子内成功（否则必然先超时）。
	case <-time.After(3 * time.Second):
		t.Fatal("projection deadlocked: tree lock held while exporting a running session")
	}
	if got := treeNodeStatus(t, tree, "live-node"); got != "running" {
		t.Fatalf("MarkRunning inside export hook did not take effect: status=%q", got)
	}
}

func treeNodeStatus(t *testing.T, tree *SubagentTree, id string) string {
	t.Helper()
	for _, node := range tree.Projection() {
		if found := findStatus(node, id); found != "" {
			return found
		}
	}
	return ""
}

func findStatus(node SubAgentTreeNode, id string) string {
	if node.ID == id {
		return string(node.Status)
	}
	for _, child := range node.Children {
		if found := findStatus(child, id); found != "" {
			return found
		}
	}
	return ""
}
