package sessionstore

import (
	"os"
	"strings"
	"testing"
	"time"
)

// corruptHeadChecksum 把 head 文件的 checksum 改坏（构造"校验失败两次"的自愈
// 前置条件）。改坏而非删除：删除走 fs.ErrNotExist 分支，不触发重建。
func corruptHeadChecksum(t *testing.T, store *storeEngine, key Key, module storageModule) {
	t.Helper()
	path := store.modulePath(key, module)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	corrupt := strings.Replace(string(data), `"checksum": "`, `"checksum": "bad`, 1)
	if corrupt == string(data) {
		t.Fatalf("%s 里找不到 checksum 字段，无法构造损坏 head", path)
	}
	if err := os.WriteFile(path, []byte(corrupt), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestMessageHeadSelfHealKeepsHeadOnlyFields 钉住 D1：自愈重建只按数据文件
// 重算派生字段，**只存在于 head 的字段必须原样取回**——否则一次自愈就抹掉
// 发言权（Floor）、LRU 淘汰水位（Watermark*）、目录枚举面（Meta 标题/创建
// 时间），并让 generation 漂移成 self-heal-repair。
func TestMessageHeadSelfHealKeepsHeadOnlyFields(t *testing.T) {
	store, key := messageFixture(t, 0)
	if _, err := store.messageCommit(key, "c1", []Event{
		messageRow(1, "u1", "user", EventKindUserInput, "第一问"),
		messageRow(2, "a1", "assistant", EventKindLLM, "第一答"),
	}); err != nil {
		t.Fatal(err)
	}
	// 真实的 LRU 淘汰前移水位（不是手写字段）：删掉 seq 1，水位 = 1。
	if _, err := store.lRUDelete(key, 1, true); err != nil {
		t.Fatalf("lru delete: %v", err)
	}
	// 补上只存在于 head 的发言权与标题。
	messageLock := store.mu(key, moduleMessage)
	messageLock.Lock()
	head, err := store.readMessageHeadLocked(key)
	if err != nil {
		messageLock.Unlock()
		t.Fatal(err)
	}
	head.Floor = &Floor{RoleName: "advisor", RoleSessionID: "role-1", RoundID: 7, Seq: 2, UpdatedAt: time.Now().UTC()}
	head.Meta.Summary = "会话标题"
	envelopeCommit := head.LastCommitID
	if _, err := store.publishModuleHead(key, moduleMessage, envelopeCommit, head, time.Now().UTC()); err != nil {
		messageLock.Unlock()
		t.Fatal(err)
	}
	messageLock.Unlock()

	carried, ok := store.readHeadEnvelopeLenient(key, moduleMessage)
	if !ok {
		t.Fatal("取不回自愈前的 head 信封")
	}
	if carried.CommitID != envelopeCommit {
		t.Fatalf("前置信封 commit_id = %q want %q", carried.CommitID, envelopeCommit)
	}
	originalCreatedAt := head.Meta.CreatedAt

	corruptHeadChecksum(t, store, key, moduleMessage)

	// 自愈读（无锁入口）。
	repaired, err := store.readMessageHead(key)
	if err != nil {
		t.Fatalf("自愈读失败: %v", err)
	}
	if repaired.Floor == nil || repaired.Floor.RoleName != "advisor" || repaired.Floor.RoundID != 7 {
		t.Fatalf("自愈丢了发言权记录: floor=%+v", repaired.Floor)
	}
	if repaired.WatermarkSeq != 1 {
		t.Fatalf("自愈丢了 LRU 水位: watermark_seq=%d want 1", repaired.WatermarkSeq)
	}
	if repaired.Meta.Summary != "会话标题" {
		t.Fatalf("自愈丢了标题: summary=%q", repaired.Meta.Summary)
	}
	if !repaired.Meta.CreatedAt.Equal(originalCreatedAt) {
		t.Fatalf("自愈改了创建时间: %v want %v", repaired.Meta.CreatedAt, originalCreatedAt)
	}
	// 派生字段仍按数据文件重算：seq 1 已被淘汰，只剩 seq 2。
	if repaired.LastSeq != 2 || repaired.TotalRows != 1 || len(repaired.Shards) != 1 {
		t.Fatalf("重算派生字段不对: last_seq=%d total_rows=%d shards=%d",
			repaired.LastSeq, repaired.TotalRows, len(repaired.Shards))
	}
	// generation 不因自愈漂移。
	after, ok := store.readHeadEnvelopeLenient(key, moduleMessage)
	if !ok {
		t.Fatal("取不回自愈后的 head 信封")
	}
	if after.CommitID != envelopeCommit {
		t.Fatalf("自愈让 generation 漂移: commit_id=%q want %q", after.CommitID, envelopeCommit)
	}
}

// TestHeadSelfHealDoesNotPublishWhileModuleLockHeld 钉住 D2：head 的重建发布
// 必须在模块锁内。他人持锁（= 正在提交）期间，无锁自愈读**不得**抢锁发布，
// 也不得改写文件；释放锁后才允许自愈。
func TestHeadSelfHealDoesNotPublishWhileModuleLockHeld(t *testing.T) {
	store, key := messageFixture(t, 0)
	if _, err := store.messageCommit(key, "c1", []Event{
		messageRow(1, "u1", "user", EventKindUserInput, "v1"),
	}); err != nil {
		t.Fatal(err)
	}
	corruptHeadChecksum(t, store, key, moduleMessage)

	messageLock := store.mu(key, moduleMessage)
	messageLock.Lock()
	done := make(chan error, 1)
	go func() {
		_, readErr := store.readMessageHead(key)
		done <- readErr
	}()
	readErr := <-done
	if readErr == nil {
		messageLock.Unlock()
		t.Fatal("他人持锁期间无锁自愈读仍成功——自愈发布逃出了模块锁")
	}
	if _, rawErr := store.readModuleHeadFileRaw(key, moduleMessage); rawErr == nil {
		messageLock.Unlock()
		t.Fatal("他人持锁期间 head 被锁外重建覆盖")
	}
	messageLock.Unlock()

	// 锁释放后：自愈读应当成功且把文件修好。
	head, err := store.readMessageHead(key)
	if err != nil {
		t.Fatalf("锁释放后自愈读失败: %v", err)
	}
	if head.LastSeq != 1 || head.LastMessageID != "u1" {
		t.Fatalf("head = %+v, want last_seq=1 last_message_id=u1", head)
	}
	if _, rawErr := store.readModuleHeadFileRaw(key, moduleMessage); rawErr != nil {
		t.Fatalf("自愈后 head 仍不可读: %v", rawErr)
	}
}

// TestHeadSelfHealUnderLockHealsInPlace 钉住持锁入口的原地自愈：提交路径
// （*Locked）在 head 损坏时必须当次修好，而不是把校验错误上抛给调用方。
func TestHeadSelfHealUnderLockHealsInPlace(t *testing.T) {
	store, key := messageFixture(t, 0)
	if _, err := store.messageCommit(key, "c1", []Event{
		messageRow(1, "u1", "user", EventKindUserInput, "v1"),
	}); err != nil {
		t.Fatal(err)
	}
	corruptHeadChecksum(t, store, key, moduleMessage)

	messageLock := store.mu(key, moduleMessage)
	messageLock.Lock()
	head, err := store.readMessageHeadLocked(key)
	messageLock.Unlock()
	if err != nil {
		t.Fatalf("持锁自愈读失败: %v", err)
	}
	if head.LastSeq != 1 {
		t.Fatalf("head = %+v, want last_seq=1", head)
	}
	if _, rawErr := store.readModuleHeadFileRaw(key, moduleMessage); rawErr != nil {
		t.Fatalf("持锁自愈未修复 head: %v", rawErr)
	}

	// 提交路径在 head 损坏后仍能继续提交（自愈 + 追加）。
	if _, err := store.messageCommit(key, "c2", []Event{
		messageRow(2, "a1", "assistant", EventKindLLM, "v2"),
	}); err != nil {
		t.Fatalf("自愈后继续提交失败: %v", err)
	}
	rows, err := store.readAllRows(key)
	if err != nil || len(rows) != 2 {
		t.Fatalf("rows len=%d err=%v want 2", len(rows), err)
	}
}
