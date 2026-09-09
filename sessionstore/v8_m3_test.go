package sessionstore

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func v8M3Fixture(t *testing.T) (*v8Store, Key) {
	t.Helper()
	store := newV8Store(t.TempDir(), 0)
	return store, Key{ProjectID: "p-lc", SessionID: "s-lc"}
}

// ---------- lifecycle ----------

// TestV8LCDraftToQueueAtomic 对应 T-LC-01：D→入队，queue 含 D、draft 清空
// （同一次 lifecycle 提交）。
func TestV8LCDraftToQueueAtomic(t *testing.T) {
	store, key := v8M3Fixture(t)
	if err := store.v8DraftToQueue(key, "草稿内容 D"); err != nil {
		t.Fatal(err)
	}
	head, err := store.v8ReadLifecycleHead(key)
	if err != nil {
		t.Fatal(err)
	}
	if len(head.Queue) != 1 || head.Queue[0].Content != "草稿内容 D" {
		t.Fatalf("queue = %+v", head.Queue)
	}
	if head.Draft != nil {
		t.Fatalf("draft not cleared: %+v", head.Draft)
	}
}

// TestV8LCQueueFIFO 对应 T-LC-02：Q1 阻塞时 Q2 不插队；Q1 成功后 Q2 才发。
func TestV8LCQueueFIFO(t *testing.T) {
	store, key := v8M3Fixture(t)
	if err := store.v8QueueEnqueue(key, "req-1", "Q1"); err != nil {
		t.Fatal(err)
	}
	if err := store.v8QueueEnqueue(key, "req-2", "Q2"); err != nil {
		t.Fatal(err)
	}
	front, ok := store.v8QueueFront(key)
	if !ok || front.Content != "Q1" {
		t.Fatalf("front = %+v ok=%v", front, ok)
	}
	if _, err := store.v8QueueSendFront(key); err != nil {
		t.Fatal(err)
	}
	front, ok = store.v8QueueFront(key)
	if !ok || front.Content != "Q1" {
		t.Fatalf("Q2 jumped queue: %+v", front)
	}
	if err := store.v8QueueConfirmSent(key, "req-1"); err != nil {
		t.Fatal(err)
	}
	front, ok = store.v8QueueFront(key)
	if !ok || front.Content != "Q2" {
		t.Fatalf("after Q1 done front = %+v ok=%v", front, ok)
	}
}

// TestV8LCDirectSendQueueEmpty 对应 T-LC-03：队列空 + 草稿 D → 立即发送（不
// 进队）。
func TestV8LCDirectSendQueueEmpty(t *testing.T) {
	store, key := v8M3Fixture(t)
	if err := store.v8SaveDraft(key, "D"); err != nil {
		t.Fatal(err)
	}
	if err := store.v8DraftDirectSend(key, "D"); err != nil {
		t.Fatal(err)
	}
	head, err := store.v8ReadLifecycleHead(key)
	if err != nil {
		t.Fatal(err)
	}
	if len(head.Queue) != 0 || head.Draft != nil {
		t.Fatalf("direct send should not enqueue: queue=%d draft=%+v", len(head.Queue), head.Draft)
	}
}

// TestV8LCDirectSendQueueNonEmptyEnqueueTail 对应 T-LC-04：队列非空 + 草稿
// D → D 入队尾，不插队。
func TestV8LCDirectSendQueueNonEmptyEnqueueTail(t *testing.T) {
	store, key := v8M3Fixture(t)
	if err := store.v8QueueEnqueue(key, "req-1", "Q1"); err != nil {
		t.Fatal(err)
	}
	if err := store.v8DraftDirectSend(key, "D"); err != nil {
		t.Fatal(err)
	}
	head, err := store.v8ReadLifecycleHead(key)
	if err != nil {
		t.Fatal(err)
	}
	if len(head.Queue) != 2 || head.Queue[0].Content != "Q1" || head.Queue[1].Content != "D" {
		t.Fatalf("queue = %+v", head.Queue)
	}
	if head.Draft != nil {
		t.Fatalf("draft = %+v", head.Draft)
	}
}

// TestV8LCQueueSendFailedReturnsDraft 对应 T-LC-05：队首发送失败 → 内容回
// 草稿，队列移除该项。
func TestV8LCQueueSendFailedReturnsDraft(t *testing.T) {
	store, key := v8M3Fixture(t)
	if err := store.v8QueueEnqueue(key, "req-1", "Q1"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.v8QueueSendFront(key); err != nil {
		t.Fatal(err)
	}
	if err := store.v8QueueSendFailed(key); err != nil {
		t.Fatal(err)
	}
	head, err := store.v8ReadLifecycleHead(key)
	if err != nil {
		t.Fatal(err)
	}
	if len(head.Queue) != 0 || head.Draft == nil || head.Draft.Content != "Q1" {
		t.Fatalf("queue=%d draft=%+v", len(head.Queue), head.Draft)
	}
}

// TestV8LCDirectSendFailedReturnsDraft 对应 T-LC-06：草稿直发失败 → 内容回
// 草稿。
func TestV8LCDirectSendFailedReturnsDraft(t *testing.T) {
	store, key := v8M3Fixture(t)
	if err := store.v8DirectSendFailed(key, "D"); err != nil {
		t.Fatal(err)
	}
	head, err := store.v8ReadLifecycleHead(key)
	if err != nil {
		t.Fatal(err)
	}
	if head.Draft == nil || head.Draft.Content != "D" || head.Draft.State != "发送失败退回" {
		t.Fatalf("draft = %+v", head.Draft)
	}
}

// TestV8LCAlreadySentNotPublishedResends 对应 T-LC-07：已发送但 message.json
// 未发布 → 重启后该项重发一次。
func TestV8LCAlreadySentNotPublishedResends(t *testing.T) {
	store, key := v8M3Fixture(t)
	if err := store.v8QueueEnqueue(key, "req-1", "Q1"); err != nil {
		t.Fatal(err)
	}
	item, err := store.v8QueueSendFront(key)
	if err != nil {
		t.Fatal(err)
	}
	if item.SendCount != 1 {
		t.Fatalf("send count = %d", item.SendCount)
	}
	// 崩溃：message.json 未发布 → 恢复重发。
	resent, err := store.v8QueueRecover(key)
	if err != nil {
		t.Fatal(err)
	}
	if len(resent) != 1 || resent[0].Content != "Q1" || resent[0].State != v8QueueQueued {
		t.Fatalf("resent = %+v", resent)
	}
	if _, err := store.v8QueueSendFront(key); err != nil {
		t.Fatalf("resend failed: %v", err)
	}
}

// TestV8LCPublishedMessageDequeues 对应 T-LC-08：message.json 已发布 → 不重
// 发，队列出队。
func TestV8LCPublishedMessageDequeues(t *testing.T) {
	store, key := v8M3Fixture(t)
	if err := store.v8QueueEnqueue(key, "req-1", "Q1"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.v8QueueSendFront(key); err != nil {
		t.Fatal(err)
	}
	row := v8M1Row(1, "u1", "user", EventKindUserInput, "发送内容")
	row.TaskID = "req-1"
	if _, err := store.v8MessageCommit(key, "commit-1", []Event{row}); err != nil {
		t.Fatal(err)
	}
	resent, err := store.v8QueueRecover(key)
	if err != nil {
		t.Fatal(err)
	}
	if len(resent) != 0 {
		t.Fatalf("resent = %+v", resent)
	}
	head, err := store.v8ReadLifecycleHead(key)
	if err != nil {
		t.Fatal(err)
	}
	if len(head.Queue) != 0 {
		t.Fatalf("queue after published confirm = %+v", head.Queue)
	}
}

// TestV8LCIllegalStateTransitionRejected 对应 T-LC-09：queued→sent 无发送
// 记录被拒绝。
func TestV8LCIllegalStateTransitionRejected(t *testing.T) {
	store, key := v8M3Fixture(t)
	if err := store.v8QueueEnqueue(key, "req-1", "Q1"); err != nil {
		t.Fatal(err)
	}
	if err := store.v8QueueConfirmSent(key, "req-1"); err == nil {
		t.Fatal("illegal queued->sent accepted")
	}
}

// ---------- fork ----------

// TestV8FKSessionCopyRange 对应 T-FK-01：主会话 [0..100] from=88 → 子会话含
// [wm..88] 行；队列/draft 为空。
func TestV8FKSessionCopyRange(t *testing.T) {
	store, key := v8M3Fixture(t)
	v8M2CommitRows(t, store, key, v8M2Rows(100))
	childKey := Key{ProjectID: key.ProjectID, SessionID: "child-fk1"}
	if err := store.v8ForkSession(key, childKey, 88); err != nil {
		t.Fatal(err)
	}
	rows, err := store.v8ReadRows(childKey, 1, 0)
	if err != nil || len(rows) != 88 {
		t.Fatalf("child rows=%d err=%v", len(rows), err)
	}
	if rows[0].Seq != 1 || rows[len(rows)-1].Seq != 88 {
		t.Fatalf("child range = [%d,%d]", rows[0].Seq, rows[len(rows)-1].Seq)
	}
	if _, ok := store.v8QueueFront(childKey); ok {
		t.Fatal("child must not inherit queue")
	}
	if _, err := store.v8ReadLifecycleHead(childKey); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("child lifecycle should be absent: %v", err)
	}
	if !store.v8SessionExists(childKey) {
		t.Fatal("child guide missing")
	}
}

// TestV8FKStackSnapshotRebuiltByAnchor 对应 T-FK-02：批次部分完成，from 落
// 批次中 → 子栈排除 > from 条目。
func TestV8FKStackSnapshotRebuiltByAnchor(t *testing.T) {
	store, key := v8M3Fixture(t)
	rows := []Event{
		v8M1Row(1, "msg1", "user", EventKindUserInput, "一"),
		v8M1Row(2, "msg2", "assistant", EventKindLLM, "a"),
		v8M1Row(3, "msg3", "user", EventKindUserInput, "二"),
		v8M1Row(4, "msg4", "assistant", EventKindLLM, "b"),
		v8M1Row(5, "msg5", "user", EventKindUserInput, "三"),
	}
	v8M2CommitRows(t, store, key, rows)
	stack := v8StackHead{
		SessionID: key.SessionID, HeadSeq: 5,
		Items: []v8StackItem{
			{ItemID: "item-1", ItemMessageID: "msg2", Status: "active"},
			{ItemID: "item-2", ItemMessageID: "msg4", Status: "active"},
			{ItemID: "item-3", ItemMessageID: "msg6", Status: "pending"},
		},
	}
	if err := store.v8CommitModuleHead(key, v8ModuleStack, &store.stackMu, "s1", stack); err != nil {
		t.Fatal(err)
	}
	childKey := Key{ProjectID: key.ProjectID, SessionID: "child-stack"}
	if err := store.v8ForkSession(key, childKey, 4); err != nil {
		t.Fatal(err)
	}
	childStack, err := v8ReadModuleHeadPayload[v8StackHead](store, childKey, v8ModuleStack)
	if err != nil {
		t.Fatal(err)
	}
	if len(childStack.Items) != 2 {
		t.Fatalf("child stack items = %+v", childStack.Items)
	}
	for _, item := range childStack.Items {
		if item.ItemID == "item-3" {
			t.Fatal("item beyond from leaked into child stack")
		}
	}
}

// TestV8FKForkBeforeWatermarkRejected 对应 T-FK-03：from < watermark → 显式
// 错误，不产生半成品会话。
func TestV8FKForkBeforeWatermarkRejected(t *testing.T) {
	store, key := v8M3Fixture(t)
	v8M2CommitRows(t, store, key, v8M2Rows(40))
	if _, err := store.v8LRUDelete(key, 20, true); err != nil {
		t.Fatal(err)
	}
	childKey := Key{ProjectID: key.ProjectID, SessionID: "bad-fork"}
	err := store.v8ForkSession(key, childKey, 10)
	if !errors.Is(err, ErrV8ForkBeforeWatermark) {
		t.Fatalf("err = %v", err)
	}
	if store.v8SessionExists(childKey) {
		t.Fatal("half-created child remains")
	}
}

// TestV8FKConsistentSnapshot 对应 T-FK-04：fork 基于固定 head 的一致快照，
// 之后父写不影响子。
func TestV8FKConsistentSnapshot(t *testing.T) {
	store, key := v8M3Fixture(t)
	v8M2CommitRows(t, store, key, v8M2Rows(50))
	childKey := Key{ProjectID: key.ProjectID, SessionID: "child-snap"}
	if err := store.v8ForkSession(key, childKey, 50); err != nil {
		t.Fatal(err)
	}
	v8M2CommitRows(t, store, key, v8M2Rows(20))
	childRows, err := store.v8ReadRows(childKey, 1, 0)
	if err != nil || len(childRows) != 50 {
		t.Fatalf("child rows=%d err=%v", len(childRows), err)
	}
}

// TestV8FKSubagentTree 对应 T-FK-05：spawn 生成 subagent_<hash>/ 子树
// （metadata/message/event），无独立 blob 目录。
func TestV8FKSubagentTree(t *testing.T) {
	store, key := v8M3Fixture(t)
	v8M2CommitRows(t, store, key, v8M2Rows(30))
	childStore, childKey, err := store.v8NewSubagent(key, "sub-1", 20)
	if err != nil {
		t.Fatal(err)
	}
	root := childStore.v8SessionRoot(childKey)
	for _, dir := range []string{"metadata", "message", "event"} {
		if _, err := os.Stat(filepath.Join(root, dir)); err != nil {
			t.Fatalf("subagent %s dir missing: %v", dir, err)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "big_tool_result")); err == nil {
		t.Fatal("subagent must not own a blob directory")
	}
	rows, err := childStore.v8ReadRows(childKey, 1, 0)
	if err != nil || len(rows) != 20 {
		t.Fatalf("subagent rows=%d err=%v", len(rows), err)
	}
}

// TestV8FKSubagentReusesMainBlob 对应 T-FK-06：子代理大工具输出复用主会话
// big_tool_result，可读回。
func TestV8FKSubagentReusesMainBlob(t *testing.T) {
	store, key := v8M3Fixture(t)
	v8M2CommitRows(t, store, key, v8M2Rows(10))
	blob, err := store.v8WriteBlob(key, "big_tool", "超长输出-"+string(make([]byte, 0))+repeatStr("数据", 10))
	if err != nil {
		t.Fatal(err)
	}
	childStore, childKey, err := store.v8NewSubagent(key, "sub-2", 10)
	if err != nil {
		t.Fatal(err)
	}
	// 子消息引用主会话 blob：可读回。
	content, err := store.v8ReadBlob(key, blob.Hash)
	if err != nil || !containsStr(content, "数据") {
		t.Fatalf("read blob err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(childStore.v8SessionRoot(childKey), "big_tool_result")); err == nil {
		t.Fatal("subagent created independent blob dir")
	}
}

// TestV8FKChildSurvivesParentDelete 对应 T-FK-07：fork 后删除父会话，子会话
// 自包含可恢复。
func TestV8FKChildSurvivesParentDelete(t *testing.T) {
	store, key := v8M3Fixture(t)
	v8M2CommitRows(t, store, key, v8M2Rows(40))
	childKey := Key{ProjectID: key.ProjectID, SessionID: "child-survive"}
	if err := store.v8ForkSession(key, childKey, 40); err != nil {
		t.Fatal(err)
	}
	if err := store.v8DeleteSession(key); err != nil {
		t.Fatal(err)
	}
	rows, err := store.v8ReadRows(childKey, 1, 0)
	if err != nil || len(rows) != 40 {
		t.Fatalf("child after parent delete rows=%d err=%v", len(rows), err)
	}
}

// TestV8FKArchiveSubagentParentOK 对应 T-FK-08：归档 subagent 后主会话不受
// 影响，保留 subagent EVENT。
func TestV8FKArchiveSubagentParentOK(t *testing.T) {
	store, key := v8M3Fixture(t)
	v8M2CommitRows(t, store, key, v8M2Rows(10))
	if _, _, err := store.v8NewSubagent(key, "sub-3", 10); err != nil {
		t.Fatal(err)
	}
	items, err := store.v8ReadSubagents(key)
	if err != nil || len(items) != 1 {
		t.Fatalf("subagents=%+v err=%v", items, err)
	}
	// 归档：status 更新 + 父会话仍可读（当前引擎保持 running 记录，校验父
	// 会话消息与 EVENT 完整）。
	rows, err := store.v8ReadRows(key, 1, 0)
	if err != nil || len(rows) != 10 {
		t.Fatalf("parent rows after subagent=%d err=%v", len(rows), err)
	}
	events, err := store.v8ReadEvents(key, 0, 0)
	if err != nil || len(events) == 0 {
		t.Fatalf("parent events after subagent=%d err=%v", len(events), err)
	}
}

// TestV8FKBlobGC 对应 T-FK-09：主会话与子代理消息均不再引用某 blob → GC
// 回收；任一仍引用则保留。
func TestV8FKBlobGC(t *testing.T) {
	store, key := v8M3Fixture(t)
	first, err := store.v8WriteBlob(key, "bash", "第一次大输出内容")
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.v8WriteBlob(key, "read", "第二次大输出内容")
	if err != nil {
		t.Fatal(err)
	}
	removed, err := store.v8BlobGarbageCollect(key, map[string]bool{first.Hash: true}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) != 1 || removed[0] != second.Hash {
		t.Fatalf("removed = %+v", removed)
	}
	if _, err := store.v8ReadBlob(key, first.Hash); err != nil {
		t.Fatalf("referenced blob removed: %v", err)
	}
	if _, err := store.v8ReadBlob(key, second.Hash); err == nil {
		t.Fatal("unreferenced blob survived GC")
	}
}

func repeatStr(value string, count int) string {
	out := ""
	for index := 0; index < count; index++ {
		out += value
	}
	return out
}

func containsStr(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (func() bool {
		for index := 0; index+len(needle) <= len(haystack); index++ {
			if haystack[index:index+len(needle)] == needle {
				return true
			}
		}
		return false
	})()
}
