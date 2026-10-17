package obs

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	frameworkevent "github.com/RedHuang-0622/Seele/event"
	frameworktelemetry "github.com/RedHuang-0622/Seele/telemetry"
	seeletelemetry "github.com/RedHuang-0622/seelex/internal/telemetry"
)

// P21-6：这些是**包内白盒用例**（原来在 `backend/engine/assemble` 同名文件里）。
func TestSummaryEventToFrameworkEvent(t *testing.T) {
	at := time.Unix(1000, 0).UTC()
	event := seeletelemetry.SummaryEvent{
		Kind: "tool", Name: "bash", Status: "failed",
		DurationMS: 2000, At: at, NodeID: "node-1",
	}
	got := summaryEventToFrameworkEvent(event)
	if got.Source != SummaryEventSource {
		t.Fatalf("source = %q, want %q", got.Source, SummaryEventSource)
	}
	if got.Type != frameworkevent.TypeLifecycle || got.Status != frameworkevent.StatusFailed {
		t.Fatalf("type/status = %q/%q, want lifecycle/failed", got.Type, got.Status)
	}
	if got.Scope.NodeID != "node-1" || got.Sequence != uint64(at.UnixNano()) {
		t.Fatalf("scope/sequence = %#v/%d, want node-1/%d", got.Scope, got.Sequence, uint64(at.UnixNano()))
	}
	var decoded seeletelemetry.SummaryEvent
	if err := json.Unmarshal(got.Content, &decoded); err != nil {
		t.Fatalf("decode content: %v", err)
	}
	if decoded != event {
		t.Fatalf("content = %#v, want %#v", decoded, event)
	}
}

func TestSummaryLogAppendAndPersister(t *testing.T) {
	log := NewSummaryLog()
	var persisted []frameworkevent.Event
	log.SetPersister(func(_ context.Context, event frameworkevent.Event) error {
		persisted = append(persisted, event)
		return nil
	})
	event := seeletelemetry.SummaryEvent{
		Kind: "llm", Name: "gpt-5", Status: "completed",
		DurationMS: 100, At: time.Unix(100, 0),
	}
	if err := log.Append(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	if len(persisted) != 1 || persisted[0].Source != SummaryEventSource {
		t.Fatalf("persisted = %#v, want one summary event", persisted)
	}
	if summaries := log.Summaries(); len(summaries) != 1 || summaries[0] != event {
		t.Fatalf("summaries = %#v, want [%#v]", summaries, event)
	}
}

func TestSummaryLogRecordSummaryBestEffort(t *testing.T) {
	log := NewSummaryLog()
	log.SetPersister(func(context.Context, frameworkevent.Event) error {
		return errors.New("persist failed")
	})
	// RecordSummary 是 void best-effort：persister 失败不 panic、不影响内存日志。
	log.RecordSummary(seeletelemetry.SummaryEvent{Kind: "tool", Name: "bash", Status: "failed", At: time.Now()})
	if summaries := log.Summaries(); len(summaries) != 1 {
		t.Fatalf("summaries = %d, want 1", len(summaries))
	}
}

func TestUnifiedEventReaderQueryFiltersByNodeAndLimit(t *testing.T) {
	events := []frameworkevent.Event{
		{ID: "fact-1", Source: "workplan.runner", Scope: frameworkevent.Scope{NodeID: "node-1"}},
		{ID: "summary-1", Source: SummaryEventSource, Scope: frameworkevent.Scope{NodeID: "node-2"}},
		{ID: "fact-2", Source: "workplan.runner", Scope: frameworkevent.Scope{NodeID: "node-2"}},
		{ID: "fact-3", Source: "workplan.runner", Scope: frameworkevent.Scope{NodeID: "node-1"}},
	}
	reader := &UnifiedEventReader{
		Load: func(_ context.Context, sessionID string) ([]frameworkevent.Event, error) {
			if sessionID != "session-1" {
				return nil, errors.New("wrong session")
			}
			return events, nil
		},
		Live: func(_ context.Context, limit int) (frameworktelemetry.ViewModel, error) {
			return frameworktelemetry.ViewModel{Traces: []frameworktelemetry.TraceSnapshot{{TraceID: "trace-1"}}}, nil
		},
	}
	view, err := reader.Query(context.Background(), "session-1", "node-2", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Events) != 2 {
		t.Fatalf("events = %d, want 2 (node-2 filtered, limit 2)", len(view.Events))
	}
	if view.Events[0].ID != "summary-1" || view.Events[1].ID != "fact-2" {
		t.Fatalf("events = %#v, want summary-1,fact-2", view.Events)
	}
	if len(view.Live.Traces) != 1 || view.Live.Traces[0].TraceID != "trace-1" {
		t.Fatalf("live = %#v, want trace-1", view.Live)
	}
}

func TestUnifiedEventReaderUnavailable(t *testing.T) {
	var reader *UnifiedEventReader
	if _, err := reader.Query(context.Background(), "session-1", "", 0); err == nil {
		t.Fatal("want error for nil reader")
	}
	empty := &UnifiedEventReader{}
	if _, err := empty.Query(context.Background(), "session-1", "", 0); err == nil {
		t.Fatal("want error for nil Load")
	}
}

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

// TestUnifiedEventTopicMapping 钉住 seelebridge→middle/protocol/event 桥的
// (channel, sid) 映射（G2 白名单：会话类事件 sid 必填，进程类必空）。
func TestUnifiedEventTopicMapping(t *testing.T) {
	scoped := frameworkevent.Event{
		Type: frameworkevent.TypeLifecycle,
		Locations: []frameworkevent.Location{{
			Kind: "agent.runtime",
			IDs:  map[string]string{"session_id": "sess-a"},
		}},
	}
	channel, sid, scopedOK := UnifiedEventTopic(scoped)
	if channel != string(frameworkevent.TypeLifecycle) || sid != "sess-a" || !scopedOK {
		t.Fatalf("scoped topic = %q/%q/%v", channel, sid, scopedOK)
	}
	process := frameworkevent.Event{Type: frameworkevent.TypeProgress}
	channel, sid, scopedOK = UnifiedEventTopic(process)
	if scopedOK || sid != "" || channel != string(frameworkevent.TypeProgress) {
		t.Fatalf("process topic = %q/%q/%v, want process-level empty sid", channel, sid, scopedOK)
	}
}
