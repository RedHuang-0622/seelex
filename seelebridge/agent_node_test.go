package seelebridge

import (
	"context"
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/skill"
)

// ── 节点预算参数注入（A3，docs/2026-08-03-subagent-fork-architecture/plan.md §7.3）

// TestNodeBudgetPrefersNodeInput 验证节点级 budget 优先于 limits 默认值：
// input.budget.max_loops/max_output_tokens 覆盖，缺省字段回退。
func TestNodeBudgetPrefersNodeInput(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()

	// 缺省（无 budget）：回退 limits（默认 15）。
	if b := runtime.nodeBudget(SeelexNodeInput{ID: "a", Input: "x"}); b.MaxLoops != 15 {
		t.Fatalf("default max_loops = %d, want 15", b.MaxLoops)
	}

	// 部分覆盖：只指定 max_loops，max_output_tokens 保持 limits。
	in := SeelexNodeInput{ID: "a", Input: "x", Budget: &NodeBudgetInput{MaxLoops: 8}}
	b := runtime.nodeBudget(in)
	if b.MaxLoops != 8 {
		t.Fatalf("budget max_loops = %d, want 8", b.MaxLoops)
	}
	if b.MaxOutputTokens <= 0 {
		t.Fatalf("fallback max_output_tokens = %d, want > 0", b.MaxOutputTokens)
	}
}

// TestPlanLoadParsesNodeBudget 验证 plan_load 规范 JSON 的节点 budget 字段
// 经 canonicalPlanDocument 解析进 SeelexNodeInput（canonical → 节点契约）。
func TestPlanLoadParsesNodeBudget(t *testing.T) {
	canonical := `{"entry":"do","nodes":{"do":{"input":"read","kind":"agent","budget":{"max_loops":5,"max_output_tokens":2000}}},"edges":{}}`
	doc, err := canonicalPlanDocument(canonical)
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Nodes) != 1 {
		t.Fatalf("nodes = %d, want 1", len(doc.Nodes))
	}
	input := doc.Nodes[0].Input
	if input.Budget == nil {
		t.Fatal("node budget must be parsed")
	}
	if input.Budget.MaxLoops != 5 || input.Budget.MaxOutputTokens != 2000 {
		t.Fatalf("budget = %+v, want {5 2000}", input.Budget)
	}
}

// TestPlanPolicyRejectsExcessNodeBudget 验证 PlanPolicy 上限校验拒绝超限预算。
func TestPlanPolicyRejectsExcessNodeBudget(t *testing.T) {
	policy := PlanPolicy{Effort: "high", MaxNodeLoops: 48, MaxNodeOutputTokens: 8000}
	ok := `{"entry":"do","nodes":{"do":{"input":"x","budget":{"max_loops":10}}},"edges":{}}`
	if _, err := policy.ValidateLoad(ok); err != nil {
		t.Fatalf("within-limit budget rejected: %v", err)
	}
	excess := `{"entry":"do","nodes":{"do":{"input":"x","budget":{"max_loops":100}}},"edges":{}}`
	if _, err := policy.ValidateLoad(excess); err == nil || !strings.Contains(err.Error(), "max_loops=100 exceeds limit") {
		t.Fatalf("excess budget must be rejected, got %v", err)
	}
	excessTokens := `{"entry":"do","nodes":{"do":{"input":"x","budget":{"max_output_tokens":9000}}},"edges":{}}`
	if _, err := policy.ValidateLoad(excessTokens); err == nil || !strings.Contains(err.Error(), "max_output_tokens=9000 exceeds limit") {
		t.Fatalf("excess output budget must be rejected, got %v", err)
	}
}

// ── skill 能力接入（A2，plan.md §7.2）────────────────────────────────

// TestNodeSkillBlocksInjected 验证装配 skill 目录 actor 后，节点 PromptBlocks
// 含目录块（全部技能名称）与激活块（与节点目标匹配的完整指令）。
func TestNodeSkillBlocksInjected(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()

	registry := skill.NewRegistry()
	registry.Register(skill.Skill{Name: "code-impl", Description: "execute code changes per plan", Prompt: "## code-impl instructions\nmake the change"})
	registry.Register(skill.Skill{Name: "readme-skill", Description: "generate README docs", Prompt: "## readme instructions\nwrite the docs"})
	runtime.SetSkillRegistry(registry)

	// registry 已装配、输入 "audit" 无激活匹配 → 章程 + 目录块 = 2 块。
	noBlocks := runtime.nodePromptBlocks(SeelexNodeInput{ID: "a", Input: "audit"})
	if len(noBlocks) != 2 {
		t.Fatalf("blocks = %d, want 2 (node-charter + skill catalog)", len(noBlocks))
	}

	// 装配后：目录块含全部技能名；激活块含匹配技能的完整指令。
	// （输入命中 code-impl 描述的 "code changes" 词。）
	blocks := runtime.nodePromptBlocks(SeelexNodeInput{ID: "a", Input: "execute code changes and verify"})
	var catalog, active bool
	for _, b := range blocks {
		switch b.Name {
		case "node-skill-catalog":
			catalog = true
			if len(b.Messages) != 1 || b.Messages[0].Content == nil {
				t.Fatalf("catalog block malformed: %+v", b)
			}
			for _, want := range []string{"code-impl", "readme-skill"} {
				if !strings.Contains(*b.Messages[0].Content, want) {
					t.Errorf("catalog missing skill %q", want)
				}
			}
		case "node-skill-active":
			active = true
			if b.Messages[0].Content == nil || !strings.Contains(*b.Messages[0].Content, "code-impl instructions") {
				t.Errorf("active block must carry matched skill instructions:\n%s", *b.Messages[0].Content)
			}
		}
	}
	if !catalog {
		t.Error("skill catalog block must be injected")
	}
	if !active {
		t.Error("matched skill active block must be injected (input mentions 'commit' → code-impl)")
	}
}

// ── 子代理章程（Claude Code 风格，plan.md §7.5 升级）─────────────────

// TestNodeCharterBlock 验证子代理章程结构化提示词：Role/Context/Task/
// Investigation/Constraints/Verification 六段齐全，含收尾协议与工作强度
// 预判（强度过大可再开子代理）。
func TestNodeCharterBlock(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()

	blocks := runtime.nodePromptBlocks(SeelexNodeInput{ID: "inspect", Input: "audit"})
	var found string
	for _, b := range blocks {
		if b.Name == "node-charter" && b.Messages[0].Content != nil {
			found = *b.Messages[0].Content
		}
	}
	if found == "" {
		t.Fatal("node-charter block must be present")
	}
	for _, want := range []string{
		"# Role", "# Context", "# Task", "# Investigation", "# Constraints", "# Verification",
		"git add -A && git commit", "git rebase", "禁止 merge", "seelex/inspect",
		"工作强度评估", "fork_subagents", // 强度预判 → 可再开子代理
		"不修改/新增任何测试文件",
	} {
		if !strings.Contains(found, want) {
			t.Errorf("charter missing %q:\n%s", want, found)
		}
	}
}

// ── 子代理可见工具面（A1，plan.md §7.1）─────────────────────────────

// TestSubAgentSeesFullToolSurface 验证子代理可见完整工具面（skill/MCP 等
// 普通工具可见），仅全局状态工具排除；Dispatch 侧复核一致。
func TestSubAgentSeesFullToolSurface(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()
	runtime.RegisterBuiltins()
	registerNamedTool(t, runtime, "mcp_demo_tool")

	subCtx := WithNodeScope(context.Background(), NodeScope{
		NodeID: "left", Role: RoleSubAgent, BranchID: "left",
	})
	visible := toolNames(runtime.VisibleTools(subCtx))
	if !containsName(visible, "mcp_demo_tool") {
		t.Errorf("subagent tools = %v, missing full-surface tool mcp_demo_tool", visible)
	}
	if containsName(visible, "plan_load") || containsName(visible, "task_complete") {
		t.Errorf("subagent tools = %v, must exclude global-state tools", visible)
	}
}
