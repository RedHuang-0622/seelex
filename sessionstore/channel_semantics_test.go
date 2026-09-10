package sessionstore

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

// TestStackChannelReplayUsesDeterministicCommitID 对应 T-STK-10（§2.0 规则 4、
// D10/S17）：head 发布失败后重放同一逻辑操作 → 凭据相同、行不重复、head 只
// 前进一次，head watermark 记录 last_commit_id。
func TestStackChannelReplayUsesDeterministicCommitID(t *testing.T) {
	harness := newJSONStackHarness(t)
	store, key := harness.store, harness.key
	harness.seedMessages(1)
	if _, err := harness.push(StackKindTask, "batch-r1", StackItemInput{ItemID: "R1"}); err != nil {
		t.Fatal(err)
	}
	published, err := os.ReadFile(store.modulePath(key, moduleForStackKind(StackKindTask)))
	if err != nil {
		t.Fatal(err)
	}

	// 第二次提交：数据（active）已落盘，head 发布失败（回退到上一版 head）。
	second, err := harness.push(StackKindTask, "batch-r2", StackItemInput{ItemID: "R2"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.modulePath(key, moduleForStackKind(StackKindTask)), published, 0o600); err != nil {
		t.Fatal(err)
	}
	store.dropStackViews(key)

	// 同一逻辑操作重放。
	retry, err := harness.push(StackKindTask, "batch-r2", StackItemInput{ItemID: "R2"})
	if err != nil {
		t.Fatalf("replay after head publish failure: %v", err)
	}
	wantCommit := stackMutationCommitID(StackKindTask, second)
	if got := stackMutationCommitID(StackKindTask, retry); got != wantCommit {
		t.Fatalf("重放凭据不稳定: first=%q retry=%q", wantCommit, got)
	}
	if got := harness.head(StackKindTask).Kinds[StackKindTask].LastCommitID; got != wantCommit {
		t.Fatalf("head last_commit_id = %q, want %q", got, wantCommit)
	}
	active := harness.active(StackKindTask)
	if len(active) != 2 {
		t.Fatalf("replay 后 active = %d 行, want 2（不得重复追加）: %+v", len(active), active)
	}
	harness.mustVerify()
}

// TestStackChannelColdReloadDeduplicatesByItemID 对应 T-STK-11（§2.0 规则 4
// 读侧、D10/S17）：重放留下的同 item_id 双行在冷重载后只保留最高 revision。
func TestStackChannelColdReloadDeduplicatesByItemID(t *testing.T) {
	harness := newJSONStackHarness(t)
	store, key := harness.store, harness.key
	harness.seedMessages(1)
	if _, err := harness.push(StackKindTask, "batch-d1", StackItemInput{ItemID: "A"}); err != nil {
		t.Fatal(err)
	}
	if _, err := harness.push(StackKindTask, "batch-d2", StackItemInput{ItemID: "B"}); err != nil {
		t.Fatal(err)
	}
	path := store.stackActivePath(key, StackKindTask)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	rows := decodeStackRows(data)
	if len(rows) != 2 {
		t.Fatalf("seed rows = %d, want 2", len(rows))
	}
	// 模拟旧版本/重放留下的同 item_id 双行（都 ≤ head_seq，高 revision 应
	// 胜出）：A@1 + A@2、B@2 + B@1。
	duplicateA := rows[0] // A@1
	duplicateA.Revision = 2
	duplicateB := rows[1] // B@2
	duplicateB.Revision = 1
	crafted := []StackItemRecord{rows[0], rows[1], duplicateA, duplicateB}
	if err := os.WriteFile(path, encodeStackRows(crafted), 0o600); err != nil {
		t.Fatal(err)
	}
	store.dropStackViews(key)

	active := harness.active(StackKindTask)
	if len(active) != 2 {
		t.Fatalf("冷重载 active = %d, want 2（读侧去重）", len(active))
	}
	byID := make(map[string]StackItemRecord, len(active))
	for _, row := range active {
		byID[row.ItemID] = row
	}
	if byID["A"].Revision != 2 || byID["B"].Revision != 2 {
		t.Fatalf("读侧必须保留高 revision 行: %+v", byID)
	}
	harness.mustVerify()
}

// TestStackChannelActiveWholeReplacement 对应 T-STK-13（§2.0 通道类型表 +
// D12/H9）：active.jsonl 属整份替换型通道——每次提交整文件原子替换，投影行
// 只来自最新一次替换，不存在「已 append 未发布」的中间态叠加。
func TestStackChannelActiveWholeReplacement(t *testing.T) {
	harness := newJSONStackHarness(t)
	store, key := harness.store, harness.key
	harness.seedMessages(1)
	if _, err := harness.push(StackKindPlan, "batch-a",
		StackItemInput{ItemID: "p1"}, StackItemInput{ItemID: "p2"},
	); err != nil {
		t.Fatal(err)
	}
	path := store.stackActivePath(key, StackKindPlan)
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(decodeStackRows(first)); got != 2 {
		t.Fatalf("active rows after first push = %d, want 2", got)
	}

	// 第二次提交：active 必须是「整份新投影」，而不是在旧文件上 append。
	if _, err := harness.push(StackKindPlan, "batch-b", StackItemInput{ItemID: "p3"}); err != nil {
		t.Fatal(err)
	}
	second, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	rows := decodeStackRows(second)
	if len(rows) != 3 {
		t.Fatalf("active rows after second push = %d, want 3（整份替换不得残留旧快照行）", len(rows))
	}
	if strings.Count(string(second), "\n") != len(rows) {
		t.Fatalf("active file 行数与投影不一致（可能叠加了旧内容）: %s", second)
	}

	// 强制冷读后，磁盘内容就是当前投影（与内存发布一致）。
	store.dropStackViews(key)
	got := harness.active(StackKindPlan)
	want := decodeStackRows(second)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("冷读投影 = %+v, want 文件内容 %+v（整份替换型无半更新中间态）", got, want)
	}
	harness.mustVerify()
}

// TestQueueFileWholeReplacement 对应 T-STK-14（§2.0 通道类型表）：队列
// queue.jsonl 是整份替换型——出队 = 新内容不再含该项，无墓碑行、无追加残
// 留，文件始终是完整的当前队列快照。
func TestQueueFileWholeReplacement(t *testing.T) {
	store, key := lifecycleFixture(t)
	if err := store.queueEnqueue(key, "req-1", "Q1"); err != nil {
		t.Fatal(err)
	}
	if err := store.queueEnqueue(key, "req-2", "Q2"); err != nil {
		t.Fatal(err)
	}
	path := store.lifecycleQueuePath(key)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(before), "\n") != 2 {
		t.Fatalf("queue rows before dequeue = %d, want 2", strings.Count(string(before), "\n"))
	}

	// Q1 发送成功（message 行发布 = 最终确认）→ 整份替换为不含 Q1 的新快照。
	if _, err := store.queueSendFront(key); err != nil {
		t.Fatal(err)
	}
	row := messageRow(1, "u1", "user", EventKindUserInput, "Q1 内容")
	row.TaskID = "req-1"
	if _, err := store.messageCommit(key, "commit-q1", []Event{row}); err != nil {
		t.Fatal(err)
	}
	if resent, err := store.queueRecover(key); err != nil || len(resent) != 0 {
		t.Fatalf("recover after published confirm: resent=%v err=%v", resent, err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if rows := strings.Count(string(after), "\n"); rows != 1 {
		t.Fatalf("queue rows after dequeue = %d, want 1（出队必须整份替换）", rows)
	}
	if strings.Contains(string(after), "Q1") || strings.Contains(string(after), "req-1") {
		t.Fatalf("出队项残留（应整份替换、无墓碑行）: %s", after)
	}
	if !strings.Contains(string(after), "Q2") {
		t.Fatalf("剩余队列项丢失: %s", after)
	}
	state, err := store.readLifecycleState(key)
	if err != nil || len(state.Queue) != 1 || state.Queue[0].Content != "Q2" {
		t.Fatalf("lifecycle state = %+v err=%v, want [Q2]", state.Queue, err)
	}
}
