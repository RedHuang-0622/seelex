package core

import (
	"strings"
	"testing"

	"github.com/RedHuang-0622/Seele/types"
	"github.com/RedHuang-0622/seelex/application/core/context_runtime"
	"github.com/RedHuang-0622/seelex/application/core/task_context"
	"github.com/RedHuang-0622/seelex/application/model"
)

// TestColdResumeContinueAfterInterruptedToolChain：重启后 continue 场景端到端
// （无 LLM，确定性）：
//  1. transcript 以残缺工具链收尾（user 任务 → assistant 调用 t1/t2 → 只有
//     t1 结果被记录，进程中断）；
//  2. 冷加载走 task_context.TranscriptTailHistory（单元切分，修复前会吞掉
//     整轮 —— UI 有、provider 无）；
//  3. chat 前装配 seam（PrepareProviderHistory）补齐 t2 合成占位；
//  4. 用户输入 continue → 引擎历史末尾追加，模型上下文含原任务、已完成
//     t1 结果与"t2 状态未知"信号，可继续未完成步骤而非失忆重启。
func TestColdResumeContinueAfterInterruptedToolChain(t *testing.T) {
	events := []model.TranscriptEvent{
		{Role: "user", Content: "任务1：重构模块并验证", TokenCount: 40},
		{Role: "assistant", Kind: model.TranscriptEventKindToolCall,
			ToolCalls: []model.TranscriptToolCall{
				{ID: "t1", Name: "read_file", Arguments: `{"path":"a.go"}`},
				{ID: "t2", Name: "grep_search", Arguments: `{"pattern":"TODO"}`},
			}, TokenCount: 20},
		{Role: "tool", ToolCallID: "t1", Name: "read_file", Content: "a.go body", TokenCount: 30},
	}
	// 冷加载：残缺轮必须构成开放单元进入 provider 历史。
	engineHistory := task_context.TranscriptTailHistory(events, 1<<20, 0)
	if len(engineHistory) != 3 || engineHistory[0].Content != "任务1：重构模块并验证" {
		t.Fatalf("cold load lost the interrupted round: %#v", engineHistory)
	}

	engine := &fakeEngine{history: engineHistory}
	service := newTestService(t, engine)
	defer service.Shutdown()

	// chat 前装配 seam：残缺链补成协议合法序列。
	if err := service.components.history.PrepareProviderHistory(); err != nil {
		t.Fatal(err)
	}
	history := engine.History()
	if len(history) != 4 {
		t.Fatalf("prepared history length = %d, want 4: %#v", len(history), history)
	}
	checks := []struct {
		index int
		role  string
		id    string
	}{
		{0, "user", ""},
		{1, "assistant", ""},
		{2, "tool", "t1"},
		{3, "tool", "t2"},
	}
	for _, check := range checks {
		if history[check.index].Role != check.role || history[check.index].ToolCallID != check.id {
			t.Fatalf("history[%d] = %+v, want role=%s id=%s", check.index, history[check.index], check.role, check.id)
		}
	}
	if !strings.HasPrefix(history[3].Content, context_runtime.InterruptedToolResultPrefix) {
		t.Fatalf("filled t2 result must carry the interrupted-tool recovery note: %q", history[3].Content)
	}

	// 重启后用户输入 continue：模型上下文包含原任务 + 已完成 t1 + 未知 t2，
	// 可继续接下来的步骤。
	continueContent := "continue"
	engine.AppendHistory(types.Message{Role: "user", Content: &continueContent})
	history = engine.History()
	if len(history) != 5 || history[4].Role != "user" || history[4].Content != "continue" {
		t.Fatalf("continue message not appended: %#v", history)
	}
	if history[0].Content != "任务1：重构模块并验证" || history[2].ToolCallID != "t1" || history[3].ToolCallID != "t2" {
		t.Fatalf("provider context lost task/progress before continue: %#v", history)
	}
}
