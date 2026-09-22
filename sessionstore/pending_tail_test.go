package sessionstore

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// 草稿尾部（seq_draft）—— message 通道「已 append、head 未发布」段的探测与
// 显式恢复。
//
// 夹具构造：先正常提交（发布点落盘），再把「下一步的增量」直接写盘但不发布
// ——即 appendRowsLocked 之后、publishModuleHead 之前崩溃的那一瞬。增量用
// 生产续号函数 deltaRowsLocked 计算，保证夹具与提交路径同源。

// appendPendingTail 用生产续号函数算出未发布增量，再直接追加到指定分片
// （模拟 append 成功、head 替换前崩溃）。
func appendPendingTail(t *testing.T, store *storeEngine, key Key, shardName string, rows []Event) {
	t.Helper()
	head, err := store.readMessageHead(key)
	if err != nil {
		t.Fatal(err)
	}
	delta, err := store.deltaRowsLocked(head, rows, "pending-commit")
	if err != nil {
		t.Fatal(err)
	}
	appendRawPendingTail(t, store, key, shardName, delta)
}

// appendRawPendingTail 原样写盘（保留调用方给的显式 seq，用于构造基座断裂）。
func appendRawPendingTail(t *testing.T, store *storeEngine, key Key, shardName string, rows []Event) {
	t.Helper()
	if err := os.MkdirAll(store.messageDir(key), 0o700); err != nil {
		t.Fatal(err)
	}
	// appendMessageRowsFile 自带 O_CREATE 并按路径关闭句柄：不要在这里另开
	// 句柄（Windows 上未关闭的句柄会让 TempDir 清理/Remove 失败）。
	if err := appendMessageRowsFile(filepath.Join(store.messageDir(key), shardName), rows); err != nil {
		t.Fatal(err)
	}
}

func lastShardName(t *testing.T, store *storeEngine, key Key) string {
	t.Helper()
	head, err := store.readMessageHead(key)
	if err != nil {
		t.Fatal(err)
	}
	if len(head.Shards) == 0 {
		t.Fatal("fixture: 无分片索引")
	}
	return head.Shards[len(head.Shards)-1].Path
}

// TestPendingTailCleanAfterNormalCommit 正常提交后没有草稿尾部。
func TestPendingTailCleanAfterNormalCommit(t *testing.T) {
	store, key := messageFixture(t, 0)
	if _, err := store.messageCommit(key, "c1", []Event{
		messageRow(0, "u1", "user", EventKindUserInput, "第一问"),
		messageRow(0, "a1", "assistant", EventKindLLM, "第一答"),
	}); err != nil {
		t.Fatal(err)
	}
	report, err := store.pendingTailReport(key)
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != PendingTailClean || report.RowCount() != 0 || report.HeadSeq != 2 {
		t.Fatalf("report = %+v, want clean/head=2/0 rows", report)
	}
}

// TestPendingTailRecoversRowsBeyondPublishedPoint 覆盖 L2 主路径：已 append
// 未发布的行可被显式恢复，恢复前后读者可见性符合「发布点即可见边界」。
func TestPendingTailRecoversRowsBeyondPublishedPoint(t *testing.T) {
	store, key := messageFixture(t, 0)
	if _, err := store.messageCommit(key, "c1", []Event{
		messageRow(0, "u1", "user", EventKindUserInput, "第一问"),
	}); err != nil {
		t.Fatal(err)
	}
	// 崩溃窗口：第二轮两行已落盘（seq 2、3），head 仍停在 1。
	shard := lastShardName(t, store, key)
	appendPendingTail(t, store, key, shard, []Event{
		messageRow(0, "a1", "assistant", EventKindLLM, "半轮：LLM 已返回"),
		messageRow(0, "t1", "tool", EventKindToolOutput, "半轮：工具已返回"),
	})

	// 夹具有效性：此刻通道自校验必定失败、读者也看不到草稿行。
	if err := store.verifyMessage(key); err == nil {
		t.Fatal("fixture 无效：追加未发布行后 verify 竟然通过")
	}
	if rows, err := store.readAllRows(key); err != nil || len(rows) != 1 {
		t.Fatalf("readAllRows = %d rows (%v), want 1（草稿对读者不可见）", len(rows), err)
	}

	report, err := store.pendingTailReport(key)
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != PendingTailRecoverable || report.RowCount() != 2 ||
		report.TailFrom != 2 || report.TailTo != 3 || report.HeadSeq != 1 {
		t.Fatalf("report = %+v, want recoverable/head=1/tail=[2,3]", report)
	}

	recovered, err := store.recoverPendingTail(key)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.Status != PendingTailRecovered || recovered.RowCount() != 2 {
		t.Fatalf("recovered = %+v, want recovered/2 rows", recovered)
	}
	head, err := store.readMessageHead(key)
	if err != nil {
		t.Fatal(err)
	}
	if head.LastSeq != 3 || head.TotalRows != 3 || head.LastMessageID != "t1" {
		t.Fatalf("head after recover = %+v, want last_seq=3 total=3 last_msg=t1", head)
	}
	if head.Meta.ShardCount != 1 || head.LastCommitID != "pending-commit" {
		t.Fatalf("head after recover = %+v, want shard_count=1 commit=pending-commit", head)
	}
	rows, err := store.readAllRows(key)
	if err != nil || len(rows) != 3 || rows[2].Content != "半轮：工具已返回" {
		t.Fatalf("readAllRows = %d rows (%v), want 3 含恢复行", len(rows), err)
	}
	if err := store.verifyMessage(key); err != nil {
		t.Fatalf("恢复后通道自校验失败: %v", err)
	}
	// 幂等：再恢复一次不应重复发布。
	again, err := store.recoverPendingTail(key)
	if err != nil {
		t.Fatal(err)
	}
	if again.Status != PendingTailClean {
		t.Fatalf("second recover = %+v, want clean（幂等）", again)
	}
	if rows, err := store.readAllRows(key); err != nil || len(rows) != 3 {
		t.Fatalf("readAllRows after second recover = %d rows (%v), want 3", len(rows), err)
	}
}

// TestPendingTailRecoversExtraShardFile 分片滚动后崩溃：head 未索引的分片
// 整个文件都是草稿，可一并恢复。
func TestPendingTailRecoversExtraShardFile(t *testing.T) {
	store, key := messageFixture(t, 2)
	if _, err := store.messageCommit(key, "c1", []Event{
		messageRow(0, "u1", "user", EventKindUserInput, "第一问"),
		messageRow(0, "a1", "assistant", EventKindLLM, "第一答"),
	}); err != nil {
		t.Fatal(err)
	}
	appendPendingTail(t, store, key, "message_3_4.jsonl", []Event{
		messageRow(0, "u2", "user", EventKindUserInput, "第二问"),
		messageRow(0, "a2", "assistant", EventKindLLM, "第二答"),
	})
	if rows, err := store.readAllRows(key); err != nil || len(rows) != 2 {
		t.Fatalf("readAllRows = %d rows (%v), want 2（新分片对读者不可见）", len(rows), err)
	}
	report, err := store.pendingTailReport(key)
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != PendingTailRecoverable || report.TailFrom != 3 || report.TailTo != 4 ||
		len(report.ExtraShards) != 1 || report.ExtraShards[0] != "message_3_4.jsonl" {
		t.Fatalf("report = %+v, want recoverable + extra shard message_3_4.jsonl", report)
	}
	if _, err := store.recoverPendingTail(key); err != nil {
		t.Fatal(err)
	}
	head, err := store.readMessageHead(key)
	if err != nil {
		t.Fatal(err)
	}
	if head.LastSeq != 4 || len(head.Shards) != 2 || head.TotalRows != 4 {
		t.Fatalf("head after recover = %+v, want last_seq=4 shards=2 total=4", head)
	}
	if err := store.verifyMessage(key); err != nil {
		t.Fatalf("恢复后通道自校验失败: %v", err)
	}
}

// TestPendingTailGapIsReportedButNotPublished 基座断裂（尾部号与发布点不
// 连续）时只报告：不发布、不隐式清理。
func TestPendingTailGapIsReportedButNotPublished(t *testing.T) {
	store, key := messageFixture(t, 0)
	if _, err := store.messageCommit(key, "c1", []Event{
		messageRow(0, "u1", "user", EventKindUserInput, "第一问"),
		messageRow(0, "a1", "assistant", EventKindLLM, "第一答"),
		messageRow(0, "t1", "tool", EventKindToolOutput, "工具"),
	}); err != nil {
		t.Fatal(err)
	}
	// 尾部行从 seq 9 起：与发布点 3 不连续（模拟发布点被别的写者推进过）。
	appendRawPendingTail(t, store, key, "message_9_10.jsonl", []Event{
		{Seq: 9, MessageID: "stale", Role: "assistant", Kind: EventKindLLM, Content: "旧号尾部 1", CommitID: "stale-commit"},
		{Seq: 10, MessageID: "stale2", Role: "assistant", Kind: EventKindLLM, Content: "旧号尾部 2", CommitID: "stale-commit"},
	})
	report, err := store.pendingTailReport(key)
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != PendingTailGap || !strings.Contains(report.Reason, "不连续") {
		t.Fatalf("report = %+v, want gap + 不连续原因", report)
	}
	if report.HeadSeq != 3 {
		t.Fatalf("report.HeadSeq = %d, want 3（发布点不变）", report.HeadSeq)
	}
	recovered, err := store.recoverPendingTail(key)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.Status != PendingTailGap {
		t.Fatalf("recover on gap = %+v, want 仍为 gap（拒绝发布）", recovered)
	}
	head, err := store.readMessageHead(key)
	if err != nil {
		t.Fatal(err)
	}
	if head.LastSeq != 3 || head.TotalRows != 3 {
		t.Fatalf("head after refused recover = %+v, want last_seq=3 total=3", head)
	}
	if _, err := os.Stat(filepath.Join(store.messageDir(key), "message_9_10.jsonl")); err != nil {
		t.Fatalf("gap 尾部文件被隐式清理（红线：只报告不清理）: %v", err)
	}
	if rows, err := store.readAllRows(key); err != nil || len(rows) != 3 {
		t.Fatalf("readAllRows = %d rows (%v), want 3（已发布前缀不受影响）", len(rows), err)
	}
}

// TestPendingTailDiscardIsExplicit 显式丢弃把「清理」变成可观测操作，并让
// 通道回到自校验通过的状态。
func TestPendingTailDiscardIsExplicit(t *testing.T) {
	store, key := messageFixture(t, 0)
	if _, err := store.messageCommit(key, "c1", []Event{
		messageRow(0, "u1", "user", EventKindUserInput, "第一问"),
	}); err != nil {
		t.Fatal(err)
	}
	appendRawPendingTail(t, store, key, "message_7_8.jsonl", []Event{
		{Seq: 7, MessageID: "stale", Role: "assistant", Kind: EventKindLLM, Content: "旧号尾部 1", CommitID: "stale-commit"},
		{Seq: 8, MessageID: "stale2", Role: "assistant", Kind: EventKindLLM, Content: "旧号尾部 2", CommitID: "stale-commit"},
	})
	report, err := store.discardPendingTail(key)
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != PendingTailDiscarded || report.RowCount() != 2 {
		t.Fatalf("report = %+v, want discarded/2 rows", report)
	}
	after, err := store.pendingTailReport(key)
	if err != nil {
		t.Fatal(err)
	}
	if after.Status != PendingTailClean {
		t.Fatalf("after discard = %+v, want clean", after)
	}
	if _, err := os.Stat(filepath.Join(store.messageDir(key), "message_7_8.jsonl")); !os.IsNotExist(err) {
		t.Fatalf("丢弃后分片文件仍存在: %v", err)
	}
	if err := store.verifyMessage(key); err != nil {
		t.Fatalf("丢弃后通道自校验失败: %v", err)
	}
}

// TestPendingTailRecoverKeepsHeadOnlyFields 恢复发布不得丢 head 独有字段
// （Floor / watermark）——与 D1 同族的回归护栏。
func TestPendingTailRecoverKeepsHeadOnlyFields(t *testing.T) {
	store, key := messageFixture(t, 0)
	if _, err := store.messageCommit(key, "c1", []Event{
		messageRow(0, "u1", "user", EventKindUserInput, "第一问"),
	}); err != nil {
		t.Fatal(err)
	}
	store.mu(key, moduleMessage).Lock()
	head, err := store.readMessageHeadLocked(key)
	if err != nil {
		store.mu(key, moduleMessage).Unlock()
		t.Fatal(err)
	}
	head.Floor = &Floor{RoleName: "advisor", RoundID: 7, Seq: 1, UpdatedAt: time.Now().UTC()}
	if _, err := store.publishModuleHead(key, moduleMessage, head.LastCommitID, head, time.Now().UTC()); err != nil {
		store.mu(key, moduleMessage).Unlock()
		t.Fatal(err)
	}
	store.mu(key, moduleMessage).Unlock()

	shard := lastShardName(t, store, key)
	appendPendingTail(t, store, key, shard, []Event{
		messageRow(0, "a1", "assistant", EventKindLLM, "半轮"),
	})
	if _, err := store.recoverPendingTail(key); err != nil {
		t.Fatal(err)
	}
	head, err = store.readMessageHead(key)
	if err != nil {
		t.Fatal(err)
	}
	if head.Floor == nil || head.Floor.RoleName != "advisor" || head.Floor.RoundID != 7 {
		t.Fatalf("恢复发布丢了发言权记录: floor=%+v", head.Floor)
	}
	if head.LastSeq != 2 {
		t.Fatalf("last_seq = %d, want 2", head.LastSeq)
	}
}
