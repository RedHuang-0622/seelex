package sessionstore

import (
	"testing"
	"time"
)

// lifecycle 队列「消费凭据」协议（durable queue 写入侧的存储语义）。
//
// 关键不变量（N 项 → 1 轮的合并语义）：
//   - 提升即标记：全部待发项 consumed_by = 该轮 turnID；
//   - 确认/失败都以「该轮是否已发布」为凭据，不以项自己的 request_id；
//   - 恢复时：该轮未发布 → 重发（不丢输入）；已发布 → 出队（不重发）；
//   - 未消费项在恢复路径中保持原样（不得被判为"已发送"）；
//   - 标记不得"偷走"更早未确认轮的项。

// queueFixture 建一个空会话（lifecycle 通道无需先写 message）。
func queueFixture(t *testing.T) (*storeEngine, Key) {
	t.Helper()
	store := newStoreEngine(t.TempDir(), storageSettings{MessageShardRows: 0})
	return store, Key{ProjectID: "project-q", SessionID: "session-q"}
}

func publishTurnRow(t *testing.T, store *storeEngine, key Key, turnID string) {
	t.Helper()
	row := Event{Seq: 0, TaskID: turnID, Role: "user", Kind: EventKindUserInput, Content: "turn " + turnID, CreatedAt: time.Now().UTC()}
	if _, err := store.messageCommit(key, "commit-"+turnID, []Event{row}); err != nil {
		t.Fatal(err)
	}
}

func mustEnqueue(t *testing.T, store *storeEngine, key Key, requestID, content string) {
	t.Helper()
	if err := store.queueEnqueue(key, requestID, content); err != nil {
		t.Fatal(err)
	}
}

func queueSnapshot(t *testing.T, store *storeEngine, key Key) []QueueItem {
	t.Helper()
	items, err := store.queueItems(key)
	if err != nil {
		t.Fatal(err)
	}
	return queueItemsView(items)
}

// TestQueueConsumeConfirmClearsBatch 提升 → 确认 出队，且幂等。
func TestQueueConsumeConfirmClearsBatch(t *testing.T) {
	store, key := queueFixture(t)
	mustEnqueue(t, store, key, "r1", "第一问")
	mustEnqueue(t, store, key, "r2", "第二问")

	if err := store.queueMarkConsumed(key, "turn-1"); err != nil {
		t.Fatal(err)
	}
	marked := queueSnapshot(t, store, key)
	if len(marked) != 2 {
		t.Fatalf("marked = %d items, want 2", len(marked))
	}
	for _, item := range marked {
		if item.TurnID != "turn-1" || item.State != string(queueSent) {
			t.Fatalf("item = %+v, want turn_id=turn-1 state=sent", item)
		}
	}
	// 标记后的项**不再**被第二次标记接管（未确认轮不得被偷）。
	if err := store.queueMarkConsumed(key, "turn-2"); err != nil {
		t.Fatal(err)
	}
	for _, item := range queueSnapshot(t, store, key) {
		if item.TurnID != "turn-1" {
			t.Fatalf("second mark stole item: %+v", item)
		}
	}

	if err := store.queueConfirmConsumed(key, "turn-1"); err != nil {
		t.Fatal(err)
	}
	if items := queueSnapshot(t, store, key); len(items) != 0 {
		t.Fatalf("after confirm = %d items, want 0", len(items))
	}
	// 幂等：重复确认不报错、不复活条目。
	if err := store.queueConfirmConsumed(key, "turn-1"); err != nil {
		t.Fatal(err)
	}
	if items := queueSnapshot(t, store, key); len(items) != 0 {
		t.Fatalf("after second confirm = %d items, want 0", len(items))
	}
}

// TestQueueConsumeFailReturnsContentToDraft 该轮未发布（失败）→ 内容回草稿。
func TestQueueConsumeFailReturnsContentToDraft(t *testing.T) {
	store, key := queueFixture(t)
	mustEnqueue(t, store, key, "r1", "第一问")
	mustEnqueue(t, store, key, "r2", "第二问")
	if err := store.queueMarkConsumed(key, "turn-1"); err != nil {
		t.Fatal(err)
	}
	if err := store.queueFailConsumed(key, "turn-1"); err != nil {
		t.Fatal(err)
	}
	if items := queueSnapshot(t, store, key); len(items) != 0 {
		t.Fatalf("after fail = %d items, want 0（失败批次出队）", len(items))
	}
	state, err := store.readLifecycleState(key)
	if err != nil {
		t.Fatal(err)
	}
	if state.Draft == nil {
		t.Fatal("fail 后草稿为空，want 内容回草稿（先队列后草稿的逆迁移）")
	}
	want := "第一问\n---\n第二问"
	if state.Draft.Content != want {
		t.Fatalf("draft = %q, want %q", state.Draft.Content, want)
	}
	if state.Draft.State != "发送失败退回" {
		t.Fatalf("draft state = %q, want 发送失败退回", state.Draft.State)
	}
}

// TestQueueRecoverResendsUnpublishedConsumedBatch 崩溃窗口：已消费但该轮未
// 发布 → 恢复必须把输入交回重发（不丢输入）。
func TestQueueRecoverResendsUnpublishedConsumedBatch(t *testing.T) {
	store, key := queueFixture(t)
	mustEnqueue(t, store, key, "r1", "第一问")
	mustEnqueue(t, store, key, "r2", "第二问")
	if err := store.queueMarkConsumed(key, "turn-1"); err != nil {
		t.Fatal(err)
	}
	report, err := store.queueRecoverItems(key)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Resent) != 2 {
		t.Fatalf("resent = %d, want 2（该轮未发布 → 重发）", len(report.Resent))
	}
	if len(report.Dropped) != 0 {
		t.Fatalf("dropped = %d, want 0", len(report.Dropped))
	}
	for _, item := range report.Resent {
		if item.State != string(queueQueued) || item.TurnID != "" {
			t.Fatalf("resent item = %+v, want state=queued turn_id 已清空", item)
		}
	}
	if len(report.Pending) != 2 {
		t.Fatalf("pending = %d, want 2（重发项仍在队列）", len(report.Pending))
	}
}

// TestQueueRecoverDropsConsumedBatchWhenTurnPublished 该轮已发布 → 出队，
// 且不重发（重复输入护栏）。
func TestQueueRecoverDropsConsumedBatchWhenTurnPublished(t *testing.T) {
	store, key := queueFixture(t)
	mustEnqueue(t, store, key, "r1", "第一问")
	mustEnqueue(t, store, key, "r2", "第二问")
	if err := store.queueMarkConsumed(key, "turn-1"); err != nil {
		t.Fatal(err)
	}
	publishTurnRow(t, store, key, "turn-1")

	report, err := store.queueRecoverItems(key)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Dropped) != 2 {
		t.Fatalf("dropped = %d, want 2（该轮已发布 = 最终确认）", len(report.Dropped))
	}
	if len(report.Resent) != 0 {
		t.Fatalf("resent = %d, want 0（已发布不得重发）", len(report.Resent))
	}
	if items := queueSnapshot(t, store, key); len(items) != 0 {
		t.Fatalf("queue = %d items, want 0", len(items))
	}
}

// TestQueueRecoverKeepsUnconsumedItems 未消费项（用户已发出但还没被提升）
// 在恢复路径中必须保持原样。
func TestQueueRecoverKeepsUnconsumedItems(t *testing.T) {
	store, key := queueFixture(t)
	mustEnqueue(t, store, key, "r1", "排队中")
	report, err := store.queueRecoverItems(key)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Resent) != 0 || len(report.Dropped) != 0 {
		t.Fatalf("report = %+v, want 无重发无出队", report)
	}
	if len(report.Pending) != 1 {
		t.Fatalf("pending = %d, want 1", len(report.Pending))
	}
	items := queueSnapshot(t, store, key)
	if len(items) != 1 || items[0].Content != "排队中" || items[0].TurnID != "" {
		t.Fatalf("items = %+v, want 原样保留", items)
	}
}

// TestQueueRecoverSeparatesTurns 两轮交错：更早的未确认轮不被后一轮的标记
// 接管，恢复时各自按"该轮是否已发布"判定。
func TestQueueRecoverSeparatesTurns(t *testing.T) {
	store, key := queueFixture(t)
	mustEnqueue(t, store, key, "r1", "第一轮输入")
	if err := store.queueMarkConsumed(key, "turn-1"); err != nil {
		t.Fatal(err)
	}
	mustEnqueue(t, store, key, "r2", "第二轮输入")
	if err := store.queueMarkConsumed(key, "turn-2"); err != nil {
		t.Fatal(err)
	}
	items := queueSnapshot(t, store, key)
	byContent := map[string]string{}
	for _, item := range items {
		byContent[item.Content] = item.TurnID
	}
	if byContent["第一轮输入"] != "turn-1" || byContent["第二轮输入"] != "turn-2" {
		t.Fatalf("consume 归属错乱: %+v", byContent)
	}
	// 只有第一轮发布了 → 第一轮出队，第二轮重发。
	publishTurnRow(t, store, key, "turn-1")
	report, err := store.queueRecoverItems(key)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Dropped) != 1 || report.Dropped[0].Content != "第一轮输入" {
		t.Fatalf("dropped = %+v, want 第一轮输入", report.Dropped)
	}
	if len(report.Resent) != 1 || report.Resent[0].Content != "第二轮输入" {
		t.Fatalf("resent = %+v, want 第二轮输入", report.Resent)
	}
}

// TestQueueRecoverItemsMatchesLegacyQueueRecover 改造后的条目级恢复与既有
// queueRecover 计数口径一致（避免两条路径语义漂移）。
func TestQueueRecoverItemsMatchesLegacyQueueRecover(t *testing.T) {
	store, key := queueFixture(t)
	mustEnqueue(t, store, key, "r1", "第一问")
	if err := store.queueMarkConsumed(key, "turn-1"); err != nil {
		t.Fatal(err)
	}
	resent, err := store.queueRecover(key)
	if err != nil {
		t.Fatal(err)
	}
	if len(resent) != 1 {
		t.Fatalf("legacy queueRecover resent = %d, want 1", len(resent))
	}
	report, err := store.queueRecoverItems(key)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Resent) != 0 {
		t.Fatalf("第二次恢复 resent = %d, want 0（第一次已把项回 queued）", len(report.Resent))
	}
	if len(report.Pending) != 1 {
		t.Fatalf("pending = %d, want 1", len(report.Pending))
	}
}
