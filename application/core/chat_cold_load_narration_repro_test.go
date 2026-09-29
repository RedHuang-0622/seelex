package core

// 冷加载缺「过程中 LLM 说了什么」复现（2026-09-29）。
//
// 现场：长会话里，用户发完一条消息后前端自动翻更早一页（回看态，可见窗口不贴尾，
// 现网靠 IntersectionObserver/sentinel 的自动触发即可发生）。此时本轮工具轮的
// **说明正文**（"我先把入口读一下" 这类先说一句再调工具的正文）在冷加载后消失。
//
// 为什么正文只在这两处存在：
//  1. 框架在 wire 上丢弃工具轮正文（Seele `session/loop.go` callLLM：带 tool_calls
//     时构造 `types.Message{Role:"assistant", Content:nil, ToolCalls:...}`），
//     LLMInfo.Response 因此为空 —— 正文只经 onChunk 进可见视图；
//  2. 应用侧唯一的 durable 归宿是工具钩子边界的按迭代归位
//     （handleToolStart → AttributeToolNarrationLocked），而它的输入
//     `streamedAssistantTextLocked` 读的是**可见窗口**里那条 assistant 消息。
//
// 回看态下 `appendVisibleDelta` 按设计跳过可见窗口写入（窗口里最后一条不是本轮
// 消息，写进去会串写），于是归位输入为空/读到旧窗口正文：工具轮事件的正文要么
// 空、要么是别的轮次的正文 → 落盘行没有本轮正文 → 冷加载（会话未驻留，从
// durable rows 冷恢复）看不到过程中 LLM 说了什么。
//
// 本用例断言两件事，任一不成立即红：
//   - 写盘侧：工具轮 transcript 事件的正文 == 本轮流式说明正文；
//   - 冷加载侧：落盘 record 冷恢复到新 Service 后，可见会话里能找到该正文。

import (
	"context"
	"strings"
	"testing"

	"github.com/RedHuang-0622/Seele/session"
	"github.com/RedHuang-0622/Seele/types"

	"github.com/RedHuang-0622/seelex/application/core/chat"
	"github.com/RedHuang-0622/seelex/application/core/session_runtime"
)

const coldNarrationRequestID = "cold-narration-1"

// coldNarrationStore 在 pagedSessionStore（长会话分页读面）之上补 record 的读写：
// 落盘走 SaveSessionRecordWorkspace（persist 快照），冷加载读回同一份 record
// ——与 v8 布局「record 由 message 行派生」同形。
type coldNarrationStore struct {
	*pagedSessionStore
	persisted SessionRecord
}

func (s *coldNarrationStore) SaveSessionRecordWorkspace(_, _ string, record SessionRecord) error {
	s.persisted = record
	return nil
}

func (s *coldNarrationStore) LoadSessionRecordWorkspace(workspaceID, sessionID string) (SessionRecord, error) {
	if s.persisted.ID != "" {
		return s.persisted, nil
	}
	return s.pagedSessionStore.LoadSessionRecordWorkspace(workspaceID, sessionID)
}

func TestColdLoadKeepsToolWheelNarration(t *testing.T) {
	defer withHistoryWindow(6)()

	const sessionID = "long-session"
	store := &coldNarrationStore{pagedSessionStore: newPagedSessionStore(sessionID, 20)}
	service := newTestService(t, &fakeEngine{}, withTestSessions(store))
	if err := service.ResumeSession(sessionID); err != nil {
		t.Fatalf("resume long session: %v", err)
	}
	// 冷加载基线的序号空间 = 既有 durable 行（20 条）。
	service.components.tasks.SeedTranscriptSeqFor(sessionID, 20)

	// 本轮：用户行落地（含可见行 + transcript 事件）→ 流式占位。
	const input = "读一下装配入口"
	service.ViewMu.Lock()
	service.Core.Snapshot.Chat = ChatState{Running: true, RequestID: coldNarrationRequestID}
	service.components.tasks.BeginTask(coldNarrationRequestID, "narration", "high", nil, TaskCheckpoint{})
	service.appendMessageLocked("user", input, nil)
	service.components.tasks.AppendTranscriptEventLocked(TranscriptEvent{
		TaskID: coldNarrationRequestID, Role: "user", Content: input,
	})
	service.appendMessageLocked("assistant", "", nil)
	service.sessionUnitLocked(sessionID).SetStream(chat.NewVisibleOutputStream(coldNarrationRequestID))
	service.ViewMu.Unlock()

	// 前端自动翻更早一页（生产 API）：可见窗口后退 → 回看态。
	if err := service.LoadMoreHistory(0); err != nil {
		t.Fatalf("load more history: %v", err)
	}

	// 本次工具轮的流式说明正文（只在 onChunk 上存在）。
	const narration = "先看一眼入口，再决定改哪里。"
	service.appendDelta(coldNarrationRequestID, narration)

	// OnLLMComplete：工具轮 wire 正文为空（框架丢正文），事件只带 tool_calls。
	callArguments := `{"path":"application/core/session_history.go"}`
	service.components.tasks.RecordLLMComplete(withSessionID(context.Background(), sessionID), session.LLMInfo{
		ToolCalls: []types.ToolCall{{
			ID: "call-1", Type: "function",
			Function: types.ToolCallFunction{Name: "read_file", Arguments: callArguments},
		}},
	})
	// OnToolStart（生产钩子桥）。
	bridge := NewToolHookBridge()
	bridge.Bind(service)
	bridge.Hooks().OnToolStart(withSessionID(context.Background(), sessionID), session.ToolCallInfo{
		Turn: 0, Name: "read_file", Arguments: callArguments,
	})

	var toolEvent *TranscriptEvent
	for index, event := range service.components.tasks.TranscriptFor(sessionID) {
		if event.Role == "assistant" && len(event.ToolCalls) > 0 {
			toolEvent = &service.components.tasks.TranscriptFor(sessionID)[index]
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
		session_runtime.Location{Meta: SessionInfo{ID: sessionID}}, sessionID,
	); err != nil {
		t.Fatalf("persist session: %v", err)
	}
	restored := newTestService(t, &fakeEngine{}, withTestSessions(store))
	if err := restored.ResumeSession(sessionID); err != nil {
		t.Fatalf("cold resume: %v", err)
	}
	visible := visibleContents(restored.Snapshot())
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
