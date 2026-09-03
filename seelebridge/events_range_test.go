package seelebridge

import (
	"context"
	"testing"

	frameworkevent "github.com/RedHuang-0622/Seele/event"
)

// TestUnifiedEventReaderQueryRange G7：事实轨按 Seq 区间读回（不再整段
// Load），node 过滤与 limit 语义与 Query 一致。
func TestUnifiedEventReaderQueryRange(t *testing.T) {
	reader := &UnifiedEventReader{
		LoadRange: func(_ context.Context, sessionID string, fromSeq, toSeq uint64) ([]frameworkevent.Event, error) {
			if sessionID != "sess-1" {
				t.Fatalf("range session = %q, want sess-1", sessionID)
			}
			if fromSeq != 10 || toSeq != 20 {
				t.Fatalf("range = %d..%d, want 10..20", fromSeq, toSeq)
			}
			events := []frameworkevent.Event{
				{Sequence: 12, Type: frameworkevent.TypeLifecycle, Scope: frameworkevent.Scope{NodeID: "node-a"}},
				{Sequence: 13, Type: frameworkevent.TypeLifecycle, Scope: frameworkevent.Scope{NodeID: "node-b"}},
				{Sequence: 14, Type: frameworkevent.TypeLifecycle, Scope: frameworkevent.Scope{NodeID: "node-a"}},
				{Sequence: 21, Type: frameworkevent.TypeLifecycle, Scope: frameworkevent.Scope{NodeID: "node-a"}},
			}
			return events, nil
		},
	}
	view, err := reader.QueryRange(context.Background(), "sess-1", "node-a", 10, 20, 2)
	if err != nil {
		t.Fatalf("QueryRange: %v", err)
	}
	if len(view.Events) != 2 || view.Events[0].Sequence != 12 || view.Events[1].Sequence != 14 {
		t.Fatalf("range events = %+v, want node-a seq 12,14 capped at 2", view.Events)
	}
	if _, err := (&UnifiedEventReader{}).QueryRange(context.Background(), "s", "", 1, 2, 10); err == nil {
		t.Fatal("range reader without LoadRange must error")
	}
}

// TestUnifiedEventTopicMapping 钉住 seelebridge→application/event 桥的
// (channel, sid) 映射（G2 白名单：会话类事件 sid 必填，进程类必空）。
func TestUnifiedEventTopicMapping(t *testing.T) {
	scoped := frameworkevent.Event{
		Type: frameworkevent.TypeLifecycle,
		Locations: []frameworkevent.Location{{
			Kind: "agent.runtime",
			IDs:  map[string]string{"session_id": "sess-a"},
		}},
	}
	channel, sid, scopedOK := unifiedEventTopic(scoped)
	if channel != string(frameworkevent.TypeLifecycle) || sid != "sess-a" || !scopedOK {
		t.Fatalf("scoped topic = %q/%q/%v", channel, sid, scopedOK)
	}
	process := frameworkevent.Event{Type: frameworkevent.TypeProgress}
	channel, sid, scopedOK = unifiedEventTopic(process)
	if scopedOK || sid != "" || channel != string(frameworkevent.TypeProgress) {
		t.Fatalf("process topic = %q/%q/%v, want process-level empty sid", channel, sid, scopedOK)
	}
}
