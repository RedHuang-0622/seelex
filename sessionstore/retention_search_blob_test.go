package sessionstore

import (
	"errors"
	"os"
	"strings"
	"testing"
)

func retentionFixture(t *testing.T) (*storeEngine, Key) {
	t.Helper()
	store := newStoreEngine(t.TempDir(), storageSettings{})
	return store, Key{ProjectID: "p-wm", SessionID: "s-wm"}
}

// ---------- LRU / retention ----------

// TestRetentionManualModeRejectsAutoDelete 对应 T-WM-01：mode=manual 触发淘汰 →
// 拒绝自动删除。
func TestRetentionManualModeRejectsAutoDelete(t *testing.T) {
	store, key := retentionFixture(t)
	commitRoundRows(t, store, key, roundRows(20))
	if _, err := store.lRUDelete(key, 5, false); !errors.Is(err, ErrRetentionRequiresConfirm) {
		t.Fatalf("err = %v", err)
	}
}

// TestRetentionConfirmDeleteKeepsHoles 对应 T-WM-02：用户确认删除前 30 行 →
// message_id/seq 空洞保留、watermark 前移。
func TestRetentionConfirmDeleteKeepsHoles(t *testing.T) {
	store, key := retentionFixture(t)
	commitRoundRows(t, store, key, roundRows(100))
	retention, err := store.lRUDelete(key, 30, true)
	if err != nil {
		t.Fatal(err)
	}
	if retention.WatermarkSeq != 30 {
		t.Fatalf("watermark = %d", retention.WatermarkSeq)
	}
	head, err := store.readMessageHead(key)
	if err != nil {
		t.Fatal(err)
	}
	if head.WatermarkSeq != 30 || head.LastSeq != 100 {
		t.Fatalf("head watermark=%d last=%d", head.WatermarkSeq, head.LastSeq)
	}
	rows, err := store.readRows(key, 1, 10)
	if err != nil || len(rows) != 0 {
		t.Fatalf("deleted prefix visible: len=%d err=%v", len(rows), err)
	}
	after, err := store.readRows(key, 31, 40)
	if err != nil || len(after) != 10 || after[0].Seq != 31 {
		t.Fatalf("rows after deletion: len=%d first=%+v err=%v", len(after), after[0], err)
	}
	page, _, err := store.pageHistoryRows(key, 0, 35)
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 30; index++ {
		if !page[index].Placeholder {
			t.Fatalf("page[%d] not placeholder", index)
		}
	}
}

// TestRetentionVerifyAfterDelete 对应 T-WM-03：删除后 verify 通过，空洞被接受。
func TestRetentionVerifyAfterDelete(t *testing.T) {
	store, key := retentionFixture(t)
	commitRoundRows(t, store, key, roundRows(100))
	if _, err := store.lRUDelete(key, 30, true); err != nil {
		t.Fatal(err)
	}
	if err := store.verifyMessage(key); err != nil {
		t.Fatalf("verify after delete: %v", err)
	}
	head, err := store.readMessageHead(key)
	if err != nil {
		t.Fatal(err)
	}
	if head.TotalRows != 70 {
		t.Fatalf("total = %d want 70", head.TotalRows)
	}
}

// TestRetentionCrashMidRewriteNoIntermediate 对应 T-WM-04：删除重写中途崩溃只能
// 落"旧分片+旧 head"或"新 head 已发布"二选一，无中间态。模拟：只写新分片
// 未发布 head → 旧 head 完整、旧行仍可读；下次提交自愈清理。
func TestRetentionCrashMidRewriteNoIntermediate(t *testing.T) {
	store, key := retentionFixture(t)
	commitRoundRows(t, store, key, roundRows(40))
	headBefore, err := store.readMessageHead(key)
	if err != nil {
		t.Fatal(err)
	}
	oldShard := headBefore.Shards[0]
	// 模拟新分片已写但 head 未发布（崩溃现场）。
	rows, err := store.readRows(key, 31, 40)
	if err != nil {
		t.Fatal(err)
	}
	newShardPath := store.shardPath(key, 31, 40)
	if err := writeMessageRowsFile(newShardPath, rows); err != nil {
		t.Fatal(err)
	}
	head, err := store.readMessageHead(key)
	if err != nil {
		t.Fatal(err)
	}
	if head.LastSeq != headBefore.LastSeq || len(head.Shards) != len(headBefore.Shards) {
		t.Fatalf("head advanced without publish: %+v", head)
	}
	all, err := store.readAllRows(key)
	if err != nil || len(all) != 40 {
		t.Fatalf("old rows broken after crash: len=%d err=%v", len(all), err)
	}
	if _, err := os.Stat(store.messageDir(key)); err != nil {
		t.Fatal(err)
	}
	// 下次提交：清理孤儿分片后正常（发布点恢复语义）。
	if _, err := store.messageCommit(key, "next", []Event{messageRow(41, "msg41", "user", EventKindUserInput, "续")}); err != nil {
		t.Fatal(err)
	}
	if err := store.verifyMessage(key); err != nil {
		t.Fatal(err)
	}
	_ = oldShard
}

// TestRetentionSearchSummaryAfterDelete 对应 T-WM-05：行删除后无引用 blob；检索
// 索引摘要仍可命中该区。
func TestRetentionSearchSummaryAfterDelete(t *testing.T) {
	store, key := retentionFixture(t)
	rows := roundRows(12)
	for index := 0; index < 5; index++ {
		rows[index].Content = "需要保留的敏感关键词-" + rows[index].Content
	}
	commitRoundRows(t, store, key, rows)
	frame := compactFrameRecord{
		FrameID: "f1", MessageFrom: "msg1", MessageTo: "msg5",
		MessageFromSeq: 1, MessageToSeq: 5,
		Summary: "敏感关键词 摘要覆盖前五轮", BoundaryStatus: "complete",
	}
	if _, err := store.compactCommit(key, frame); err != nil {
		t.Fatal(err)
	}
	if _, err := store.lRUDelete(key, 5, true); err != nil {
		t.Fatal(err)
	}
	if err := store.rebuildSearchIndex(key); err != nil {
		t.Fatal(err)
	}
	hits, err := store.searchQuery(key, "敏感关键词")
	if err != nil {
		t.Fatal(err)
	}
	foundSummary := false
	for _, hit := range hits {
		if hit.SummaryOnly && hit.FromSeq == 1 && hit.ToSeq == 5 {
			foundSummary = true
		}
	}
	if !foundSummary {
		t.Fatalf("summary-only hit missing: %+v", hits)
	}
}

// ---------- EVENT / 锚点 ----------

// TestStructuralEventAnchorAfterMessage 对应 T-EV-01：compacted anchor=msg7 → 事件位于
// msg7 之后；R2 tail 从 msg8 开始（含端点语义）。
func TestStructuralEventAnchorAfterMessage(t *testing.T) {
	store, key := retentionFixture(t)
	commitRoundRows(t, store, key, roundRows(12))
	event := structuralEvent{Kind: structuralEventCompacted, AnchorMessageID: "msg7", AnchorSeq: 7, Payload: rawJSON(`{"reason":"budget"}`)}
	if _, err := store.structuralEventCommit(key, "ev-c", []structuralEvent{event}); err != nil {
		t.Fatal(err)
	}
	events, err := store.readEvents(key, 0, 0)
	if err != nil || len(events) != 1 || events[0].AnchorSeq != 7 || events[0].AnchorMessageID != "msg7" {
		t.Fatalf("events=%+v err=%v", events, err)
	}
	// wire 装配只依赖 compact/message；这里用 compact head 验证端点语义。
	frame := compactFrameRecord{
		FrameID: "f1", MessageFrom: "msg3", MessageTo: "msg7",
		MessageFromSeq: 3, MessageToSeq: 7, Summary: "摘要", BoundaryStatus: "complete",
	}
	if _, err := store.compactCommit(key, frame); err != nil {
		t.Fatal(err)
	}
	result, err := store.assembleWire(key, nil, wireParams{Budget: 200_000})
	if err != nil || result.TailStartSeq != 8 {
		t.Fatalf("tail start=%d err=%v", result.TailStartSeq, err)
	}
}

// TestStructuralEventDuplicateCommitIdempotent 对应 T-EV-02：同 commit 重复持久化 →
// 事件不重复（head.last_commit_id 水位幂等，零历史重读）。
func TestStructuralEventDuplicateCommitIdempotent(t *testing.T) {
	store, key := retentionFixture(t)
	events := []structuralEvent{{Kind: structuralEventFork, AnchorSeq: 3, Payload: rawJSON(`{"child":"c1"}`)}}
	if _, err := store.structuralEventCommit(key, "ev-dup", events); err != nil {
		t.Fatal(err)
	}
	if _, err := store.structuralEventCommit(key, "ev-dup", events); err != nil {
		t.Fatal(err)
	}
	all, err := store.readEvents(key, 0, 0)
	if err != nil || len(all) != 1 {
		t.Fatalf("events len=%d err=%v", len(all), err)
	}
}

// TestStructuralEventMissingEventOK 对应 T-EV-03：message 完整但 compacted 事件缺失 →
// 不补事件也可正常装配（compact.json head 为准）。
func TestStructuralEventMissingEventOK(t *testing.T) {
	store, key := retentionFixture(t)
	commitRoundRows(t, store, key, roundRows(8))
	frame := compactFrameRecord{
		FrameID: "f1", MessageFrom: "msg1", MessageTo: "msg4",
		MessageFromSeq: 1, MessageToSeq: 4, Summary: "摘要", BoundaryStatus: "complete",
	}
	if _, err := store.compactCommit(key, frame); err != nil {
		t.Fatal(err)
	}
	if err := store.deleteModule(key, moduleEvent); err != nil {
		t.Fatal(err)
	}
	result, err := store.assembleWire(key, nil, wireParams{Budget: 200_000})
	if err != nil || len(result.Messages) == 0 {
		t.Fatalf("assemble err=%v", err)
	}
}

// TestStructuralEventAnchorAtOrBelowWatermarkOK 对应 T-EV-04：event 锚 ≤ watermark 视
// 为已淘汰区引用，不算损坏。
func TestStructuralEventAnchorAtOrBelowWatermarkOK(t *testing.T) {
	store, key := retentionFixture(t)
	commitRoundRows(t, store, key, roundRows(30))
	if _, err := store.lRUDelete(key, 10, true); err != nil {
		t.Fatal(err)
	}
	if _, err := store.structuralEventCommit(key, "ev-old", []structuralEvent{{Kind: structuralEventRolledBack, AnchorSeq: 5, AnchorMessageID: "msg5", Payload: rawJSON(`{"reason":"audit"}`)}}); err != nil {
		t.Fatal(err)
	}
	if err := store.verifyMessage(key); err != nil {
		t.Fatalf("verify with stale anchor: %v", err)
	}
	events, err := store.readEvents(key, 0, 0)
	if err != nil || len(events) != 1 {
		t.Fatalf("events=%+v err=%v", events, err)
	}
}

// ---------- 检索 ----------

// TestSearchPerSessionHits 对应 T-SR-01：关键词命中正确片段区间；范围=本会话
// （不跨会话）。
func TestSearchPerSessionHits(t *testing.T) {
	storeA, keyA := retentionFixture(t)
	rowsA := roundRows(8)
	rowsA[1].Content = "Alpha 项目关键结论"
	rowsA[3].Content = "继续讨论 Alpha 项目"
	commitRoundRows(t, storeA, keyA, rowsA)
	storeB, keyB := retentionFixture(t)
	rowsB := roundRows(8)
	rowsB[1].Content = "Beta 项目结论"
	commitRoundRows(t, storeB, keyB, rowsB)
	if err := storeA.rebuildSearchIndex(keyA); err != nil {
		t.Fatal(err)
	}
	if err := storeB.rebuildSearchIndex(keyB); err != nil {
		t.Fatal(err)
	}
	hits, err := storeA.searchQuery(keyA, "Alpha 项目")
	if err != nil || len(hits) == 0 {
		t.Fatalf("hits=%+v err=%v", hits, err)
	}
	for _, hit := range hits {
		if hit.ToSeq > 8 {
			t.Fatalf("hit out of session range: %+v", hit)
		}
	}
	cross, err := storeA.searchQuery(keyA, "Beta")
	if err != nil || len(cross) != 0 {
		t.Fatalf("cross-session hit: %+v err=%v", cross, err)
	}
}

// TestSearchLagAndRebuild 对应 T-SR-02：append 后未重建允许落后；重建后结果
// 一致（可重建断言）。
func TestSearchLagAndRebuild(t *testing.T) {
	store, key := retentionFixture(t)
	commitRoundRows(t, store, key, roundRows(6))
	if err := store.rebuildSearchIndex(key); err != nil {
		t.Fatal(err)
	}
	// append 新关键词后索引未重建：旧查询仍命中旧内容（允许落后）。
	extra := messageRow(7, "msg7", "user", EventKindUserInput, "崭新话题词")
	commitRoundRows(t, store, key, []Event{extra})
	before, err := store.searchQuery(key, "崭新话题词")
	if err != nil {
		t.Fatal(err)
	}
	if len(before) != 0 {
		t.Fatalf("stale index unexpectedly hits: %+v", before)
	}
	if err := store.rebuildSearchIndex(key); err != nil {
		t.Fatal(err)
	}
	after, err := store.searchQuery(key, "崭新话题词")
	if err != nil || len(after) == 0 {
		t.Fatalf("rebuild missed: hits=%+v err=%v", after, err)
	}
}

// TestSearchDeletedRegionSummaryOnly 对应 T-SR-03：LRU 删除后重建 → 被删区只
// 回摘要命中，无悬空原文（WM-05 覆盖，这里再断言无原始 snippet 命中）。
func TestSearchDeletedRegionSummaryOnly(t *testing.T) {
	store, key := retentionFixture(t)
	rows := roundRows(20)
	for index := 0; index < 10; index++ {
		rows[index].Content = "已删除独家原文文本-" + rows[index].Content
	}
	commitRoundRows(t, store, key, rows)
	if _, err := store.lRUDelete(key, 10, true); err != nil {
		t.Fatal(err)
	}
	if err := store.rebuildSearchIndex(key); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(store.searchIndexPath(key))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "已删除独家原文文本") {
		t.Fatal("deleted original text leaked into search index")
	}
}

// ---------- blob ----------

// TestBigToolResultHardLimitNotPersisted 对应 T-BL-01：输出 > hard_limit → 不落盘、
// 返回明确错误。
func TestBigToolResultHardLimitNotPersisted(t *testing.T) {
	store, key := retentionFixture(t)
	huge := strings.Repeat("x", defaultStorageSettings().BlobHardLimitBytes+1)
	if _, err := store.writeBlob(key, "bash", huge); !errors.Is(err, ErrBigToolResultTooLarge) {
		t.Fatalf("err = %v", err)
	}
	hashes, err := store.listBlobHashes(key)
	if err != nil || len(hashes) != 0 {
		t.Fatalf("blob persisted: %+v err=%v", hashes, err)
	}
}

// TestBigToolResultSoftLimitTruncatesWithRef 对应 T-BL-02：输出 > soft_limit → 截断 +
// result_ref；read_tool_result 可读回。
func TestBigToolResultSoftLimitTruncatesWithRef(t *testing.T) {
	store, key := retentionFixture(t)
	long := strings.Repeat("长", defaultStorageSettings().BlobSoftLimitChars+100)
	blob, err := store.writeBlob(key, "read", long)
	if err != nil {
		t.Fatal(err)
	}
	if !blob.Truncated || blob.Size != defaultStorageSettings().BlobSoftLimitChars {
		t.Fatalf("blob = %+v", blob)
	}
	content, err := store.readBlob(key, blob.Hash)
	if err != nil || len(content) != defaultStorageSettings().BlobSoftLimitChars {
		t.Fatalf("read content len=%d err=%v", len(content), err)
	}
	if !strings.HasPrefix(blobRefOf(blobRefPrefix+blob.Hash), blob.Hash) {
		t.Fatal("ref normalize broken")
	}
}

// TestBigToolResultQuotaDryRun 对应 T-BL-03：超会话配额 GC dry-run 输出可回收清单，
// 不自动删。
func TestBigToolResultQuotaDryRun(t *testing.T) {
	store, key := retentionFixture(t)
	if _, err := store.writeBlob(key, "a", "AAA"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.writeBlob(key, "b", "BBB"); err != nil {
		t.Fatal(err)
	}
	removed, err := store.blobGarbageCollect(key, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) != 2 {
		t.Fatalf("dry-run candidates = %+v", removed)
	}
	hashes, err := store.listBlobHashes(key)
	if err != nil || len(hashes) != 2 {
		t.Fatalf("dry-run deleted blobs: %+v err=%v", hashes, err)
	}
}

// ---------- config ----------

// TestStorageSettingsOverrideAndValidation 对应 T-CFG-01：修改 wire_recent_errors/阈值
// 覆盖默认值；非法值被拒绝。
func TestStorageSettingsOverrideAndValidation(t *testing.T) {
	defaults := defaultStorageSettings()
	if defaults.WireRecentErrors != 3 || defaults.MessageShardRows != 100 {
		t.Fatalf("defaults = %+v", defaults)
	}
	config := defaults
	config.WireRecentErrors = 5
	config.CompactFrameThreshold = 60
	if err := validateStorageSettings(config); err != nil {
		t.Fatal(err)
	}
	bad := defaults
	bad.WireRecentErrors = 0
	if err := validateStorageSettings(bad); err == nil {
		t.Fatal("invalid wire_recent_errors accepted")
	}
	bad2 := defaults
	bad2.BlobSoftLimitChars = -1
	if err := validateStorageSettings(bad2); err == nil {
		t.Fatal("invalid soft limit accepted")
	}
	if defaults.WireBudgetTokens != 200000 || defaults.WireSoftRatio != 0.75 || defaults.WireTargetRatio != 0.60 {
		t.Fatalf("§5.2 wire 预算默认值 = %+v", defaults)
	}
	if budget, soft, target := defaults.wireBudget(); budget != 200000 || soft != 150000 || target != 120000 {
		t.Fatalf("wireBudget = %d/%d/%d", budget, soft, target)
	}
	// 覆盖链：后一层只覆盖它显式给出的字段。
	limits := storageSettings{MessageShardRows: 50, StaleAfterSeconds: 60}
	persisted := storageSettings{RetryCacheMaxItems: 8}
	merged := resolveStorageSettings(limits, persisted)
	if merged.MessageShardRows != 50 || merged.StaleAfterSeconds != 60 || merged.RetryCacheMaxItems != 8 {
		t.Fatalf("merged = %+v", merged)
	}
	if merged.CompactFrameThreshold != defaults.CompactFrameThreshold ||
		merged.RawBytesAlert != defaults.RawBytesAlert || merged.WireRecentErrors != defaults.WireRecentErrors {
		t.Fatalf("未覆盖的键必须保持默认: %+v", merged)
	}
	if boolValue(defaults.AutoRecover, true) || !boolValue(defaults.QueuePersistPending, false) {
		t.Fatal("auto_recover 默认 false / queue.persist_pending 默认 true")
	}
	if err := validateStorageSettings(resolveStorageSettings()); err != nil {
		t.Fatalf("默认值必须自洽: %v", err)
	}
	// 零值布尔项：显式 false 也要能覆盖默认 true。
	off := false
	overridden := resolveStorageSettings(storageSettings{QueuePersistPending: &off})
	if boolValue(overridden.QueuePersistPending, true) {
		t.Fatal("queue.persist_pending=false 被吞掉")
	}
}
