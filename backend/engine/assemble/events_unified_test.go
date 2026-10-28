package assemble

import (
	"context"
	"strings"
	"testing"
	"time"

	frameworkevent "github.com/RedHuang-0622/Seele/event"
	"github.com/RedHuang-0622/seelex/backend/engine/obs"
	seeletelemetry "github.com/RedHuang-0622/seelex/internal/telemetry"
	"github.com/RedHuang-0622/seelex/store"
)

// P21-6：本文件只留**宿主级**用例（走 `Runtime` 装配与两个公开读法）；包内白盒用例
// （摘要转事件、SummaryLog、Reader 过滤/区间、话题映射）随实现搬进 `backend/engine/obs`。

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
	eventStore := store.NewEventStore(router)
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
		case obs.SummaryEventSource:
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
	if len(filtered.Events) != 1 || filtered.Events[0].Source != obs.SummaryEventSource {
		t.Fatalf("node filter = %#v, want only the node-1 summary", filtered.Events)
	}
}

// TestUnifiedEventsRequiresReader 验证未装配事件读面时统一查询返回可读错误
// （不 panic、不静默降级），两个入口共用同一句。
func TestUnifiedEventsRequiresReader(t *testing.T) {
	const want = "unified events: event reader is not attached"
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()

	t.Run("UnifiedEvents", func(t *testing.T) {
		_, err := runtime.UnifiedEvents(context.Background(), "session-x", "", 10)
		if err == nil {
			t.Fatalf("未装配 router 应返回错误（期望 %q）", want)
		}
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("错误 = %q，期望含 %q", err.Error(), want)
		}
		if strings.Contains(err.Error(), "sessionstore") {
			t.Fatalf("错误文案残留旧包名：%q", err.Error())
		}
	})

	t.Run("UnifiedEventsRange", func(t *testing.T) {
		_, err := runtime.UnifiedEventsRange(context.Background(), "session-x", "", 0, 10, 10)
		if err == nil {
			t.Fatalf("未装配 router 应返回错误（期望 %q）", want)
		}
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("错误 = %q，期望含 %q", err.Error(), want)
		}
	})
}
