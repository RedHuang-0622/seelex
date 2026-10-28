package integration

// 冷加载缺「过程中 LLM 说了什么」复现（2026-09-29）。
//
// 现场：长会话里，用户发完一条消息后前端自动翻更早一页（回看态，可见窗口不贴尾，
// 现网靠 IntersectionObserver/sentinel 的自动触发即可发生）。此时本轮工具轮的
// **说明正文**（"我先把入口读一下" 这类先说一句再调工具的正文）在冷加载后消失。
//
// 工具轮正文必须按本次可见流归位，不能读到回看窗口的旧正文。
// 本用例断言两件事，任一不成立即红：
//   - 写盘侧：工具轮 transcript 事件的正文 == 本轮流式说明正文；
//   - 冷加载侧：落盘 record 冷恢复到新 Service 后，可见会话里能找到该正文。

import (
	"context"
	"strings"
	"testing"

	"github.com/RedHuang-0622/Seele/session"
	"github.com/RedHuang-0622/Seele/types"

	assembly "github.com/RedHuang-0622/seelex/application/assembly"
	"github.com/RedHuang-0622/seelex/contract/protocol"
	"github.com/RedHuang-0622/seelex/kernel/react"
	sessionruntime "github.com/RedHuang-0622/seelex/kernel/session"
)

const coldNarrationRequestID = "cold-narration-1"

// coldNarrationStore 在 pagedSessionStore（长会话分页读面）之上补 record 的读写：
// 落盘走 SaveSessionRecordWorkspace（persist 快照），冷加载读回同一份 record
// ——与 v8 布局「record 由 message 行派生」同形。
type coldNarrationStore struct {
	*pagedSessionStore
	persisted model.SessionRecord
}

func (s *coldNarrationStore) SaveSessionRecordWorkspace(_, _ string, record model.SessionRecord) error {
	s.persisted = record
	return nil
}

func (s *coldNarrationStore) LoadSessionRecordWorkspace(workspaceID, sessionID string) (model.SessionRecord, error) {
	if s.persisted.ID != "" {
		return s.persisted, nil
	}
	return s.pagedSessionStore.LoadSessionRecordWorkspace(workspaceID, sessionID)
}

func TestColdLoadKeepsToolWheelNarration(t *testing.T) {
	defer withHistoryWindow(6)()

	const sessionID = "long-session"
	store := &coldNarrationStore{pagedSessionStore: newPagedSessionStore(sessionID, 20)}
	service := newLegacyTestService(t, &fakeEngine{}, withTestSessions(store))
	if err := service.components.sessionResume.Resume(sessionID); err != nil {
		t.Fatalf("resume long session: %v", err)
	}
	// 冷加载基线的序号空间 = 既有 durable 行（20 条）。
	service.components.tasks.SeedTranscriptSeqFor(sessionID, 20)

	// 本轮：用户行落地（含可见行 + transcript 事件）→ 流式占位。
	const input = "读一下装配入口"
	service.ViewMu.Lock()
	service.Core.Snapshot.Chat = model.ChatState{Running: true, RequestID: coldNarrationRequestID}
	service.components.tasks.BeginTask(coldNarrationRequestID, "narration", "high", nil, model.TaskCheckpoint{})
	service.components.view.AppendMessageLocked("user", input, nil)
	service.components.tasks.AppendTranscriptEventLocked(model.TranscriptEvent{
		TaskID: coldNarrationRequestID, Role: "user", Content: input,
	})
	service.components.view.AppendMessageLocked("assistant", "", nil)
	service.sessionUnitLocked(sessionID).SetStream(chat.NewVisibleOutputStream(coldNarrationRequestID))
	service.ViewMu.Unlock()

	// 前端自动翻更早一页（生产 API）：可见窗口后退 → 回看态。
	if err := service.components.conversationHistoryQuery.LoadMoreHistory(0); err != nil {
		t.Fatalf("load more history: %v", err)
	}

	// 本次工具轮的流式说明正文（只在 onChunk 上存在）。
	const narration = "先看一眼入口，再决定改哪里。"
	service.components.conversationStreamRuntime.AppendDelta(coldNarrationRequestID, narration)

	// OnLLMComplete：工具轮正文与 tool_calls 同属模型输出。
	callArguments := `{"path":"application/assembly/session_history.go"}`
	service.components.tasks.RecordLLMComplete(withSessionID(context.Background(), sessionID), session.LLMInfo{
		Response: narration,
		ToolCalls: []types.ToolCall{{
			ID: "call-1", Type: "function",
			Function: types.ToolCallFunction{Name: "read_file", Arguments: callArguments},
		}},
	})
	// OnToolStart（生产钩子桥）。
	bridge := assembly.NewToolHookBridge()
	bridge.Bind(service.Service)
	bridge.Hooks().OnToolStart(withSessionID(context.Background(), sessionID), session.ToolCallInfo{
		Turn: 0, Name: "read_file", Arguments: callArguments,
	})

	var toolEvent *model.TranscriptEvent
	events := service.components.tasks.TranscriptFor(sessionID)
	for index, event := range events {
		if event.Role == "assistant" && len(event.ToolCalls) > 0 {
			toolEvent = &events[index]
		}
	}
	if toolEvent == nil {
		t.Fatal("本轮工具轮 transcript 事件缺失")
	}
	if got := strings.TrimSpace(toolEvent.Content); got != narration {
		t.Errorf("写盘侧：工具轮事件正文 = %q，期望 %q（本轮流式说明正文）", got, narration)
	}

	// 冷加载侧：落盘后换新 Service 冷恢复，可见会话里必须能看到该正文。
	if err := service.components.sessions.PersistCurrentSession(
		sessionruntime.Location{Meta: model.SessionInfo{ID: sessionID}}, sessionID,
	); err != nil {
		t.Fatalf("persist session: %v", err)
	}
	restored := newLegacyTestService(t, &fakeEngine{}, withTestSessions(store))
	if err := restored.components.sessionResume.Resume(sessionID); err != nil {
		t.Fatalf("cold resume: %v", err)
	}
	visible := visibleContents(restored.components.frontendSnapshot.Snapshot())
	found := false
	for _, content := range visible {
		if strings.TrimSpace(content) == narration {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("冷加载侧：可见会话 = %v，期望其中含工具轮说明正文 %q", visible, narration)
	}
}
