// 栈通道契约测试（my_design §2.4/§3.1/§3.2/§4）。
//
// 覆盖两类事实：
//  1. 通道语义（批次弹栈、水位、锚、迁移 EVENT、fork 按锚重建、verify）在
//     **每个后端**上都成立（json 文件 / sqlite；同一份表驱动用例跑两遍）；
//  2. JSON 后端的落盘形状与锁延迟归因（active.jsonl vs history.jsonl）。
package sessionstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RedHuang-0622/Seele/types"
)

// stackHarness 是「同一套栈通道用例 × 不同后端」的夹具。
type stackHarness struct {
	t          *testing.T
	name       string
	journal    stackJournal
	repository Repository
	sqlDB      *sql.DB
	store      *storeEngine // 仅 JSON 后端用于断言物理放置
	key        Key
	// carriesMessageIDs = 后端消息通道是否有事件行键（JSON 有；SQL/Redis 的
	// 消息通道仍是整块 shard 快照 → 锚只有 seq）。
	carriesMessageIDs bool
}

func newJSONStackHarness(t *testing.T) *stackHarness {
	t.Helper()
	store := newStoreEngine(t.TempDir(), storageSettings{})
	return &stackHarness{
		t: t, name: "json", store: store, journal: store.stackJournal(),
		key:               Key{ProjectID: "p-stack", SessionID: "s-stack"},
		carriesMessageIDs: true,
	}
}

func newSQLStackHarness(t *testing.T) *stackHarness {
	t.Helper()
	repository, err := Open(context.Background(), Config{
		Backend: BackendSQLite, Path: filepath.Join(t.TempDir(), "sessions.db"),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repository.Close() })
	sqlBackend, ok := repository.(*sqlRepository)
	if !ok {
		t.Fatalf("sqlite 后端类型不符: %T", repository)
	}
	return &stackHarness{
		t: t, name: "sqlite", repository: repository, sqlDB: sqlBackend.db, journal: repository.stackJournal(),
		key: Key{ProjectID: "p-stack", SessionID: "s-stack"},
	}
}

// forEachStackBackend 把用例在后端矩阵上各跑一遍。
func forEachStackBackend(t *testing.T, fn func(t *testing.T, harness *stackHarness)) {
	t.Helper()
	t.Run("json", func(t *testing.T) { fn(t, newJSONStackHarness(t)) })
	t.Run("sqlite", func(t *testing.T) { fn(t, newSQLStackHarness(t)) })
}

// seedMessages 把 message 事实写成恰好 n 条（锚的来源）。两个后端都是
// 「按总数写入」：JSON 侧靠 seq 幂等续写，SQL 侧整块重写。
func (harness *stackHarness) seedMessages(n int) {
	harness.t.Helper()
	if harness.store != nil {
		rows := make([]Event, 0, n)
		for index := 0; index < n; index++ {
			rows = append(rows, messageRow(uint64(index+1), harness.anchorID(index+1), "user", EventKindUserInput,
				fmt.Sprintf("第 %d 条", index+1)))
		}
		commitRoundRows(harness.t, harness.store, harness.key, rows)
		return
	}
	messages := make([]types.Message, 0, n)
	for index := 0; index < n; index++ {
		content := fmt.Sprintf("第 %d 条", index+1)
		messages = append(messages, types.Message{Role: "user", Content: &content})
	}
	if err := harness.repository.WriteAtomic(context.Background(), harness.key, messages); err != nil {
		harness.t.Fatal(err)
	}
}

func (harness *stackHarness) anchorID(index int) string {
	if !harness.carriesMessageIDs {
		return ""
	}
	return "msg" + strconv.Itoa(index)
}

func (harness *stackHarness) push(kind StackKind, batchID string, items ...StackItemInput) (StackMutation, error) {
	for index := range items {
		if items[index].Kind == "" {
			items[index].Kind = kind
		}
	}
	return stackCommit(harness.journal, harness.key, kind, stackPushMessage(kind, batchID, items))
}

func (harness *stackHarness) setStatus(kind StackKind, itemID, status string) (StackMutation, error) {
	return stackCommit(harness.journal, harness.key, kind, stackSetStatusMessage(itemID, status))
}

func (harness *stackHarness) popTop(kind StackKind, itemID, status string) (StackMutation, error) {
	return stackCommit(harness.journal, harness.key, kind, stackPopTopMessage(itemID, status))
}

func (harness *stackHarness) closeBatch(kind StackKind, batchID, status string) (StackMutation, error) {
	return stackCommit(harness.journal, harness.key, kind, stackCloseBatchMessage(batchID, status))
}

func (harness *stackHarness) replace(kind StackKind, items []StackItemInput, closedStatus string) (StackMutation, error) {
	return stackCommit(harness.journal, harness.key, kind, stackReplaceMessage(kind, items, closedStatus))
}

func (harness *stackHarness) active(kind StackKind) []StackItemRecord {
	harness.t.Helper()
	rows, err := stackReadActive(harness.journal, harness.key, kind)
	if err != nil {
		harness.t.Fatal(err)
	}
	return rows
}

func (harness *stackHarness) history(kind StackKind) []StackItemRecord {
	harness.t.Helper()
	rows, err := stackReadHistory(harness.journal, harness.key, kind)
	if err != nil {
		harness.t.Fatal(err)
	}
	return rows
}

// readEventKinds 返回该会话已记录的结构性 EVENT kind 列表。
func (harness *stackHarness) readEventKinds() []string {
	harness.t.Helper()
	if harness.store != nil {
		events, err := harness.store.readEvents(harness.key, 0, 0)
		if err != nil {
			harness.t.Fatal(err)
		}
		kinds := make([]string, 0, len(events))
		for _, event := range events {
			kinds = append(kinds, string(event.Kind))
		}
		return kinds
	}
	rows, err := harness.sqlDB.Query(
		`SELECT kind FROM `+structuralEventTable+` WHERE project_id=? AND session_id=? ORDER BY event_id`,
		harness.key.ProjectID, harness.key.SessionID)
	if err != nil {
		harness.t.Fatal(err)
	}
	defer rows.Close()
	var kinds []string
	for rows.Next() {
		var kind string
		if err := rows.Scan(&kind); err != nil {
			harness.t.Fatal(err)
		}
		kinds = append(kinds, kind)
	}
	if err := rows.Err(); err != nil {
		harness.t.Fatal(err)
	}
	return kinds
}

// stats 返回该后端的延迟归因。
func (harness *stackHarness) stats() stackJournalStats {
	harness.t.Helper()
	return harness.journal.stats()
}

func (harness *stackHarness) verify() error {
	return stackVerify(harness.journal, harness.key)
}

func (harness *stackHarness) mustVerify() {
	harness.t.Helper()
	if err := harness.verify(); err != nil {
		harness.t.Fatal(err)
	}
}

// head 返回指定 kind 的栈模块 head（JSON 读 metadata/stack_<kind>.json；
// SQL 读共享 head 行中的该 kind 水位）。
func (harness *stackHarness) head(kind StackKind) stackModuleHead {
	harness.t.Helper()
	if harness.store != nil {
		head, err := harness.store.stackJournal().(*jsonStackJournal).readStackHead(harness.key, kind)
		if err != nil {
			harness.t.Fatal(err)
		}
		return head
	}
	head, err := harness.journal.(*sqlStackJournal).readHead(context.Background(), harness.key)
	if err != nil {
		harness.t.Fatal(err)
	}
	return head
}

// ---------- 通道语义（后端矩阵） ----------

// TestStackChannelHeadCarriesWatermarkOnly 断言 head 只有水位/计数/开放批次
// （§2.0 规则 1：模块 json 不装条目内容）。
func TestStackChannelHeadCarriesWatermarkOnly(t *testing.T) {
	forEachStackBackend(t, func(t *testing.T, harness *stackHarness) {
		harness.seedMessages(1)
		if _, err := harness.push(StackKindTask, "batch-b",
			StackItemInput{ItemID: "t1"}, StackItemInput{ItemID: "t2"}); err != nil {
			t.Fatal(err)
		}
		head := harness.head(StackKindTask)
		water := head.Kinds[StackKindTask]
		if head.HeadSeq == 0 || water.ActiveCount != 2 || water.HeadSeq != 2 {
			t.Fatalf("head = %+v water = %+v", head, water)
		}
		harness.mustVerify()
	})
}

// TestStackChannelBatchStaysActiveUntilAllComplete 断言 §2.4 批次语义：批次内
// 未完成 → 整批留 active，不落归档。
func TestStackChannelBatchStaysActiveUntilAllComplete(t *testing.T) {
	forEachStackBackend(t, func(t *testing.T, harness *stackHarness) {
		harness.seedMessages(1)
		if _, err := harness.push(StackKindTask, "batch-c",
			StackItemInput{ItemID: "c1"}, StackItemInput{ItemID: "c2"}); err != nil {
			t.Fatal(err)
		}
		if _, err := harness.setStatus(StackKindTask, "c1", "completed"); err != nil {
			t.Fatal(err)
		}
		if active := harness.active(StackKindTask); len(active) != 2 {
			t.Fatalf("批次未完成必须整批留栈: %+v", active)
		}
		if history := harness.history(StackKindTask); len(history) != 0 {
			t.Fatalf("批次未完成不得归档: %+v", history)
		}
		harness.mustVerify()
	})
}

// TestStackChannelBatchPopsWhenAllComplete 断言整批完成 → 整批弹栈归档，并盖上
// batch_message_to（弹栈时的 message 坐标）。
func TestStackChannelBatchPopsWhenAllComplete(t *testing.T) {
	forEachStackBackend(t, func(t *testing.T, harness *stackHarness) {
		harness.seedMessages(2)
		if _, err := harness.push(StackKindTask, "batch-d",
			StackItemInput{ItemID: "d1"}, StackItemInput{ItemID: "d2"}); err != nil {
			t.Fatal(err)
		}
		if _, err := harness.setStatus(StackKindTask, "d1", "completed"); err != nil {
			t.Fatal(err)
		}
		if _, err := harness.setStatus(StackKindTask, "d2", "completed"); err != nil {
			t.Fatal(err)
		}
		if active := harness.active(StackKindTask); len(active) != 0 {
			t.Fatalf("整批必须一起弹栈: %+v", active)
		}
		archived := harness.history(StackKindTask)
		if len(archived) != 2 {
			t.Fatalf("归档 = %+v", archived)
		}
		wantTo := harness.anchorID(2)
		if archived[0].BatchMessageTo != wantTo || archived[0].BatchMessageToSeq != 2 {
			t.Fatalf("batch_message_to = %q/%d want %q/2", archived[0].BatchMessageTo, archived[0].BatchMessageToSeq, wantTo)
		}
		if archived[0].BatchMessageFrom != wantTo {
			t.Fatalf("batch_message_from = %q want %q", archived[0].BatchMessageFrom, wantTo)
		}
		water := harness.head(StackKindTask).Kinds[StackKindTask]
		if water.ActiveCount != 0 || water.HistoryCount != 2 {
			t.Fatalf("water = %+v", water)
		}
		harness.mustVerify()
	})
}

// TestStackChannelAnchorsFromMessageRows 断言 item_message_id = 条目进入 active
// 时的 message 坐标，且同批共享批次首条目坐标（§4 history 锚）。
func TestStackChannelAnchorsFromMessageRows(t *testing.T) {
	forEachStackBackend(t, func(t *testing.T, harness *stackHarness) {
		harness.seedMessages(1)
		if _, err := harness.push(StackKindPlan, "batch-e", StackItemInput{ItemID: "e1"}); err != nil {
			t.Fatal(err)
		}
		harness.seedMessages(2)
		if _, err := harness.push(StackKindPlan, "batch-e", StackItemInput{ItemID: "e2"}); err != nil {
			t.Fatal(err)
		}
		active := harness.active(StackKindPlan)
		if len(active) != 2 {
			t.Fatalf("active = %+v", active)
		}
		if active[0].ItemMessageID != harness.anchorID(1) || active[0].ItemMessageSeq != 1 {
			t.Fatalf("e1 anchor = %q/%d", active[0].ItemMessageID, active[0].ItemMessageSeq)
		}
		if active[1].ItemMessageID != harness.anchorID(2) || active[1].ItemMessageSeq != 2 {
			t.Fatalf("e2 anchor = %q/%d", active[1].ItemMessageID, active[1].ItemMessageSeq)
		}
		if active[0].BatchMessageFrom != harness.anchorID(1) || active[1].BatchMessageFrom != harness.anchorID(1) {
			t.Fatalf("batch_from = %q / %q want %q",
				active[0].BatchMessageFrom, active[1].BatchMessageFrom, harness.anchorID(1))
		}
	})
}

// TestStackChannelRecordsTransitionEvents 断言状态迁移由 EVENT（plan.*/task.*/
// goal.*）记录（§2.4 + 附录 A.1）。
func TestStackChannelRecordsTransitionEvents(t *testing.T) {
	forEachStackBackend(t, func(t *testing.T, harness *stackHarness) {
		harness.seedMessages(1)
		if _, err := harness.push(StackKindGoal, "", StackItemInput{ItemID: "g1", Status: "active"}); err != nil {
			t.Fatal(err)
		}
		if _, err := harness.popTop(StackKindGoal, "g1", "completed"); err != nil {
			t.Fatal(err)
		}
		kinds := harness.readEventKinds()
		if !slicesContains(kinds, "goal.active") || !slicesContains(kinds, "goal.completed") {
			t.Fatalf("event kinds = %+v", kinds)
		}
	})
}

// TestStackChannelGoalRejectsNonTopPop 断言 goal 是 LIFO：只有栈顶可收口。
func TestStackChannelGoalRejectsNonTopPop(t *testing.T) {
	forEachStackBackend(t, func(t *testing.T, harness *stackHarness) {
		harness.seedMessages(1)
		if _, err := harness.push(StackKindGoal, "outer", StackItemInput{ItemID: "go1"}); err != nil {
			t.Fatal(err)
		}
		if _, err := harness.push(StackKindGoal, "inner", StackItemInput{ItemID: "go2"}); err != nil {
			t.Fatal(err)
		}
		if _, err := harness.popTop(StackKindGoal, "go1", "completed"); err == nil {
			t.Fatal("弹出非栈顶必须失败")
		}
		if _, err := harness.popTop(StackKindGoal, "go2", "aborted"); err != nil {
			t.Fatal(err)
		}
		active := harness.active(StackKindGoal)
		if len(active) != 1 || active[0].ItemID != "go1" {
			t.Fatalf("active = %+v", active)
		}
	})
}

// TestStackChannelReplaceActiveDerivesTransitions 断言「保存栈投影」落到逐条迁移：
// 新增压栈、状态变化更新、投影中缺失的条目按末态归档。
func TestStackChannelReplaceActiveDerivesTransitions(t *testing.T) {
	forEachStackBackend(t, func(t *testing.T, harness *stackHarness) {
		harness.seedMessages(1)
		if _, err := harness.push(StackKindGoal, "b1",
			StackItemInput{ItemID: "h1", Status: "active"},
			StackItemInput{ItemID: "h2", Status: "active"}); err != nil {
			t.Fatal(err)
		}
		if _, err := harness.replace(StackKindGoal, []StackItemInput{
			{ItemID: "h1", Status: "paused"}, {ItemID: "h3", Status: "active"},
		}, "closed"); err != nil {
			t.Fatal(err)
		}
		active := harness.active(StackKindGoal)
		if len(active) != 2 || active[0].Status != "paused" || active[1].ItemID != "h3" {
			t.Fatalf("active = %+v", active)
		}
		archived := harness.history(StackKindGoal)
		if len(archived) != 1 || archived[0].ItemID != "h2" || archived[0].Status != "closed" {
			t.Fatalf("archived = %+v", archived)
		}
		harness.mustVerify()
	})
}

// TestStackChannelRejectsUnknownKindAndDuplicate 断言非法 kind 与重复入栈显式
// 失败（不留半成品条目）。
func TestStackChannelRejectsUnknownKindAndDuplicate(t *testing.T) {
	forEachStackBackend(t, func(t *testing.T, harness *stackHarness) {
		harness.seedMessages(1)
		if _, err := stackCommit(harness.journal, harness.key, StackKind("nope"),
			stackPushMessage(StackKind("nope"), "", []StackItemInput{{ItemID: "x1"}})); err == nil {
			t.Fatal("未知 kind 必须被拒绝")
		}
		if _, err := harness.push(StackKindPlan, "b", StackItemInput{ItemID: "p1"}); err != nil {
			t.Fatal(err)
		}
		if _, err := harness.push(StackKindPlan, "b2", StackItemInput{ItemID: "p1"}); err == nil {
			t.Fatal("重复入栈必须失败")
		}
		if active := harness.active(StackKindPlan); len(active) != 1 {
			t.Fatalf("被拒绝的压栈不得留下条目: %+v", active)
		}
	})
}

// TestStackChannelMissingHeadIsNoop 断言从未写过栈的会话读取为空且不报错。
func TestStackChannelMissingHeadIsNoop(t *testing.T) {
	forEachStackBackend(t, func(t *testing.T, harness *stackHarness) {
		if active := harness.active(StackKindTask); len(active) != 0 {
			t.Fatalf("active = %+v", active)
		}
		if history := harness.history(StackKindTask); len(history) != 0 {
			t.Fatalf("history = %+v", history)
		}
		harness.mustVerify()
		if _, err := harness.setStatus(StackKindTask, "missing", "closed"); err == nil {
			t.Fatal("未知条目的状态更新必须失败")
		}
	})
}

// TestStackChannelPersistsAcrossInstances 断言通道数据只属于存储，不依赖某个
// journal 实例（换后端实例 = 换进程重启）。
func TestStackChannelPersistsAcrossInstances(t *testing.T) {
	t.Run("json", func(t *testing.T) {
		root := t.TempDir()
		key := Key{ProjectID: "p-stack", SessionID: "s-reopen"}
		first := newStoreEngine(root, storageSettings{})
		commitRoundRows(t, first, key, []Event{messageRow(1, "msg1", "user", EventKindUserInput, "x")})
		if _, err := stackCommit(first.stackJournal(), key, StackKindPlan,
			stackPushMessage(StackKindPlan, "b", []StackItemInput{{ItemID: "again", Kind: StackKindPlan}})); err != nil {
			t.Fatal(err)
		}
		second := newStoreEngine(root, storageSettings{})
		rows, err := stackReadActive(second.stackJournal(), key, StackKindPlan)
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != 1 || rows[0].ItemID != "again" {
			t.Fatalf("reopened stack = %+v", rows)
		}
	})
	t.Run("sqlite", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "sessions.db")
		key := Key{ProjectID: "p-stack", SessionID: "s-reopen"}
		first, err := Open(context.Background(), Config{Backend: BackendSQLite, Path: path})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := stackCommit(first.stackJournal(), key, StackKindGoal,
			stackPushMessage(StackKindGoal, "b", []StackItemInput{{ItemID: "again", Kind: StackKindGoal}})); err != nil {
			t.Fatal(err)
		}
		if err := first.Close(); err != nil {
			t.Fatal(err)
		}
		second, err := Open(context.Background(), Config{Backend: BackendSQLite, Path: path})
		if err != nil {
			t.Fatal(err)
		}
		defer second.Close()
		rows, err := stackReadActive(second.stackJournal(), key, StackKindGoal)
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != 1 || rows[0].ItemID != "again" {
			t.Fatalf("reopened stack = %+v", rows)
		}
	})
}

// TestStackChannelForkByMessageAnchor 断言 fork 只带「锚 ≤ from」的条目
// （§8.1 + T-FK-02），且该判定与后端无关。
func TestStackChannelForkByMessageAnchor(t *testing.T) {
	forEachStackBackend(t, func(t *testing.T, harness *stackHarness) {
		child := Key{ProjectID: harness.key.ProjectID, SessionID: "child"}
		harness.seedMessages(2)
		if _, err := harness.push(StackKindPlan, "b1", StackItemInput{ItemID: "old"}); err != nil {
			t.Fatal(err)
		}
		harness.seedMessages(4)
		if _, err := harness.push(StackKindPlan, "b2", StackItemInput{ItemID: "future"}); err != nil {
			t.Fatal(err)
		}
		if _, err := harness.push(StackKindTask, "t1", StackItemInput{ItemID: "other-kind"}); err != nil {
			t.Fatal(err)
		}
		if err := stackFork(harness.journal, harness.key, child, 3); err != nil {
			t.Fatal(err)
		}
		rows, err := stackReadActive(harness.journal, child, StackKindPlan)
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != 1 || rows[0].ItemID != "old" {
			t.Fatalf("子会话 plan 栈 = %+v want 只含锚 ≤3 的条目", rows)
		}
		tasks, err := stackReadActive(harness.journal, child, StackKindTask)
		if err != nil {
			t.Fatal(err)
		}
		if len(tasks) != 0 {
			t.Fatalf("子会话 task 栈 = %+v want 空（压栈锚 4 > 切断点 3 的条目必须排除）", tasks)
		}
		if parent := harness.active(StackKindPlan); len(parent) != 2 {
			t.Fatalf("fork 不得改动父会话栈: %+v", parent)
		}
	})
}

// TestStackChannelConcurrentKindsSingleWriter 断言多 kind 并发提交不丢更新：
// head_seq 单调、逐 kind 水位与数据一致（§2.0 规则 2 + actor 单写者）。
func TestStackChannelConcurrentKindsSingleWriter(t *testing.T) {
	forEachStackBackend(t, func(t *testing.T, harness *stackHarness) {
		harness.seedMessages(1)
		kinds := []StackKind{StackKindPlan, StackKindTask, StackKindGoal}
		var wg sync.WaitGroup
		errs := make(chan error, len(kinds))
		for _, kind := range kinds {
			wg.Add(1)
			go func(kind StackKind) {
				defer wg.Done()
				for step := 0; step < 10; step++ {
					itemID := string(kind) + "-" + strconv.Itoa(step)
					if _, err := harness.push(kind, "b-"+itemID, StackItemInput{ItemID: itemID}); err != nil {
						errs <- err
						return
					}
					if _, err := harness.popTop(kind, itemID, "completed"); err != nil {
						errs <- err
						return
					}
				}
			}(kind)
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			t.Fatal(err)
		}
		for _, kind := range kinds {
			if active := harness.active(kind); len(active) != 0 {
				t.Fatalf("%s active = %+v want empty", kind, active)
			}
			if history := harness.history(kind); len(history) != 10 {
				t.Fatalf("%s history len = %d want 10", kind, len(history))
			}
			if water := harness.head(kind).Kinds[kind]; water.HistoryCount != 10 || water.ActiveCount != 0 {
				t.Fatalf("%s water = %+v", kind, water)
			}
		}
		harness.mustVerify()
	})
}

// stackStatsDelta 将两个后端归因快照相减（累加器在 store 生命周期内单调）。
func stackStatsDelta(before, after stackJournalStats) stackJournalStats {
	return stackJournalStats{
		Backend:       after.Backend,
		Commits:       after.Commits - before.Commits,
		LockWait:      after.LockWait - before.LockWait,
		ActiveIO:      after.ActiveIO - before.ActiveIO,
		HistoryAppend: after.HistoryAppend - before.HistoryAppend,
		HistoryRead:   after.HistoryRead - before.HistoryRead,
		HeadIO:        after.HeadIO - before.HeadIO,
		GuideIO:       after.GuideIO - before.GuideIO,
		EventIO:       after.EventIO - before.EventIO,
		ColdLoads:     after.ColdLoads - before.ColdLoads,
	}
}

// TestStackChannelLockAttribution 回答两件事：
//
//	(a) 延迟落在 active.jsonl 还是 history.jsonl → 逐域计量并打印；
//	(b) 是数据竞争还是锁竞争 → 同一负载在 -race 下跑：无竞争报告 = 纯锁等待，
//	    读者最坏延迟即锁外可读性的上界（读者走 actor 发布的内存投影，既不取
//	    锁也不开文件句柄）。
//
// 结构性断言不依赖机器抖动：预热后不得再冷读磁盘；写路径不得解析归档文件。
func TestStackChannelLockAttribution(t *testing.T) {
	harness := newJSONStackHarness(t)
	harness.seedMessages(1)
	kinds := []StackKind{StackKindPlan, StackKindTask, StackKindGoal}
	for _, kind := range kinds {
		warmID := "warm-" + string(kind)
		if _, err := harness.push(kind, warmID, StackItemInput{ItemID: warmID}); err != nil {
			t.Fatal(err)
		}
		if _, err := harness.popTop(kind, warmID, "completed"); err != nil {
			t.Fatal(err)
		}
	}
	before := harness.stats()

	const rounds = 25
	readerStop := make(chan struct{})
	type readSample struct {
		worst   time.Duration
		total   time.Duration
		samples uint64
	}
	samples := make([]readSample, 2)
	var readers sync.WaitGroup
	for index := range samples {
		readers.Add(1)
		go func(index int) {
			defer readers.Done()
			worstSince := time.Duration(0)
			var total time.Duration
			var count uint64
			for {
				select {
				case <-readerStop:
					samples[index] = readSample{worst: worstSince, total: total, samples: count}
					return
				default:
				}
				for _, kind := range kinds {
					begin := time.Now()
					if _, err := stackReadActive(harness.journal, harness.key, kind); err != nil {
						t.Errorf("reader: %v", err)
						return
					}
					spent := time.Since(begin)
					total += spent
					count++
					if spent > worstSince {
						worstSince = spent
					}
				}
				// 只测读路径自身耗时，不把 CPU 抢满（否则同机并跑的用例会失真）。
				time.Sleep(100 * time.Microsecond)
			}
		}(index)
	}
	var writers sync.WaitGroup
	for _, kind := range kinds {
		writers.Add(1)
		go func(kind StackKind) {
			defer writers.Done()
			for step := 0; step < rounds; step++ {
				itemID := string(kind) + "-c-" + strconv.Itoa(step)
				if _, err := harness.push(kind, "c-"+itemID, StackItemInput{ItemID: itemID}); err != nil {
					t.Errorf("push %s: %v", itemID, err)
					return
				}
				if _, err := harness.popTop(kind, itemID, "completed"); err != nil {
					t.Errorf("pop %s: %v", itemID, err)
					return
				}
			}
		}(kind)
	}
	writers.Wait()
	close(readerStop)
	readers.Wait()

	delta := stackStatsDelta(before, harness.stats())
	var worst, mean, count uint64
	var total time.Duration
	for _, sample := range samples {
		if uint64(sample.worst) > worst {
			worst = uint64(sample.worst)
		}
		total += sample.total
		count += sample.samples
	}
	if count > 0 {
		mean = uint64(total / time.Duration(count))
	}
	t.Logf("栈通道延迟归因（%d 次提交，与并发读者同轮）：锁等待=%v active=%v history_append=%v history_read=%v head=%v guide=%v event=%v；冷读=%d 次；读者 %d 次读，最坏=%v、均值=%v",
		delta.Commits, delta.LockWait, delta.ActiveIO, delta.HistoryAppend, delta.HistoryRead,
		delta.HeadIO, delta.GuideIO, delta.EventIO, delta.ColdLoads, count, time.Duration(worst), time.Duration(mean))

	if delta.ColdLoads != 0 {
		t.Fatalf("预热后仍冷读磁盘 %d 次：读/写路径又回到整文件载入", delta.ColdLoads)
	}
	if delta.HistoryRead != 0 {
		t.Fatalf("写路径解析了 history.jsonl（%v）：归档文件必须只 append", delta.HistoryRead)
	}
	if delta.HistoryAppend == 0 {
		t.Fatal("归档 append 未被计量：本轮负载没有实际发生归档")
	}
	if want := uint64(2 * rounds * len(kinds)); delta.Commits != want {
		t.Fatalf("commits = %d want %d", delta.Commits, want)
	}
	// 读者均值延迟必须停留在微秒级：读路径只做一次 atomic load + 小切片复制，
	// 均值涨到毫秒级说明读又回到锁/磁盘上。最坏值只给兜底上限（调度抖动）。
	if time.Duration(mean) > 500*time.Microsecond {
		t.Fatalf("读者单次读均值 %v 超过 500µs：读侧未被移出写路径", time.Duration(mean))
	}
	// 最坏值只记录不断言：整仓并跑时调度抖动可到数百毫秒，与读路径无关。
	if worst > 0 {
		t.Logf("读者最坏单次读 %v（调度抖动量级，不参与判定）", time.Duration(worst))
	}
	for _, kind := range kinds {
		if history := harness.history(kind); len(history) != rounds+1 {
			t.Fatalf("%s history len = %d want %d（含预热那一轮）", kind, len(history), rounds+1)
		}
		if active := harness.active(kind); len(active) != 0 {
			t.Fatalf("%s active = %+v want empty", kind, active)
		}
	}
	harness.mustVerify()
}

// ---------- JSON 后端：物理放置与崩溃语义 ----------

// TestStackChannelWritesDesignedFiles 断言 §3.2 实体→文件：栈条目落在
// session/{kind}/active.jsonl，而不是任何 blob 字段；head 只装水位。
func TestStackChannelWritesDesignedFiles(t *testing.T) {
	harness := newJSONStackHarness(t)
	store, key := harness.store, harness.key
	harness.seedMessages(1)
	if _, err := harness.push(StackKindPlan, "batch-a",
		StackItemInput{ItemID: "plan-1", Payload: json.RawMessage(`{"plan_id":"p1"}`)},
		StackItemInput{ItemID: "task-1"}); err != nil {
		t.Fatal(err)
	}
	assertStackFilesExist(t, store, key, StackKindPlan, true, false)
	data, err := os.ReadFile(store.stackActivePath(key, StackKindPlan))
	if err != nil {
		t.Fatal(err)
	}
	if lines := strings.Count(string(data), "\n"); lines != 2 {
		t.Fatalf("active.jsonl lines = %d want 2\n%s", lines, data)
	}
	// S16：每 kind 独立 head 文件（metadata/stack_plan.json 等），不再有共享
	// metadata/stack.json。
	if _, err := os.Stat(store.modulePath(key, moduleStackPlan)); err != nil {
		t.Fatalf("stack_plan head missing after push: %v", err)
	}
	if _, err := os.Stat(filepath.Join(store.metadataDir(key), "stack.json")); err == nil {
		t.Fatal("legacy 共享 metadata/stack.json 仍存在（S16 应按 kind 拆分）")
	}
	headData, err := os.ReadFile(store.modulePath(key, moduleStackPlan))
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{`"item_id"`, `"batch_id"`, `"stack_id"`, `"status"`} {
		if strings.Contains(string(headData), forbidden) {
			t.Fatalf("栈 head 只能装水位，发现 %s in %s", forbidden, headData)
		}
	}
}

// TestStackChannelUnpublishedProjectionInvisible 断言 head 是发布点：revision
// 超前于 head 的投影行不可见（崩溃在数据文件之后、head 之前）。
func TestStackChannelUnpublishedProjectionInvisible(t *testing.T) {
	harness := newJSONStackHarness(t)
	store, key := harness.store, harness.key
	harness.seedMessages(1)
	if _, err := harness.push(StackKindTask, "batch-f", StackItemInput{ItemID: "f1"}); err != nil {
		t.Fatal(err)
	}
	path := store.stackActivePath(key, StackKindTask)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	ghost := StackItemRecord{ItemID: "f2", BatchID: "batch-f", Kind: StackKindTask, Seq: 99,
		Status: "active", Revision: 9999, ItemMessageID: "msg1", ItemMessageSeq: 1}
	encoded, _ := json.Marshal(ghost)
	if err := os.WriteFile(path, append(data, encoded...), 0o600); err != nil {
		t.Fatal(err)
	}
	store.dropStackViews(key)
	for _, row := range harness.active(StackKindTask) {
		if row.ItemID == "f2" {
			t.Fatal("未发布的栈投影行变得可见")
		}
	}
	harness.mustVerify()
}

// TestStackChannelCrashTailIgnored 断言 JSONL 崩溃残尾被忽略且下一次提交从
// 完整行边界续写。
func TestStackChannelCrashTailIgnored(t *testing.T) {
	harness := newJSONStackHarness(t)
	store, key := harness.store, harness.key
	harness.seedMessages(1)
	if _, err := harness.push(StackKindTask, "batch-g", StackItemInput{ItemID: "g1"}); err != nil {
		t.Fatal(err)
	}
	path := store.stackHistoryPath(key, StackKindTask)
	if err := os.WriteFile(path, []byte(`{"item_id":"half","seq":`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := harness.push(StackKindTask, "batch-g2", StackItemInput{ItemID: "g2"}); err != nil {
		t.Fatal(err)
	}
	if _, err := harness.closeBatch(StackKindTask, "batch-g2", "completed"); err != nil {
		t.Fatal(err)
	}
	archived := harness.history(StackKindTask)
	if len(archived) != 1 || archived[0].ItemID != "g2" {
		t.Fatalf("归档 = %+v", archived)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "half") {
		t.Fatalf("崩溃残尾存活: %s", data)
	}
	harness.mustVerify()
}

// TestStackChannelUnpublishedHistoryReaped 断言 head 未发布的归档行在下次提交
// 时被截回（不会因 head 追上同一 revision 而复活成重复条目）。
func TestStackChannelUnpublishedHistoryReaped(t *testing.T) {
	harness := newJSONStackHarness(t)
	store, key := harness.store, harness.key
	harness.seedMessages(1)
	if _, err := harness.push(StackKindGoal, "r1", StackItemInput{ItemID: "r1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := harness.popTop(StackKindGoal, "r1", "completed"); err != nil {
		t.Fatal(err)
	}
	published, err := os.ReadFile(store.stackHistoryPath(key, StackKindGoal))
	if err != nil {
		t.Fatal(err)
	}
	orphan := StackItemRecord{ItemID: "orphan", BatchID: "r2", Kind: StackKindGoal, Seq: 500,
		Status: "closed", Revision: harness.head(StackKindGoal).HeadSeq + 1, StackID: "goal|history"}
	encoded, _ := json.Marshal(orphan)
	if err := os.WriteFile(store.stackHistoryPath(key, StackKindGoal),
		append(published, encoded...), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := harness.push(StackKindGoal, "r3", StackItemInput{ItemID: "r3"}); err != nil {
		t.Fatal(err)
	}
	if _, err := harness.popTop(StackKindGoal, "r3", "completed"); err != nil {
		t.Fatal(err)
	}
	for _, row := range harness.history(StackKindGoal) {
		if row.ItemID == "orphan" {
			t.Fatal("head 未发布的归档行复活")
		}
	}
	harness.mustVerify()
}

func assertStackFilesExist(t *testing.T, store *storeEngine, key Key, kind StackKind, wantActive, wantHistory bool) {
	t.Helper()
	if _, err := os.Stat(store.stackActivePath(key, kind)); (err == nil) != wantActive {
		t.Fatalf("active.jsonl presence for %s = %v want %v (err=%v)", kind, err == nil, wantActive, err)
	}
	if _, err := os.Stat(store.stackHistoryPath(key, kind)); (err == nil) != wantHistory {
		t.Fatalf("history.jsonl presence for %s = %v want %v (err=%v)", kind, err == nil, wantHistory, err)
	}
}

func slicesContains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
