package seelexctx

import (
	"context"
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
		StackBlocks:  func() []seelectx.PromptBlock { return RenderStackBlocks(stacks) },
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
	// 投影顺序（架构文档 4.8.4）：system → project → 栈块 → 调用方块 → 窗口。
	messages := assembled.Messages
	if len(messages) != 1+1+4+1+1 {
		t.Fatalf("message count = %d, want 8", len(messages))
	}
	assertContent(t, messages[0], "system-prompt-v1")
	assertContent(t, messages[1], "项目模块语义")
	assertContent(t, messages[2], "now using plan")
	assertContent(t, messages[3], "now using task")
	assertContent(t, messages[4], "now using skill")
	assertContent(t, messages[5], "now using compact context")
	// 调用方块占位符被解析（只作用于块内消息）。
	assertContent(t, messages[6], "证据块 解析目标")
	// WorkingHistory = 窗口轮次（覆盖调用方传入历史）。
	assertContent(t, messages[7], "窗口轮次")
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
