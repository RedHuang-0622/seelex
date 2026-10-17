package assemble

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/RedHuang-0622/Seele/agent/bridge"
	"github.com/RedHuang-0622/Seele/seelectx"
	"github.com/RedHuang-0622/Seele/workplan/codec"
	seenode "github.com/RedHuang-0622/seelex/backend/tool/node"

	"github.com/RedHuang-0622/seelex/internal/model"
	"github.com/RedHuang-0622/seelex/store"
)

// ── NodeScope 上下文助手 ─────────────────────────────────────────────

func TestNodeScopeContextRoundTrip(t *testing.T) {
	ctx := context.Background()
	if scope, ok := seenode.NodeScopeFromContext(ctx); ok {
		t.Fatalf("empty ctx must not carry a scope: %+v", scope)
	}
	scope := seenode.NodeScope{NodeID: "left", Role: model.RoleSubAgent, BranchID: "left", WorkspaceID: "w1"}
	child := seenode.WithNodeScope(ctx, scope)
	got, ok := seenode.NodeScopeFromContext(child)
	if !ok || got != scope {
		t.Fatalf("scope round-trip = %+v ok=%v, want %+v", got, ok, scope)
	}
	if empty := seenode.NodeScopeFromContextOrEmpty(ctx); empty.NodeID != "" {
		t.Fatalf("empty ctx scope = %+v, want empty", empty)
	}
	if filled := seenode.NodeScopeFromContextOrEmpty(child); filled != scope {
		t.Fatalf("or-empty scope = %+v, want %+v", filled, scope)
	}

	// 节点 PromptBlocks 注入/读取（拷贝隔离）。
	blocks := []seelectx.PromptBlock{{Name: "node-goal"}}
	withBlocks := seenode.WithNodePromptBlocks(child, blocks)
	blocks[0].Name = "mutated"
	gotBlocks := seenode.NodePromptBlocksFromContext(withBlocks)
	if len(gotBlocks) != 1 || gotBlocks[0].Name != "node-goal" {
		t.Fatalf("block round-trip = %+v", gotBlocks)
	}
	if got := seenode.NodePromptBlocksFromContext(child); len(got) != 0 {
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
	// 子代理（model.RoleSubAgent）：与主代理能力一致（完整工具面），仅排除
	// 操作全局状态的工具（plan 工具族 / task 终态工具）。
	subCtx := seenode.WithNodeScope(context.Background(), seenode.NodeScope{
		NodeID: "left", Role: model.RoleSubAgent, BranchID: "left", WorkspaceID: "w1",
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

	// entry 节点（model.RoleAgent）：与主代理一致的全量可见（goal 未激活时
	// plan 同样隐藏，避免 DAG 内递归 plan）。
	entryCtx := seenode.WithNodeScope(context.Background(), seenode.NodeScope{
		NodeID: "start", Role: model.RoleAgent, BranchID: "start",
	})
	if got := toolNames(runtime.VisibleTools(entryCtx)); len(got) != len(all) {
		t.Fatalf("entry node tools = %v, want all %v", got, all)
	}

	// plan 工具面已退役（teamwork 足够）：goal skill 激活也不再可见。
	runtime.SetRuntimeVisibilityProjection(RuntimeVisibilityProjection{GoalSkillActive: true})
	for _, ctx := range []context.Context{context.Background(), seenode.WithNodeScope(context.Background(), seenode.NodeScope{
		NodeID: "start", Role: model.RoleAgent, BranchID: "start",
	})} {
		got := toolNames(runtime.VisibleTools(ctx))
		for _, hidden := range []string{"plan_run", "plan_load"} {
			if containsName(got, hidden) {
				t.Fatalf("goal-active tools = %v, plan tool %q must stay retired", got, hidden)
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

	subCtx := seenode.WithNodeScope(context.Background(), seenode.NodeScope{
		NodeID: "left", Role: model.RoleSubAgent, BranchID: "left",
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

	// 主代理 ctx：plan_run 同样不可见（工具面已退役）。
	_, err = runtime.agentDispatch(context.Background(), "plan_run", `{}`)
	if !errors.Is(err, bridge.ErrToolNotVisible) {
		t.Fatalf("main agent plan_run dispatch error = %v, want ErrToolNotVisible", err)
	}
}

// ── SeelexAgentNode 节点块（目标/父证据/预算）────────────────────────

func TestSeelexAgentNodeBlocksCarryEvidenceAndBudget(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()
	runtime.SetParentEvidenceProjection(ParentEvidenceProjection{SessionID: "src-1", Goal: "parent-goal", ConversationCount: 1})

	node := seenode.NewAgentNode(codec.NodeSpec[seenode.SeelexNodeInput]{
		ID:    "left",
		Input: seenode.SeelexNodeInput{ID: "left", Input: "do left", Kind: "agent"},
	}, runtime.nodeDeps())

	// 作用域：分支即节点；角色按 binding 判定（未设 binding 时 entry 为空，
	// 非 entry 节点 → subagent）。
	scope := node.Scope()()
	if scope.NodeID != "left" || scope.BranchID != "left" || scope.Role != model.RoleSubAgent {
		t.Fatalf("node scope = %+v", scope)
	}
	ctx := seenode.WithNodeScope(context.Background(), scope)
	ctx = seenode.WithNodePromptBlocks(ctx, node.Blocks()())

	assembled, err := (seenode.ScopeAssembler{}).Assemble(ctx, seelectx.AssemblyRequest{})
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
	ctxNoEvidence := seenode.WithNodePromptBlocks(context.Background(), runtime.node.PromptBlocks(node.Input()))
	assembledNoEvidence, err := (seenode.ScopeAssembler{}).Assemble(ctxNoEvidence, seelectx.AssemblyRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if text := joinMessageContents(assembledNoEvidence.Messages); strings.Contains(text, "## 继承上下文") {
		t.Errorf("evidence block must be absent without parent evidence:\n%s", text)
	}
}

// TestNodeScopeAssemblerInheritsStableContextBlocks 验证子代理会话继承主代理
// 的稳定上下文块：project 块（项目语义）插在节点 charter 之前，内容一致
// 且可被同会话后续请求复用（缓存前缀稳定）；未装配提供者时不注入空块。
func TestNodeScopeAssemblerInheritsStableContextBlocks(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()
	runtime.SetParentEvidenceProjection(ParentEvidenceProjection{SessionID: "src-1", Goal: "parent-goal", ConversationCount: 1})
	runtime.SetProjectKnowledgeProvider(func() *store.ProjectRecord {
		return &store.ProjectRecord{
			Version: "v1",
			Modules: []store.ModuleSemantics{
				{Name: "billing", Summary: "payment subsystem", Path: "internal/billing"},
			},
		}
	})

	node := seenode.NewAgentNode(codec.NodeSpec[seenode.SeelexNodeInput]{
		ID:    "left",
		Input: seenode.SeelexNodeInput{ID: "left", Input: "do left", Kind: "agent"},
	}, runtime.nodeDeps())
	ctx := seenode.WithNodeScope(context.Background(), node.Scope()())
	ctx = seenode.WithNodePromptBlocks(ctx, node.Blocks()())

	assembled, err := (seenode.ScopeAssembler{Coordinator: runtime.node}).Assemble(ctx, seelectx.AssemblyRequest{})
	if err != nil {
		t.Fatal(err)
	}
	messages := assembled.Messages
	text := joinMessageContents(messages)
	for _, want := range []string{"## 项目模块语义", "billing", "payment subsystem", "# Role", "# Task"} {
		if !strings.Contains(text, want) {
			t.Errorf("inherited request missing %q:\n%s", want, text)
		}
	}
	// project 块必须位于节点 charter（# Role）之前：稳定前缀在前，动态
	// 节点块在后，同一会话内前缀稳定可命中缓存。
	projectAt := strings.Index(text, "## 项目模块语义")
	roleAt := strings.Index(text, "# Role")
	if projectAt < 0 || roleAt < 0 || projectAt > roleAt {
		t.Errorf("project block must precede node charter (project=%d role=%d):\n%s", projectAt, roleAt, text)
	}

	// 未装配 project 提供者 → 不注入空块，请求仍正常。
	runtime.SetProjectKnowledgeProvider(nil)
	assembledNone, err := (seenode.ScopeAssembler{Coordinator: runtime.node}).Assemble(ctx, seelectx.AssemblyRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if text := joinMessageContents(assembledNone.Messages); strings.Contains(text, "## 项目模块语义") {
		t.Errorf("project block must be absent without provider:\n%s", text)
	}
}

// ── DAG 两分支并行（确定性 completer）────────────────────────────────
