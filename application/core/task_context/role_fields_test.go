package task_context

import (
	"testing"

	"github.com/RedHuang-0622/seelex/application/model"
)

// TestTranscriptRoleFieldsDefaults 钉住 R4 生产者默认：user 行开启新 round，
// assistant/tool 行归 main，role_session_id 取本会话 ID，unit_seq 轮内递增。
func TestTranscriptRoleFieldsDefaults(t *testing.T) {
	coordinator := &Coordinator{}
	st := newSessionTaskRuntime("session-main")

	user := model.TranscriptEvent{Role: "user", Content: "q1"}
	coordinator.applyTranscriptRoleFieldsLocked(st, &user)
	if user.RoleName != "user" || user.RoleSessionID != "session-main" ||
		user.RoundID != 1 || user.UnitSeq != 1 {
		t.Fatalf("user role fields = %+v", user)
	}
	assistant := model.TranscriptEvent{Role: "assistant", Content: "a1"}
	coordinator.applyTranscriptRoleFieldsLocked(st, &assistant)
	if assistant.RoleName != "main" || assistant.RoleSessionID != "session-main" ||
		assistant.RoundID != 1 || assistant.UnitSeq != 2 {
		t.Fatalf("assistant role fields = %+v", assistant)
	}
	next := model.TranscriptEvent{Role: "user", Content: "q2"}
	coordinator.applyTranscriptRoleFieldsLocked(st, &next)
	if next.RoleName != "user" || next.RoundID != 2 || next.UnitSeq != 1 {
		t.Fatalf("next round role fields = %+v", next)
	}
	explicit := model.TranscriptEvent{Role: "assistant", RoleName: "tl", RoleSessionID: "tl-1", RoundID: 2, UnitSeq: 9}
	coordinator.applyTranscriptRoleFieldsLocked(st, &explicit)
	if explicit.RoleName != "tl" || explicit.RoleSessionID != "tl-1" ||
		explicit.RoundID != 2 || explicit.UnitSeq != 9 {
		t.Fatalf("explicit role fields overwritten: %+v", explicit)
	}
	system := model.TranscriptEvent{Role: "system", Kind: model.TranscriptEventKindSystem, Content: "task active state"}
	coordinator.applyTranscriptRoleFieldsLocked(st, &system)
	if system.RoleName != "system" || system.RoleSessionID != "session-main" || system.RoundID != 2 {
		t.Fatalf("system role fields = %+v", system)
	}
	internal := model.TranscriptEvent{Role: "user", Kind: model.TranscriptEventKindInternal, Content: "<!-- seelex:context-checkpoint:v1 -->{}"}
	coordinator.applyTranscriptRoleFieldsLocked(st, &internal)
	if internal.RoleName != "system" {
		t.Fatalf("internal material must be system-owned, got %+v", internal)
	}
}
