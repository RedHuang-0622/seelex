package core

// 红灯复现：热挂载（驻留会话只换视图指针、不重建正文）时，回合「过程」里
// 工具轮之间的 assistant 步骤在可见窗口里没有 reasoning_content。
//
// 引擎历史里每个 assistant 步骤都带推理（落盘回填
// task_context.BackfillAssistantReasoning 也是按步骤回填的，所以冷加载/
// 分页回读拿到的 durable rows 里每个步骤都有），只有可见窗口靠
// attachLatestReasoning 事后补——它只挂「最后一次」推理，工具轮的步骤因此
// 永远看不到思考过程。

import (
	"testing"
)

func TestHotMountKeepsStepReasoningInVisibleWindow(t *testing.T) {
	engine := &fakeEngine{}
	service := newTestService(t, engine)
	defer service.Shutdown()

	sessionID := service.Core.Snapshot.Session.ID
	engine.ReplaceHistory(sessionID, []EngineMessage{
		{Role: "user", Content: "看看项目"},
		{Role: "assistant", ReasoningContent: "先列目录", ToolCalls: []EngineToolCall{{ID: "call-1", Name: "read", Arguments: `{}`}}},
		{Role: "tool", ToolCallID: "call-1", Name: "read", Content: "ok"},
		{Role: "assistant", ReasoningContent: "再看文件", ToolCalls: []EngineToolCall{{ID: "call-2", Name: "read", Arguments: `{}`}}},
		{Role: "tool", ToolCallID: "call-2", Name: "read", Content: "ok"},
		{Role: "assistant", Content: "结论", ReasoningContent: "综合结果"},
	})

	// 可见窗口 = 实时回合的落点：两个工具轮各一条 assistant 步骤（正文为空，
	// 工具调用自成一条 tool 消息），最后是带正文的最终回答。
	service.ViewMu.Lock()
	service.appendMessageLocked("user", "看看项目", nil)
	service.appendMessageLocked("assistant", "", nil)
	service.appendMessageLocked("tool", "", &ToolCall{ID: "call-1", Name: "read", Status: "success"})
	service.appendMessageLocked("assistant", "", nil)
	service.appendMessageLocked("tool", "", &ToolCall{ID: "call-2", Name: "read", Status: "success"})
	service.appendMessageLocked("assistant", "结论", nil)
	service.ViewMu.Unlock()

	service.attachLatestReasoning(sessionID, "request-1")

	// 热挂载不重建正文：前端拿到的就是这条可见窗口（mirror 进 Snapshot）。
	snapshot := service.Snapshot()
	want := []string{"先列目录", "再看文件", "综合结果"}
	got := make([]string, 0, len(want))
	for _, message := range snapshot.Conversation {
		if message.Role != "assistant" || message.Tool != nil {
			continue
		}
		got = append(got, message.ReasoningContent)
	}
	t.Logf("visible assistant reasoning = %q, want %q", got, want)
	if len(got) != len(want) {
		t.Fatalf("可见 assistant 步骤数 = %d, want %d", len(got), len(want))
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("assistant 步骤[%d] reasoning = %q, want %q（工具轮的思考过程在热挂载窗口里丢了）",
				index, got[index], want[index])
		}
	}
}
