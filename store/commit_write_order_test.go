package store

// commit_write_order_test.go — M0 基线门禁：Commit 写序与半失败语义特征测试。
//
// 为什么需要它：docs/2026-10-27-store-ddd-split/milestones.md §4 要求 M4 把
// `writeCommitLayout` 从 store 提升为 application 的 `CommitTurn` 用例，同时
// “保留当前失败和幂等语义”。要做到这一点，先得把**当前**语义写成可执行证据：
//
//	artifact 文件 → refs 索引 → message 事件行 → compaction 记录
//
// 两条用例各钉一段跨界写序，都用“让下一步必然失败”的注入点，而不是靠读取
// 代码注释相信自己：
//
//  1. refs 索引不可写时，message 行**不得**可见（artifact 先于 message）；
//  2. compaction 不可写时，message 行**已经**可见，但整次提交仍返回错误
//     （message 先于 compaction；当前不是跨通道原子事务）。
//
// 这两条是“兼容要求”而非“正确性主张”：搬迁后如果行为变了，必须先单独裁决，
// 不允许用“新实现更合理”直接改语义。

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	storeroot "github.com/RedHuang-0622/seelex/store/root"
)

const (
	writeOrderProjectID = "project-write-order"
	writeOrderSessionID = "session-write-order"
	secondSessionID     = "session-write-order-2"
)

// openWriteOrderRepository 打开一个临时数据根的仓储（含数据根锁）。
func openWriteOrderRepository(t *testing.T) (*jsonRepository, string) {
	t.Helper()
	root := t.TempDir()
	repository, err := newJSONRepositoryWithLayout(root, storeroot.Settings{})
	if err != nil {
		t.Fatalf("open repository: %v", err)
	}
	t.Cleanup(func() { _ = repository.Close() })
	return repository, root
}

// seedSessionRoot 预建会话目录，用来在会话目录里放“毒化”路径。
// 不能先提交一次再毒化：那会顺带把 head 推进，混淆后面的断言。
func seedSessionRoot(t *testing.T, repository *jsonRepository, key Key) string {
	t.Helper()
	sessionRoot := repository.sessionDir(key)
	if err := os.MkdirAll(sessionRoot, 0o700); err != nil {
		t.Fatalf("seed session root: %v", err)
	}
	return sessionRoot
}

// writeOrderVisibleRows 返回消息 head 当前发布的可见行数。
func writeOrderVisibleRows(t *testing.T, repository *jsonRepository, key Key) int {
	t.Helper()
	rows, err := repository.layout.readRows(key, 1, 0)
	if err != nil {
		t.Fatalf("read published rows: %v", err)
	}
	return len(rows)
}

// TestCommitWritesArtifactBeforeMessage 钉写序第一段：artifact → refs → message。
//
// 注入点：把 `metadata-index` 造成**普通文件**，于是 `publishToolResultRefs` 的
// `MkdirAll(metadata-index)` 必然失败。此时结果文件已经写出，但 message 行一行
// 都不许可见——否则可见行会引用一个没有索引的结果。
func TestCommitWritesArtifactBeforeMessage(t *testing.T) {
	repository, _ := openWriteOrderRepository(t)
	key := Key{ProjectID: writeOrderProjectID, SessionID: writeOrderSessionID}
	sessionRoot := seedSessionRoot(t, repository, key)
	if err := os.WriteFile(filepath.Join(sessionRoot, "metadata-index"), []byte("poison"), 0o600); err != nil {
		t.Fatalf("poison metadata-index: %v", err)
	}

	created := time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC)
	result := ToolResult{
		Ref:       "write-order-result",
		Tool:      "bash",
		Content:   "artifact body",
		Size:      len("artifact body"),
		CreatedAt: created,
	}
	err := repository.WriteCommit(context.Background(), key, Commit{
		Events: []Event{
			{MessageID: "m0", Role: "user", Kind: EventKindUserInput, Content: "hi", CreatedAt: created},
		},
		ToolResults: []ToolResult{result},
	})
	if err == nil {
		t.Fatal("refs 索引不可写时提交必须失败（当前 writeCommitLayout 第 1 段）")
	}

	// artifact 已经落盘：证明“结果文件先写”这一步确实发生在失败点之前。
	if _, statErr := os.Stat(repository.toolResultPath(key, result.Ref)); statErr != nil {
		t.Fatalf("结果文件应在失败点之前写出：%v", statErr)
	}
	// refs 索引不是文件（它是我们放的毒化文件），也不是目录条目。
	if _, statErr := os.Stat(filepath.Join(sessionRoot, "metadata-index", "toolresult.json")); statErr == nil {
		t.Fatal("refs 索引不应被发布：它位于毒化文件之下")
	}
	// 关键断言：message 行没有越过发布点。
	if rows := writeOrderVisibleRows(t, repository, key); rows != 0 {
		t.Fatalf("artifact 未发布索引时 message 行必须不可见，实际可见 %d 行", rows)
	}
}

// TestCommitPublishesMessageBeforeCompactionFailure 钉写序第二段与半失败语义：
// message → compaction，且 compaction 失败后整次提交报错但 message 行保留。
//
// 注入点：把 `compaction-records.jsonl` 造成**目录**，于是
// `appendCompactionRecordRows` 的 `os.OpenFile` 必然失败。当前实现的语义是
// 「message 已可见 + 返回错误」，这是 M4 必须逐字保持的兼容要求。
func TestCommitPublishesMessageBeforeCompactionFailure(t *testing.T) {
	repository, _ := openWriteOrderRepository(t)
	key := Key{ProjectID: writeOrderProjectID, SessionID: writeOrderSessionID}
	created := time.Date(2026, 10, 27, 9, 30, 0, 0, time.UTC)
	ctx := context.Background()

	// 先成功提交两行，把布局和 message head 建起来。
	if err := repository.WriteCommit(ctx, key, Commit{Events: []Event{
		{Seq: 1, MessageID: "u1", Role: "user", Kind: EventKindUserInput, Content: "第一问", CreatedAt: created},
		{Seq: 2, MessageID: "a1", Role: "assistant", Kind: EventKindLLM, Content: "第一答", CreatedAt: created},
	}}); err != nil {
		t.Fatalf("baseline commit: %v", err)
	}
	if rows := writeOrderVisibleRows(t, repository, key); rows != 2 {
		t.Fatalf("baseline 应有 2 行可见，实际 %d", rows)
	}

	sessionRoot := repository.sessionDir(key)
	if err := os.MkdirAll(filepath.Join(sessionRoot, "compaction-records.jsonl"), 0o700); err != nil {
		t.Fatalf("poison compaction-records.jsonl: %v", err)
	}

	err := repository.WriteCommit(ctx, key, Commit{
		Events: []Event{
			{Seq: 3, MessageID: "u2", Role: "user", Kind: EventKindUserInput, Content: "第二问", CreatedAt: created},
		},
		CompactionRecords: []json.RawMessage{json.RawMessage(`{"summary":"second"}`)},
	})
	if err == nil {
		t.Fatal("compaction 通道不可写时提交必须失败（当前 writeCommitLayout 第 3 段）")
	}

	// message 行已经越过发布点：当前不是跨通道原子事务。
	if rows := writeOrderVisibleRows(t, repository, key); rows != 3 {
		t.Fatalf("message 先于 compaction：失败后应保留 3 行可见，实际 %d", rows)
	}
	// compaction head 未发布 = 未提交。
	if _, statErr := os.Stat(filepath.Join(sessionRoot, "metadata", "compaction.json")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("compaction head 不应被发布，实际 stat 结果：%v", statErr)
	}
}

// TestCommitRetryWithExplicitSeqIsIdempotent 钉“同一次提交重试”的幂等边界。
//
// 实测语义（M0 基线，不是猜测）：幂等只对**显式携带 Seq** 的行成立——
// `assignRowSeqs` 把 `Seq <= 已发布基准` 视为重复投递并跳过。`Seq == 0` 的行由
// 存储层重新编号，因此重放同一批 `Seq == 0` 的行会**再追加一份**。M4 把提交拆成
// 三个聚合提交时，必须保留这条边界：要么继续由调用方带显式 Seq，要么单独裁决
// 是否改成按 commit_id 去重。
func TestCommitRetryWithExplicitSeqIsIdempotent(t *testing.T) {
	repository, _ := openWriteOrderRepository(t)
	key := Key{ProjectID: writeOrderProjectID, SessionID: secondSessionID}
	created := time.Date(2026, 10, 27, 10, 0, 0, 0, time.UTC)
	ctx := context.Background()

	commit := Commit{
		Events: []Event{
			{Seq: 1, MessageID: "u1", Role: "user", Kind: EventKindUserInput, Content: "重试问", CreatedAt: created},
			{Seq: 2, MessageID: "a1", Role: "assistant", Kind: EventKindLLM, Content: "重试答", CreatedAt: created},
		},
		ToolResults: []ToolResult{{
			Ref: "retry-result", Tool: "bash", Content: "retry body",
			Size: len("retry body"), CreatedAt: created,
		}},
	}
	if err := repository.WriteCommit(ctx, key, commit); err != nil {
		t.Fatalf("first commit: %v", err)
	}
	first := writeOrderVisibleRows(t, repository, key)
	if first != 2 {
		t.Fatalf("首次提交应发布 2 行，实际 %d", first)
	}

	// 重放同一批显式 Seq 的行：Seq <= 已发布基准 → 幂等空操作。
	if err := repository.WriteCommit(ctx, key, commit); err != nil {
		t.Fatalf("retry commit: %v", err)
	}
	if second := writeOrderVisibleRows(t, repository, key); second != first {
		t.Fatalf("显式 Seq 重试必须幂等：首次 %d 行，重试后 %d 行", first, second)
	}
}
