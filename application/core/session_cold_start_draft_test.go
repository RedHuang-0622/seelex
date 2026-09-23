package core

// 冷启动"启动即草稿"回归（初始会话切换后不消失）。
//
// 冷启动（engine.SessionID()==""）时装配期只早分配了草稿 SID，若不同步落
// service.draft 槽位，`Snapshot()` 的草稿行注入（gated on service.draft != nil）
// 就永远不触发：该行只靠前端"当前会话兜底行"（app.js renderSessions）显示，
// 一旦切换视图，兜底行消失且后端目录也没有这一行——用户看到"初始会话消失"。

import (
	"testing"
)

func TestColdStartDraftSlotIsRetainedAcrossSwitch(t *testing.T) {
	engine := newMultiSessionEngine() // active == "" → 冷启动走草稿分支
	service := newTestService(t, engine)

	coldStart := service.Snapshot()
	if !coldStart.Session.Draft {
		t.Fatalf("cold-start session must be a draft: %+v", coldStart.Session)
	}
	draftID := coldStart.Session.ID
	if draftID == "" {
		t.Fatal("cold-start draft has no pre-assigned session ID")
	}
	// 后端目录必须自带这一行（不能只靠前端兜底行）。
	if !containsSessionID(sessionIDsOf(coldStart.Sessions), draftID) {
		t.Fatalf("BUG REPRO: cold-start draft row %q missing from Snapshot().Sessions = %v",
			draftID, sessionIDsOf(coldStart.Sessions))
	}

	// 切到另一个会话：初始草稿行必须仍在（与 BeginNewSession 草稿同一不变量）。
	target := "sess-cold-switch-target"
	engine.register(target)
	if err := service.ResumeSession(target); err != nil {
		t.Fatalf("resume target: %v", err)
	}
	switched := service.Snapshot()
	if switched.Session.ID != target || switched.Session.Draft {
		t.Fatalf("after switch session = %+v, want id=%q draft=false", switched.Session, target)
	}
	if !containsSessionID(sessionIDsOf(switched.Sessions), draftID) {
		t.Fatalf("BUG REPRO: initial session %q disappeared from the list after switching to %q: %v",
			draftID, target, sessionIDsOf(switched.Sessions))
	}

	// 再新建：必须恢复同一份冷启动草稿（幂等复用槽位，不另开 ID）。
	if err := service.BeginNewSession(); err != nil {
		t.Fatalf("BeginNewSession: %v", err)
	}
	if got := service.Snapshot().Session.ID; got != draftID {
		t.Fatalf("BeginNewSession changed cold-start draft identity: %q -> %q", draftID, got)
	}
}
