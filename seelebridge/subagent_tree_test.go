package seelebridge

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/RedHuang-0622/Seele/agent"
	"github.com/RedHuang-0622/seelex/application/contract/dto"
	"github.com/RedHuang-0622/seelex/seelebridge/fork"
	"github.com/RedHuang-0622/seelex/seelexctx/snapshot"
)

// ── 子代理树（fork 内存态可视化，subagent_tree.go）──────────────────

// TestSubAgentTreeRegisterForkProjection 验证 fork 注册 parent/child 链并
// 投影为以主代理为根、含全部层级子节点的只读树（状态/goal/parent 挂在节点上）。
func TestSubAgentTreeRegisterForkProjection(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()

	runtime.subagentTree.RegisterFork(mainAgentNodeID, []fork.SubagentSpec{
		{ID: "s1", Goal: "audit module A"},
		{ID: "s2", Goal: "audit module B"},
	})
	tree := runtime.SubAgentTree()
	if len(tree) != 1 {
		t.Fatalf("tree roots = %d, want 1 (main)", len(tree))
	}
	root := tree[0]
	if root.ID != mainAgentNodeID {
		t.Fatalf("root id = %q, want %q", root.ID, mainAgentNodeID)
	}
	if len(root.Children) != 2 {
		t.Fatalf("root children = %d, want 2", len(root.Children))
	}
	if root.Children[0].ID != "s1" || root.Children[0].ParentID != mainAgentNodeID {
		t.Fatalf("child s1 = %+v", root.Children[0])
	}
	if root.Children[0].Goal != "audit module A" || root.Children[0].Status != dto.SubAgentQueued {
		t.Fatalf("child s1 goal/status = %+v", root.Children[0])
	}
	if root.Children[1].ID != "s2" || root.Status != dto.SubAgentRunning {
		t.Fatalf("root status must be running while any child runs: %+v", root)
	}
}

// TestSubAgentTreeContextProjection 验证紧凑上下文挂在树节点上：
// 结束后快照（noteSnapshot）直接复用；运行中节点经实时导出。
func TestSubAgentTreeContextProjection(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()
	runtime.subagentTree.RegisterFork(mainAgentNodeID, []fork.SubagentSpec{
		{ID: "done-node", Goal: "audit"},
		{ID: "live-node", Goal: "live"},
	})
	// 运行中节点：挂载一个会话（实时导出路径；子代理会话即独立 Session）。
	liveSession, err := runtime.NewMainSession(nil)
	if err != nil {
		t.Fatal(err)
	}
	runtime.subagentTree.NoteSession("live-node", liveSession)
	longFinding := strings.Repeat("x", 120+50)
	runtime.subagentTree.NoteSnapshot("done-node", &snapshot.ContextSnapshot{
		Goal: "audit", MessageCount: 7, TokenEstimate: 1234,
		Findings: []string{longFinding, "found race", "extra-1", "extra-2"},
	})

	done := treeNode(t, runtime, "done-node")
	if done.Context == nil {
		t.Fatal("ended node must carry compact context")
	}
	if done.Context.MessageCount != 7 || done.Context.TokenEstimate != 1234 {
		t.Fatalf("compact context = %+v", done.Context)
	}
	// 单条截断到 limit + 省略号；findings 上限 3 条。
	if len(done.Context.Findings[0]) != 120+3 {
		t.Fatalf("finding must be truncated to %d chars, got %d", 120+3, len(done.Context.Findings[0]))
	}
	if len(done.Context.Findings) != 3 {
		t.Fatalf("findings = %d, want %d", len(done.Context.Findings), 3)
	}
	// 运行中节点（有会话但无结束快照）→ 实时导出（有界兜底：无遥测 → 仅引擎面）。
	live := treeNode(t, runtime, "live-node")
	if live.Context == nil || live.Context.Goal != "live" {
		t.Fatalf("running node context = %+v", live.Context)
	}
	// 无记录节点 → 无上下文。
	unknown := treeNode(t, runtime, "unknown")
	if unknown.Context != nil {
		t.Fatal("unknown node must not carry context")
	}
}

// TestSubAgentTreeLifecycleTransitions 验证节点终态：成功 → done 有界保留
// （工作表格被动证据；详情数据面在 nodeSessions 注册表独立保留）；失败 →
// failed + 错误（保留现场供排查）；noteSession 挂载会话 ID。
func TestSubAgentTreeLifecycleTransitions(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()
	runtime.subagentTree.RegisterFork(mainAgentNodeID, []fork.SubagentSpec{
		{ID: "ok", Goal: "g1"},
		{ID: "bad", Goal: "g2"},
	})

	// 运行中挂载会话（registerNodeSession 路径）。
	runtime.subagentTree.NoteSession("ok", nil) // nil 会话 no-op，不 panic
	if got := treeNode(t, runtime, "ok"); got.Status != dto.SubAgentQueued {
		t.Fatalf("spawned node must stay queued before its session starts, got %+v", got)
	}
	// 真实会话挂载仍保持 queued（挂载 ≠ 执行）。
	liveSession, err := runtime.NewMainSession(nil)
	if err != nil {
		t.Fatal(err)
	}
	runtime.subagentTree.NoteSession("ok", liveSession)
	if got := treeNode(t, runtime, "ok"); got.Status != dto.SubAgentQueued {
		t.Fatalf("noteSession must keep queued (not executing yet), got %+v", got)
	}
	// 首次请求组装（SSE 流开启）→ queued 转 running。
	runtime.node.MarkStarted("ok")
	if got := treeNode(t, runtime, "ok"); got.Status != dto.SubAgentRunning {
		t.Fatalf("markNodeStarted must transition queued -> running, got %+v", got)
	}

	runtime.node.CompleteSubagentNode("ok", "audit done", nil)
	runtime.node.CompleteSubagentNode("bad", "partial", errors.New("boom"))

	// done 节点有界保留：状态 done + 摘要，供工作表格被动展示。
	okNode := treeNode(t, runtime, "ok")
	if okNode.Status != dto.SubAgentDone || okNode.Summary != "audit done" {
		t.Fatalf("done node must be retained with done status, got %+v", okNode)
	}
	bad := treeNode(t, runtime, "bad")
	if bad.Status != dto.SubAgentFailed || bad.Summary != "partial" || !strings.Contains(bad.Error, "boom") {
		t.Fatalf("failed node = %+v", bad)
	}

	// 非 fork 节点（plan_run 的 agent 节点未入树）→ no-op。
	runtime.node.CompleteSubagentNode("not-a-fork-node", "x", nil)
	if treeNode(t, runtime, "not-a-fork-node").ID != "" {
		t.Fatal("unknown node must not appear in the tree")
	}
	// 仅剩失败子代理 → 主代理合成根状态 failed。
	if root := runtime.SubAgentTree()[0]; root.Status != dto.SubAgentFailed {
		t.Fatalf("main status with failed child = %+v", root.Status)
	}
	// 失败节点可手动清空（清空入口：ClearSubagentTree）。
	if err := runtime.subagentTree.Clear(); err != nil {
		t.Fatalf("clear tree: %v", err)
	}
	if tree := runtime.SubAgentTree(); tree != nil {
		t.Fatalf("tree must be empty after clear, got %+v", tree)
	}
}

// TestSubAgentTreeClearRemovesEverything 清空入口：整树（含失败节点与
// 嵌套层级）一次移除。
func TestSubAgentTreeClearRemovesEverything(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()
	runtime.subagentTree.RegisterFork(mainAgentNodeID, []fork.SubagentSpec{{ID: "a", Goal: "top"}})
	runtime.subagentTree.RegisterFork("a", []fork.SubagentSpec{{ID: "a1", Goal: "nested"}})
	runtime.node.CompleteSubagentNode("a1", "nested done", errors.New("boom"))
	if err := runtime.subagentTree.Clear(); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if tree := runtime.SubAgentTree(); tree != nil {
		t.Fatalf("tree must be empty after clear, got %+v", tree)
	}
}

// TestSubAgentTreeNestedFork 验证嵌套 fork：子代理再 fork → 三层树，
// 中间层父节点挂接正确；嵌套子代理完成后保留为 done（有界）。
func TestSubAgentTreeNestedFork(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()
	runtime.subagentTree.RegisterFork(mainAgentNodeID, []fork.SubagentSpec{{ID: "a", Goal: "top"}})
	runtime.subagentTree.RegisterFork("a", []fork.SubagentSpec{{ID: "a1", Goal: "nested"}})

	// 嵌套子代理运行中 → 挂在父节点 a 下。
	root := runtime.SubAgentTree()[0]
	if len(root.Children) != 1 || root.Children[0].ID != "a" {
		t.Fatalf("root children = %+v", root.Children)
	}
	child := root.Children[0]
	if len(child.Children) != 1 || child.Children[0].ID != "a1" || child.Children[0].ParentID != "a" {
		t.Fatalf("nested child = %+v", child.Children)
	}
	if child.Children[0].Status != dto.SubAgentQueued {
		t.Fatalf("nested child status = %+v, want queued (session not started yet)", child.Children[0].Status)
	}
	// 父运行中子代理未完成 → 主代理根保持 running。
	if root.Status != dto.SubAgentRunning {
		t.Fatalf("main status = %+v, want running", root.Status)
	}

	// 嵌套子代理完成 → 保留为 done（有界；工作表格证据）。
	runtime.node.CompleteSubagentNode("a1", "nested done", nil)
	root = runtime.SubAgentTree()[0]
	if len(root.Children) != 1 || root.Children[0].ID != "a" {
		t.Fatalf("root children after nested done = %+v", root.Children)
	}
	if len(root.Children[0].Children) != 1 || root.Children[0].Children[0].Status != dto.SubAgentDone {
		t.Fatalf("nested done child must be retained as done, got %+v", root.Children[0].Children)
	}
}

// TestSubAgentTreeEmptyAndOrphan 验证空树返回 nil；孤儿节点（父已不在
// 注册表）归到主代理下，树保持完整。
func TestSubAgentTreeEmptyAndOrphan(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()
	if tree := runtime.SubAgentTree(); tree != nil {
		t.Fatalf("empty tree = %+v, want nil", tree)
	}
	// 模拟父节点缺失（正常路径不会发生，防御性兜底）：父节点未注册 → 孤儿归主代理。
	runtime.subagentTree.RegisterFork("gone", []fork.SubagentSpec{{ID: "orphan", Goal: "x"}})
	runtime.subagentTree.CompleteSubagentNode("orphan", "done", nil)
	root := runtime.SubAgentTree()[0]
	if len(root.Children) != 1 || root.Children[0].ID != "orphan" || root.Children[0].ParentID != "gone" {
		t.Fatalf("orphan attribution = %+v", root.Children)
	}
	// nil Runtime 安全。
	var nilRuntime *Runtime
	if tree := nilRuntime.SubAgentTree(); tree != nil {
		t.Fatal("nil runtime tree must be nil")
	}
}

// TestForkSubagentsRecordsTree 验证 fork 全链路：两个并行子代理完成后树
// 保留 done 节点（有界），详情数据面（上下文快照/会话记录）仍在
// nodeSessions 注册表独立保留。
func TestForkSubagentsRecordsTree(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()
	runtime.RegisterBuiltins()
	injectScriptedCompleters(t, runtime, map[string]agent.Completer{
		"sub-1": newScriptedNodeCompleter("fork-left: audit module A done"),
		"sub-2": newScriptedNodeCompleter("fork-right: audit module B done"),
	})

	result, err := runtime.Agent().DirectDispatch(context.Background(), "fork_subagents",
		`{"subagents":[{"id":"s1","goal":"audit module A"},{"id":"s2","goal":"audit module B"}]}`)
	if err != nil {
		t.Fatalf("fork_subagents failed: %v", err)
	}
	if !strings.Contains(result, `"status":"completed"`) {
		t.Fatalf("fork result must be completed, got: %s", result)
	}
	// 两个子代理都完成后树保留 done 节点（工作表格被动证据）。
	tree := runtime.SubAgentTree()
	if tree == nil || len(tree[0].Children) != 2 {
		t.Fatalf("tree must retain done fork nodes, got %+v", tree)
	}
	for _, child := range tree[0].Children {
		if child.Status != dto.SubAgentDone || !strings.Contains(child.Summary, "done") {
			t.Fatalf("fork node must be done with summary, got %+v", child)
		}
	}
	// 详情数据面独立保留：完整快照/会话记录仍可经 nodeSessions 注册表读取。
	snap, ok := runtime.NodeContextSnapshot("s1")
	if !ok || snap == nil || snap.Goal != "audit module A" {
		t.Fatalf("node context snapshot missing after fork: %+v", snap)
	}
}

// TestSubAgentTreeEventsChannelNotifiesOnForkLifecycle 验证被动触发（CSP）：
// fork 注册与节点完成都经 channel 投递信号（application 消费者借此自动
// 刷新工作表格，不依赖模型主观意愿）。
func TestSubAgentTreeEventsChannelNotifiesOnForkLifecycle(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()

	events := runtime.SubagentTreeEvents()
	received := 0
	waitSignal := func(want int) {
		t.Helper()
		deadline := time.After(time.Second)
		for received < want {
			select {
			case <-events:
				received++
			case <-deadline:
				t.Fatalf("expected %d lifecycle signals, got %d", want, received)
			}
		}
	}

	runtime.subagentTree.RegisterFork(mainAgentNodeID, []fork.SubagentSpec{{ID: "s1", Goal: "g"}})
	waitSignal(1) // fork 注册

	runtime.node.CompleteSubagentNode("s1", "done", nil)
	waitSignal(2) // 节点完成

	// nil Runtime 安全。
	var nilRuntime *Runtime
	if got := nilRuntime.SubagentTreeEvents(); got != nil {
		t.Fatal("nil runtime must return nil events channel")
	}
}

// TestSubAgentTreeRetainsBoundedDoneNodes 验证 done 节点有界保留：
// 超 subagentTreeRetainDone 清理最旧 done；failed 不受上限影响。
func TestSubAgentTreeRetainsBoundedDoneNodes(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()

	specs := make([]fork.SubagentSpec, 0, 55)
	for index := 0; index < 55; index++ {
		specs = append(specs, fork.SubagentSpec{ID: fmt.Sprintf("s%d", index), Goal: "g"})
	}
	runtime.subagentTree.RegisterFork(mainAgentNodeID, specs)
	for index := 0; index < 55; index++ {
		runtime.node.CompleteSubagentNode(fmt.Sprintf("s%d", index), "done", nil)
	}

	count := countSubagentTreeNodes(t, runtime)
	if count != 50 {
		t.Fatalf("retained done nodes = %d, want %d", count, 50)
	}

	// failed 节点不受 done 上限影响。
	runtime.subagentTree.RegisterFork(mainAgentNodeID, []fork.SubagentSpec{{ID: "f1", Goal: "g"}})
	runtime.node.CompleteSubagentNode("f1", "x", errors.New("boom"))
	if got := treeNode(t, runtime, "f1"); got.Status != dto.SubAgentFailed {
		t.Fatalf("failed node = %+v, want failed", got)
	}
}

func countSubagentTreeNodes(t *testing.T, runtime *Runtime) int {
	t.Helper()
	tree := runtime.SubAgentTree()
	if tree == nil {
		return 0
	}
	count := 0
	var walk func(items []dto.SubAgentTreeNode)
	walk = func(items []dto.SubAgentTreeNode) {
		for _, item := range items {
			if item.ID != mainAgentNodeID {
				count++
			}
			walk(item.Children)
		}
	}
	walk(tree)
	return count
}

// treeNode 从投影树中按 id 查找节点；未找到返回零值节点。
func treeNode(t *testing.T, runtime *Runtime, id string) dto.SubAgentTreeNode {
	t.Helper()
	var find func(items []dto.SubAgentTreeNode) dto.SubAgentTreeNode
	find = func(items []dto.SubAgentTreeNode) dto.SubAgentTreeNode {
		for _, item := range items {
			if item.ID == id {
				return item
			}
			if found := find(item.Children); found.ID != "" {
				return found
			}
		}
		return dto.SubAgentTreeNode{}
	}
	tree := runtime.SubAgentTree()
	if len(tree) == 0 {
		return dto.SubAgentTreeNode{}
	}
	return find(tree)
}
