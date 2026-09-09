package sessionstore

import (
	"errors"
	"os"
	"strings"
	"testing"
)

func v8M4Fixture(t *testing.T) (*v8Store, Key) {
	t.Helper()
	store := newV8Store(t.TempDir(), 0)
	return store, Key{ProjectID: "p-wm", SessionID: "s-wm"}
}

// ---------- LRU / retention ----------

// TestV8WMManualModeRejectsAutoDelete 对应 T-WM-01：mode=manual 触发淘汰 →
// 拒绝自动删除。
func TestV8WMManualModeRejectsAutoDelete(t *testing.T) {
	store, key := v8M4Fixture(t)
	v8M2CommitRows(t, store, key, v8M2Rows(20))
	if _, err := store.v8LRUDelete(key, 5, false); !errors.Is(err, ErrV8RetentionRequiresConfirm) {
		t.Fatalf("err = %v", err)
	}
}

// TestV8WMConfirmDeleteKeepsHoles 对应 T-WM-02：用户确认删除前 30 行 →
// message_id/seq 空洞保留、watermark 前移。
func TestV8WMConfirmDeleteKeepsHoles(t *testing.T) {
	store, key := v8M4Fixture(t)
	v8M2CommitRows(t, store, key, v8M2Rows(100))
	retention, err := store.v8LRUDelete(key, 30, true)
	if err != nil {
		t.Fatal(err)
	}
	if retention.WatermarkSeq != 30 {
		t.Fatalf("watermark = %d", retention.WatermarkSeq)
	}
	head, err := store.v8ReadMessageHead(key)
	if err != nil {
		t.Fatal(err)
	}
	if head.WatermarkSeq != 30 || head.LastSeq != 100 {
		t.Fatalf("head watermark=%d last=%d", head.WatermarkSeq, head.LastSeq)
	}
	rows, err := store.v8ReadRows(key, 1, 10)
	if err != nil || len(rows) != 0 {
		t.Fatalf("deleted prefix visible: len=%d err=%v", len(rows), err)
	}
	after, err := store.v8ReadRows(key, 31, 40)
	if err != nil || len(after) != 10 || after[0].Seq != 31 {
		t.Fatalf("rows after deletion: len=%d first=%+v err=%v", len(after), after[0], err)
	}
	page, _, err := store.v8R1Page(key, 0, 35)
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 30; index++ {
		if !page[index].Placeholder {
			t.Fatalf("page[%d] not placeholder", index)
		}
	}
}

// TestV8WMVerifyAfterDelete 对应 T-WM-03：删除后 verify 通过，空洞被接受。
func TestV8WMVerifyAfterDelete(t *testing.T) {
	store, key := v8M4Fixture(t)
	v8M2CommitRows(t, store, key, v8M2Rows(100))
	if _, err := store.v8LRUDelete(key, 30, true); err != nil {
		t.Fatal(err)
	}
	if err := store.v8VerifyMessage(key); err != nil {
		t.Fatalf("verify after delete: %v", err)
	}
	head, err := store.v8ReadMessageHead(key)
	if err != nil {
		t.Fatal(err)
	}
	if head.TotalRows != 70 {
		t.Fatalf("total = %d want 70", head.TotalRows)
	}
}

// TestV8WMCrashMidRewriteNoIntermediate 对应 T-WM-04：删除重写中途崩溃只能
// 落"旧分片+旧 head"或"新 head 已发布"二选一，无中间态。模拟：只写新分片
// 未发布 head → 旧 head 完整、旧行仍可读；下次提交自愈清理。
func TestV8WMCrashMidRewriteNoIntermediate(t *testing.T) {
	store, key := v8M4Fixture(t)
	v8M2CommitRows(t, store, key, v8M2Rows(40))
	headBefore, err := store.v8ReadMessageHead(key)
	if err != nil {
		t.Fatal(err)
	}
	oldShard := headBefore.Shards[0]
	// 模拟新分片已写但 head 未发布（崩溃现场）。
	rows, err := store.v8ReadRows(key, 31, 40)
	if err != nil {
		t.Fatal(err)
	}
	newShardPath := store.v8ShardPath(key, 31, 40)
	if err := writeV8RowsNewFile(newShardPath, rows); err != nil {
		t.Fatal(err)
	}
	head, err := store.v8ReadMessageHead(key)
	if err != nil {
		t.Fatal(err)
	}
	if head.LastSeq != headBefore.LastSeq || len(head.Shards) != len(headBefore.Shards) {
		t.Fatalf("head advanced without publish: %+v", head)
	}
	all, err := store.v8ReadAllRows(key)
	if err != nil || len(all) != 40 {
		t.Fatalf("old rows broken after crash: len=%d err=%v", len(all), err)
	}
	if _, err := os.Stat(store.v8MessageDir(key)); err != nil {
		t.Fatal(err)
	}
	// 下次提交：清理孤儿分片后正常（发布点恢复语义）。
	if _, err := store.v8MessageCommit(key, "next", []Event{v8M1Row(41, "msg41", "user", EventKindUserInput, "续")}); err != nil {
		t.Fatal(err)
	}
	if err := store.v8VerifyMessage(key); err != nil {
		t.Fatal(err)
	}
	_ = oldShard
}

// TestV8WMSearchSummaryAfterDelete 对应 T-WM-05：行删除后无引用 blob；检索
// 索引摘要仍可命中该区。
func TestV8WMSearchSummaryAfterDelete(t *testing.T) {
	store, key := v8M4Fixture(t)
	rows := v8M2Rows(12)
	for index := 0; index < 5; index++ {
		rows[index].Content = "需要保留的敏感关键词-" + rows[index].Content
	}
	v8M2CommitRows(t, store, key, rows)
	frame := v8CompactFrame{
		FrameID: "f1", MessageFrom: "msg1", MessageTo: "msg5",
		MessageFromSeq: 1, MessageToSeq: 5,
		Summary: "敏感关键词 摘要覆盖前五轮", BoundaryStatus: "complete",
	}
	if _, err := store.v8CompactCommit(key, frame); err != nil {
		t.Fatal(err)
	}
	if _, err := store.v8LRUDelete(key, 5, true); err != nil {
		t.Fatal(err)
	}
	if err := store.v8RebuildSearchIndex(key); err != nil {
		t.Fatal(err)
	}
	hits, err := store.v8SearchQuery(key, "敏感关键词")
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

// TestV8EVAnchorAfterMessage 对应 T-EV-01：compacted anchor=msg7 → 事件位于
// msg7 之后；R2 tail 从 msg8 开始（含端点语义）。
func TestV8EVAnchorAfterMessage(t *testing.T) {
	store, key := v8M4Fixture(t)
	v8M2CommitRows(t, store, key, v8M2Rows(12))
	event := v8Event{Kind: v8EventCompacted, AnchorMessageID: "msg7", AnchorSeq: 7, Payload: rawJSON(`{"reason":"budget"}`)}
	if _, err := store.v8EventCommit(key, "ev-c", []v8Event{event}); err != nil {
		t.Fatal(err)
	}
	events, err := store.v8ReadEvents(key, 0, 0)
	if err != nil || len(events) != 1 || events[0].AnchorSeq != 7 || events[0].AnchorMessageID != "msg7" {
		t.Fatalf("events=%+v err=%v", events, err)
	}
	// R2 装配只依赖 compact/message；这里用 compact head 验证端点语义。
	frame := v8CompactFrame{
		FrameID: "f1", MessageFrom: "msg3", MessageTo: "msg7",
		MessageFromSeq: 3, MessageToSeq: 7, Summary: "摘要", BoundaryStatus: "complete",
	}
	if _, err := store.v8CompactCommit(key, frame); err != nil {
		t.Fatal(err)
	}
	result, err := store.v8AssembleWire(key, nil, v8R2Params{Budget: 200_000})
	if err != nil || result.TailStartSeq != 8 {
		t.Fatalf("tail start=%d err=%v", result.TailStartSeq, err)
	}
}

// TestV8EVDuplicateCommitIdempotent 对应 T-EV-02：同 commit 重复持久化 →
// 事件不重复（指纹幂等）。
func TestV8EVDuplicateCommitIdempotent(t *testing.T) {
	store, key := v8M4Fixture(t)
	events := []v8Event{{Kind: v8EventFork, AnchorSeq: 3, Payload: rawJSON(`{"child":"c1"}`)}}
	if _, err := store.v8EventCommit(key, "ev-dup", events); err != nil {
		t.Fatal(err)
	}
	if _, err := store.v8EventCommit(key, "ev-dup", events); err != nil {
		t.Fatal(err)
	}
	all, err := store.v8ReadEvents(key, 0, 0)
	if err != nil || len(all) != 1 {
		t.Fatalf("events len=%d err=%v", len(all), err)
	}
}

// TestV8EVMissingEventOK 对应 T-EV-03：message 完整但 compacted 事件缺失 →
// 不补事件也可正常装配（compact.json head 为准）。
func TestV8EVMissingEventOK(t *testing.T) {
	store, key := v8M4Fixture(t)
	v8M2CommitRows(t, store, key, v8M2Rows(8))
	frame := v8CompactFrame{
		FrameID: "f1", MessageFrom: "msg1", MessageTo: "msg4",
		MessageFromSeq: 1, MessageToSeq: 4, Summary: "摘要", BoundaryStatus: "complete",
	}
	if _, err := store.v8CompactCommit(key, frame); err != nil {
		t.Fatal(err)
	}
	if err := store.deleteV8Module(key, v8ModuleEvent); err != nil {
		t.Fatal(err)
	}
	result, err := store.v8AssembleWire(key, nil, v8R2Params{Budget: 200_000})
	if err != nil || len(result.Messages) == 0 {
		t.Fatalf("assemble err=%v", err)
	}
}

// TestV8EVAnchorAtOrBelowWatermarkOK 对应 T-EV-04：event 锚 ≤ watermark 视
// 为已淘汰区引用，不算损坏。
func TestV8EVAnchorAtOrBelowWatermarkOK(t *testing.T) {
	store, key := v8M4Fixture(t)
	v8M2CommitRows(t, store, key, v8M2Rows(30))
	if _, err := store.v8LRUDelete(key, 10, true); err != nil {
		t.Fatal(err)
	}
	if _, err := store.v8EventCommit(key, "ev-old", []v8Event{{Kind: v8EventRolledBack, AnchorSeq: 5, AnchorMessageID: "msg5", Payload: rawJSON(`{"reason":"audit"}`)}}); err != nil {
		t.Fatal(err)
	}
	if err := store.v8VerifyMessage(key); err != nil {
		t.Fatalf("verify with stale anchor: %v", err)
	}
	events, err := store.v8ReadEvents(key, 0, 0)
	if err != nil || len(events) != 1 {
		t.Fatalf("events=%+v err=%v", events, err)
	}
}

// ---------- 检索 ----------

// TestV8SRPerSessionHits 对应 T-SR-01：关键词命中正确片段区间；范围=本会话
// （不跨会话）。
func TestV8SRPerSessionHits(t *testing.T) {
	storeA, keyA := v8M4Fixture(t)
	rowsA := v8M2Rows(8)
	rowsA[1].Content = "Alpha 项目关键结论"
	rowsA[3].Content = "继续讨论 Alpha 项目"
	v8M2CommitRows(t, storeA, keyA, rowsA)
	storeB, keyB := v8M4Fixture(t)
	rowsB := v8M2Rows(8)
	rowsB[1].Content = "Beta 项目结论"
	v8M2CommitRows(t, storeB, keyB, rowsB)
	if err := storeA.v8RebuildSearchIndex(keyA); err != nil {
		t.Fatal(err)
	}
	if err := storeB.v8RebuildSearchIndex(keyB); err != nil {
		t.Fatal(err)
	}
	hits, err := storeA.v8SearchQuery(keyA, "Alpha 项目")
	if err != nil || len(hits) == 0 {
		t.Fatalf("hits=%+v err=%v", hits, err)
	}
	for _, hit := range hits {
		if hit.ToSeq > 8 {
			t.Fatalf("hit out of session range: %+v", hit)
		}
	}
	cross, err := storeA.v8SearchQuery(keyA, "Beta")
	if err != nil || len(cross) != 0 {
		t.Fatalf("cross-session hit: %+v err=%v", cross, err)
	}
}

// TestV8SRLagAndRebuild 对应 T-SR-02：append 后未重建允许落后；重建后结果
// 一致（可重建断言）。
func TestV8SRLagAndRebuild(t *testing.T) {
	store, key := v8M4Fixture(t)
	v8M2CommitRows(t, store, key, v8M2Rows(6))
	if err := store.v8RebuildSearchIndex(key); err != nil {
		t.Fatal(err)
	}
	// append 新关键词后索引未重建：旧查询仍命中旧内容（允许落后）。
	extra := v8M1Row(7, "msg7", "user", EventKindUserInput, "崭新话题词")
	v8M2CommitRows(t, store, key, []Event{extra})
	before, err := store.v8SearchQuery(key, "崭新话题词")
	if err != nil {
		t.Fatal(err)
	}
	if len(before) != 0 {
		t.Fatalf("stale index unexpectedly hits: %+v", before)
	}
	if err := store.v8RebuildSearchIndex(key); err != nil {
		t.Fatal(err)
	}
	after, err := store.v8SearchQuery(key, "崭新话题词")
	if err != nil || len(after) == 0 {
		t.Fatalf("rebuild missed: hits=%+v err=%v", after, err)
	}
}

// TestV8SRDeletedRegionSummaryOnly 对应 T-SR-03：LRU 删除后重建 → 被删区只
// 回摘要命中，无悬空原文（WM-05 覆盖，这里再断言无原始 snippet 命中）。
func TestV8SRDeletedRegionSummaryOnly(t *testing.T) {
	store, key := v8M4Fixture(t)
	rows := v8M2Rows(20)
	for index := 0; index < 10; index++ {
		rows[index].Content = "已删除独家原文文本-" + rows[index].Content
	}
	v8M2CommitRows(t, store, key, rows)
	if _, err := store.v8LRUDelete(key, 10, true); err != nil {
		t.Fatal(err)
	}
	if err := store.v8RebuildSearchIndex(key); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(store.v8SearchIndexPath(key))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "已删除独家原文文本") {
		t.Fatal("deleted original text leaked into search index")
	}
}

// ---------- blob ----------

// TestV8BLHardLimitNotPersisted 对应 T-BL-01：输出 > hard_limit → 不落盘、
// 返回明确错误。
func TestV8BLHardLimitNotPersisted(t *testing.T) {
	store, key := v8M4Fixture(t)
	huge := strings.Repeat("x", v8BlobHardLimitBytes+1)
	if _, err := store.v8WriteBlob(key, "bash", huge); !errors.Is(err, ErrV8BlobTooLarge) {
		t.Fatalf("err = %v", err)
	}
	hashes, err := store.v8ListBlobHashes(key)
	if err != nil || len(hashes) != 0 {
		t.Fatalf("blob persisted: %+v err=%v", hashes, err)
	}
}

// TestV8BLSoftLimitTruncatesWithRef 对应 T-BL-02：输出 > soft_limit → 截断 +
// result_ref；read_tool_result 可读回。
func TestV8BLSoftLimitTruncatesWithRef(t *testing.T) {
	store, key := v8M4Fixture(t)
	long := strings.Repeat("长", v8BlobSoftLimitChars+100)
	blob, err := store.v8WriteBlob(key, "read", long)
	if err != nil {
		t.Fatal(err)
	}
	if !blob.Truncated || blob.Size != v8BlobSoftLimitChars {
		t.Fatalf("blob = %+v", blob)
	}
	content, err := store.v8ReadBlob(key, blob.Hash)
	if err != nil || len(content) != v8BlobSoftLimitChars {
		t.Fatalf("read content len=%d err=%v", len(content), err)
	}
	if !strings.HasPrefix(v8BlobRefOf(v8BlobRefPrefix+blob.Hash), blob.Hash) {
		t.Fatal("ref normalize broken")
	}
}

// TestV8BLQuotaDryRun 对应 T-BL-03：超会话配额 GC dry-run 输出可回收清单，
// 不自动删。
func TestV8BLQuotaDryRun(t *testing.T) {
	store, key := v8M4Fixture(t)
	if _, err := store.v8WriteBlob(key, "a", "AAA"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.v8WriteBlob(key, "b", "BBB"); err != nil {
		t.Fatal(err)
	}
	removed, err := store.v8BlobGarbageCollect(key, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) != 2 {
		t.Fatalf("dry-run candidates = %+v", removed)
	}
	hashes, err := store.v8ListBlobHashes(key)
	if err != nil || len(hashes) != 2 {
		t.Fatalf("dry-run deleted blobs: %+v err=%v", hashes, err)
	}
}

// ---------- config ----------

// TestV8CFGOverrideAndValidation 对应 T-CFG-01：修改 wire_recent_errors/阈值
// 覆盖默认值；非法值被拒绝。
func TestV8CFGOverrideAndValidation(t *testing.T) {
	defaults := v8DefaultConfig()
	if defaults.WireRecentErrors != 3 || defaults.MessageShardRows != 100 {
		t.Fatalf("defaults = %+v", defaults)
	}
	config := defaults
	config.WireRecentErrors = 5
	config.CompactFrameThreshold = 60
	if err := v8ConfigValidate(config); err != nil {
		t.Fatal(err)
	}
	bad := defaults
	bad.WireRecentErrors = 0
	if err := v8ConfigValidate(bad); err == nil {
		t.Fatal("invalid wire_recent_errors accepted")
	}
	bad2 := defaults
	bad2.BlobSoftLimitChars = -1
	if err := v8ConfigValidate(bad2); err == nil {
		t.Fatal("invalid soft limit accepted")
	}
}
