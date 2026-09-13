// visible_role_attribution_test.go — 可见会话重建必须保留群聊角色归属。
//
// conversationFromTranscriptLocked 把 transcript 事件投影成可见会话消息
// （落盘 record 的 conversation 子树由它生成，冷恢复后的聊天区也读这份投影）。
// 它此前只搬 Role/Content/ReasoningContent，丢掉了 R4 的角色归属字段，
// 于是恢复后的 ADVISOR 行又退化成 AGENT（与实时聊天同样的「没区分」症状）。
package session_runtime

import (
	"testing"

	"github.com/RedHuang-0622/seelex/application/model"
)

// TestConversationFromTranscriptPreservesRoleAttribution 钉住事件 → 可见消息
// 的字段搬运：role_name / role_session_id / round_id / unit_seq 一样都不能丢。
func TestConversationFromTranscriptPreservesRoleAttribution(t *testing.T) {
	coordinator := &Coordinator{isInternalContent: func(string) bool { return false }}
	events := []model.TranscriptEvent{
		{Seq: 1, Role: "user", Content: "把仓库重构一下", RoleName: "user", RoleSessionID: "main-1", RoundID: 1, UnitSeq: 1},
		{Seq: 2, Role: "assistant", Content: "开始执行", RoleName: "main", RoleSessionID: "main-1", RoundID: 1, UnitSeq: 2},
		{Seq: 3, Role: "assistant", Content: "评审：先补负路径单测", RoleName: "tl", RoleSessionID: "tl-1", RoundID: 1, UnitSeq: 3},
	}

	messages := coordinator.conversationFromTranscriptLocked(events)
	if len(messages) != len(events) {
		t.Fatalf("可见消息数 = %d, want %d（%#v）", len(messages), len(events), messages)
	}
	for index, want := range events {
		got := messages[index]
		if got.RoleName != want.RoleName || got.RoleSessionID != want.RoleSessionID ||
			got.RoundID != want.RoundID || got.UnitSeq != want.UnitSeq {
			t.Errorf("消息 %d 归属字段丢失：got role_name=%q role_session_id=%q round=%d unit=%d, want role_name=%q role_session_id=%q round=%d unit=%d",
				index, got.RoleName, got.RoleSessionID, got.RoundID, got.UnitSeq,
				want.RoleName, want.RoleSessionID, want.RoundID, want.UnitSeq)
		}
	}
}

// TestConversationFromTranscriptPreservesToolRoleAttribution 工具调起/结果行
// 派生出的多条可见消息同样要带调用方归属（EXEC 主持这一轮的过程）。
func TestConversationFromTranscriptPreservesToolRoleAttribution(t *testing.T) {
	coordinator := &Coordinator{isInternalContent: func(string) bool { return false }}
	events := []model.TranscriptEvent{{
		Seq: 1, Role: "assistant", Content: "我来读文件",
		RoleName: "main", RoleSessionID: "main-1", RoundID: 2, UnitSeq: 1,
		ToolCalls: []model.TranscriptToolCall{{ID: "call-1", Name: "read_file"}},
	}}

	messages := coordinator.conversationFromTranscriptLocked(events)
	if len(messages) != 2 {
		t.Fatalf("工具轮应派生「正文 + 调用」两条消息，got %#v", messages)
	}
	for index, message := range messages {
		if message.RoleName != "main" || message.RoleSessionID != "main-1" || message.RoundID != 2 {
			t.Errorf("工具轮消息 %d 归属字段丢失：%+v", index, message)
		}
	}
}
