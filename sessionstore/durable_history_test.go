package sessionstore

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"
)

func newTestRouter(t *testing.T) *Router {
	t.Helper()
	root := t.TempDir()
	router, err := NewRouter(filepath.Join(root, "session-storage.json"), root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = router.Close() })
	return router
}

func TestDurableHistoryRoundTrip(t *testing.T) {
	router := newTestRouter(t)
	history := NewDurableHistory(router, "session-roundtrip")

	stored := messages(3, "round")
	if err := history.Save(context.Background(), stored); err != nil {
		t.Fatal(err)
	}
	loaded, err := history.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 3 || *loaded[0].Content != "round-0" {
		t.Fatalf("loaded = %#v", loaded)
	}
}

func TestDurableHistoryClearIsResetSemantics(t *testing.T) {
	router := newTestRouter(t)
	history := NewDurableHistory(router, "session-clear")
	if err := history.Save(context.Background(), messages(2, "keep")); err != nil {
		t.Fatal(err)
	}
	if err := history.Clear(context.Background()); err != nil {
		t.Fatal(err)
	}
	loaded, err := history.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 0 {
		t.Fatalf("cleared history = %d messages, want empty", len(loaded))
	}
	// 显式 Clear 后可重新开始会话（对应旧 ClearHistory + store 清理）。
	if err := history.Save(context.Background(), messages(1, "fresh")); err != nil {
		t.Fatal(err)
	}
	if loaded, err := history.Load(context.Background()); err != nil || len(loaded) != 1 {
		t.Fatalf("re-saved history = %#v err=%v", loaded, err)
	}
}

func TestDurableHistorySaveOrchestratesStateBlob(t *testing.T) {
	router := newTestRouter(t)
	history := NewDurableHistory(router, "session-state")
	contextStore := NewSessionContextStore(router, "session-state")
	history.AttachStateStore(contextStore)

	if err := contextStore.PushTask(TaskFrame{TaskID: "task-1", Objective: "inspect", Status: "active"}); err != nil {
		t.Fatal(err)
	}
	// Save 时编排：ProviderHistory 原子写 + state blob 持久化。
	if err := history.Save(context.Background(), messages(1, "state")); err != nil {
		t.Fatal(err)
	}
	reloaded := NewSessionContextStore(router, "session-state")
	if err := reloaded.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	record := reloaded.Snapshot()
	if len(record.TaskStack) != 1 || record.TaskStack[0].TaskID != "task-1" {
		t.Fatalf("state blob after Save = %+v", record.TaskStack)
	}
	// Clear 后 state 缓存一并重置。
	if err := history.Clear(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := contextStore.Snapshot(); len(got.TaskStack) != 0 {
		t.Fatalf("state cache after Clear = %+v", got.TaskStack)
	}
}

func TestDurableHistoryLoadEventTailKeepsWindowUnits(t *testing.T) {
	router := newTestRouter(t)
	history := NewDurableHistory(router, "session-window")
	// 3 个完整协议单元：1:(1,2) 2:(3,4,5,6) 3:(7,8)。
	commit := Commit{Events: []Event{
		{Seq: 1, Role: "user", Content: "q1", TokenCount: 1},
		{Seq: 2, Role: "assistant", Content: "a1", TokenCount: 1},
		{Seq: 3, Role: "user", Content: "q2", TokenCount: 1},
		{Seq: 4, Role: "assistant", ToolCalls: []EventToolCall{{ID: "a"}}, TokenCount: 1},
		{Seq: 5, Role: "tool", ToolCallID: "a", Content: "r2", TokenCount: 1},
		{Seq: 6, Role: "assistant", Content: "a2", TokenCount: 1},
		{Seq: 7, Role: "user", Content: "q3", TokenCount: 1},
		{Seq: 8, Role: "assistant", Content: "a3", TokenCount: 1},
	}}
	if err := router.SaveCommit("session-window", commit); err != nil {
		t.Fatal(err)
	}
	// maxUnits=2 → 窗口保留最后 2 轮（窗口外第 1 轮被排除）。
	tail, err := history.LoadEventTail(context.Background(), 100, 2)
	if err != nil {
		t.Fatal(err)
	}
	wantSeq := []uint64{3, 4, 5, 6, 7, 8}
	gotSeq := make([]uint64, len(tail))
	for index := range tail {
		gotSeq[index] = tail[index].Seq
	}
	if !reflect.DeepEqual(gotSeq, wantSeq) {
		t.Fatalf("window tail seq=%v, want %v", gotSeq, wantSeq)
	}
	// token 预算收紧 → 只保留预算内单元。
	tight, err := history.LoadEventTail(context.Background(), 2, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(tight) != 2 || tight[0].Seq != 7 || tight[1].Seq != 8 {
		t.Fatalf("token-bounded tail = %+v", tight)
	}
}

// TestDurableHistoryLoadUsesWindowTailBudget 验证滑动窗口加载区间（D1）：
// SetTailBudget 后 Load 只装载窗口轮数（不再全量），未配置时保持全量。
func TestDurableHistoryLoadUsesWindowTailBudget(t *testing.T) {
	router := newTestRouter(t)
	events := []Event{
		{Seq: 1, Role: "user", Content: "q1", TokenCount: 1},
		{Seq: 2, Role: "assistant", Content: "a1", TokenCount: 1},
		{Seq: 3, Role: "user", Content: "q2", TokenCount: 1},
		{Seq: 4, Role: "assistant", Content: "a2", TokenCount: 1},
		{Seq: 5, Role: "user", Content: "q3", TokenCount: 1},
		{Seq: 6, Role: "assistant", Content: "a3", TokenCount: 1},
	}
	if err := router.SaveCommit("session-windowed-load", Commit{Events: events}); err != nil {
		t.Fatal(err)
	}

	// 未配置预算 → 全量路径（Router.Load；此处事件未转消息 blob → 空，
	// 语义与旧行为一致——全量读由消息 blob 承载）。
	history := NewDurableHistory(router, "session-windowed-load")
	if all, err := history.Load(context.Background()); err != nil || len(all) != 0 {
		t.Fatalf("unconfigured load = %d err=%v, want legacy Read semantics", len(all), err)
	}

	// 配置窗口预算（maxUnits=2 轮）→ Load 只返回最后 2 轮（4 条消息，
	// 事件库窗口读）。
	windowed := NewDurableHistory(router, "session-windowed-load")
	windowed.SetTailBudget(100, 2)
	window, err := windowed.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(window) != 4 {
		t.Fatalf("windowed load = %d messages, want 4 (last 2 rounds)", len(window))
	}
	if window[0].Content == nil || *window[0].Content != "q2" {
		t.Fatalf("windowed load must start at round 2 (last 2 rounds), got %+v", window[0])
	}
	// 工具调用字段跨事件流转保留（ToolCalls 映射）。
	if err := router.SaveCommit("session-windowed-load", Commit{Events: []Event{
		{Seq: 7, Role: "user", Content: "q4", TokenCount: 1},
		{Seq: 8, Role: "assistant", ToolCalls: []EventToolCall{{ID: "t1", Name: "read_file", Arguments: `{"path":"a"}`}}, TokenCount: 1},
		{Seq: 9, Role: "tool", ToolCallID: "t1", Content: "file", TokenCount: 1},
	}}); err != nil {
		t.Fatal(err)
	}
	toolWindow, err := windowed.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	foundCall := false
	for _, msg := range toolWindow {
		for _, call := range msg.ToolCalls {
			if call.Function.Name == "read_file" {
				foundCall = true
			}
		}
	}
	if !foundCall {
		t.Fatalf("windowed load must preserve tool calls: %+v", toolWindow)
	}
}

func TestDurableHistoryNilRouterIsInMemory(t *testing.T) {
	history := NewDurableHistory(nil, "memory")
	if err := history.Save(context.Background(), messages(1, "mem")); err != nil {
		t.Fatal(err)
	}
	loaded, err := history.Load(context.Background())
	if err != nil || len(loaded) != 0 {
		t.Fatalf("memory history = %#v err=%v", loaded, err)
	}
	if err := history.Clear(context.Background()); err != nil {
		t.Fatal(err)
	}
}
