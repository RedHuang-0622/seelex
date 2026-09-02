package event

import "testing"

// TestReplaySubscriptionKeepsOverflowReplayable 覆盖 C4 的核心改变：开启重放
// 窗口的订阅在缓冲写满时既不丢弃载荷、也不排空缓冲——事件留在订阅内窗口里，
// 落后消费者按 delivery_seq 增量补取即可闭合，不需要整份重拉快照。
func TestReplaySubscriptionKeepsOverflowReplayable(t *testing.T) {
	hub := NewEventHub()
	subscription := hub.SubscribeWithReplay(nil, 2, 8)
	defer subscription.Close()

	for index := 0; index < 6; index++ {
		hub.Publish(EventRuntimeChanged, uint64(index+1), "", nil)
	}
	if got := subscription.DeliveryWatermark(); got != 6 {
		t.Fatalf("delivery watermark = %d, want 6", got)
	}

	// 缓冲只装得下前两条，且没有被"排空 + resync.required"改写。
	var drained []Event
	for len(drained) < 2 {
		drained = append(drained, <-subscription.Events)
	}
	for _, item := range drained {
		if item.Kind == EventResyncRequired {
			t.Fatalf("replay subscription must not force resync on overflow: %#v", item)
		}
	}
	if drained[0].DeliverySeq != 1 || drained[1].DeliverySeq != 2 {
		t.Fatalf("buffered delivery_seq = %d,%d want 1,2", drained[0].DeliverySeq, drained[1].DeliverySeq)
	}

	replay := subscription.ReplaySince(2)
	if !replay.Covered {
		t.Fatal("window of 8 must still cover 3..6")
	}
	if len(replay.Events) != 4 ||
		replay.Events[0].DeliverySeq != 3 || replay.Events[3].DeliverySeq != 6 {
		t.Fatalf("replay events = %#v, want delivery_seq 3..6", replay.Events)
	}
	// 已追平时补取应为空且仍算覆盖（调用方据此停止补取，而不是当成丢失）。
	caught := subscription.ReplaySince(6)
	if !caught.Covered || len(caught.Events) != 0 {
		t.Fatalf("ReplaySince(watermark) = %#v, want covered and empty", caught)
	}
}

// TestReplayWindowEvictionRequiresFullResync 钉住窗口上限：落后超过窗口时
// Covered=false，调用方必须整份重拉，不能拿到一份"看起来连续其实缺事件"的补取。
func TestReplayWindowEvictionRequiresFullResync(t *testing.T) {
	hub := NewEventHub()
	subscription := hub.SubscribeWithReplay(nil, 1, 2)
	defer subscription.Close()

	for index := 0; index < 5; index++ {
		hub.Publish(EventRuntimeChanged, uint64(index+1), "", nil)
	}
	if stale := subscription.ReplaySince(1); stale.Covered {
		t.Fatalf("2-deep window must have evicted 2..3, got %#v", stale)
	}
	fresh := subscription.ReplaySince(3)
	if !fresh.Covered || len(fresh.Events) != 2 ||
		fresh.Events[0].DeliverySeq != 4 || fresh.Events[1].DeliverySeq != 5 {
		t.Fatalf("replay since 3 = %#v, want covered 4..5", fresh.Events)
	}
}

// TestReplayWindowRespectsDeliveryFilter 确认补取与投递同一套归属口径：被过滤
// 掉的别会话事件既不占 delivery_seq，也不进重放窗口。
func TestReplayWindowRespectsDeliveryFilter(t *testing.T) {
	hub := NewEventHub()
	subscription := hub.SubscribeWithReplay(func(item Event) bool {
		return item.SessionID == "" || item.SessionID == "session-a"
	}, 4, 8)
	defer subscription.Close()

	hub.PublishSession(EventMessageAdded, 1, "", "session-b", nil)
	hub.PublishSession(EventMessageAdded, 2, "", "session-a", nil)

	if got := subscription.DeliveryWatermark(); got != 1 {
		t.Fatalf("watermark = %d, want 1 (foreign session event must not consume a seq)", got)
	}
	replay := subscription.ReplaySince(0)
	if !replay.Covered || len(replay.Events) != 1 || replay.Events[0].SessionID != "session-a" {
		t.Fatalf("replay = %#v, want only the session-a event", replay.Events)
	}
}

// TestReplaySubscriptionSurvivesClose 确认关闭后的订阅不会给出可用窗口：
// 已关闭订阅的补取必须显式失败，让调用方走快照路径。
func TestReplaySubscriptionSurvivesClose(t *testing.T) {
	hub := NewEventHub()
	subscription := hub.SubscribeWithReplay(nil, 2, 4)
	hub.Publish(EventRuntimeChanged, 1, "", nil)
	subscription.Close()

	if replay := subscription.ReplaySince(0); replay.Covered {
		t.Fatalf("closed subscription must not serve replay: %#v", replay.Events)
	}
}
