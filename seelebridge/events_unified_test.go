package seelebridge

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	frameworkevent "github.com/RedHuang-0622/Seele/event"
	frameworktelemetry "github.com/RedHuang-0622/Seele/telemetry"
	seeletelemetry "github.com/RedHuang-0622/seelex/seelebridge/internal/telemetry"
	"github.com/RedHuang-0622/seelex/sessionstore"
)

func TestSummaryEventToFrameworkEvent(t *testing.T) {
	at := time.Unix(1000, 0).UTC()
	event := seeletelemetry.SummaryEvent{
		Kind: "tool", Name: "bash", Status: "failed",
		DurationMS: 2000, At: at, NodeID: "node-1",
	}
	got := summaryEventToFrameworkEvent(event)
	if got.Source != summaryEventSource {
		t.Fatalf("source = %q, want %q", got.Source, summaryEventSource)
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
	if len(persisted) != 1 || persisted[0].Source != summaryEventSource {
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
		{ID: "summary-1", Source: summaryEventSource, Scope: frameworkevent.Scope{NodeID: "node-2"}},
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

// TestUnifiedEventsEndToEndPersistsSummaryAndMergesFacts 验证统一事件库的
// 端到端链路：B 类摘要经 Runtime 装配（SummaryLog → SetEventPersister →
// correlateMainSessionID 补 session 关联 → EventStore 落盘）后，与 A 类
// 事实同库，Runtime.UnifiedEvents 按 sessionID 读回并可合并、可按 nodeID
// 过滤。覆盖任务验收要求"摘要经 EventStore 落盘后统一查询能按 sessionID
// 读回并与 A 类合并"。
func TestUnifiedEventsEndToEndPersistsSummaryAndMergesFacts(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()
	router := newTestRouter(t)
	runtime.AttachHistoryRouter(router)
	eventStore := sessionstore.NewEventStore(router)
	runtime.SetEventPersister(eventStore.Append)
	if _, err := runtime.NewMainSession(nil); err != nil {
		t.Fatal(err)
	}
	sessionID := runtime.MainSessionID()
	if sessionID == "" {
		t.Fatal("main session id is empty")
	}

	// B 类摘要：经 SummaryLog（同一 persister）落库，session 关联由
	// correlateMainSessionID 补全。
	summaryAt := time.Unix(1700000000, 0)
	runtime.summaryLog.RecordSummary(seeletelemetry.SummaryEvent{
		Kind: "tool", Name: "bash", Status: "failed",
		DurationMS: 2000, At: summaryAt, NodeID: "node-1",
	})

	// A 类事实：与摘要同库（同一 session_id 的 agent.runtime Location）。
	if err := eventStore.Append(context.Background(), frameworkevent.Event{
		ID: "fact-1", Sequence: 1, Source: "workplan.runner",
		Type: frameworkevent.TypeLifecycle, Status: frameworkevent.StatusCompleted,
		OccurredAt: summaryAt.Add(-time.Minute),
		Scope:      frameworkevent.Scope{NodeID: "node-a"},
		Locations: []frameworkevent.Location{{
			Kind: "agent.runtime",
			IDs:  map[string]string{"session_id": sessionID},
		}},
	}); err != nil {
		t.Fatal(err)
	}

	// 统一查询：按 sessionID 合并 A + B 摘要。
	view, err := runtime.UnifiedEvents(context.Background(), sessionID, "", 100)
	if err != nil {
		t.Fatal(err)
	}
	var sawSummary, sawFact bool
	for _, event := range view.Events {
		switch event.Source {
		case summaryEventSource:
			sawSummary = true
		case "workplan.runner":
			sawFact = true
		}
	}
	if !sawSummary || !sawFact {
		t.Fatalf("unified events must merge B summary and A facts, got %#v", view.Events)
	}

	// nodeID 过滤：只留该节点的摘要，A 事实（node-a）被滤掉。
	filtered, err := runtime.UnifiedEvents(context.Background(), sessionID, "node-1", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered.Events) != 1 || filtered.Events[0].Source != summaryEventSource {
		t.Fatalf("node filter = %#v, want only the node-1 summary", filtered.Events)
	}
}

// TestUnifiedEventsRequiresRouter 验证未装配 sessionstore Router 时统一
// 查询返回可读错误（不 panic、不静默降级）。
func TestUnifiedEventsRequiresRouter(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()
	if _, err := runtime.UnifiedEvents(context.Background(), "session-x", "", 10); err == nil {
		t.Fatal("want error when sessionstore router is not attached")
	}
}
