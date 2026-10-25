package store

import (
	"fmt"
	"path/filepath"
	"sync/atomic"
	"testing"
)

// TestAppendMessageDraftWorkspaceKeepsPublishedPoint 覆盖运行期周期快照的
// 存储接口：草稿后缀可恢复，但对已发布读者不可见。
func TestAppendMessageDraftWorkspaceKeepsPublishedPoint(t *testing.T) {
	router, err := NewRouter(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = router.Close() })
	const projectID = "p-draft-snapshot"
	const sessionID = "s-draft-snapshot"
	if err := router.SaveCommitWorkspace(projectID, sessionID, Commit{Events: []Event{
		messageRow(1, "u1", "user", EventKindUserInput, "question"),
		messageRow(2, "a1", "assistant", EventKindLLM, "answer"),
	}}); err != nil {
		t.Fatal(err)
	}
	assigned, supported, err := router.AppendMessageDraftWorkspace(projectID, sessionID, []Event{
		messageRow(3, "u2", "user", EventKindUserInput, "draft question"),
		messageRow(4, "a2", "assistant", EventKindLLM, "draft answer"),
	})
	if err != nil || !supported || len(assigned) != 2 {
		t.Fatalf("append draft = %d/%v/%v, want 2/true/nil", len(assigned), supported, err)
	}
	published, total, err := router.LoadRangeWorkspace(projectID, sessionID, 0, 0)
	if err != nil || total != 2 || len(published) != 0 {
		t.Fatalf("published view = rows:%d total:%d err:%v, want 0/2/nil", len(published), total, err)
	}
	report, supported, err := router.PendingMessageTailWorkspace(projectID, sessionID)
	if err != nil || !supported || report.Status != PendingTailRecoverable ||
		report.TailFrom != 3 || report.TailTo != 4 || report.RowCount() != 2 {
		t.Fatalf("pending report = %+v supported=%v err=%v", report, supported, err)
	}
}

// TestDraftSnapshotReusesTailPointer 钉住周期快照的双指针写侧：
// 连续追加 `(left,right]` 时，已核对的末分片索引可直接续写，
// 不再为每一次快照解码整片 JSONL。
func TestDraftSnapshotReusesTailPointer(t *testing.T) {
	store, key := messageFixture(t, 100)
	if _, err := store.messageCommit(key, "published", roundRows(50)); err != nil {
		t.Fatal(err)
	}
	var decodes atomic.Int64
	shardDecodeHook = func() { decodes.Add(1) }
	t.Cleanup(func() { shardDecodeHook = nil })

	for seq := uint64(51); seq <= 60; seq++ {
		rows, err := store.appendDraftRows(key, "turn-draft", []Event{
			messageRow(seq, fmt.Sprintf("draft-%d", seq), "assistant", EventKindLLM, "draft"),
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != 1 || rows[0].Seq != seq {
			t.Fatalf("draft append seq=%d rows=%+v", seq, rows)
		}
	}
	if got := decodes.Load(); got != 0 {
		t.Fatalf("连续 10 次草稿快照解码了 %d 个分片，want 0（复用末分片指针）", got)
	}
}

// 逐步 append（A2）：message 通道的 seq 分配基准脱离发布点 + 写者草稿尾。
//
// 契约：
//   - 续号基准 = **物理末行**（不是 head.LastSeq）：草稿行已经在发布点之后，
//     后续写入必须接在它们之后，否则草稿行与新一轮行会撞 seq；
//   - 草稿 append 不推进发布点：读者可见范围仍是 head.LastSeq（长回合的中间行
//     不必让读者等一次"回合结束大重写"）；
//   - 发布/丢弃只走显式入口，不存在隐式提升（D2 红线）。

func draftRowsOf(t *testing.T, store *storeEngine, key Key) []Event {
	t.Helper()
	rows, err := store.readAllRows(key)
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

func physicalRows(t *testing.T, store *storeEngine, key Key, shard string) []Event {
	t.Helper()
	rows, err := readMessageRowsFileAt(filepath.Join(store.messageDir(key), shard))
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

// TestDraftAppendInvisibleUntilPublished 草稿 append 不进读者可见范围。
func TestDraftAppendInvisibleUntilPublished(t *testing.T) {
	store, key := messageFixture(t, 0)
	if _, err := store.messageCommit(key, "c1", []Event{
		messageRow(0, "u1", "user", EventKindUserInput, "第一问"),
		messageRow(0, "a1", "assistant", EventKindLLM, "第一答"),
	}); err != nil {
		t.Fatal(err)
	}
	assigned, err := store.appendDraftRows(key, "turn-2", []Event{
		messageRow(0, "u2", "user", EventKindUserInput, "第二问"),
		messageRow(0, "a2", "assistant", EventKindLLM, "第二答"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(assigned) != 2 || assigned[0].Seq != 3 || assigned[1].Seq != 4 {
		t.Fatalf("assigned = %+v, want seq 3,4", assigned)
	}
	if assigned[0].CommitID != "turn-2" || assigned[1].CommitID != "turn-2" {
		t.Fatalf("commit_id 未打上：%+v", assigned)
	}
	// 读者视角：只有已发布两行。
	visible := draftRowsOf(t, store, key)
	if len(visible) != 2 || visible[1].Seq != 2 {
		t.Fatalf("读者可见 = %d 行（末 seq %d），want 2 行/末 seq 2", len(visible), visible[len(visible)-1].Seq)
	}
	head, err := store.readMessageHead(key)
	if err != nil {
		t.Fatal(err)
	}
	if head.LastSeq != 2 || head.LastCommitID != "c1" {
		t.Fatalf("发布点被草稿推进：last_seq=%d commit=%q", head.LastSeq, head.LastCommitID)
	}
	// 物理面：4 行都在。
	if rows := physicalRows(t, store, key, lastShardName(t, store, key)); len(rows) != 4 {
		t.Fatalf("物理行数 = %d, want 4", len(rows))
	}
	report, err := store.pendingTailReport(key)
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != PendingTailRecoverable || report.HeadSeq != 2 || report.TailFrom != 3 || report.TailTo != 4 {
		t.Fatalf("report = %+v, want recoverable/head=2/tail=[3,4]", report)
	}
}

// TestDraftAppendContinuesFromPhysicalTail 续号基准是物理末行（A2 核心改动）：
// 连续两次草稿 append 必须得到 [3,4] 与 [5,6]，不撞 seq。
func TestDraftAppendContinuesFromPhysicalTail(t *testing.T) {
	store, key := messageFixture(t, 0)
	if _, err := store.messageCommit(key, "c1", []Event{
		messageRow(0, "u1", "user", EventKindUserInput, "第一问"),
		messageRow(0, "a1", "assistant", EventKindLLM, "第一答"),
	}); err != nil {
		t.Fatal(err)
	}
	first, err := store.appendDraftRows(key, "turn-2", []Event{
		messageRow(0, "u2", "user", EventKindUserInput, "第二问"),
		messageRow(0, "a2", "assistant", EventKindLLM, "第二答"),
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.appendDraftRows(key, "turn-3", []Event{
		messageRow(0, "u3", "user", EventKindUserInput, "第三问"),
		messageRow(0, "a3", "assistant", EventKindLLM, "第三答"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if first[0].Seq != 3 || first[1].Seq != 4 {
		t.Fatalf("第一次分配 = %d,%d, want 3,4", first[0].Seq, first[1].Seq)
	}
	if second[0].Seq != 5 || second[1].Seq != 6 {
		t.Fatalf("第二次分配 = %d,%d, want 5,6（基准必须是物理末行 4，不是发布点 2）", second[0].Seq, second[1].Seq)
	}
	report, err := store.pendingTailReport(key)
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != PendingTailRecoverable || report.RowCount() != 4 || report.TailFrom != 3 || report.TailTo != 6 {
		t.Fatalf("report = %+v, want recoverable/4 行/[3,6]", report)
	}
}

// TestPublishDraftTailPromotesAllRows 发布 = 显式入口：一次性把草稿尾提升为
// 已提交，行内容与顺序不变，且重复调用幂等。
func TestPublishDraftTailPromotesAllRows(t *testing.T) {
	store, key := messageFixture(t, 0)
	if _, err := store.messageCommit(key, "c1", []Event{
		messageRow(0, "u1", "user", EventKindUserInput, "第一问"),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.appendDraftRows(key, "turn-2", []Event{
		messageRow(0, "a1", "assistant", EventKindLLM, "第一答"),
		messageRow(0, "u2", "user", EventKindUserInput, "第二问"),
	}); err != nil {
		t.Fatal(err)
	}
	report, err := store.publishDraftTail(key)
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != PendingTailRecovered {
		t.Fatalf("发布状态 = %q, want %q", report.Status, PendingTailRecovered)
	}
	head, err := store.readMessageHead(key)
	if err != nil {
		t.Fatal(err)
	}
	if head.LastSeq != 3 || head.TotalRows != 3 {
		t.Fatalf("发布点 = %d/total = %d, want 3/3", head.LastSeq, head.TotalRows)
	}
	if head.LastCommitID != "turn-2" {
		t.Fatalf("发布凭据 = %q, want 草稿行自带 turn-2", head.LastCommitID)
	}
	rows := draftRowsOf(t, store, key)
	if len(rows) != 3 {
		t.Fatalf("发布后可见行 = %d, want 3", len(rows))
	}
	for index, row := range rows {
		if row.Seq != uint64(index+1) {
			t.Fatalf("第 %d 行 seq = %d, want %d（顺序/编号必须保持）", index, row.Seq, index+1)
		}
	}
	// 幂等：再发布一次是 clean 空操作。
	again, err := store.publishDraftTail(key)
	if err != nil {
		t.Fatal(err)
	}
	if again.Status != PendingTailClean {
		t.Fatalf("二次发布 = %q, want clean", again.Status)
	}
}

// TestDiscardDraftTailKeepsPublishPointAndRows 丢弃 = 显式入口：发布点不动、
// 已发布行不动，草稿行从物理面清掉。
func TestDiscardDraftTailKeepsPublishPointAndRows(t *testing.T) {
	store, key := messageFixture(t, 0)
	if _, err := store.messageCommit(key, "c1", []Event{
		messageRow(0, "u1", "user", EventKindUserInput, "第一问"),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.appendDraftRows(key, "turn-2", []Event{
		messageRow(0, "a1", "assistant", EventKindLLM, "第一答"),
	}); err != nil {
		t.Fatal(err)
	}
	report, err := store.discardPendingTail(key)
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != PendingTailDiscarded || report.RowCount() != 1 {
		t.Fatalf("丢弃报告 = %+v, want discarded/1 行", report)
	}
	head, err := store.readMessageHead(key)
	if err != nil {
		t.Fatal(err)
	}
	if head.LastSeq != 1 || head.TotalRows != 1 {
		t.Fatalf("丢弃后发布点 = %d/total = %d, want 1/1", head.LastSeq, head.TotalRows)
	}
	if rows := physicalRows(t, store, key, lastShardName(t, store, key)); len(rows) != 1 {
		t.Fatalf("物理行数 = %d, want 1（草稿行已清）", len(rows))
	}
}

// TestDraftAppendRollsShardBeyondPublishedIndex 草稿 append 触发的分片滚动
// 产生 head 未索引的新分片（整个文件都是草稿）；发布时索引被重建。
func TestDraftAppendRollsShardBeyondPublishedIndex(t *testing.T) {
	store, key := messageFixture(t, 2) // 2 行/片：第二次写入必然滚动
	if _, err := store.messageCommit(key, "c1", []Event{
		messageRow(0, "u1", "user", EventKindUserInput, "第一问"),
		messageRow(0, "a1", "assistant", EventKindLLM, "第一答"),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.appendDraftRows(key, "turn-2", []Event{
		messageRow(0, "u2", "user", EventKindUserInput, "第二问"),
		messageRow(0, "a2", "assistant", EventKindLLM, "第二答"),
		messageRow(0, "u3", "user", EventKindUserInput, "第三问"),
	}); err != nil {
		t.Fatal(err)
	}
	head, err := store.readMessageHead(key)
	if err != nil {
		t.Fatal(err)
	}
	if len(head.Shards) != 1 || head.LastSeq != 2 {
		t.Fatalf("草稿滚动污染了索引：shards=%d last_seq=%d", len(head.Shards), head.LastSeq)
	}
	report, err := store.pendingTailReport(key)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.ExtraShards) == 0 {
		t.Fatalf("report = %+v, want 至少一个 head 未索引的草稿分片", report)
	}
	published, err := store.publishDraftTail(key)
	if err != nil {
		t.Fatal(err)
	}
	if published.Status != PendingTailRecovered {
		t.Fatalf("发布状态 = %q（草稿分片在 head 之外，基座仍应连续）", published.Status)
	}
	head, err = store.readMessageHead(key)
	if err != nil {
		t.Fatal(err)
	}
	if head.LastSeq != 5 || len(head.Shards) != 3 || head.TotalRows != 5 {
		t.Fatalf("发布后 last_seq=%d shards=%d total=%d, want 5/3/5", head.LastSeq, len(head.Shards), head.TotalRows)
	}
	if rows := draftRowsOf(t, store, key); len(rows) != 5 {
		t.Fatalf("发布后可见行 = %d, want 5", len(rows))
	}
}

// TestNormalCommitReapsDraftTail 普通提交会 reap 掉草稿尾（崩溃残尾语义）。
// 这条把"逐步 append 与普通提交必须由同一写者串行"钉成契约：A3 的写者 actor
// 负责串行化，否则同一会话并发写会把对方的草稿尾当残尾清掉。
func TestNormalCommitReapsDraftTail(t *testing.T) {
	store, key := messageFixture(t, 0)
	if _, err := store.messageCommit(key, "c1", []Event{
		messageRow(0, "u1", "user", EventKindUserInput, "第一问"),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.appendDraftRows(key, "turn-2", []Event{
		messageRow(0, "draft-1", "assistant", EventKindLLM, "草稿行"),
	}); err != nil {
		t.Fatal(err)
	}
	// 普通提交（重新从发布点续号）：草稿行被 reap 掉，seq 3 被本次提交重用。
	if _, err := store.messageCommit(key, "c2", []Event{
		messageRow(0, "a1", "assistant", EventKindLLM, "正式第一答"),
	}); err != nil {
		t.Fatal(err)
	}
	rows := draftRowsOf(t, store, key)
	if len(rows) != 2 {
		t.Fatalf("行数 = %d, want 2", len(rows))
	}
	if rows[1].MessageID != "a1" || rows[1].CommitID != "c2" {
		t.Fatalf("末行 = %+v, want 本次提交的 a1（草稿行已被 reap）", rows[1])
	}
	report, err := store.pendingTailReport(key)
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != PendingTailClean {
		t.Fatalf("report = %+v, want clean（无残留草稿）", report)
	}
}
