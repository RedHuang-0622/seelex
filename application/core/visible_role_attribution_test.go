// visible_role_attribution_test.go — 「teammates 之间没有做出区分」的复现与回归。
//
// 前端拿到的结果就是本用例断言的对象：gui/bridge.go 把 Service 的 Snapshot
// 原样下发给前端，聊天区渲染 snapshot.conversation，而
// gui/frontend/dist/components.js 的 roleIdentity 只按 message.role_name 渲染
// EXEC（main）/ ADVISOR（tl）身份；role_name 缺失时两个 agent 都退化成 AGENT。
//
// 因此应用层必须保证可见会话消息带群聊归属（my_design §8.3）：
//   - 用户行 = role_name:user；
//   - EXEC（main）的回合行 = role_name:main；
//   - ADVISOR（tl）的指令回放 = assistant 行 + role_name:tl（不是 system 行）；
//   - 同一轮的行共享 round_id（前端 R 徽标）。
package core

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/session"
)

// TestVisibleConversationCarriesRoleAttribution 复现实时聊天载荷缺归属：
// 一轮对话后，前端收到的 user / assistant 行必须分别归属 user 与 main，
// 且同属一个 round。
func TestVisibleConversationCarriesRoleAttribution(t *testing.T) {
	service := newTestService(t, &fakeEngine{chunks: []string{"an", "swer"}})
	if err := service.Submit(context.Background(), "hello"); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	snapshot := waitForSnapshot(t, service, func(snapshot Snapshot) bool { return !snapshot.Chat.Running })
	sessionID := snapshot.Session.ID

	user := visibleMessage(t, snapshot.Conversation, func(message Message) bool {
		return message.Role == "user" && strings.Contains(message.Content, "hello")
	})
	if user.RoleName != "user" {
		t.Errorf("用户行 role_name = %q, want %q（前端 roleIdentity 依赖该字段）", user.RoleName, "user")
	}
	if user.RoundID == 0 {
		t.Errorf("用户行缺 round_id：前端无法把这一轮的过程归到同一 R 徽标下：%+v", user)
	}

	assistant := visibleMessage(t, snapshot.Conversation, func(message Message) bool {
		return message.Role == "assistant" && strings.Contains(message.Content, "answer")
	})
	if assistant.RoleName != "main" {
		t.Errorf("EXEC 行 role_name = %q, want %q（缺这段归属会与 ADVISOR 一起渲染成 AGENT）",
			assistant.RoleName, "main")
	}
	if assistant.RoundID != user.RoundID {
		t.Errorf("同一轮的行 round_id 不一致：user=%d assistant=%d", user.RoundID, assistant.RoundID)
	}
	if sessionID != "" && assistant.RoleSessionID != sessionID {
		t.Errorf("EXEC 行 role_session_id = %q, want %q（main 复用主会话）", assistant.RoleSessionID, sessionID)
	}

	// 前端拿到的就是这段 JSON（Bridge 原样下发 Snapshot）：字段名与取值一起钉住。
	payload, err := json.Marshal(snapshot.Conversation)
	if err != nil {
		t.Fatalf("marshal conversation: %v", err)
	}
	for _, want := range []string{`"role_name":"user"`, `"role_name":"main"`, `"round_id":1`} {
		if !strings.Contains(string(payload), want) {
			t.Errorf("前端载荷缺 %s：%s", want, payload)
		}
	}
}

// TestTeamDirectiveReplayCarriesAdvisorIdentity 复现 ADVISOR 不可辨：TL 回合的
// 指令回放此前以 role=system 写进可见会话，前端渲染成「系统」。它必须是一条
// assistant 行 + role_name:tl，前端才会按 ADVISOR 归属。
func TestTeamDirectiveReplayCarriesAdvisorIdentity(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	const sessionID = "session-team"
	const directive = "[TL 指令 tl-1] 先补负路径单测再收口"

	service.components.goal.NoteInjected(sessionID, []string{directive})
	service.injectGoalDirectivesFor(sessionID)

	replay := visibleMessage(t, visibleConversationFor(service, sessionID), func(message Message) bool {
		return strings.Contains(message.Content, "先补负路径单测再收口")
	})
	if replay.Role != "assistant" {
		t.Errorf("TL 指令回放 role = %q, want %q（system 行在聊天区渲染成「系统」，不是 ADVISOR）",
			replay.Role, "assistant")
	}
	if replay.RoleName != "tl" {
		t.Errorf("TL 指令回放 role_name = %q, want %q", replay.RoleName, "tl")
	}
}

// visibleMessage 取第一条满足条件的可见消息（缺失即用例失败）。
func visibleMessage(t *testing.T, messages []Message, match func(Message) bool) Message {
	t.Helper()
	for _, message := range messages {
		if match(message) {
			return message
		}
	}
	t.Fatalf("可见会话中找不到目标消息：%#v", messages)
	return Message{}
}

// visibleConversationFor 读取指定会话的可见投影（非活跃会话也能读）。
func visibleConversationFor(service *Service, sessionID string) []Message {
	var messages []Message
	service.components.view.SessionViewReadLocked(sessionID, func(view *session.View) {
		messages = append([]Message(nil), view.Conversation...)
	})
	return messages
}
