package seelexctx

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/RedHuang-0622/Seele/seelectx"
	"github.com/RedHuang-0622/Seele/types"

	"github.com/RedHuang-0622/seelex/sessionstore"
)

func TestAssemblerProjectionOrder(t *testing.T) {
	project := RenderProjectBlock(sessionstore.ProjectRecord{
		Version: "v1",
		Modules: []sessionstore.ModuleSemantics{{Name: "context", Summary: "上下文模块", Path: "seelexctx"}},
	})
	if project == nil {
		t.Fatal("project block must render for non-empty record")
	}
	stacks := sessionstore.SessionContextRecord{
		PlanStack:    []sessionstore.PlanFrame{{PlanID: "plan-1", Title: "重构", Status: "active"}},
		TaskStack:    []sessionstore.TaskFrame{{TaskID: "task-1", Objective: "迁移上下文", Status: "active"}},
		SkillStack:   []sessionstore.SkillFrame{{SkillID: "skill-1", Name: "go"}},
		CompactStack: []sessionstore.CompactFrame{{SegmentID: "compact-1", From: 0, To: 2, Summary: "先期摘要"}},
	}
	assembler := NewAssembler(AssemblerOptions{
		SystemPrompt: func() string { return "system-prompt-v1" },
		ProjectBlock: func() *seelectx.PromptBlock { return project },
		PrefixStacks: func() []seelectx.PromptBlock { return RenderStablePrefixBlocks(stacks) },
		TailStacks:   func() []seelectx.PromptBlock { return RenderTailBlocks(stacks) },
		Memories: func(_ context.Context, _ string) []seelectx.PromptBlock {
			return []seelectx.PromptBlock{{Name: "memory", Messages: []types.Message{textMessage("user", "相关记忆块")}}}
		},
		Window: func(context.Context) ([]types.Message, error) {
			return []types.Message{textMessage("user", "窗口轮次")}, nil
		},
		Resolver: seelectx.PlaceholderResolverFunc(func(_ context.Context, name string) (string, error) {
			if name == "goal" {
				return "解析目标", nil
			}
			return "", nil
		}),
	})

	assembled, err := assembler.Assemble(context.Background(), seelectx.AssemblyRequest{
		Blocks: []seelectx.PromptBlock{{
			Name:     "evidence",
			Messages: []types.Message{textMessage("user", "证据块 {{goal}}")},
		}},
		WorkingHistory: []types.Message{textMessage("user", "调用方历史")},
	})
	if err != nil {
		t.Fatal(err)
	}
	// 投影顺序（context-prefix-chain）：system → project → memory →
	// 稳定前缀栈（skill/compact）→ 调用方块 → WorkingHistory（累积 context）
	// → 动态尾部栈（plan/task）。
	messages := assembled.Messages
	if len(messages) != 1+1+1+2+1+1+2 {
		t.Fatalf("message count = %d, want 9", len(messages))
	}
	assertContent(t, messages[0], "system-prompt-v1")
	assertContent(t, messages[1], "项目模块语义")
	assertContent(t, messages[2], "相关记忆块")
	assertContent(t, messages[3], "now using skill")
	assertContent(t, messages[4], "now using compact context")
	// 调用方块占位符被解析（只作用于块内消息）。
	assertContent(t, messages[5], "证据块 解析目标")
	// WorkingHistory = 窗口轮次（覆盖调用方传入历史；累积 context）。
	assertContent(t, messages[6], "窗口轮次")
	// plan/task 后置贴近当前输入。
	assertContent(t, messages[7], "now using plan")
	assertContent(t, messages[8], "now using task")
}

func TestAssemblerPlaceholderOnlyInBlocks(t *testing.T) {
	assembler := NewAssembler(AssemblerOptions{
		Resolver: seelectx.PlaceholderResolverFunc(func(_ context.Context, name string) (string, error) {
			return "resolved-" + name, nil
		}),
	})
	assembled, err := assembler.Assemble(context.Background(), seelectx.AssemblyRequest{
		Blocks: []seelectx.PromptBlock{{
			Name:     "skill",
			Messages: []types.Message{textMessage("user", "技能 {{skill}} 说明")},
		}},
		WorkingHistory: []types.Message{textMessage("user", "历史 {{skill}} 保留")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(assembled.Messages) != 2 {
		t.Fatalf("message count = %d, want 2", len(assembled.Messages))
	}
	assertContent(t, assembled.Messages[0], "技能 resolved-skill 说明")
	// 占位符只解析块内消息，不碰 WorkingHistory（框架契约）。
	assertContent(t, assembled.Messages[1], "历史 {{skill}} 保留")
}

func TestAssemblerWindowFallbackOnError(t *testing.T) {
	assembler := NewAssembler(AssemblerOptions{
		Window: func(context.Context) ([]types.Message, error) {
			return nil, errTestWindowUnavailable
		},
	})
	assembled, err := assembler.Assemble(context.Background(), seelectx.AssemblyRequest{
		WorkingHistory: []types.Message{textMessage("user", "原始历史")},
	})
	if err != nil {
		t.Fatalf("window read error must fall back conservatively: %v", err)
	}
	if len(assembled.Messages) != 1 {
		t.Fatalf("message count = %d, want 1", len(assembled.Messages))
	}
	assertContent(t, assembled.Messages[0], "原始历史")
}

var errTestWindowUnavailable = &windowPolicyError{"test: window unavailable"}

func TestAssemblerEmptySystemAndBlocks(t *testing.T) {
	assembler := NewAssembler(AssemblerOptions{})
	assembled, err := assembler.Assemble(context.Background(), seelectx.AssemblyRequest{
		WorkingHistory: []types.Message{textMessage("user", "只有历史")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(assembled.Messages) != 1 {
		t.Fatalf("message count = %d, want 1", len(assembled.Messages))
	}
	assertContent(t, assembled.Messages[0], "只有历史")
}

func TestAssemblerMemoriesInjectedWithLastUserQuery(t *testing.T) {
	var gotQuery string
	assembler := NewAssembler(AssemblerOptions{
		TailStacks: func() []seelectx.PromptBlock {
			return []seelectx.PromptBlock{{Name: "plan", Messages: []types.Message{textMessage("user", "尾部计划块")}}}
		},
		Memories: func(_ context.Context, query string) []seelectx.PromptBlock {
			gotQuery = query
			return []seelectx.PromptBlock{{Name: "memory", Messages: []types.Message{textMessage("user", "相关记忆块")}}}
		},
	})
	assembled, err := assembler.Assemble(context.Background(), seelectx.AssemblyRequest{
		WorkingHistory: []types.Message{
			textMessage("user", "旧问题"),
			textMessage("assistant", "旧回答"),
			textMessage("user", "当前查询：权限 gate"),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotQuery != "当前查询：权限 gate" {
		t.Fatalf("memory provider must receive last user query, got %q", gotQuery)
	}
	// 投影顺序：记忆块 → WorkingHistory → 动态尾部栈块。
	if len(assembled.Messages) != 5 {
		t.Fatalf("message count = %d, want 5", len(assembled.Messages))
	}
	assertContent(t, assembled.Messages[0], "相关记忆块")
	assertContent(t, assembled.Messages[3], "当前查询：权限 gate")
	assertContent(t, assembled.Messages[4], "尾部计划块")
}

func TestAssemblerMemoriesSkipControlBlocksAndNil(t *testing.T) {
	marker := compactContextMarker + " segment=compact-1 from=0 to=2"
	var called bool
	assembler := NewAssembler(AssemblerOptions{
		Memories: func(_ context.Context, query string) []seelectx.PromptBlock {
			called = true
			return nil // nil → 不注入
		},
	})
	assembled, err := assembler.Assemble(context.Background(), seelectx.AssemblyRequest{
		WorkingHistory: []types.Message{
			textMessage("user", marker), // 控制块不作为查询
			textMessage("user", "真实问题"),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("memory provider must be invoked")
	}
	if len(assembled.Messages) != 2 {
		t.Fatalf("nil memory blocks must not add messages, got %d", len(assembled.Messages))
	}
}

func TestRenderStackBlocksTopOnly(t *testing.T) {
	record := sessionstore.SessionContextRecord{
		PlanStack: []sessionstore.PlanFrame{
			{PlanID: "plan-old", Title: "旧计划", Status: "closed"},
			{PlanID: "plan-new", Title: "新计划", Status: "active"},
		},
	}
	blocks := RenderStackBlocks(record)
	if len(blocks) != 1 {
		t.Fatalf("blocks = %d, want 1 (top frame only)", len(blocks))
	}
	if !strings.Contains(*blocks[0].Messages[0].Content, "新计划") {
		t.Fatal("stack block must render the top frame (now using)")
	}
	if strings.Contains(*blocks[0].Messages[0].Content, "旧计划") {
		t.Fatal("closed frames must not render")
	}
}

func TestRenderStackBlocksSplitPrefixAndTail(t *testing.T) {
	record := sessionstore.SessionContextRecord{
		PlanStack:    []sessionstore.PlanFrame{{PlanID: "plan-1", Title: "重构", Status: "active"}},
		TaskStack:    []sessionstore.TaskFrame{{TaskID: "task-1", Objective: "迁移上下文", Status: "active"}},
		SkillStack:   []sessionstore.SkillFrame{{SkillID: "skill-1", Name: "go"}},
		CompactStack: []sessionstore.CompactFrame{{SegmentID: "compact-1", From: 0, To: 2, Summary: "先期摘要"}},
	}
	prefix := RenderStablePrefixBlocks(record)
	if got := blockNames(prefix); !reflect.DeepEqual(got, []string{"skill", "compact"}) {
		t.Fatalf("stable prefix blocks = %v, want [skill compact]", got)
	}
	tail := RenderTailBlocks(record)
	if got := blockNames(tail); !reflect.DeepEqual(got, []string{"plan", "task"}) {
		t.Fatalf("tail blocks = %v, want [plan task]", got)
	}
	all := RenderStackBlocks(record)
	if got := blockNames(all); !reflect.DeepEqual(got, []string{"skill", "compact", "plan", "task"}) {
		t.Fatalf("stack blocks = %v, want [skill compact plan task]", got)
	}
}

func TestRenderTailBlocksPlanFrameStaysCompact(t *testing.T) {
	record := sessionstore.SessionContextRecord{
		PlanStack: []sessionstore.PlanFrame{{
			PlanID: "plan-1", Title: "重构", Status: "active",
			Nodes: []sessionstore.NodeSummary{
				{ID: "n1", Label: "inspect", Status: "running"},
				{ID: "n2", Label: "implement", Status: "pending"},
			},
		}},
		TaskStack: []sessionstore.TaskFrame{{TaskID: "task-1", Objective: "迁移上下文", Status: "active"}},
	}
	tail := RenderTailBlocks(record)
	if got := blockNames(tail); !reflect.DeepEqual(got, []string{"plan", "task"}) {
		t.Fatalf("tail blocks = %v, want [plan task]", got)
	}
	planContent := *tail[0].Messages[0].Content
	// plan 尾部内容克制：只带 plan_ref/title/status，整计划 nodes 不入尾部。
	if strings.Contains(planContent, `"nodes"`) || strings.Contains(planContent, "n1") {
		t.Fatalf("plan tail must not carry the full plan nodes: %q", planContent)
	}
	if !strings.Contains(planContent, "plan-1") || !strings.Contains(planContent, "重构") {
		t.Fatalf("plan tail lost plan_ref/title: %q", planContent)
	}
}

func TestLastUserQuerySkipsPlanTailMessage(t *testing.T) {
	history := []types.Message{
		textMessage("user", ActivePlanContextMarker+"\n{}"),
		textMessage("user", "真实查询"),
	}
	if got := LastUserQuery(history); got != "真实查询" {
		t.Fatalf("query = %q, want the real user query (plan tail skipped)", got)
	}
}

func blockNames(blocks []seelectx.PromptBlock) []string {
	names := make([]string, len(blocks))
	for index, block := range blocks {
		names[index] = block.Name
	}
	return names
}

func TestRenderProjectBlockEmpty(t *testing.T) {
	if block := RenderProjectBlock(sessionstore.ProjectRecord{}); block != nil {
		t.Fatal("empty project record must not render a block")
	}
}

func assertContent(t *testing.T, message types.Message, want string) {
	t.Helper()
	if message.Content == nil {
		t.Fatalf("message %q has nil content, want %q", message.Role, want)
	}
	if !strings.Contains(*message.Content, want) {
		t.Fatalf("content = %q, want contains %q", *message.Content, want)
	}
}
