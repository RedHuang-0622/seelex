package core

import (
	"context"
	"testing"
)

// TestVisibleMessageSeqIsScopedPerSession 钉住可见消息派号的会话作用域：
// 会话 A 跑过若干轮之后新建会话 B，B 的第一条可见消息必须是 `message-1`——
// 而不是接着 A 的号往下派（旧实现是进程级单计数器，B 会从 message-137 这种号
// 开始；而恢复路径的 ID 由本会话事件 seq 派生，两套空间不一致会让同一消息跨
// 重启换 ID）。2026-09-11 修复。
func TestVisibleMessageSeqIsScopedPerSession(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	defer service.Shutdown()

	submitAndIdle := func(text string) {
		t.Helper()
		if err := service.Submit(context.Background(), text); err != nil {
			t.Fatalf("submit %q: %v", text, err)
		}
		if err := service.WaitForIdle(context.Background()); err != nil {
			t.Fatalf("wait idle after %q: %v", text, err)
		}
	}

	// 会话 A：跑两轮，把它的派号推到 message-2 以上。
	submitAndIdle("A first")
	submitAndIdle("A second")
	sessionA := service.Snapshot().Session.ID
	conversationA := service.Snapshot().Conversation
	if len(conversationA) == 0 {
		t.Fatal("session A conversation is empty")
	}
	lastA := conversationA[len(conversationA)-1].ID
	if lastA == "message-1" {
		t.Fatalf("前置条件不成立：会话 A 的消息 ID 仍停在 %q，无法证明派号是全局的", lastA)
	}

	// 新建会话 B：它的第一条消息必须从 1 起。
	if err := service.BeginNewSession(); err != nil {
		t.Fatalf("BeginNewSession: %v", err)
	}
	submitAndIdle("B first")
	snapshotB := service.Snapshot()
	if sessionB := snapshotB.Session.ID; sessionB == sessionA || sessionB == "" {
		t.Fatalf("新建会话 ID = %q，期望一个不同于 A(%q) 的会话", sessionB, sessionA)
	}
	if len(snapshotB.Conversation) == 0 {
		t.Fatal("session B conversation is empty")
	}
	firstB := snapshotB.Conversation[0].ID
	if firstB != "message-1" {
		t.Fatalf("新建会话的首条消息 ID = %q，want message-1（派号必须按会话独立，旧实现是进程级全局计数器）", firstB)
	}
}
