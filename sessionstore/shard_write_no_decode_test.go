package sessionstore

import (
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// 写侧「免整片解码」优化（C1/H3）的验收面。
//
// 背景（探针 `TestProbeCommitCostBreakdown`，1500 行 / 每行 8 KB / 每片 100 行）：
//   - 完整提交均值 ≈28.6 ms；其中「仅 head 发布」≈1.3 ms、「整片 sha256」≈1.1 ms、
//     「append（含 fsync）」≈1.0 ms，而**整片读回 + JSON 解码**≈16.9 ms；
//   - 提交路径此前把这段解码做了两次（reap 的 truncateMessageRowsAfter 一次、
//     写入器 writeShardRowsLocked 一次），合计 ≈34 ms 的重复解码。
//
// 优化后的不变式：head 索引里的末分片项带 Bytes（文件字节数），且它的末行坐标
// 就是发布点、文件以换行收尾时，行数/区间直接取自索引，摘要用「原字节 + 追加
// 字节」增量算出——两条路径都不再整片解码。索引不可信（草稿行 / 崩溃残尾 /
// 老 head 没记 Bytes）时自动退回整片读回，语义与优化前一致。
func seedMessageRows(t *testing.T, store *storeEngine, key Key, count int) {
	t.Helper()
	for index := 0; index < count; index++ {
		if _, err := store.messageCommit(key, "seed", []Event{{
			Role: "user", Content: "seed-row", CreatedAt: time.Now().UTC(),
		}}); err != nil {
			t.Fatalf("seed row %d: %v", index, err)
		}
	}
}

// TestCommitSkipsTailShardDecode 结构性证明：末分片索引可信时，一次提交不解码
// 任何分片。
//
// 有牙：把 head 的 Bytes 抹掉（= 索引没记账，优化前的状态）后同一个提交立刻
// 产生 2 次整片解码，本用例变红——它钉的是"解码次数"，不是耗时。
func TestCommitSkipsTailShardDecode(t *testing.T) {
	store := newStoreEngine(t.TempDir(), storageSettings{})
	key := Key{ProjectID: "p", SessionID: "s"}
	// 10 行：末分片未满（默认每片 100 行），提交会**续写同一片**，所以两条路径
	// 都必须知道末分片已有多少行——快路径取索引，慢路径整片读回。
	seedMessageRows(t, store, key, 10)

	var decodes atomic.Int64
	shardDecodeHook = func() { decodes.Add(1) }
	t.Cleanup(func() { shardDecodeHook = nil })

	if _, err := store.messageCommit(key, "fast", []Event{{Role: "user", Content: "fast-row", CreatedAt: time.Now().UTC()}}); err != nil {
		t.Fatal(err)
	}
	if got := decodes.Load(); got != 0 {
		t.Fatalf("可信索引下提交仍整片解码 %d 次（快路径未命中；优化前为 2 次）", got)
	}

	// 有牙证明：抹掉索引记账 → 退回慢路径 → 同一个提交必须出现 2 次解码
	// （reap 的 truncateMessageRowsAfter 一次 + 写入器一次）。
	head, err := store.readMessageHead(key)
	if err != nil {
		t.Fatal(err)
	}
	head.Shards[len(head.Shards)-1].Bytes = 0
	if _, err := store.publishModuleHead(key, moduleMessage, "untrusted", head, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	decodes.Store(0)
	if _, err := store.messageCommit(key, "slow", []Event{{Role: "user", Content: "slow-row", CreatedAt: time.Now().UTC()}}); err != nil {
		t.Fatal(err)
	}
	if got := decodes.Load(); got == 0 {
		t.Fatal("抹掉索引记账后仍然零解码：本用例失去判别力（快路径没被真正约束）")
	}
}

// TestTrustedTailKeepsIndexExact 快路径不改语义：行数/区间/摘要必须与真实文件
// 完全一致（摘要仍然覆盖**整片**，不是只覆盖本次追加），且通道自校验通过。
func TestTrustedTailKeepsIndexExact(t *testing.T) {
	root := t.TempDir()
	store := newStoreEngine(root, storageSettings{})
	key := Key{ProjectID: "p", SessionID: "s"}
	seedMessageRows(t, store, key, 10)
	for round := 0; round < 3; round++ {
		if _, err := store.messageCommit(key, "fast", []Event{{Role: "user", Content: "row", CreatedAt: time.Now().UTC()}}); err != nil {
			t.Fatal(err)
		}
	}
	head, err := store.readMessageHead(key)
	if err != nil {
		t.Fatal(err)
	}
	last := head.Shards[len(head.Shards)-1]
	path := filepath.Join(store.messageDir(key), last.Path)
	rows, err := readMessageRowsFileAt(path)
	if err != nil {
		t.Fatal(err)
	}
	if last.Count != len(rows) {
		t.Fatalf("索引行数=%d 文件行数=%d（快路径把行数算歪了）", last.Count, len(rows))
	}
	if last.FromSeq != rows[0].Seq || last.ToSeq != rows[len(rows)-1].Seq {
		t.Fatalf("索引区间=[%d,%d] 文件区间=[%d,%d]", last.FromSeq, last.ToSeq, rows[0].Seq, rows[len(rows)-1].Seq)
	}
	if want := fileSHA256(path); last.SHA256 != want {
		t.Fatalf("索引摘要=%q 整片摘要=%q（增量摘要在快路径上算错了）", last.SHA256, want)
	}
	if info, statErr := os.Stat(path); statErr != nil || info.Size() != last.Bytes {
		t.Fatalf("索引字节数=%d 文件字节数=%v err=%v", last.Bytes, info, statErr)
	}
	if err := store.verifyMessage(key); err != nil {
		t.Fatalf("快路径提交后通道自校验失败：%v", err)
	}
}

// TestTrustedTailFallsBackWhenDraftRowsPresent 安全性：草稿行 append 到**已索引**
// 的末分片后，索引的 Bytes/Count 立刻失信（字节数不等）——后续提交必须走慢路径
// 把草稿行按"未提交"处理（截断），并且不能把行数算歪。
//
// 这条钉的是用户点名的红线："逐步草稿尾与普通提交由同一写者串行——否则两者会
// 互相 reap 草稿"：此处证明即便草稿行落在同一分片上，提交也不会把索引当真相。
func TestTrustedTailFallsBackWhenDraftRowsPresent(t *testing.T) {
	store := newStoreEngine(t.TempDir(), storageSettings{})
	key := Key{ProjectID: "p", SessionID: "s"}
	seedMessageRows(t, store, key, 10)
	head, err := store.readMessageHead(key)
	if err != nil {
		t.Fatal(err)
	}
	beforeLast := head.LastSeq
	draft, err := store.appendDraftRows(key, "draft-1", []Event{
		{Role: "assistant", Content: "draft-a", CreatedAt: time.Now().UTC()},
		{Role: "assistant", Content: "draft-b", CreatedAt: time.Now().UTC()},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(draft) != 2 || draft[0].Seq != beforeLast+1 {
		t.Fatalf("草稿续号=%v（want 首行 %d）", draft, beforeLast+1)
	}
	// 草稿行已落盘、发布点未动；此时索引末分片仍然只在发布点——索引记账的字节数
	// 与文件实际字节数不等，快路径必须失效。
	head, err = store.readMessageHead(key)
	if err != nil {
		t.Fatal(err)
	}
	if trusted := store.trustedTailFor(key, head); trusted != nil {
		t.Fatalf("草稿行在片上却仍给出可信末分片索引：%+v", trusted)
	}
	if _, err := store.messageCommit(key, "commit-after-draft", []Event{{Role: "user", Content: "real", CreatedAt: time.Now().UTC()}}); err != nil {
		t.Fatal(err)
	}
	head, err = store.readMessageHead(key)
	if err != nil {
		t.Fatal(err)
	}
	if head.LastSeq != beforeLast+1 {
		t.Fatalf("提交后发布点=%d want %d（草稿行必须按未提交被 reap，不能占号）", head.LastSeq, beforeLast+1)
	}
	last := head.Shards[len(head.Shards)-1]
	decoded, err := readMessageRowsFileAt(filepath.Join(store.messageDir(key), last.Path))
	if err != nil {
		t.Fatal(err)
	}
	if last.Count != len(decoded) || len(decoded) != int(head.TotalRows) {
		t.Fatalf("计数不一致：索引=%d 文件=%d head.TotalRows=%d", last.Count, len(decoded), head.TotalRows)
	}
	if err := store.verifyMessage(key); err != nil {
		t.Fatalf("草稿尾 + 提交后自校验失败：%v", err)
	}
}
