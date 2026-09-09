package sessionstore

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// v8M1Fixture 构造临时数据根 + 一个会话（D.0 通用前置：显式路径、不依赖
// 目录扫描）。
func v8M1Fixture(t *testing.T, shardRows int) (*v8Store, Key) {
	t.Helper()
	store := newV8Store(t.TempDir(), shardRows)
	return store, Key{ProjectID: "project-p1", SessionID: "session-s1"}
}

// v8M1Row 生成一条事件行。
func v8M1Row(seq uint64, messageID, role, kind, content string) Event {
	return Event{
		Seq:       seq,
		MessageID: messageID,
		Role:      role,
		Kind:      kind,
		Content:   content,
		CreatedAt: time.Now().UTC(),
	}
}

// TestV8M1FirstCommitCreatesGuideAndMessageHead 对应 T-M1-01。
func TestV8M1FirstCommitCreatesGuideAndMessageHead(t *testing.T) {
	store, key := v8M1Fixture(t, 0)
	u1 := v8M1Row(0, "u1", "user", EventKindUserInput, "你好")

	if _, err := store.v8MessageCommit(key, "commit-1", []Event{u1}); err != nil {
		t.Fatal(err)
	}
	sessionRoot := store.v8SessionRoot(key)
	guidePath := filepath.Join(sessionRoot, "metadata", "guide.json")
	headPath := filepath.Join(sessionRoot, "metadata", "message.json")
	if _, err := os.Stat(guidePath); err != nil {
		t.Fatalf("guide.json missing: %v", err)
	}
	if _, err := os.Stat(headPath); err != nil {
		t.Fatalf("message.json missing: %v", err)
	}
	head, err := store.v8ReadMessageHead(key)
	if err != nil {
		t.Fatal(err)
	}
	if head.LastSeq != 1 || head.LastMessageID != "u1" || head.TotalRows != 1 {
		t.Fatalf("head = %+v, want last_seq=1 last_message_id=u1 total=1", head)
	}
	rows, err := store.v8ReadAllRows(key)
	if err != nil || len(rows) != 1 || rows[0].Role != "user" || rows[0].Content != "你好" {
		t.Fatalf("read rows len=%d err=%v", len(rows), err)
	}
}

// TestV8M1CommitRowsShareCommitIDAndReplayBySeq 对应 T-M1-02。
func TestV8M1CommitRowsShareCommitIDAndReplayBySeq(t *testing.T) {
	store, key := v8M1Fixture(t, 0)
	rows := []Event{
		v8M1Row(1, "u1", "user", EventKindUserInput, "问题"),
		v8M1Row(2, "a1", "assistant", EventKindLLM, "回答"),
		v8M1Row(3, "t1", "tool", EventKindToolOutput, "结果"),
	}
	if _, err := store.v8MessageCommit(key, "commit-abc", rows); err != nil {
		t.Fatal(err)
	}
	all, err := store.v8ReadAllRows(key)
	if err != nil || len(all) != 3 {
		t.Fatalf("read len=%d err=%v", len(all), err)
	}
	for index, row := range all {
		if row.CommitID != "commit-abc" {
			t.Fatalf("row[%d] commit_id=%q want commit-abc", index, row.CommitID)
		}
	}
	expected := []string{"u1", "a1", "t1"}
	for index, row := range all {
		if row.MessageID != expected[index] || row.Seq != uint64(index+1) {
			t.Fatalf("row[%d] = seq=%d id=%q want seq=%d id=%q", index, row.Seq, row.MessageID, index+1, expected[index])
		}
	}
}

// TestV8M1CrashTornTailIgnored 对应 T-M1-03：文件尾部存在半行 → 截断/忽略；
// head 不前进；verify 通过。
func TestV8M1CrashTornTailIgnored(t *testing.T) {
	store, key := v8M1Fixture(t, 0)
	if _, err := store.v8MessageCommit(key, "c1", []Event{v8M1Row(1, "u1", "user", EventKindUserInput, "ok")}); err != nil {
		t.Fatal(err)
	}
	head, err := store.v8ReadMessageHead(key)
	if err != nil {
		t.Fatal(err)
	}
	shardPath := filepath.Join(store.v8MessageDir(key), head.Shards[0].Path)
	file, err := os.OpenFile(shardPath, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(`{"seq":2,"role":"user","content":"半行`); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	rows, err := store.v8ReadAllRows(key)
	if err != nil || len(rows) != 1 {
		t.Fatalf("torn tail visible: len=%d err=%v", len(rows), err)
	}
	if err := store.v8VerifyMessage(key); err != nil {
		t.Fatalf("verify with torn tail: %v", err)
	}
	// 下一次提交应截掉残尾并正常续写。
	if _, err := store.v8MessageCommit(key, "c2", []Event{v8M1Row(2, "a1", "assistant", EventKindLLM, "回答")}); err != nil {
		t.Fatal(err)
	}
	all, err := store.v8ReadAllRows(key)
	if err != nil || len(all) != 2 {
		t.Fatalf("after resume len=%d err=%v", len(all), err)
	}
	if err := store.v8VerifyMessage(key); err != nil {
		t.Fatal(err)
	}
}

// TestV8M1UnpublishedRowsInvisible 对应 T-M1-04：append 完成但 message.json
// 未替换 → 新行不可见（reader 以 head 为准），下次提交自愈不重复。
func TestV8M1UnpublishedRowsInvisible(t *testing.T) {
	store, key := v8M1Fixture(t, 0)
	if _, err := store.v8MessageCommit(key, "c1", []Event{v8M1Row(1, "u1", "user", EventKindUserInput, "ok")}); err != nil {
		t.Fatal(err)
	}
	head, err := store.v8ReadMessageHead(key)
	if err != nil {
		t.Fatal(err)
	}
	// 模拟崩溃点：行已 append 但 head 未发布（直接补写完整行到分片文件）。
	shardPath := filepath.Join(store.v8MessageDir(key), head.Shards[0].Path)
	file, err := os.OpenFile(shardPath, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	data, err := marshalV8Row(v8M1Row(2, "ghost", "assistant", EventKindLLM, "未发布"))
	if err != nil {
		file.Close()
		t.Fatal(err)
	}
	if _, err := file.WriteString(data + "\n"); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	rows, err := store.v8ReadAllRows(key)
	if err != nil || len(rows) != 1 || rows[0].MessageID != "u1" {
		t.Fatalf("unpublished row visible: len=%d err=%v", len(rows), err)
	}
	if _, err := store.v8MessageCommit(key, "c1", []Event{v8M1Row(2, "a1", "assistant", EventKindLLM, "正式")}); err != nil {
		t.Fatal(err)
	}
	all, err := store.v8ReadAllRows(key)
	if err != nil || len(all) != 2 {
		t.Fatalf("recovered len=%d err=%v", len(all), err)
	}
	if all[1].MessageID != "a1" || all[1].Content != "正式" {
		t.Fatalf("second row = %+v", all[1])
	}
	if err := store.v8VerifyMessage(key); err != nil {
		t.Fatal(err)
	}
}

// TestV8M1IndependentModuleLocks 对应 T-M1-05：并发写 stack 与 message，
// 模块锁独立、互不阻塞，两端 head 各自正确。
func TestV8M1IndependentModuleLocks(t *testing.T) {
	store, key := v8M1Fixture(t, 0)
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 1; i <= 50; i++ {
			row := v8M1Row(0, fmt.Sprintf("m-%d", i), "user", EventKindUserInput, fmt.Sprintf("message-%d", i))
			if _, err := store.v8MessageCommit(key, fmt.Sprintf("mc-%d", i), []Event{row}); err != nil {
				errs <- err
				return
			}
		}
	}()
	go func() {
		defer wg.Done()
		for i := 1; i <= 50; i++ {
			payload := v8StackHead{
				SessionID: key.SessionID,
				HeadSeq:   uint64(i),
				Items:     []v8StackItem{{ItemID: fmt.Sprintf("item-%d", i), Status: "active"}},
			}
			if err := store.v8CommitModuleHead(key, v8ModuleStack, &store.stackMu, fmt.Sprintf("sc-%d", i), payload); err != nil {
				errs <- err
				return
			}
		}
	}()
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	rows, err := store.v8ReadAllRows(key)
	if err != nil || len(rows) != 50 {
		t.Fatalf("message rows len=%d err=%v", len(rows), err)
	}
	stackHead, err := v8ReadModuleHeadPayload[v8StackHead](store, key, v8ModuleStack)
	if err != nil {
		t.Fatal(err)
	}
	if stackHead.HeadSeq != 50 || len(stackHead.Items) != 1 || stackHead.Items[0].ItemID != "item-50" {
		t.Fatalf("stack head = %+v", stackHead)
	}
}

// TestV8M1ReaderSelfHealsOnChecksumMismatch 对应 T-M1-06：reader 读到旧
// message.json（writer 正替换）→ 校验 checksum 失败后自愈重读，结果一致。
func TestV8M1ReaderSelfHealsOnChecksumMismatch(t *testing.T) {
	store, key := v8M1Fixture(t, 0)
	if _, err := store.v8MessageCommit(key, "c1", []Event{v8M1Row(1, "u1", "user", EventKindUserInput, "v1")}); err != nil {
		t.Fatal(err)
	}
	headPath := store.v8ModulePath(key, v8ModuleMessage)
	data, err := os.ReadFile(headPath)
	if err != nil {
		t.Fatal(err)
	}
	corrupt := strings.Replace(string(data), `"checksum": "`, `"checksum": "bad`, 1)
	if err := os.WriteFile(headPath, []byte(corrupt), 0o600); err != nil {
		t.Fatal(err)
	}
	// 自愈场景：首次读因 checksum 不匹配失败，重读前 writer 完成原子替换
	// 为新版本；重读应命中新 head，返回一致结果。
	originalHook := v8ReadSelfHealHook
	v8ReadSelfHealHook = func() {
		// 模拟并发 writer 的原子替换：直接把第二个分片与合法新 head 写盘
		// （不读取损坏 head，避免自愈递归）。
		a1 := v8M1Row(2, "a1", "assistant", EventKindLLM, "v2")
		a1.CommitID = "c2"
		secondPath := store.v8ShardPath(key, 2, 2)
		if err := appendV8RowsFile(secondPath, []Event{a1}); err != nil {
			t.Errorf("writer append: %v", err)
			return
		}
		head := v8MessageHead{
			SessionID:     key.SessionID,
			LastSeq:       2,
			LastMessageID: "a1",
			Shards:        []v8ShardInfo{{Path: "message_1_1.jsonl", FromSeq: 1, ToSeq: 1, Count: 1, SHA256: v8FileSHA256(store.v8ShardPath(key, 1, 1))}, {Path: "message_2_2.jsonl", FromSeq: 2, ToSeq: 2, Count: 1, SHA256: v8FileSHA256(secondPath)}},
			TotalRows:     2,
		}
		if _, err := store.publishV8ModuleHead(key, v8ModuleMessage, "c2", head, time.Now().UTC()); err != nil {
			t.Errorf("writer publish: %v", err)
		}
	}
	t.Cleanup(func() { v8ReadSelfHealHook = originalHook })

	head, err := store.v8ReadMessageHead(key)
	if err != nil {
		t.Fatalf("self-heal read failed: %v", err)
	}
	if head.LastSeq != 2 || head.LastMessageID != "a1" {
		t.Fatalf("head = %+v, want last_seq=2 last_message_id=a1", head)
	}
	if _, err := os.Stat(headPath); err != nil {
		t.Fatal(err)
	}
	rows, err := store.v8ReadAllRows(key)
	if err != nil || len(rows) != 2 {
		t.Fatalf("rows len=%d err=%v", len(rows), err)
	}
}

// TestV8M1ShardRolloverAt101Rows 对应 T-M1-07：已有 100 行后 commit 第 101
// 行 → 滚动到新分片；message.json 指向新分片；读序完整。
func TestV8M1ShardRolloverAt101Rows(t *testing.T) {
	store, key := v8M1Fixture(t, 0)
	batch := make([]Event, 0, 101)
	for index := 1; index <= 101; index++ {
		batch = append(batch, v8M1Row(0, fmt.Sprintf("message-%d", index), "user", EventKindUserInput, fmt.Sprintf("content-%d", index)))
	}
	if _, err := store.v8MessageCommit(key, "big", batch); err != nil {
		t.Fatal(err)
	}
	head, err := store.v8ReadMessageHead(key)
	if err != nil {
		t.Fatal(err)
	}
	if len(head.Shards) != 2 {
		t.Fatalf("shard count = %d, want 2: %+v", len(head.Shards), head.Shards)
	}
	first := filepath.Base(head.Shards[0].Path)
	second := filepath.Base(head.Shards[1].Path)
	if first != "message_1_100.jsonl" {
		t.Fatalf("first shard = %q", first)
	}
	if second != "message_101_101.jsonl" {
		t.Fatalf("second shard = %q", second)
	}
	all, err := store.v8ReadAllRows(key)
	if err != nil || len(all) != 101 {
		t.Fatalf("rows len=%d err=%v", len(all), err)
	}
	for index, row := range all {
		if row.Seq != uint64(index+1) {
			t.Fatalf("row[%d] seq=%d", index, row.Seq)
		}
	}
	if err := store.v8VerifyMessage(key); err != nil {
		t.Fatal(err)
	}
}

// TestV8M1DuplicateCommitIDIdempotent 对应 T-M1-08：重复同一 commit_id →
// 行不重复、head 不双跳。
func TestV8M1DuplicateCommitIDIdempotent(t *testing.T) {
	store, key := v8M1Fixture(t, 0)
	rows := []Event{
		v8M1Row(1, "u1", "user", EventKindUserInput, "q"),
		v8M1Row(2, "a1", "assistant", EventKindLLM, "a"),
		v8M1Row(3, "t1", "tool", EventKindToolOutput, "r"),
	}
	if _, err := store.v8MessageCommit(key, "commit-dup", rows); err != nil {
		t.Fatal(err)
	}
	if _, err := store.v8MessageCommit(key, "commit-dup", rows); err != nil {
		t.Fatal(err)
	}
	// 部分重放（同 commit_id，包含已发布行 + 新行）也只补新行。
	if _, err := store.v8MessageCommit(key, "commit-dup", append(rows, v8M1Row(4, "u2", "user", EventKindUserInput, "再问"))); err != nil {
		t.Fatal(err)
	}
	all, err := store.v8ReadAllRows(key)
	if err != nil || len(all) != 4 {
		t.Fatalf("rows len=%d err=%v", len(all), err)
	}
	for index, row := range all {
		if row.CommitID != "commit-dup" {
			t.Fatalf("row[%d] commit_id=%q", index, row.CommitID)
		}
	}
	head, err := store.v8ReadMessageHead(key)
	if err != nil {
		t.Fatal(err)
	}
	if head.LastSeq != 4 || head.TotalRows != 4 {
		t.Fatalf("head=%+v want last_seq=4 total=4", head)
	}
}

// TestV8M1GuidePresentForReadRoutes 验证 guide 只做读索引：模块 head 已发布
// 但 guide 缺失/滞后时读路径仍以模块 head 为准（I9 配套）。
func TestV8M1GuidePresentForReadRoutes(t *testing.T) {
	store, key := v8M1Fixture(t, 0)
	if _, err := store.v8MessageCommit(key, "c1", []Event{v8M1Row(1, "u1", "user", EventKindUserInput, "ok")}); err != nil {
		t.Fatal(err)
	}
	guidePath := store.v8GuidePath(key)
	if err := os.Remove(guidePath); err != nil {
		t.Fatal(err)
	}
	rows, err := store.v8ReadAllRows(key)
	if err != nil {
		t.Fatalf("read without guide failed: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows len=%d", len(rows))
	}
	// 重写 guide（路由自愈）。
	if _, err := store.ensureV8Guide(key); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(guidePath); err != nil {
		t.Fatal(err)
	}
}

// TestV8M1VerifyEmptySession 验证空会话 verify 通过（没有 message.json 也
// 不算损坏）。
func TestV8M1VerifyEmptySession(t *testing.T) {
	store, key := v8M1Fixture(t, 0)
	if err := store.v8VerifyMessage(key); err != nil {
		t.Fatalf("empty verify: %v", err)
	}
	if _, err := store.v8MessageCommit(key, "c0", nil); err != nil {
		t.Fatalf("empty commit: %v", err)
	}
	if err := store.v8VerifyMessage(key); err != nil {
		t.Fatalf("verify after empty commit: %v", err)
	}
}

// TestV8M1MissingSessionReturnsEmpty 验证未知会话读取 = 空而非错误（与
// Repository 空历史语义一致）。
func TestV8M1MissingSessionReturnsEmpty(t *testing.T) {
	store, _ := v8M1Fixture(t, 0)
	key := Key{ProjectID: "p", SessionID: "missing"}
	rows, err := store.v8ReadAllRows(key)
	if err != nil || len(rows) != 0 {
		t.Fatalf("rows=%d err=%v", len(rows), err)
	}
}

// TestV8M1ChecksumCorruptionRejected 校验 checksum 损坏（非并发替换）在两
// 次读取后显式失败，不静默吞错。
func TestV8M1ChecksumCorruptionRejected(t *testing.T) {
	store, key := v8M1Fixture(t, 0)
	if _, err := store.v8MessageCommit(key, "c1", []Event{v8M1Row(1, "u1", "user", EventKindUserInput, "ok")}); err != nil {
		t.Fatal(err)
	}
	headPath := store.v8ModulePath(key, v8ModuleMessage)
	data, err := os.ReadFile(headPath)
	if err != nil {
		t.Fatal(err)
	}
	corrupt := strings.Replace(string(data), `"checksum": "`, `"checksum": "bad`, 1)
	if err := os.WriteFile(headPath, []byte(corrupt), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.v8ReadMessageHead(key); err == nil {
		t.Fatal("corrupt head accepted")
	} else if errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("unexpected not-exist: %v", err)
	}
}

func marshalV8Row(row Event) (string, error) {
	data, err := json.Marshal(row)
	return string(data), err
}
