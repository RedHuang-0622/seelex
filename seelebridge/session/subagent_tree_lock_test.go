package session

import (
	"context"
	"sync"
	"testing"
	"time"

	frameworkSession "github.com/RedHuang-0622/Seele/session"
	"github.com/RedHuang-0622/Seele/types"
	"github.com/RedHuang-0622/seelex/seelebridge/fork"
	"github.com/RedHuang-0622/seelex/seelebridge/internal/model"
)

// blockingChatCompleter 是“长流式”的可控替身：进入 CompleteStream 后一直阻塞
// 到 ctx 取消。它复现“整段 ChatStream 期间持有会话锁”的现场——真实场景见
// docs/test/REPORT-a2a-agentteam-subagent-recovery-2026-09-10.md §4.4：
// 子代理跑 42s 长文流式时，Session.mu 被 ChatStream 从进函数持到出函数。
type blockingChatCompleter struct {
	entered chan struct{}
	once    sync.Once
}

func newBlockingChatCompleter() *blockingChatCompleter {
	return &blockingChatCompleter{entered: make(chan struct{})}
}

func (c *blockingChatCompleter) Complete(ctx context.Context, _ []types.Message, _ []types.Tool) (types.Message, error) {
	c.once.Do(func() { close(c.entered) })
	<-ctx.Done()
	return types.Message{}, ctx.Err()
}

func (c *blockingChatCompleter) CompleteStream(ctx context.Context, _ []types.Message, _ []types.Tool, _ func(string)) (string, string, []types.ToolCall, error) {
	c.once.Do(func() { close(c.entered) })
	<-ctx.Done()
	return "", "", nil, ctx.Err()
}

func (c *blockingChatCompleter) CompleteStreamEvents(ctx context.Context, messages []types.Message, tools []types.Tool, _ func(types.StreamEvent)) (string, string, []types.ToolCall, error) {
	return c.CompleteStream(ctx, messages, tools, nil)
}

// blockingAgent 是只提供阻塞 LLM 的最小 Agent 实现（SessionComponents 要求
// Agent 与其 LLM 均非 nil；工具面在本用例中不被调用）。
type blockingAgent struct {
	llm types.ChatCompleter
}

func (a blockingAgent) VisibleTools(context.Context) []types.Tool { return nil }
func (a blockingAgent) Dispatch(context.Context, string, string) (string, error) {
	return "", nil
}
func (a blockingAgent) LLM() types.ChatCompleter { return a.llm }

// TestProjectionDoesNotBlockOnLiveSessionStream 是 2026-09-10 报告 §4.4 长流
// 热点的确定性回归：子代理整段流式期间会话锁被 ChatStream 持有，观测面
// （SubagentTree.Projection）必须读节点自己的无锁增量面，不得在运行中会话上
// 取会话锁——否则表格投影被长流挡住数十秒（pprof 实测 28.19s，表现为“表格
// 卡住不动”）。用例只依赖公开行为：真会话真的在流、真的持锁，投影必须及时返回。
func TestProjectionDoesNotBlockOnLiveSessionStream(t *testing.T) {
	tree := NewSubagentTree(nil)
	tree.RegisterFork(model.MainAgentNodeID, []fork.SubagentSpec{
		{ID: "live-node", Goal: "long stream"},
	})

	completer := newBlockingChatCompleter()
	live, err := frameworkSession.NewSession(frameworkSession.SessionComponents{
		Agent: blockingAgent{llm: completer},
	})
	if err != nil {
		t.Fatalf("new live session: %v", err)
	}
	tree.NoteSession("live-node", live)

	ctx, cancel := context.WithCancel(context.Background())
	streamDone := make(chan struct{})
	go func() {
		defer close(streamDone)
		// ChatStream 持会话锁跑整个 ReAct 循环；completer 阻塞即锁被持有。
		_, _ = live.ChatStream(ctx, "start", func(string) {})
	}()
	defer func() {
		cancel()
		select {
		case <-streamDone:
		case <-time.After(5 * time.Second):
		}
	}()

	select {
	case <-completer.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("blocking completer never entered: live stream did not start")
	}
	// 节点首次组装请求（SSE 流开启）→ queued 转 running；该路径发生在会话锁内。
	tree.MarkRunning("live-node")

	type projectionResult struct {
		nodes []SubAgentTreeNode
	}
	projected := make(chan projectionResult, 1)
	go func() { projected <- projectionResult{nodes: tree.Projection()} }()

	select {
	case result := <-projected:
		liveNode := findTreeNode(result.nodes, "live-node")
		if liveNode == nil {
			t.Fatalf("projection missing live-node: %+v", result.nodes)
		}
		if liveNode.Status != SubAgentRunning {
			t.Fatalf("live-node status = %q, want running", liveNode.Status)
		}
		if liveNode.Context == nil || liveNode.Context.Goal != "long stream" {
			t.Fatalf("live-node compact context = %+v, want goal carried", liveNode.Context)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Projection 在运行中子代理的会话锁上被阻塞（观测面等执行面）")
	}
}

func findTreeNode(nodes []SubAgentTreeNode, id string) *SubAgentTreeNode {
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

// TestProjectionCarriesNodeReportedMessageCount 覆盖 2026-09-10 长流修复后的
// 运行中数据面：投影的消息数来自节点自己上报的计数（NoteMessageCount），
// 不再读运行中子会话。这条用例与
// TestProjectionDoesNotBlockOnLiveSessionStream 配对——后者证明不阻塞，
// 本用例证明不阻塞的同时数据仍然到位（GUI 工作表格/详情可见）。
func TestProjectionCarriesNodeReportedMessageCount(t *testing.T) {
	tree := NewSubagentTree(nil)
	tree.RegisterFork(model.MainAgentNodeID, []fork.SubagentSpec{
		{ID: "live-node", Goal: "live"},
	})
	live := frameworkSession.New(nil)
	tree.NoteSession("live-node", live)
	tree.MarkRunning("live-node")
	tree.NoteMessageCount("live-node", 5)

	node := findTreeNode(tree.Projection(), "live-node")
	if node == nil {
		t.Fatal("projection missing live-node")
	}
	if node.Status != SubAgentRunning {
		t.Fatalf("live-node status = %q, want running", node.Status)
	}
	if node.Context == nil {
		t.Fatal("running node must carry compact context")
	}
	if node.Context.MessageCount != 5 {
		t.Fatalf("live message count = %d, want node-reported 5", node.Context.MessageCount)
	}
	if node.Context.Goal != "live" {
		t.Fatalf("live context goal = %q, want live", node.Context.Goal)
	}
}
