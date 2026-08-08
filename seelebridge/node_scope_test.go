package seelebridge

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RedHuang-0622/Seele/accountpool"
	"github.com/RedHuang-0622/Seele/agent"
	"github.com/RedHuang-0622/Seele/agent/bridge"
	"github.com/RedHuang-0622/Seele/seelectx"
	"github.com/RedHuang-0622/Seele/types"
	"github.com/RedHuang-0622/Seele/workplan/codec"
)

// ── NodeScope 上下文助手 ─────────────────────────────────────────────

func TestNodeScopeContextRoundTrip(t *testing.T) {
	ctx := context.Background()
	if scope, ok := NodeScopeFromContext(ctx); ok {
		t.Fatalf("empty ctx must not carry a scope: %+v", scope)
	}
	scope := NodeScope{NodeID: "left", Role: RoleSubAgent, BranchID: "left", WorkspaceID: "w1"}
	child := WithNodeScope(ctx, scope)
	got, ok := NodeScopeFromContext(child)
	if !ok || got != scope {
		t.Fatalf("scope round-trip = %+v ok=%v, want %+v", got, ok, scope)
	}
	if empty := nodeScopeFromContextOrEmpty(ctx); empty.NodeID != "" {
		t.Fatalf("empty ctx scope = %+v, want empty", empty)
	}
	if filled := nodeScopeFromContextOrEmpty(child); filled != scope {
		t.Fatalf("or-empty scope = %+v, want %+v", filled, scope)
	}

	// 节点 PromptBlocks 注入/读取（拷贝隔离）。
	blocks := []seelectx.PromptBlock{{Name: "node-goal"}}
	withBlocks := withNodePromptBlocks(child, blocks)
	blocks[0].Name = "mutated"
	gotBlocks := nodePromptBlocksFromContext(withBlocks)
	if len(gotBlocks) != 1 || gotBlocks[0].Name != "node-goal" {
		t.Fatalf("block round-trip = %+v", gotBlocks)
	}
	if got := nodePromptBlocksFromContext(child); len(got) != 0 {
		t.Fatalf("ctx without blocks = %+v", got)
	}
}

// ── 子代理工具可见性（NodeScope → VisibilityPolicy）──────────────────

func TestNodeScopeVisibilityFiltersSubagentTools(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()
	runtime.RegisterBuiltins()
	registerNamedTool(t, runtime, "task_complete")
	registerNamedTool(t, runtime, "web_search")

	// 主代理（无 NodeScope）：默认面 = 完整工具面减去 plan 工具族
	// （goal skill 未激活时 plan 隐藏，plan.md §6；显式关闭测试基座的默认激活）。
	runtime.SetRuntimeVisibilityProjection(RuntimeVisibilityProjection{})
	all := toolNames(runtime.VisibleTools(context.Background()))
	for _, want := range []string{"read_file", "task_complete", "web_search", "todolist_init", "fork_subagents"} {
		if !containsName(all, want) {
			t.Fatalf("main agent tools = %v, missing %q", all, want)
		}
	}
	for _, hidden := range []string{"plan_run", "plan_load", "plan_clear", "plan_status", "plan_export", "plan_validate"} {
		if containsName(all, hidden) {
			t.Fatalf("main agent tools = %v, plan family must be hidden without goal skill", all)
		}
	}
	// 子代理（RoleSubAgent）：与主代理能力一致（完整工具面），仅排除
	// 操作全局状态的工具（plan 工具族 / task 终态工具）。
	subCtx := WithNodeScope(context.Background(), NodeScope{
		NodeID: "left", Role: RoleSubAgent, BranchID: "left", WorkspaceID: "w1",
	})
	scoped := toolNames(runtime.VisibleTools(subCtx))
	// 完整工具面：项目工具 + 注册的普通工具（web_search）均可见。
	for _, want := range []string{"read_file", "grep_search", "glob", "write_file", "edit_file", "bash", "web_search"} {
		if !containsName(scoped, want) {
			t.Errorf("subagent tools = %v, missing tool %q", scoped, want)
		}
	}
	// 仅排除操作全局状态的工具（plan 工具族 / task 终态工具）。
	for _, hidden := range []string{"plan_run", "plan_load", "plan_clear", "plan_status", "plan_export", "plan_validate", "task_complete", "task_failed", "task_needs_user_decision"} {
		if containsName(scoped, hidden) {
			t.Errorf("subagent tools = %v, must not contain global-state tool %q", scoped, hidden)
		}
	}

	// entry 节点（RoleAgent）：与主代理一致的全量可见（goal 未激活时
	// plan 同样隐藏，避免 DAG 内递归 plan）。
	entryCtx := WithNodeScope(context.Background(), NodeScope{
		NodeID: "start", Role: RoleAgent, BranchID: "start",
	})
	if got := toolNames(runtime.VisibleTools(entryCtx)); len(got) != len(all) {
		t.Fatalf("entry node tools = %v, want all %v", got, all)
	}

	// goal skill 激活 → 主代理与 entry 节点可见 plan 工具。
	runtime.SetRuntimeVisibilityProjection(RuntimeVisibilityProjection{GoalSkillActive: true})
	for _, ctx := range []context.Context{context.Background(), WithNodeScope(context.Background(), NodeScope{
		NodeID: "start", Role: RoleAgent, BranchID: "start",
	})} {
		got := toolNames(runtime.VisibleTools(ctx))
		for _, want := range []string{"plan_run", "plan_load"} {
			if !containsName(got, want) {
				t.Fatalf("goal-active tools = %v, missing plan tool %q", got, want)
			}
		}
	}
}

// TestNodeScopeHiddenToolDispatchRejected 验证 Dispatch 侧复核同一策略：
// 子代理调用隐藏工具被拒绝（ErrToolNotVisible），可见工具正常进入 handler。
func TestNodeScopeHiddenToolDispatchRejected(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()
	runtime.RegisterBuiltins()

	subCtx := WithNodeScope(context.Background(), NodeScope{
		NodeID: "left", Role: RoleSubAgent, BranchID: "left",
	})
	// 隐藏工具（已注册但不在节点可见集）：ErrToolNotVisible。
	_, err := runtime.agentDispatch(subCtx, "plan_run", `{}`)
	if !errors.Is(err, bridge.ErrToolNotVisible) {
		t.Fatalf("hidden tool dispatch error = %v, want ErrToolNotVisible", err)
	}
	// 项目作用域工具可分发：未绑定项目根时按作用域错误拒绝，而非不可见。
	if _, err := runtime.agentDispatch(subCtx, "read_file", `{"path":"x"}`); err == nil || errors.Is(err, bridge.ErrToolNotVisible) {
		t.Fatalf("read_file dispatch error = %v, want scoped error (not visibility)", err)
	}

	// 主代理 ctx：plan_run 可见可分发（无加载 plan 时报 no-plan，而非隐藏）。
	_, err = runtime.agentDispatch(context.Background(), "plan_run", `{}`)
	if err == nil || !strings.Contains(err.Error(), "no plan is loaded") {
		t.Fatalf("main agent plan_run dispatch error = %v, want no-plan notice", err)
	}
}

// ── SeelexAgentNode 节点块（目标/父证据/预算）────────────────────────

func TestSeelexAgentNodeBlocksCarryEvidenceAndBudget(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()
	runtime.SetParentEvidenceProjection(ParentEvidenceProjection{SessionID: "src-1", Goal: "parent-goal", ConversationCount: 1})

	node := newSeelexAgentNode(codec.NodeSpec[SeelexNodeInput]{
		ID:    "left",
		Input: SeelexNodeInput{ID: "left", Input: "do left", Kind: "agent"},
	}, runtime)

	// 作用域：分支即节点；角色按 binding 判定（未设 binding 时 entry 为空，
	// 非 entry 节点 → subagent）。
	scope := node.scope()
	if scope.NodeID != "left" || scope.BranchID != "left" || scope.Role != RoleSubAgent {
		t.Fatalf("node scope = %+v", scope)
	}
	ctx := WithNodeScope(context.Background(), scope)
	ctx = withNodePromptBlocks(ctx, node.blocks())

	assembled, err := (nodeScopeAssembler{}).Assemble(ctx, seelectx.AssemblyRequest{})
	if err != nil {
		t.Fatal(err)
	}
	text := joinMessageContents(assembled.Messages)
	for _, want := range []string{"# Task", "do left", "## 继承上下文", "parent-goal", "# Context", "最大迭代轮数", "# Role"} {
		if !strings.Contains(text, want) {
			t.Errorf("node request missing %q:\n%s", want, text)
		}
	}

	// 未注入父证据：节点请求不含证据块。
	runtime.SetParentEvidenceProjection(ParentEvidenceProjection{})
	ctxNoEvidence := withNodePromptBlocks(context.Background(), runtime.nodePromptBlocks(node.input))
	assembledNoEvidence, err := (nodeScopeAssembler{}).Assemble(ctxNoEvidence, seelectx.AssemblyRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if text := joinMessageContents(assembledNoEvidence.Messages); strings.Contains(text, "## 继承上下文") {
		t.Errorf("evidence block must be absent without parent evidence:\n%s", text)
	}
}

// ── DAG 两分支并行（确定性 completer）────────────────────────────────

// TestPlanRunParallelAgentBranches 验证子代理 DAG 真并行：
//   - 两个 agent 分支按账号 hash 路由到不同账号（subagent-1/subagent-2）；
//   - 左分支阻塞期间右分支已完成（并行而非串行）；
//   - 子代理请求携带父证据块与节点目标；
//   - 子代理只看到项目作用域工具（read_file 在、plan_run 不在）；
//   - 节点完成事件流入投影（PlanNodeEvent）。
func TestPlanRunParallelAgentBranches(t *testing.T) {
	runtime := newTestRuntimeWithSubagents(t)
	defer runtime.Shutdown()
	runtime.RegisterBuiltins()

	runtime.SetParentEvidenceProjection(ParentEvidenceProjection{SessionID: "src-1", Goal: "parent-goal", ConversationCount: 1})

	// hash 路由断言：left/right 必须落到不同子代理账号（否则无法证明并行）。
	leftAccount, err := ResolveAccountForBranch(runtime.pool, RoleSubAgent, "plan-1:left")
	if err != nil {
		t.Fatal(err)
	}
	rightAccount, err := ResolveAccountForBranch(runtime.pool, RoleSubAgent, "plan-1:right")
	if err != nil {
		t.Fatal(err)
	}
	if leftAccount == rightAccount {
		t.Fatalf("branch hash collision: both resolve to %q; pick different plan IDs", leftAccount)
	}

	// 两个分支的 completer 都阻塞：同时"在途"即为并行调度（串行执行器
	// 永远无法让第二个分支启动）。
	blockingLeft := newScriptedNodeCompleter("left-done")
	blockingLeft.release = make(chan struct{})
	blockingRight := newScriptedNodeCompleter("right-done")
	blockingRight.release = make(chan struct{})
	blockingRight.probeTool = "plan_run" // 首轮尝试调用隐藏工具，验证 Dispatch 拒绝后继续

	injectScriptedCompleters(t, runtime, map[string]agent.Completer{
		"agent-1":    newScriptedNodeCompleter("main-done"),
		leftAccount:  blockingLeft,
		rightAccount: blockingRight,
	})

	runtime.SetPlanBranchBinding(PlanBranchBinding{
		SessionID: "session-1", WorkspaceID: "workspace-1", PlanID: "plan-1", EntryNodeID: "start",
	})

	projected := make(chan PlanNodeEvent, 64)
	runtime.SetPlanNodeCallback(func(ev PlanNodeEvent) { projected <- ev })

	plan := `{"entry":"start","nodes":{"start":{"input":"start"},"left":{"input":"do left","kind":"agent"},"right":{"input":"do right","kind":"agent"},"finish":{"input":"finish"}},"edges":{"start":["left","right"],"left":["finish"],"right":["finish"]}}`
	if _, err := runtime.Agent().DirectDispatch(context.Background(), "plan_load", plan); err != nil {
		t.Fatal(err)
	}

	runDone := make(chan struct{})
	go func() {
		defer close(runDone)
		result, err := runtime.Agent().DirectDispatch(context.Background(), "plan_run", `{}`)
		if err != nil {
			t.Errorf("plan_run failed: %v", err)
			return
		}
		if !strings.Contains(result, `"status":"completed"`) {
			t.Errorf("plan_run result = %q, want completed", result)
		}
	}()

	// 两分支同时阻塞 = 并行调度（串行执行器不可能让第二个分支启动）。
	select {
	case <-blockingLeft.started:
	case <-time.After(5 * time.Second):
		t.Fatal("left branch never started")
	}
	select {
	case <-blockingRight.started:
	case <-time.After(5 * time.Second):
		close(blockingLeft.release)
		t.Fatal("right branch did not start while left is blocked: branches are serialized")
	}

	// 子代理请求断言：两分支启动时都已记录请求与工具集（含父证据块）。
	for _, branch := range []struct {
		name      string
		completer *scriptedNodeCompleter
		goal      string
	}{{name: "left", completer: blockingLeft, goal: "do left"}, {name: "right", completer: blockingRight, goal: "do right"}} {
		branch.completer.mu.Lock()
		requests := branch.completer.requests
		tools := branch.completer.seenTools
		branch.completer.mu.Unlock()
		if len(requests) == 0 {
			t.Fatalf("%s branch never produced an LLM request", branch.name)
		}
		requestText := joinMessageContents(requests[0])
		for _, want := range []string{branch.goal, "## 继承上下文", "parent-goal"} {
			if !strings.Contains(requestText, want) {
				t.Errorf("%s branch request missing %q:\n%s", branch.name, want, requestText)
			}
		}
		if len(tools) == 0 {
			t.Fatalf("%s branch never received a tool set", branch.name)
		}
		seen := toolNamesFromTypes(tools[0])
		if !containsName(seen, "read_file") {
			t.Errorf("%s branch tools = %v, missing scoped read_file", branch.name, seen)
		}
		if containsName(seen, "plan_run") {
			t.Errorf("%s branch tools = %v, hidden plan_run leaked to subagent", branch.name, seen)
		}
	}

	// 释放两个分支 → 全图完成。
	close(blockingLeft.release)
	close(blockingRight.release)
	select {
	case <-runDone:
	case <-time.After(10 * time.Second):
		t.Fatal("plan_run did not finish after releasing both branches")
	}

	// right 分支探测的隐藏工具调用被拒绝后仍完成：探测轮 + 终轮。
	blockingRight.mu.Lock()
	rightRequestCount := len(blockingRight.requests)
	blockingRight.mu.Unlock()
	if rightRequestCount < 2 {
		t.Errorf("right branch requests = %d, want probe round + final round", rightRequestCount)
	}

	// 节点事件流入投影：left/right 均出现 completed（kind=agent）。
	statuses := map[string][]string{}
	kinds := map[string]string{}
	for {
		select {
		case ev := <-projected:
			statuses[ev.NodeID] = append(statuses[ev.NodeID], ev.Status)
			if ev.NodeID != "" && ev.Kind != "" {
				kinds[ev.NodeID] = ev.Kind
			}
		default:
			goto drained
		}
	}
drained:
	for _, id := range []string{"left", "right"} {
		seen := statuses[id]
		if len(seen) == 0 || seen[len(seen)-1] != "completed" {
			t.Errorf("node %q projection = %v, want final completed", id, seen)
		}
		if kinds[id] != "agent" {
			t.Errorf("node %q projected kind = %q, want agent", id, kinds[id])
		}
	}
}

// ── 测试辅助 ─────────────────────────────────────────────────────────

// scriptedNodeCompleter 是确定性节点 completer（example 08 模式，无网络）：
// 记录每次请求的完整消息与可见工具；release 非 nil 时阻塞到关闭（模拟慢
// 节点）；probeTool 非空时首轮发出该工具调用（验证 Dispatch 可见性）。
type scriptedNodeCompleter struct {
	mu        sync.Mutex
	startOnce sync.Once
	started   chan struct{}
	release   chan struct{}
	requests  [][]types.Message
	seenTools [][]types.Tool
	probeTool string
	reply     string
}

func newScriptedNodeCompleter(reply string) *scriptedNodeCompleter {
	return &scriptedNodeCompleter{started: make(chan struct{}), reply: reply}
}

func (c *scriptedNodeCompleter) Complete(ctx context.Context, messages []types.Message, tools []types.Tool) (types.Message, error) {
	c.mu.Lock()
	c.requests = append(c.requests, cloneMessages(messages))
	c.seenTools = append(c.seenTools, cloneTools(tools))
	c.mu.Unlock()
	c.startOnce.Do(func() { close(c.started) })
	if c.release != nil {
		select {
		case <-c.release:
		case <-ctx.Done():
			return types.Message{}, ctx.Err()
		}
	}
	// 首轮：发出 probeTool 工具调用（下一轮返回最终文本）。
	if c.probeTool != "" && len(c.requests) == 1 {
		return types.Message{
			Role: "assistant",
			ToolCalls: []types.ToolCall{{
				ID: "call-1", Type: "function",
				Function: types.ToolCallFunction{Name: c.probeTool, Arguments: "{}"},
			}},
		}, nil
	}
	reply := c.reply
	if reply == "" {
		reply = "node-done"
	}
	return types.Message{Role: "assistant", Content: &reply}, nil
}

// newTestRuntimeWithSubagents 构造带两个子代理账号的测试 Runtime
// （并行分支各占一个账号，MaxConcurrency=1 时并行性由 DAG 执行器保证）。
func newTestRuntimeWithSubagents(t testing.TB) *Runtime {
	t.Helper()
	path := filepath.Join(t.TempDir(), "accounts.yaml")
	content := `roles:
  agent:
    - model: main-model
      base_url: http://localhost
      api_key: test-key
  subagent:
    - model: child-one
      base_url: http://localhost
      api_key: test-key
    - model: child-two
      base_url: http://localhost
      api_key: test-key
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	runtime, err := NewRuntime(RuntimeConfig{AccountsPath: path, ToolCallTimeout: 30 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	runtime.SetRuntimeVisibilityProjection(RuntimeVisibilityProjection{GoalSkillActive: true})
	return runtime
}

// injectScriptedCompleters 用确定性 completer 替换账号池（不发起网络调用）。
func injectScriptedCompleters(t *testing.T, runtime *Runtime, completers map[string]agent.Completer) {
	t.Helper()
	for _, entry := range runtime.pool.Entries() {
		if err := runtime.pool.Unregister(entry.Snapshot.ID); err != nil {
			t.Fatalf("unregister %q: %v", entry.Snapshot.ID, err)
		}
	}
	for id, value := range completers {
		if err := runtime.pool.Register(accountpool.Account[agent.Completer]{
			ID: id, Value: value, MaxConcurrency: 1,
			Metadata: map[string]string{"provider": "openai", "model": "test-model"},
		}); err != nil {
			t.Fatal(err)
		}
	}
}

func cloneMessages(messages []types.Message) []types.Message {
	cloned := make([]types.Message, len(messages))
	copy(cloned, messages)
	for index := range cloned {
		if cloned[index].Content != nil {
			value := *cloned[index].Content
			cloned[index].Content = &value
		}
	}
	return cloned
}

func cloneTools(tools []types.Tool) []types.Tool {
	return append([]types.Tool(nil), tools...)
}

func joinMessageContents(messages []types.Message) string {
	var builder strings.Builder
	for _, message := range messages {
		if message.Content != nil {
			builder.WriteString(*message.Content)
			builder.WriteString("\n")
		}
	}
	return builder.String()
}

func toolNamesFromTypes(tools []types.Tool) []string {
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Function.Name)
	}
	return names
}

func containsName(names []string, want string) bool {
	for _, name := range names {
		if name == want {
			return true
		}
	}
	return false
}
