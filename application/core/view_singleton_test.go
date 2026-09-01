package core

import (
	"context"
	"testing"
	"time"
)

// TestViewSingletonMirrorConsistent（M4/INV-M4）：V 的唯一镜像
// （session.Domain.ActiveID 与 Snapshot.Session.ID）在装配/草稿物化/
// 活跃卸载全部路径保持一致；切换只移 V，不留下分叉的"当前会话"。
func TestViewSingletonMirrorConsistent(t *testing.T) {
	service := newTestService(t, &fakeEngine{})

	// 装配期：两镜像一致。
	initial := service.Snapshot().Session.ID
	if got := service.sessions.ActiveID(); got != initial {
		t.Fatalf("assemble V mirrors diverge: ActiveID=%q Snapshot=%q", got, initial)
	}

	// 草稿：V 镜像同步为空（draft）。
	if err := service.BeginNewSession(); err != nil {
		t.Fatal(err)
	}
	if got := service.sessions.ActiveID(); got != "" {
		t.Fatalf("draft V mirrors diverge: ActiveID=%q, want empty", got)
	}
	if !service.Snapshot().Session.Draft {
		t.Fatal("snapshot not draft after BeginNewSession")
	}

	// 草稿物化：V 镜像随物化切到新会话（流式增量才能按活跃路径镜像 Snapshot）。
	if err := service.Submit(context.Background(), "first request"); err != nil {
		t.Fatal(err)
	}
	if err := service.WaitForIdle(context.Background()); err != nil {
		t.Fatal(err)
	}
	newID := service.Snapshot().Session.ID
	if newID == "" {
		t.Fatal("materialized session ID is empty")
	}
	if got := service.sessions.ActiveID(); got != newID {
		t.Fatalf("materialize V mirrors diverge: ActiveID=%q Snapshot=%q", got, newID)
	}

	// 活跃卸载：V 镜像同步回 draft（不残留旧 ActiveID）。
	if err := service.UnloadSession(newID); err != nil {
		t.Fatal(err)
	}
	if got := service.sessions.ActiveID(); got != "" {
		t.Fatalf("unload active left stale ActiveID=%q", got)
	}
	if !service.Snapshot().Session.Draft {
		t.Fatal("snapshot not draft after unloading active session")
	}
}

// TestDraftMaterializeStreamsMirrorToSnapshot：草稿物化后流式增量走活跃路径，
// Snapshot 能收到增量（V 镜像一致性的行为验证）。
func TestDraftMaterializeStreamsMirrorToSnapshot(t *testing.T) {
	engine := &fakeEngine{chunks: []string{"hel", "lo"}}
	service := newTestService(t, engine)
	if err := service.BeginNewSession(); err != nil {
		t.Fatal(err)
	}
	if err := service.Submit(context.Background(), "hello"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		snapshot := service.Snapshot()
		if !snapshot.Chat.Running && len(snapshot.Conversation) > 0 {
			last := snapshot.Conversation[len(snapshot.Conversation)-1]
			if last.Role == "assistant" && containsText(last.Content, "hello") {
				return
			}
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("materialized draft streaming not mirrored to Snapshot: %+v", service.Snapshot().Conversation)
}
