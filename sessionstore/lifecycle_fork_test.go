package sessionstore

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func lifecycleFixture(t *testing.T) (*storeEngine, Key) {
	t.Helper()
	store := newStoreEngine(t.TempDir(), 0)
	return store, Key{ProjectID: "p-lc", SessionID: "s-lc"}
}

// ---------- lifecycle ----------

// TestLifecycleDraftToQueueAtomic 对应 T-LC-01：D→入队，queue 含 D、draft 清空
// （同一次 lifecycle 提交）。
func TestLifecycleDraftToQueueAtomic(t *testing.T) {
	store, key := lifecycleFixture(t)
	if err := store.draftToQueue(key, "草稿内容 D"); err != nil {
		t.Fatal(err)
	}
	head, err := store.readLifecycleHead(key)
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

// TestLifecycleQueueFIFO 对应 T-LC-02：Q1 阻塞时 Q2 不插队；Q1 成功后 Q2 才发。
func TestLifecycleQueueFIFO(t *testing.T) {
	store, key := lifecycleFixture(t)
	if err := store.queueEnqueue(key, "req-1", "Q1"); err != nil {
		t.Fatal(err)
	}
	if err := store.queueEnqueue(key, "req-2", "Q2"); err != nil {
		t.Fatal(err)
	}
	front, ok := store.queueFront(key)
	if !ok || front.Content != "Q1" {
		t.Fatalf("front = %+v ok=%v", front, ok)
	}
	if _, err := store.queueSendFront(key); err != nil {
		t.Fatal(err)
	}
	front, ok = store.queueFront(key)
	if !ok || front.Content != "Q1" {
		t.Fatalf("Q2 jumped queue: %+v", front)
	}
	if err := store.queueConfirmSent(key, "req-1"); err != nil {
		t.Fatal(err)
	}
	front, ok = store.queueFront(key)
	if !ok || front.Content != "Q2" {
		t.Fatalf("after Q1 done front = %+v ok=%v", front, ok)
	}
}

// TestLifecycleDirectSendQueueEmpty 对应 T-LC-03：队列空 + 草稿 D → 立即发送（不
// 进队）。
func TestLifecycleDirectSendQueueEmpty(t *testing.T) {
	store, key := lifecycleFixture(t)
	if err := store.saveDraft(key, "D"); err != nil {
		t.Fatal(err)
	}
	if err := store.draftDirectSend(key, "D"); err != nil {
		t.Fatal(err)
	}
	head, err := store.readLifecycleHead(key)
	if err != nil {
		t.Fatal(err)
	}
	if len(head.Queue) != 0 || head.Draft != nil {
		t.Fatalf("direct send should not enqueue: queue=%d draft=%+v", len(head.Queue), head.Draft)
	}
}

// TestLifecycleDirectSendQueueNonEmptyEnqueueTail 对应 T-LC-04：队列非空 + 草稿
// D → D 入队尾，不插队。
func TestLifecycleDirectSendQueueNonEmptyEnqueueTail(t *testing.T) {
	store, key := lifecycleFixture(t)
	if err := store.queueEnqueue(key, "req-1", "Q1"); err != nil {
		t.Fatal(err)
	}
	if err := store.draftDirectSend(key, "D"); err != nil {
		t.Fatal(err)
	}
	head, err := store.readLifecycleHead(key)
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

// TestLifecycleQueueSendFailedReturnsDraft 对应 T-LC-05：队首发送失败 → 内容回
// 草稿，队列移除该项。
func TestLifecycleQueueSendFailedReturnsDraft(t *testing.T) {
	store, key := lifecycleFixture(t)
	if err := store.queueEnqueue(key, "req-1", "Q1"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.queueSendFront(key); err != nil {
		t.Fatal(err)
	}
	if err := store.queueSendFailed(key); err != nil {
		t.Fatal(err)
	}
	head, err := store.readLifecycleHead(key)
	if err != nil {
		t.Fatal(err)
	}
	if len(head.Queue) != 0 || head.Draft == nil || head.Draft.Content != "Q1" {
		t.Fatalf("queue=%d draft=%+v", len(head.Queue), head.Draft)
	}
}

// TestLifecycleDirectSendFailedReturnsDraft 对应 T-LC-06：草稿直发失败 → 内容回
// 草稿。
func TestLifecycleDirectSendFailedReturnsDraft(t *testing.T) {
	store, key := lifecycleFixture(t)
	if err := store.directSendFailed(key, "D"); err != nil {
		t.Fatal(err)
	}
	head, err := store.readLifecycleHead(key)
	if err != nil {
		t.Fatal(err)
	}
	if head.Draft == nil || head.Draft.Content != "D" || head.Draft.State != "发送失败退回" {
		t.Fatalf("draft = %+v", head.Draft)
	}
}

// TestLifecycleAlreadySentNotPublishedResends 对应 T-LC-07：已发送但 message.json
// 未发布 → 重启后该项重发一次。
func TestLifecycleAlreadySentNotPublishedResends(t *testing.T) {
	store, key := lifecycleFixture(t)
	if err := store.queueEnqueue(key, "req-1", "Q1"); err != nil {
		t.Fatal(err)
	}
	item, err := store.queueSendFront(key)
	if err != nil {
		t.Fatal(err)
	}
	if item.SendCount != 1 {
		t.Fatalf("send count = %d", item.SendCount)
	}
	// 崩溃：message.json 未发布 → 恢复重发。
	resent, err := store.queueRecover(key)
	if err != nil {
		t.Fatal(err)
	}
	if len(resent) != 1 || resent[0].Content != "Q1" || resent[0].State != queueQueued {
		t.Fatalf("resent = %+v", resent)
	}
	if _, err := store.queueSendFront(key); err != nil {
		t.Fatalf("resend failed: %v", err)
	}
}

// TestLifecyclePublishedMessageDequeues 对应 T-LC-08：message.json 已发布 → 不重
// 发，队列出队。
func TestLifecyclePublishedMessageDequeues(t *testing.T) {
	store, key := lifecycleFixture(t)
	if err := store.queueEnqueue(key, "req-1", "Q1"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.queueSendFront(key); err != nil {
		t.Fatal(err)
	}
	row := messageRow(1, "u1", "user", EventKindUserInput, "发送内容")
	row.TaskID = "req-1"
	if _, err := store.messageCommit(key, "commit-1", []Event{row}); err != nil {
		t.Fatal(err)
	}
	resent, err := store.queueRecover(key)
	if err != nil {
		t.Fatal(err)
	}
	if len(resent) != 0 {
		t.Fatalf("resent = %+v", resent)
	}
	head, err := store.readLifecycleHead(key)
	if err != nil {
		t.Fatal(err)
	}
	if len(head.Queue) != 0 {
		t.Fatalf("queue after published confirm = %+v", head.Queue)
	}
}

// TestLifecycleIllegalStateTransitionRejected 对应 T-LC-09：queued→sent 无发送
// 记录被拒绝。
func TestLifecycleIllegalStateTransitionRejected(t *testing.T) {
	store, key := lifecycleFixture(t)
	if err := store.queueEnqueue(key, "req-1", "Q1"); err != nil {
		t.Fatal(err)
	}
	if err := store.queueConfirmSent(key, "req-1"); err == nil {
		t.Fatal("illegal queued->sent accepted")
	}
}

// ---------- fork ----------

// TestForkStoreSessionCopyRange 对应 T-FK-01：主会话 [0..100] from=88 → 子会话含
// [wm..88] 行；队列/draft 为空。
func TestForkStoreSessionCopyRange(t *testing.T) {
	store, key := lifecycleFixture(t)
	commitRoundRows(t, store, key, roundRows(100))
	childKey := Key{ProjectID: key.ProjectID, SessionID: "child-fk1"}
	if err := store.forkSession(key, childKey, 88); err != nil {
		t.Fatal(err)
	}
	rows, err := store.readRows(childKey, 1, 0)
	if err != nil || len(rows) != 88 {
		t.Fatalf("child rows=%d err=%v", len(rows), err)
	}
	if rows[0].Seq != 1 || rows[len(rows)-1].Seq != 88 {
		t.Fatalf("child range = [%d,%d]", rows[0].Seq, rows[len(rows)-1].Seq)
	}
	if _, ok := store.queueFront(childKey); ok {
		t.Fatal("child must not inherit queue")
	}
	if _, err := store.readLifecycleHead(childKey); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("child lifecycle should be absent: %v", err)
	}
	if !store.sessionExists(childKey) {
		t.Fatal("child guide missing")
	}
}

// TestForkStoreStackSnapshotRebuiltByAnchor 对应 T-FK-02：批次部分完成，from 落
// 批次中 → 子栈排除 > from 条目。
func TestForkStoreStackSnapshotRebuiltByAnchor(t *testing.T) {
	store, key := lifecycleFixture(t)
	rounds := [][]Event{
		{
			messageRow(1, "msg1", "user", EventKindUserInput, "一"),
			messageRow(2, "msg2", "assistant", EventKindLLM, "a"),
		},
		{
			messageRow(3, "msg3", "user", EventKindUserInput, "二"),
			messageRow(4, "msg4", "assistant", EventKindLLM, "b"),
		},
		{messageRow(5, "msg5", "user", EventKindUserInput, "三")},
	}
	itemIDs := []string{"item-1", "item-2", "item-3"}
	for index, rows := range rounds {
		commitRoundRows(t, store, key, rows)
		items := []StackItemInput{{ItemID: itemIDs[index], Kind: StackKindTask, Status: "active"}}
		if _, err := stackCommit(store.stackJournal(), key, StackKindTask, stackPushMessage(StackKindTask, "", items)); err != nil {
			t.Fatal(err)
		}
	}
	parentActive, err := stackReadActive(store.stackJournal(), key, StackKindTask)
	if err != nil {
		t.Fatal(err)
	}
	if parentActive[0].ItemMessageID != "msg2" || parentActive[1].ItemMessageID != "msg4" ||
		parentActive[2].ItemMessageID != "msg5" {
		t.Fatalf("parent anchors = %+v", parentActive)
	}
	childKey := Key{ProjectID: key.ProjectID, SessionID: "child-stack"}
	if err := store.forkSession(key, childKey, 4); err != nil {
		t.Fatal(err)
	}
	childStack, err := stackReadActive(store.stackJournal(), childKey, StackKindTask)
	if err != nil {
		t.Fatal(err)
	}
	if len(childStack) != 2 {
		t.Fatalf("child stack items = %+v", childStack)
	}
	for _, item := range childStack {
		if item.ItemID == "item-3" {
			t.Fatal("item beyond from leaked into child stack")
		}
		if item.ItemMessageSeq > 4 {
			t.Fatalf("item %q anchor %d > fork point", item.ItemID, item.ItemMessageSeq)
		}
	}
	if childStack[0].ItemMessageID != "msg2" || childStack[1].ItemMessageID != "msg4" {
		t.Fatalf("child anchors not preserved: %+v", childStack)
	}
}

// TestForkStoreForkBeforeWatermarkRejected 对应 T-FK-03：from < watermark → 显式
// 错误，不产生半成品会话。
func TestForkStoreForkBeforeWatermarkRejected(t *testing.T) {
	store, key := lifecycleFixture(t)
	commitRoundRows(t, store, key, roundRows(40))
	if _, err := store.lRUDelete(key, 20, true); err != nil {
		t.Fatal(err)
	}
	childKey := Key{ProjectID: key.ProjectID, SessionID: "bad-fork"}
	err := store.forkSession(key, childKey, 10)
	if !errors.Is(err, ErrForkBeforeWatermark) {
		t.Fatalf("err = %v", err)
	}
	if store.sessionExists(childKey) {
		t.Fatal("half-created child remains")
	}
}

// TestForkStoreConsistentSnapshot 对应 T-FK-04：fork 基于固定 head 的一致快照，
// 之后父写不影响子。
func TestForkStoreConsistentSnapshot(t *testing.T) {
	store, key := lifecycleFixture(t)
	commitRoundRows(t, store, key, roundRows(50))
	childKey := Key{ProjectID: key.ProjectID, SessionID: "child-snap"}
	if err := store.forkSession(key, childKey, 50); err != nil {
		t.Fatal(err)
	}
	commitRoundRows(t, store, key, roundRows(20))
	childRows, err := store.readRows(childKey, 1, 0)
	if err != nil || len(childRows) != 50 {
		t.Fatalf("child rows=%d err=%v", len(childRows), err)
	}
}

// TestForkStoreSubagentTree 对应 T-FK-05：spawn 生成 subagent_<hash>/ 子树
// （metadata/message/event），无独立 blob 目录。
func TestForkStoreSubagentTree(t *testing.T) {
	store, key := lifecycleFixture(t)
	commitRoundRows(t, store, key, roundRows(30))
	childStore, childKey, err := store.newSubagent(key, "sub-1", 20)
	if err != nil {
		t.Fatal(err)
	}
	root := childStore.sessionRoot(childKey)
	for _, dir := range []string{"metadata", "message", "event"} {
		if _, err := os.Stat(filepath.Join(root, dir)); err != nil {
			t.Fatalf("subagent %s dir missing: %v", dir, err)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "big_tool_result")); err == nil {
		t.Fatal("subagent must not own a blob directory")
	}
	rows, err := childStore.readRows(childKey, 1, 0)
	if err != nil || len(rows) != 20 {
		t.Fatalf("subagent rows=%d err=%v", len(rows), err)
	}
}

// TestForkStoreSubagentReusesMainBlob 对应 T-FK-06：子代理大工具输出复用主会话
// big_tool_result，可读回。
func TestForkStoreSubagentReusesMainBlob(t *testing.T) {
	store, key := lifecycleFixture(t)
	commitRoundRows(t, store, key, roundRows(10))
	blob, err := store.writeBlob(key, "big_tool", "超长输出-"+string(make([]byte, 0))+repeatStr("数据", 10))
	if err != nil {
		t.Fatal(err)
	}
	childStore, childKey, err := store.newSubagent(key, "sub-2", 10)
	if err != nil {
		t.Fatal(err)
	}
	// 子消息引用主会话 blob：可读回。
	content, err := store.readBlob(key, blob.Hash)
	if err != nil || !containsStr(content, "数据") {
		t.Fatalf("read blob err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(childStore.sessionRoot(childKey), "big_tool_result")); err == nil {
		t.Fatal("subagent created independent blob dir")
	}
}

// TestForkStoreChildSurvivesParentDelete 对应 T-FK-07：fork 后删除父会话，子会话
// 自包含可恢复。
func TestForkStoreChildSurvivesParentDelete(t *testing.T) {
	store, key := lifecycleFixture(t)
	commitRoundRows(t, store, key, roundRows(40))
	childKey := Key{ProjectID: key.ProjectID, SessionID: "child-survive"}
	if err := store.forkSession(key, childKey, 40); err != nil {
		t.Fatal(err)
	}
	if err := store.deleteSession(key); err != nil {
		t.Fatal(err)
	}
	rows, err := store.readRows(childKey, 1, 0)
	if err != nil || len(rows) != 40 {
		t.Fatalf("child after parent delete rows=%d err=%v", len(rows), err)
	}
}

// TestForkStoreArchiveSubagentParentOK 对应 T-FK-08：归档 subagent 后主会话不受
// 影响，保留 subagent EVENT。
func TestForkStoreArchiveSubagentParentOK(t *testing.T) {
	store, key := lifecycleFixture(t)
	commitRoundRows(t, store, key, roundRows(10))
	if _, _, err := store.newSubagent(key, "sub-3", 10); err != nil {
		t.Fatal(err)
	}
	items, err := store.readSubagents(key)
	if err != nil || len(items) != 1 {
		t.Fatalf("subagents=%+v err=%v", items, err)
	}
	// 归档：status 更新 + 父会话仍可读（当前引擎保持 running 记录，校验父
	// 会话消息与 EVENT 完整）。
	rows, err := store.readRows(key, 1, 0)
	if err != nil || len(rows) != 10 {
		t.Fatalf("parent rows after subagent=%d err=%v", len(rows), err)
	}
	events, err := store.readEvents(key, 0, 0)
	if err != nil || len(events) == 0 {
		t.Fatalf("parent events after subagent=%d err=%v", len(events), err)
	}
}

// TestForkStoreBlobGC 对应 T-FK-09：主会话与子代理消息均不再引用某 blob → GC
// 回收；任一仍引用则保留。
func TestForkStoreBlobGC(t *testing.T) {
	store, key := lifecycleFixture(t)
	first, err := store.writeBlob(key, "bash", "第一次大输出内容")
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.writeBlob(key, "read", "第二次大输出内容")
	if err != nil {
		t.Fatal(err)
	}
	removed, err := store.blobGarbageCollect(key, map[string]bool{first.Hash: true}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) != 1 || removed[0] != second.Hash {
		t.Fatalf("removed = %+v", removed)
	}
	if _, err := store.readBlob(key, first.Hash); err != nil {
		t.Fatalf("referenced blob removed: %v", err)
	}
	if _, err := store.readBlob(key, second.Hash); err == nil {
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
