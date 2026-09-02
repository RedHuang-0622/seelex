package core

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

// TestSubscribeSessionFollowsDraftMaterialization 验证草稿态订阅（空
// sessionID）在首轮提交物化后自动收到新会话事件。
//
// 草稿没有真实 ID——ID 由首次提交时的引擎生成，客户端无从预知；若归属判定放在
// 客户端，它只能在"全放行（污染）"与"全丢弃（首轮无输出）"之间二选一。
func TestSubscribeSessionFollowsDraftMaterialization(t *testing.T) {
	service := newTestService(t, &fakeEngine{lazyStart: true})
	subscription, err := service.SubscribeSession("", 256)
	if err != nil {
		t.Fatalf("SubscribeSession(draft): %v", err)
	}
	defer subscription.Close()

	if !service.Snapshot().Session.Draft {
		t.Fatal("expected the service to start as an unmaterialized draft")
	}
	if err := service.Submit(context.Background(), "first question"); err != nil {
		t.Fatal(err)
	}
	if err := service.WaitForIdle(context.Background()); err != nil {
		t.Fatal(err)
	}
	materialized := service.Snapshot().Session.ID
	if materialized == "" {
		t.Fatal("expected the draft to materialize into a real session")
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

// TestSubscribeSessionFollowsViewPointer 验证视图指针移动即改变归属：旧会话
// 的后续事件不再投递，新视图与全局事件投递，且投递序号保持连续。
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
	service.publishSessionEvent(EventSnapshotChanged, 4, "", "", nil)

	delivered := map[string]bool{}
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
	if !delivered[":"] {
		t.Fatal("global events must stay reachable through a view subscription")
	}
}
