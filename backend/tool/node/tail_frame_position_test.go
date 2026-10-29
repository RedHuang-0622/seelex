package node

import (
	"context"
	"strings"
	"testing"

	"github.com/RedHuang-0622/Seele/seelectx"
	"github.com/RedHuang-0622/Seele/types"

	seelexctx "github.com/RedHuang-0622/seelex/backend/engine/context/seelectx"
	storecontext "github.com/RedHuang-0622/seelex/store/context"
)

func TestInheritedTailFramesStayAfterWorkingHistory(t *testing.T) {
	snapshot := storecontext.PromptStackSnapshot{
		SkillStack: []storecontext.SkillFrame{{SkillID: "skill-1", Name: "node-work"}},
		CompactStack: []storecontext.CompactFrame{{
			SegmentID: "compact-1", From: 0, To: 4, Summary: "## 摘要\n稳定前缀",
		}},
		PlanStack: []storecontext.PlanFrame{{PlanID: "plan-1", Title: "框架稳定", Status: "active"}},
		TaskStack: []storecontext.TaskFrame{{TaskID: "task-1", Objective: "核对帧位", Status: "running"}},
	}
	coordinator := NewCoordinator(CoordinatorDeps{
		InheritedBlocks: func() []seelectx.PromptBlock {
			return seelexctx.RenderStablePrefixBlocks(snapshot)
		},
		InheritedTailBlocks: func() []seelectx.PromptBlock {
			return seelexctx.RenderTailBlocks(snapshot)
		},
	})
	assembler := coordinator.Assembler()
	history := []types.Message{textMessage("user", "第一轮")}

	first, err := assembler.Assemble(context.Background(), seelectx.AssemblyRequest{WorkingHistory: history})
	if err != nil {
		t.Fatal(err)
	}
	assertTailAtEnd(t, first.Messages, "plan-1", "task-1")
	if got := contentAt(first.Messages, len(first.Messages)-3); !strings.Contains(got, "第一轮") {
		t.Fatalf("WorkingHistory 不在尾部帧之前：%q", got)
	}
	if len(history) != 1 {
		t.Fatalf("装配改写了调用方 WorkingHistory：len=%d", len(history))
	}

	// 历史继续增长且 task/plan 帧变化：尾部帧仍占最后两格，前面的历史保持原序。
	history = append(history,
		textMessage("assistant", "第一轮结论"),
		textMessage("user", "第二轮"),
	)
	snapshot.TaskStack[0].Status = "completed"
	snapshot.PlanStack[0].Status = "completed"
	second, err := assembler.Assemble(context.Background(), seelectx.AssemblyRequest{WorkingHistory: history})
	if err != nil {
		t.Fatal(err)
	}
	assertTailAtEnd(t, second.Messages, "plan-1", "task-1")
	historyStart := len(second.Messages) - 2 - len(history)
	for index, want := range history {
		if got := contentAt(second.Messages, historyStart+index); got != messageContent(want) {
			t.Fatalf("历史 msg#%d 被尾部帧挤走：got=%q want=%q", index, got, messageContent(want))
		}
	}
}

func assertTailAtEnd(t *testing.T, messages []types.Message, planID, taskID string) {
	t.Helper()
	if len(messages) < 2 {
		t.Fatalf("装配消息不足：%d", len(messages))
	}
	if got := contentAt(messages, len(messages)-2); !strings.Contains(got, planID) {
		t.Fatalf("倒数第二格不是 plan 尾部帧：%q", got)
	}
	if got := contentAt(messages, len(messages)-1); !strings.Contains(got, taskID) {
		t.Fatalf("最后一格不是 task 尾部帧：%q", got)
	}
}

func textMessage(role, content string) types.Message {
	return types.Message{Role: role, Content: &content}
}

func contentAt(messages []types.Message, index int) string {
	if index < 0 || index >= len(messages) {
		return ""
	}
	return messageContent(messages[index])
}

func messageContent(message types.Message) string {
	if message.Content == nil {
		return ""
	}
	return *message.Content
}
