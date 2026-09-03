package core

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

// TestSubscribeSessionFollowsDraftMaterialization 验证早分配 SID 后，草稿
// 订阅直接使用草稿真实 ID：首轮提交物化复用同一 ID，订阅全程收到本会话
// 事件（G4 先行：草稿持有真实 sessionID，不再需要"空 sid 跟随视图"）。
func TestSubscribeSessionFollowsDraftMaterialization(t *testing.T) {
	service := newTestService(t, &fakeEngine{lazyStart: true})
	draftID := service.Snapshot().Session.ID
	if draftID == "" || !service.Snapshot().Session.Draft {
		t.Fatal("expected the service to start as an unmaterialized draft with pre-assigned ID")
	}
	subscription, err := service.SubscribeSession(draftID, 256)
	if err != nil {
		t.Fatalf("SubscribeSession(draft id): %v", err)
	}
	defer subscription.Close()

	if err := service.Submit(context.Background(), "first question"); err != nil {
		t.Fatal(err)
	}
	if err := service.WaitForIdle(context.Background()); err != nil {
		t.Fatal(err)
	}
	materialized := service.Snapshot().Session.ID
	if materialized != draftID {
		t.Fatalf("materialized ID = %q, want pre-assigned draft ID %q", materialized, draftID)
	}

	deadline := time.After(2 * time.Second)
	for {
		select {
		case event := <-subscription.Events:
			if event.SessionID == materialized {
				return
			}
		case <-deadline:
			t.Fatalf("draft subscription never received events of materialized session %q", materialized)
		}
	}
}

// TestSubscribeSessionFollowsViewPointer 验证空 sid 订阅（过渡口径）仍按
// 视图指针判定归属：旧会话的后续事件不再投递，新视图事件投递；进程类全局
// 事件（resync/exit）始终可达；会话类 kind 必须带 sid（INV-G3），不再以
// 空 sid 充当"全局会话事件"。
func TestSubscribeSessionFollowsViewPointer(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	subscription, err := service.SubscribeSession("", 64)
	if err != nil {
		t.Fatalf("SubscribeSession(view): %v", err)
	}
	defer subscription.Close()

	service.sessions.SetActive("session-old")
	service.publishSessionEvent(EventMessageDelta, 1, "", "session-old", MessageDelta{MessageID: "m-before-switch"})
	// 切换/物化在 core 里都收口为 session.Domain.SetActive，视图归属随之改变。
	service.sessions.SetActive("session-new")
	service.publishSessionEvent(EventMessageDelta, 2, "", "session-old", MessageDelta{MessageID: "m-stale"})
	service.publishSessionEvent(EventMessageDelta, 3, "", "session-new", MessageDelta{MessageID: "m-new"})
	service.publishSessionEvent(EventResyncRequired, 4, "", "", nil)

	delivered := map[string]bool{}
	deliveredKinds := map[string]bool{}
	count := 0
	deadline := time.After(500 * time.Millisecond)
collect:
	for {
		select {
		case event := <-subscription.Events:
			count++
			if event.DeliverySeq != uint64(count) {
				t.Fatalf("delivery seq = %d, want contiguous %d", event.DeliverySeq, count)
			}
			var delta MessageDelta
			_ = json.Unmarshal(event.Payload, &delta)
			delivered[event.SessionID+":"+delta.MessageID] = true
			deliveredKinds[string(event.Kind)] = true
		case <-deadline:
			break collect
		}
	}
	if !delivered["session-old:m-before-switch"] {
		t.Fatal("the view's own event before the switch was dropped")
	}
	if delivered["session-old:m-stale"] {
		t.Fatal("stale-session events leaked after the view moved")
	}
	if !delivered["session-new:m-new"] {
		t.Fatal("the new view session's events never reached the subscription")
	}
	if !deliveredKinds["resync.required"] {
		t.Fatal("process-class global events must stay reachable through a view subscription")
	}
}
