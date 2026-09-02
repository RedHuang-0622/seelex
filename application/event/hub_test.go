package event

import "testing"

// TestSubscribeSessionDropsForeignEventsAtDelivery 验证会话级订阅在投递端
// 过滤：别会话的事件根本不进入本订阅缓冲，因此既不挤占容量，也不要求客户端
// 再判一次会话归属。
func TestSubscribeSessionDropsForeignEventsAtDelivery(t *testing.T) {
	hub := NewEventHub()
	subscription := hub.SubscribeSession("session-a", 4)
	defer subscription.Close()

	hub.PublishSession(EventMessageDelta, 1, "", "session-b", nil)
	hub.PublishSession(EventMessageDelta, 2, "", "session-a", nil)
	hub.Publish(EventSnapshotChanged, 3, "", nil)

	first := <-subscription.Events
	if first.SessionID != "session-a" {
		t.Fatalf("first delivered event = %q, want session-a", first.SessionID)
	}
	second := <-subscription.Events
	if second.SessionID != "" {
		t.Fatalf("second delivered event = %q, want global (empty session)", second.SessionID)
	}
	select {
	case extra := <-subscription.Events:
		t.Fatalf("unexpected extra event: %+v", extra)
	default:
	}
}

// TestFilteredSubscriptionDeliverySeqIsContiguous 验证过滤后的投递序号连续：
// 全局 Seq 因过滤必然跳号，客户端只能以 DeliverySeq 判定是否真的丢了事件。
func TestFilteredSubscriptionDeliverySeqIsContiguous(t *testing.T) {
	hub := NewEventHub()
	subscription := hub.SubscribeSession("session-a", 8)
	defer subscription.Close()

	for index := 0; index < 3; index++ {
		hub.PublishSession(EventMessageDelta, uint64(index), "", "session-b", nil)
		hub.PublishSession(EventMessageDelta, uint64(index), "", "session-a", nil)
	}
	for want := uint64(1); want <= 3; want++ {
		event := <-subscription.Events
		if event.DeliverySeq != want {
			t.Fatalf("delivery seq = %d, want contiguous %d", event.DeliverySeq, want)
		}
	}
}

// TestOverflowResyncIsDeliveredGlobally 验证缓冲溢出的 resync 一定可达：它必须
// 丢掉会话路由键，否则会被本订阅自己的过滤条件吞掉，客户端从此静默地看旧数据。
func TestOverflowResyncIsDeliveredGlobally(t *testing.T) {
	hub := NewEventHub()
	subscription := hub.SubscribeSession("session-a", 1)
	defer subscription.Close()

	hub.PublishSession(EventMessageDelta, 1, "", "session-a", nil)
	hub.PublishSession(EventMessageDelta, 2, "", "session-a", nil)

	event := <-subscription.Events
	if event.Kind != EventResyncRequired {
		t.Fatalf("overflow event kind = %q, want %q", event.Kind, EventResyncRequired)
	}
	if event.SessionID != "" {
		t.Fatalf("overflow resync must be global, got session %q", event.SessionID)
	}
	if event.DeliverySeq != 2 {
		t.Fatalf("overflow resync delivery seq = %d, want 2", event.DeliverySeq)
	}
}

// TestSubscribeFilteredFollowsLivePredicate 验证谓词在每次投递时求值：视图
// 指针移动后，同一订阅立刻改投新会话，无需客户端重新订阅。
func TestSubscribeFilteredFollowsLivePredicate(t *testing.T) {
	hub := NewEventHub()
	view := "session-a"
	subscription := hub.SubscribeFiltered(func(event Event) bool {
		return event.SessionID == "" || event.SessionID == view
	}, 8)
	defer subscription.Close()

	hub.PublishSession(EventMessageDelta, 1, "", "session-a", nil)
	view = "session-b"
	hub.PublishSession(EventMessageDelta, 2, "", "session-a", nil)
	hub.PublishSession(EventMessageDelta, 3, "", "session-b", nil)

	delivered := []string{}
	for range 2 {
		delivered = append(delivered, (<-subscription.Events).SessionID)
	}
	if len(subscription.Events) != 0 {
		t.Fatalf("stale-session event leaked after the view moved: %d pending", len(subscription.Events))
	}
	if delivered[0] != "session-a" || delivered[1] != "session-b" {
		t.Fatalf("delivered sessions = %v, want [session-a session-b]", delivered)
	}
}
