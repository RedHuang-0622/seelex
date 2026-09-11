package core

// 并发会话的内容隔离守卫：多会话同时跑时，本会话的正文/工具结果绝不落到别的
// 会话的 record（transcript）或可见视图上。
//
// 生产装配把会话 ID 注入 chat ctx（chat.go:145 `ctx = withSessionID(ctx, ...)`），
// 所有会话作用域的写入（transcript 事件、视图消息、工具轮说明正文的按迭代
// 归位）都以它为键。本用例把 A 放后台、B 放活跃视图，交错驱动同样形状的一轮
// （说明正文 → 工具轮宣告 → 工具起止 → 终答），断言：
//   - 两侧 transcript / 视图都保留本会话内容；
//   - 两侧都不出现另一侧的文本；
//   - 说明正文归位到本会话自己的工具轮事件（不跨会话、不跨迭代）。
//
// 运行：go test ./application/core -run ConcurrentSessions -v -count=1

import (
	"context"
	"strings"
	"testing"
	"time"

	seelesession "github.com/RedHuang-0622/Seele/session"
	"github.com/RedHuang-0622/Seele/types"
	"github.com/RedHuang-0622/seelex/application/core/chat"
	selexsession "github.com/RedHuang-0622/seelex/session"
)

func TestConcurrentSessionsKeepOwnContent(t *testing.T) {
	service := newTestService(t, &fakeEngine{sessionID: "session-b"})
	defer service.Shutdown()
	ctx := context.Background()

	type concurrentCase struct {
		session string
		request string
		marker  string
	}
	cases := []concurrentCase{
		{session: "session-a", request: "req-a", marker: "SESS-A"},
		{session: "session-b", request: "req-b", marker: "SESS-B"},
	}

	service.ViewMu.Lock()
	// 活跃视图 = B（前台），A 在后台并发运行。
	service.Core.Snapshot.Session = SessionState{ID: "session-b"}
	service.Core.Snapshot.Chat = ChatState{Running: true, RequestID: "req-b"}
	for _, item := range cases {
		service.components.tasks.BeginTaskFor(item.session, item.request, "并发会话 "+item.marker, "high", nil, TaskCheckpoint{})
		// 每轮开头的可见占位（生产 startChatFor 语义）：assistant 占位承载
		// 本轮流式正文。
		service.appendSessionMessageLocked(item.session, "assistant", "", nil)
	}
	service.ViewMu.Unlock()
	for _, item := range cases {
		unit := service.sessionUnitLocked(item.session)
		unit.SetChatState(ChatState{Running: true, RequestID: item.request, StartedAt: time.Now()}, nil)
		unit.SetStream(chat.NewVisibleOutputStream(item.request))
	}

	for _, item := range cases {
		sessionCtx := withSessionID(ctx, item.session)
		lower := strings.ToLower(item.marker)
		callID := "call-" + lower + "-1"
		arguments := `{"path":"` + lower + `.go"}`
		narration := item.marker + " 的说明正文：我先读取入口。"

		// 说明正文（流式进本会话视图）→ LLM 完成宣告工具调用 → 工具起止。
		service.appendDelta(item.request, narration)
		service.components.tasks.RecordLLMComplete(sessionCtx, seelesession.LLMInfo{
			Turn: 0,
			ToolCalls: []types.ToolCall{{
				ID: callID, Type: "function",
				Function: types.ToolCallFunction{Name: "read_file", Arguments: arguments},
			}},
		})
		service.handleToolStart(sessionCtx, "read_file", "tool-"+lower+"-1", arguments)
		service.handleToolCompleteObserved(sessionCtx, "read_file", "tool-"+lower+"-1", arguments,
			item.marker+" 的工具结果", nil, time.Millisecond, nil)
		service.components.tasks.EnsureFinalAssistantTranscript(item.request, item.marker+" 的结论。")
	}

	for _, item := range cases {
		transcript := service.components.tasks.TranscriptFor(item.session)
		joined := concurrentTranscriptText(transcript)
		for _, want := range []string{
			item.marker + " 的说明正文：我先读取入口。",
			item.marker + " 的工具结果",
			item.marker + " 的结论。",
		} {
			if !strings.Contains(joined, want) {
				t.Fatalf("%s 的 record 丢了本会话内容 %q：%s", item.session, want, joined)
			}
		}
		view := concurrentViewText(service, item.session)
		if !strings.Contains(view, item.marker+" 的说明正文") {
			t.Fatalf("%s 的视图丢了本会话流式正文：%q", item.session, view)
		}
		for _, other := range cases {
			if other.session == item.session {
				continue
			}
			if strings.Contains(joined, other.marker) {
				t.Fatalf("%s 的 record 混入 %s 的内容：%s", item.session, other.session, joined)
			}
			if strings.Contains(view, other.marker) {
				t.Fatalf("%s 的视图混入 %s 的内容：%q", item.session, other.session, view)
			}
		}
		toolEvents := 0
		for _, event := range transcript {
			if event.Role != "assistant" || len(event.ToolCalls) == 0 {
				continue
			}
			toolEvents++
			if !strings.Contains(event.Content, item.marker+" 的说明正文") {
				t.Fatalf("%s 的工具轮事件 %s 的正文 = %q，期望本会话本次迭代的说明正文",
					item.session, event.ToolCalls[0].ID, event.Content)
			}
		}
		if toolEvents != 1 {
			t.Fatalf("%s 的工具轮事件数 = %d，期望 1", item.session, toolEvents)
		}
	}
}

// concurrentTranscriptText 把 transcript 拼成可搜索文本（角色 + 正文 + 调用 ID）。
func concurrentTranscriptText(events []TranscriptEvent) string {
	var builder strings.Builder
	for _, event := range events {
		builder.WriteString(event.Role)
		builder.WriteString(":")
		builder.WriteString(event.Content)
		for _, call := range event.ToolCalls {
			builder.WriteString("|")
			builder.WriteString(call.ID)
		}
		builder.WriteString("\n")
	}
	return builder.String()
}

// concurrentViewText 返回指定会话可见视图的全部消息文本（用户 + assistant +
// 工具结果；只读快照）。
func concurrentViewText(service *Service, sessionID string) string {
	var builder strings.Builder
	unit := service.sessions.Unit(sessionID)
	if unit == nil {
		return ""
	}
	unit.View.Read(func(view *selexsession.View) {
		for _, message := range view.Conversation {
			builder.WriteString(message.Role)
			builder.WriteString(":")
			builder.WriteString(message.Content)
			if message.Tool != nil {
				builder.WriteString("|")
				builder.WriteString(message.Tool.Name)
				builder.WriteString("=")
				builder.WriteString(message.Tool.Result)
			}
			builder.WriteString("\n")
		}
	})
	return builder.String()
}
