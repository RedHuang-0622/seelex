// EVENT 写形态（append-only，与 message 同构）契约测试：my_design 附录 A.2 /
// D.8 T-EV-02、T-EV-05。
//
// 判定重点：提交只依据 head 水位，因此**读历史分片次数必须为 0**（只有崩溃
// 恢复路径允许读一次尾分片），且未发布的行不可见、下次提交被截回。
package sessionstore

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func eventFixture(t *testing.T, shardRows int) (*storeEngine, Key) {
	t.Helper()
	store := newStoreEngine(t.TempDir(), storageSettings{MessageShardRows: shardRows})
	return store, Key{ProjectID: "p-ev", SessionID: "s-ev"}
}

func eventHeadShards(t *testing.T, store *storeEngine, key Key) []eventShardInfo {
	t.Helper()
	head, err := store.readEventHead(key)
	if err != nil {
		t.Fatal(err)
	}
	return head.Shards
}

// eventTailShardPath 返回 head 记录的尾分片物理路径。
func eventTailShardPath(t *testing.T, store *storeEngine, key Key) string {
	t.Helper()
	shards := eventHeadShards(t, store, key)
	if len(shards) == 0 {
		t.Fatal("event head has no shards")
	}
	return filepath.Join(store.structuralEventDir(key), shards[len(shards)-1].Path)
}

func turnEvent(round int) structuralEvent {
	return structuralEvent{
		Kind: structuralEventTurnBegin, AnchorSeq: uint64(round),
		Payload: rawJSON(`{"turn":` + strconv.Itoa(round) + `}`),
	}
}

// TestStructuralEventCommitNeverReadsHistoryShards 对应 T-EV-05：分片已滚动多片
// 后继续提交 → 只 append 尾分片，读历史分片 0 次。
func TestStructuralEventCommitNeverReadsHistoryShards(t *testing.T) {
	const shardRows = 5
	store, key := eventFixture(t, shardRows)
	for round := 1; round <= 40; round++ {
		if _, err := store.structuralEventCommit(key, "ev-"+strconv.Itoa(round),
			[]structuralEvent{turnEvent(round)}); err != nil {
			t.Fatalf("commit %d: %v", round, err)
		}
		if got := store.event.shardReads.Load(); got != 0 {
			t.Fatalf("第 %d 次提交读历史分片 %d 次：EVENT 写路径又回看历史", round, got)
		}
	}
	shards := eventHeadShards(t, store, key)
	if len(shards) < 40/shardRows {
		t.Fatalf("分片数 = %d want >= %d（shard_rows=%d 必须滚动）", len(shards), 40/shardRows, shardRows)
	}
	all, err := store.readEvents(key, 0, 0)
	if err != nil || len(all) != 40 {
		t.Fatalf("events len=%d err=%v", len(all), err)
	}
	for index, row := range all {
		if row.EventID != uint64(index+1) {
			t.Fatalf("events[%d].id = %d want %d", index, row.EventID, index+1)
		}
	}
	if err := store.verifyEvents(key); err != nil {
		t.Fatal(err)
	}
}

// TestStructuralEventWatermarkIdempotent 对应 A.2 规则 1/2/3：event_id=0 续号、
// 显式 id ≤ 水位跳过、同 commit 重复持久化整次空操作。
func TestStructuralEventWatermarkIdempotent(t *testing.T) {
	store, key := eventFixture(t, 100)
	head, err := store.structuralEventCommit(key, "ev-a", []structuralEvent{turnEvent(1), turnEvent(2)})
	if err != nil || head.LastID != 2 || head.LastCommitID != "ev-a" {
		t.Fatalf("head=%+v err=%v", head, err)
	}
	// 规则 3：紧邻重复提交同一 commit_id → 空操作，水位不动。
	again, err := store.structuralEventCommit(key, "ev-a", []structuralEvent{turnEvent(1)})
	if err != nil || again.LastID != 2 {
		t.Fatalf("repeat commit head=%+v err=%v", again, err)
	}
	// 规则 2：水位内的显式 id 跳过；水位外的显式 id 采纳并以其续号。
	head, err = store.structuralEventCommit(key, "ev-b", []structuralEvent{
		{EventID: 1, Kind: structuralEventRolledBack},
		{EventID: 9, Kind: structuralEventTokenUsage},
		turnEvent(3),
	})
	if err != nil || head.LastID != 10 {
		t.Fatalf("head=%+v err=%v", head, err)
	}
	all, err := store.readEvents(key, 0, 0)
	if err != nil || len(all) != 4 {
		t.Fatalf("events len=%d err=%v", len(all), err)
	}
	// 水位允许空洞（3..8）：显式 event_id 由生产者负责，引擎只保证严格递增。
	if all[2].EventID != 9 || all[3].EventID != 10 {
		t.Fatalf("tail ids = [%d,%d] want [9,10]", all[2].EventID, all[3].EventID)
	}
	if err := store.verifyEvents(key); err != nil {
		t.Fatal(err)
	}
	// 显式 id 非严格递增（越界后又回落）必须显式拒绝。
	if _, err := store.structuralEventCommit(key, "ev-c", []structuralEvent{
		{EventID: 20, Kind: structuralEventRolledBack}, {EventID: 12, Kind: structuralEventRolledBack},
	}); err == nil {
		t.Fatal("非严格递增 event_id 被接受")
	}
}

// TestStructuralEventUnpublishedAppendInvisible 对应发布点语义（I5）：append 完成但
// head 未发布 = 未提交 → 读者不可见，下次提交先截回再续写。
func TestStructuralEventUnpublishedAppendInvisible(t *testing.T) {
	store, key := eventFixture(t, 100)
	if _, err := store.structuralEventCommit(key, "ev-1", []structuralEvent{turnEvent(1)}); err != nil {
		t.Fatal(err)
	}
	readsBefore := store.event.shardReads.Load()
	// 模拟崩溃：物理追加一行，但 head 未推进。
	tail := eventTailShardPath(t, store, key)
	if _, err := appendStructuralEvents(tail, []structuralEvent{
		{EventID: 2, Kind: structuralEventInterrupted, CommitID: "ev-crash"},
	}); err != nil {
		t.Fatal(err)
	}
	all, err := store.readEvents(key, 0, 0)
	if err != nil || len(all) != 1 || all[0].EventID != 1 {
		t.Fatalf("未发布行可见: len=%d err=%v", len(all), err)
	}
	head, err := store.structuralEventCommit(key, "ev-2", []structuralEvent{turnEvent(2)})
	if err != nil || head.LastID != 2 {
		t.Fatalf("head=%+v err=%v", head, err)
	}
	if got := store.event.shardReads.Load(); got != readsBefore+1 {
		t.Fatalf("恢复读分片次数 = %d want %d（崩溃后只允许读一次尾分片）", got, readsBefore+1)
	}
	all, err = store.readEvents(key, 0, 0)
	if err != nil || len(all) != 2 || all[1].Kind != structuralEventTurnBegin {
		t.Fatalf("events=%+v err=%v（崩溃行必须被截回，不与会重号）", all, err)
	}
	if err := store.verifyEvents(key); err != nil {
		t.Fatal(err)
	}
	// 修补后的 head 已落盘：后续提交回到零重读。
	if _, err := store.structuralEventCommit(key, "ev-3", []structuralEvent{turnEvent(3)}); err != nil {
		t.Fatal(err)
	}
	if got := store.event.shardReads.Load(); got != readsBefore+1 {
		t.Fatalf("恢复后仍重复读分片 = %d want %d", got, readsBefore+1)
	}
}

// TestStructuralEventHeadAbsentMeansNothingPublished 对应 head = 发布点：删除
// event.json 后事件不可见，未发布分片被清扫，重新提交从水位 0 续号。
func TestStructuralEventHeadAbsentMeansNothingPublished(t *testing.T) {
	store, key := eventFixture(t, 100)
	if _, err := store.structuralEventCommit(key, "ev-1", []structuralEvent{turnEvent(1), turnEvent(2)}); err != nil {
		t.Fatal(err)
	}
	tail := eventTailShardPath(t, store, key)
	if err := os.Remove(store.modulePath(key, moduleEvent)); err != nil {
		t.Fatal(err)
	}
	visible, readErr := store.readEvents(key, 0, 0)
	if readErr != nil || len(visible) != 0 {
		t.Fatalf("head 缺失仍有可见事件 len=%d err=%v", len(visible), readErr)
	}
	if _, err := store.structuralEventCommit(key, "ev-2", []structuralEvent{turnEvent(3)}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(tail); !os.IsNotExist(err) {
		t.Fatalf("head 未索引的旧分片未被清扫: err=%v", err)
	}
	shards := eventHeadShards(t, store, key)
	if len(shards) != 1 || shards[0].FromID != 1 || shards[0].Count != 1 {
		t.Fatalf("shards = %+v want 单片 from_id=1 count=1", shards)
	}
	if err := store.verifyEvents(key); err != nil {
		t.Fatal(err)
	}
}

// TestStructuralEventCrashTailDropped 对应崩溃残尾：半行不发布且被截净，恢复
// 只读一次尾分片。
func TestStructuralEventCrashTailDropped(t *testing.T) {
	store, key := eventFixture(t, 100)
	if _, err := store.structuralEventCommit(key, "ev-1", []structuralEvent{turnEvent(1)}); err != nil {
		t.Fatal(err)
	}
	tail := eventTailShardPath(t, store, key)
	data, err := os.ReadFile(tail)
	if err != nil {
		t.Fatal(err)
	}
	partial, err := json.Marshal(structuralEvent{EventID: 2, Kind: structuralEventInterrupted, CommitID: "ev-tail"})
	if err != nil {
		t.Fatal(err)
	}
	// 半行（去掉换行收尾）= 写入过程中崩溃。
	if err := os.WriteFile(tail, append(append([]byte{}, data...), partial[:len(partial)-1]...), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.structuralEventCommit(key, "ev-2", []structuralEvent{turnEvent(2)}); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(tail)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasSuffix(body, []byte("\n")) {
		t.Fatalf("残尾未被截净: %q", body[len(body)-16:])
	}
	if lines := bytes.Count(body, []byte("\n")); lines != 2 {
		t.Fatalf("尾分片行数 = %d want 2: %s", lines, body)
	}
	all, err := store.readEvents(key, 0, 0)
	if err != nil || len(all) != 2 {
		t.Fatalf("events len=%d err=%v", len(all), err)
	}
	if err := store.verifyEvents(key); err != nil {
		t.Fatal(err)
	}
	reads := store.event.shardReads.Load()
	if reads != 1 {
		t.Fatalf("残尾恢复读分片次数 = %d want 1", reads)
	}
	if _, err := store.structuralEventCommit(key, "ev-3", []structuralEvent{turnEvent(3)}); err != nil {
		t.Fatal(err)
	}
	if got := store.event.shardReads.Load(); got != reads {
		t.Fatalf("残尾恢复后仍重复读分片 = %d want %d", got, reads)
	}
}

// TestStructuralEventForkRowsSurviveConsecutiveForks 对应 T-EV-06（§2.0 规则 4、
// D13）：同一父会话连续 fork 两个子会话，两条 fork EVENT 都必须在。
//
// 两个方向都要钉住：常量凭据 → 第二次 fork 被 A.2 规则 3 判成重复提交而**整次
// 丢弃**（丢真数据）；随机凭据 → 发布失败后的重放认不出同一逻辑操作而留下重复
// 行。因此凭据必须逐操作唯一**且**可由调用方从「这次到底是哪一次操作」推出。
func TestStructuralEventForkRowsSurviveConsecutiveForks(t *testing.T) {
	store := newStoreEngine(t.TempDir(), storageSettings{})
	parent := Key{ProjectID: "p-ev", SessionID: "parent"}
	if _, err := store.messageCommit(parent, "", []Event{
		{Role: "user", Content: "a"}, {Role: "assistant", Content: "b"},
	}); err != nil {
		t.Fatal(err)
	}
	children := []string{"child-1", "child-2"}
	for _, child := range children {
		if err := store.forkSession(parent, Key{ProjectID: "p-ev", SessionID: child}, 2); err != nil {
			t.Fatal(err)
		}
	}
	events, err := store.readEvents(parent, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != len(children) {
		t.Fatalf("fork EVENT rows = %d want %d：常量凭据会把后一次 fork 整次吞掉", len(events), len(children))
	}
	credentials := make(map[string]bool, len(events))
	for index, row := range events {
		if row.Kind != structuralEventFork {
			t.Fatalf("events[%d].kind = %q want fork", index, row.Kind)
		}
		if row.AnchorSeq != 2 {
			t.Fatalf("events[%d].anchor_seq = %d want 2", index, row.AnchorSeq)
		}
		if credentials[row.CommitID] {
			t.Fatalf("两次 fork 共用凭据 %q：后一次必然被判成重复提交", row.CommitID)
		}
		credentials[row.CommitID] = true
		// 凭据由逻辑操作（被 fork 出的子会话）决定 → 同一次 fork 重放必得同一值。
		var payload struct {
			ChildID string `json:"child_id"`
		}
		if err := json.Unmarshal(row.Payload, &payload); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(row.CommitID, payload.ChildID) {
			t.Fatalf("events[%d].commit_id = %q 不含 child_id %q：凭据不是调用方可重算的操作标识",
				index, row.CommitID, payload.ChildID)
		}
	}
	if err := store.verifyEvents(parent); err != nil {
		t.Fatal(err)
	}
}
