package task_context

import (
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/backend/tool/task"
	"github.com/RedHuang-0622/seelex/contract/dto"
)

// fakeLedgerHost 是台账域宿主面的最小替身：只实现 LedgerHost 的读写格，
// 并把"宿主被怎么调用"记下来（装配点注入的判据：批次只在当前会话下发、
// 会话切换整表替换）。真实宿主（*task.TaskRegistry）的一致性由
// backend/engine/assemble 的端到端用例（task_partition_test.go 等）钉住。
type fakeLedgerHost struct {
	live        []dto.TaskRecord
	todo        []dto.TaskRecord
	defaultSeen []string
	replaced    [][]dto.TaskRecord
	changed     chan dto.TaskRecord
}

var _ LedgerHost = (*fakeLedgerHost)(nil)

func newFakeLedgerHost() *fakeLedgerHost {
	return &fakeLedgerHost{changed: make(chan dto.TaskRecord, 1)}
}

func (h *fakeLedgerHost) Snapshot() []dto.TaskRecord {
	return append([]dto.TaskRecord(nil), h.live...)
}

func (h *fakeLedgerHost) Add(spec dto.TaskSpec) (dto.TaskRecord, bool, error) {
	if spec.Key != "" {
		for _, record := range h.live {
			if record.Key == spec.Key {
				return record, false, nil
			}
		}
	}
	id := spec.ID
	if id == "" {
		id = task.NextAutoID("task")
	} else {
		task.ObserveAutoID(id)
	}
	record := dto.TaskRecord{
		ID: id, Key: spec.Key, Phase: spec.Phase, Task: spec.Task,
		Status: dto.TaskPending, Kind: spec.Kind,
	}
	h.live = append(h.live, record)
	return record, true, nil
}

func (h *fakeLedgerHost) ResolveByKey(key string) (dto.TaskRecord, bool, error) {
	for _, record := range h.live {
		if record.Key == key {
			return record, true, nil
		}
	}
	return dto.TaskRecord{}, false, nil
}

func (h *fakeLedgerHost) SetStatus(id string, status dto.TaskStatus, _ string) (dto.TaskRecord, error) {
	for index := range h.live {
		if h.live[index].ID != id {
			continue
		}
		h.live[index].Status = status
		return h.live[index], nil
	}
	return dto.TaskRecord{}, errTaskRegistryUnavailable
}

func (h *fakeLedgerHost) AttachParticipant(id, participant string) (dto.TaskRecord, error) {
	for index := range h.live {
		if h.live[index].ID != id {
			continue
		}
		h.live[index].Participants = append(h.live[index].Participants, participant)
		return h.live[index], nil
	}
	return dto.TaskRecord{}, errTaskRegistryUnavailable
}

func (h *fakeLedgerHost) AppendTrace(id string, point dto.TaskTracePoint) (dto.TaskRecord, error) {
	for index := range h.live {
		if h.live[index].ID != id {
			continue
		}
		h.live[index].Trace = append(h.live[index].Trace, point)
		return h.live[index], nil
	}
	return dto.TaskRecord{}, errTaskRegistryUnavailable
}

func (h *fakeLedgerHost) ReplaceAll(records []dto.TaskRecord) error {
	h.live = append([]dto.TaskRecord(nil), records...)
	h.replaced = append(h.replaced, records)
	return nil
}

func (h *fakeLedgerHost) SetDefaultBatch(batchID string) error {
	h.defaultSeen = append(h.defaultSeen, batchID)
	return nil
}

func (h *fakeLedgerHost) TaskChanged() <-chan dto.TaskRecord {
	return h.changed
}

func (h *fakeLedgerHost) TodoSnapshot() []dto.TaskRecord {
	return append([]dto.TaskRecord(nil), h.todo...)
}

func (h *fakeLedgerHost) ReplaceTodo(items []dto.TodoItem) error {
	h.todo = h.todo[:0]
	for _, item := range items {
		h.todo = append(h.todo, dto.TaskRecord{
			ID: task.NextAutoID("todo"), Key: "todo:" + item.Text,
			Phase: dto.TaskPhaseTasklist, Task: item.Text,
			Status: task.TodoToTaskStatus(item.Status), Kind: "todo",
		})
	}
	return nil
}

func (h *fakeLedgerHost) AppendTodo(item dto.TodoItem, limit int) error {
	if limit > 0 && len(h.todo) >= limit {
		return errTodolistUnavailable
	}
	return h.ReplaceTodo(append(todoItemsToSlice(h.todo), item))
}

func (h *fakeLedgerHost) SetTodoStatusByIndex(index int, status dto.TaskStatus) (dto.TaskRecord, error) {
	if index < 0 || index >= len(h.todo) {
		return dto.TaskRecord{}, errTodolistUnavailable
	}
	h.todo[index].Status = status
	return h.todo[index], nil
}

func todoItemsToSlice(records []dto.TaskRecord) []dto.TodoItem {
	items := make([]dto.TodoItem, 0, len(records))
	for _, record := range records {
		items = append(items, task.TaskToTodoItem(record))
	}
	return items
}

func TestLedgerIdentityPrefersKeyOverRowID(t *testing.T) {
	cases := []struct {
		record dto.TaskRecord
		want   string
	}{
		{dto.TaskRecord{ID: "task:7"}, "id:task:7"},
		{dto.TaskRecord{ID: "task:7", Key: "plan:n1"}, "key:plan:n1"},
		{dto.TaskRecord{Key: "todo:写文档"}, "key:todo:写文档"},
	}
	for _, one := range cases {
		if got := LedgerIdentity(one.record); got != one.want {
			t.Fatalf("LedgerIdentity(%+v) = %q, want %q", one.record, got, one.want)
		}
	}
}

// 全局读面是跨会话台账：实时注册表（归当前会话）+ 各会话分区（归分区键）
// 合并，按行 ID 稳定排序；会话级读面刻意保持会话粒度。
func TestLedgerSnapshotMergesRegistryAndPartitions(t *testing.T) {
	host := newFakeLedgerHost()
	ledger := NewLedger(host)
	ledger.SwitchSession("session-b", nil)

	if _, _, err := ledger.Add(dto.TaskSpec{ID: "task:1", Task: "当前会话的任务", Kind: "task"}); err != nil {
		t.Fatal(err)
	}
	if _, created, err := ledger.AddFor("session-a", dto.TaskSpec{ID: "task:2", Task: "后台会话的任务", Kind: "task"}); err != nil || !created {
		t.Fatalf("AddFor(A) created=%v err=%v", created, err)
	}

	table := ledger.Snapshot()
	if len(table) != 2 {
		t.Fatalf("台账 = %+v, want 两行（实时 + 分区）", table)
	}
	if table[0].ID != "task:1" || table[1].ID != "task:2" {
		t.Fatalf("台账未按行 ID 排序：%+v", table)
	}
	if table[0].SessionID != "session-b" {
		t.Fatalf("实时注册表行归属会话 = %q, want session-b", table[0].SessionID)
	}
	if table[1].SessionID != "session-a" {
		t.Fatalf("分区行归属会话 = %q, want session-a", table[1].SessionID)
	}

	// 会话级读面：当前/空 = 实时注册表；后台 = 分区，且刻意不标 SessionID。
	if got := ledger.SnapshotFor(""); len(got) != 1 || got[0].ID != "task:1" {
		t.Fatalf("SnapshotFor(\"\") = %+v, want 实时注册表一行", got)
	}
	partition := ledger.SnapshotFor("session-a")
	if len(partition) != 1 || partition[0].SessionID != "" {
		t.Fatalf("SnapshotFor(A) = %+v, want 分区一行且不带归属会话", partition)
	}
	// 分区返回拷贝：改它不影响台账。
	partition[0].Task = "被改坏"
	if again := ledger.SnapshotFor("session-a"); again[0].Task == "被改坏" {
		t.Fatalf("SnapshotFor 返回的切片是分区本体（调用方可改坏台账）")
	}
}

// 跨会话去重身份：实时注册表优先；同一幂等键在两处出现只留一行。
func TestLedgerSnapshotDeduplicatesByKeyAcrossSources(t *testing.T) {
	host := newFakeLedgerHost()
	ledger := NewLedger(host)
	ledger.SwitchSession("live", nil)
	if _, _, err := ledger.Add(dto.TaskSpec{ID: "task:9", Key: "goal:same", Task: "实时那行", Kind: "task"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ledger.AddFor("other", dto.TaskSpec{ID: "task:10", Key: "goal:same", Task: "分区那行", Kind: "task"}); err != nil {
		t.Fatal(err)
	}
	table := ledger.Snapshot()
	if len(table) != 1 || table[0].Task != "实时那行" {
		t.Fatalf("台账 = %+v, want 只留实时注册表那行", table)
	}
}

// 写面按归属会话路由：当前/空会话写实时注册表，后台会话写自身 scope 分区
// （写自有域）；分区幂等按 Key，自动 ID 取进程级分配器（跨分区不同号）。
func TestLedgerAddForRoutesToOwningSession(t *testing.T) {
	host := newFakeLedgerHost()
	ledger := NewLedger(host)
	ledger.SwitchSession("live", nil)

	if _, _, err := ledger.AddFor("live", dto.TaskSpec{Key: "k-live", Task: "当前", Kind: "task"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ledger.AddFor("", dto.TaskSpec{Key: "k-blank", Task: "空会话", Kind: "task"}); err != nil {
		t.Fatal(err)
	}
	if len(host.live) != 2 {
		t.Fatalf("实时注册表 = %+v, want 当前会话 + 空会话两条", host.live)
	}

	first, created, err := ledger.AddFor("bg-a", dto.TaskSpec{Key: "k-bg", Task: "后台", Kind: "task"})
	if err != nil || !created {
		t.Fatalf("AddFor(bg-a) created=%v err=%v", created, err)
	}
	if _, created, err := ledger.AddFor("bg-a", dto.TaskSpec{Key: "k-bg", Task: "后台重复", Kind: "task"}); err != nil || created {
		t.Fatalf("分区幂等失败 created=%v err=%v", created, err)
	}
	second, _, err := ledger.AddFor("bg-b", dto.TaskSpec{Task: "另一分区无键条目", Kind: "task"})
	if err != nil {
		t.Fatal(err)
	}
	if first.ID == second.ID {
		t.Fatalf("两个分区的自动 ID 同号：%q", first.ID)
	}
	if len(host.live) != 2 {
		t.Fatalf("后台写污染了实时注册表：%+v", host.live)
	}
}

// 状态写面：当前会话走注册表，后台会话改分区本体并记重试数；查不到给显式错误。
func TestLedgerSetStatusForRoutesAndCountsRetries(t *testing.T) {
	host := newFakeLedgerHost()
	ledger := NewLedger(host)
	ledger.SwitchSession("live", nil)

	live, _, err := ledger.Add(dto.TaskSpec{ID: "task:1", Task: "当前", Kind: "task"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ledger.SetStatusFor("live", live.ID, dto.TaskRunning, "spawn"); err != nil {
		t.Fatal(err)
	}
	if host.live[0].Status != dto.TaskRunning {
		t.Fatalf("当前会话状态未落到注册表：%+v", host.live[0])
	}

	background, _, err := ledger.AddFor("bg", dto.TaskSpec{ID: "task:2", Task: "后台", Kind: "task"})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := ledger.SetStatusFor("bg", background.ID, dto.TaskRetry, "retry")
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != dto.TaskRetry || updated.RetryCount != 1 {
		t.Fatalf("分区重试打点 = %+v, want retry + RetryCount 1", updated)
	}
	if _, err := ledger.SetStatusFor("bg", "task:missing", dto.TaskCompleted, ""); err == nil ||
		!strings.Contains(err.Error(), "not found in session") {
		t.Fatalf("查不到的行应给显式错误，got %v", err)
	}
}

// 会话切换：旧会话实时注册表落进自己的分区，目标会话分区被装载并清理。
func TestLedgerSwitchSessionMovesRegistryIntoPartition(t *testing.T) {
	host := newFakeLedgerHost()
	ledger := NewLedger(host)
	ledger.SwitchSession("session-a", nil)
	if _, _, err := ledger.Add(dto.TaskSpec{ID: "task:1", Task: "A 的任务", Kind: "task"}); err != nil {
		t.Fatal(err)
	}

	loaded := []dto.TaskRecord{{ID: "task:2", Task: "B 的任务", Status: dto.TaskPending, Kind: "task"}}
	replacedBefore := len(host.replaced)
	ledger.SwitchSession("session-b", loaded)

	if len(host.live) != 1 || host.live[0].ID != "task:2" {
		t.Fatalf("切换后实时注册表 = %+v, want B 的记录", host.live)
	}
	if len(host.replaced) != replacedBefore+1 {
		t.Fatalf("切换应整表替换一次，got %d 次", len(host.replaced)-replacedBefore)
	}
	if a := ledger.SnapshotFor("session-a"); len(a) != 1 || a[0].ID != "task:1" {
		t.Fatalf("旧会话分区 = %+v, want A 的快照", a)
	}
	// 目标会话已装载进实时注册表，其分区必须过期（否则读到陈旧分片）。
	if b := ledger.SnapshotFor("session-b"); len(b) != 1 || b[0].ID != "task:2" {
		t.Fatalf("目标会话读面 = %+v, want 实时注册表", b)
	}
	if table := ledger.Snapshot(); len(table) != 2 {
		t.Fatalf("切换后台账 = %+v, want A + B 两行（切走不丢行）", table)
	}
}

// 会话级批次：只记会话归属；只有当前会话才下发注册表默认批次（L3 写隔离）。
func TestLedgerBatchIsSessionScoped(t *testing.T) {
	host := newFakeLedgerHost()
	ledger := NewLedger(host)
	ledger.SwitchSession("live", nil)

	ledger.SetBatch("bg", "batch-bg")
	if got := ledger.BatchFor("bg"); got != "batch-bg" {
		t.Fatalf("BatchFor(bg) = %q, want batch-bg", got)
	}
	if len(host.defaultSeen) != 0 {
		t.Fatalf("后台会话批次不得下发注册表：%+v", host.defaultSeen)
	}
	ledger.SetBatch("live", "batch-live")
	if len(host.defaultSeen) != 1 || host.defaultSeen[0] != "batch-live" {
		t.Fatalf("当前会话批次应下发一次：%+v", host.defaultSeen)
	}
	if got := ledger.BatchFor("missing"); got != "" {
		t.Fatalf("未启动会话批次 = %q, want 空", got)
	}
}

// 清单面：分区清单按行顺序读回；整体替换保留任务行；上限与三态判据各有显式错误。
func TestLedgerTodoPartitionOrderLimitAndThreeStates(t *testing.T) {
	host := newFakeLedgerHost()
	ledger := NewLedger(host)
	ledger.SwitchSession("live", nil)
	if _, _, err := ledger.AddFor("bg", dto.TaskSpec{ID: "task:1", Task: "分区里的任务行", Kind: "task"}); err != nil {
		t.Fatal(err)
	}

	items := []dto.TodoItem{
		{Text: "第一步", Status: dto.TodoItemDone},
		{Text: "第二步", Status: dto.TodoItemDoing},
	}
	if err := ledger.ReplaceTodoFor("bg", items); err != nil {
		t.Fatal(err)
	}
	got := ledger.TodoSnapshotFor("bg")
	if len(got) != 2 || got[0].Text != "第一步" || got[1].Text != "第二步" {
		t.Fatalf("分区清单顺序 = %+v", got)
	}
	if got[0].Status != dto.TodoItemDone {
		t.Fatalf("清单三态映射丢了：%+v", got[0])
	}
	// 整体替换清单不动任务行（分区里任务行原样保留）。
	if records := ledger.SnapshotFor("bg"); len(records) != 3 {
		t.Fatalf("分区 = %+v, want 任务行 + 两个清单行", records)
	}

	if err := ledger.AppendTodoFor("bg", dto.TodoItem{Text: "第三步"}, 2); err == nil ||
		!strings.Contains(err.Error(), "already at limit") {
		t.Fatalf("超上限应给显式错误，got %v", err)
	}
	if err := ledger.AppendTodoFor("bg", dto.TodoItem{Text: "第三步"}, 3); err != nil {
		t.Fatal(err)
	}
	if _, err := ledger.SetTodoStatusFor("bg", 9, dto.TaskCompleted); err == nil ||
		!strings.Contains(err.Error(), "out of range") {
		t.Fatalf("越界序号应给显式错误，got %v", err)
	}
	if _, err := ledger.SetTodoStatusFor("bg", 2, dto.TaskFailed); err == nil ||
		!strings.Contains(err.Error(), "pending/doing/completed") {
		t.Fatalf("非三态应被拒绝，got %v", err)
	}
	done, err := ledger.SetTodoStatusFor("bg", 2, dto.TaskCompleted)
	if err != nil || done.Status != dto.TaskCompleted {
		t.Fatalf("三态写入失败：%+v err=%v", done, err)
	}

	// 当前会话走注册表（这里的替身只记 todo 面）。
	if err := ledger.ReplaceTodoFor("live", items); err != nil {
		t.Fatal(err)
	}
	if got := ledger.TodoSnapshotFor("live"); len(got) != 2 {
		t.Fatalf("当前会话清单 = %+v, want 走注册表面", got)
	}
	if err := ledger.SetTodoStatus(0, dto.TodoItemDoing); err != nil {
		t.Fatal(err)
	}
	if err := ledger.SetTodoStatus(0, dto.TodoItemStatus("bogus")); err == nil ||
		!strings.Contains(err.Error(), "invalid status") {
		t.Fatalf("非法三态词应被拒绝，got %v", err)
	}
}

// 未装配宿主（零值 Ledger / nil 句柄）语义与迁移前一致：读面 nil、写面显式错误，
// 且判据各只有一处定义。
func TestLedgerWithoutHostReportsUnavailable(t *testing.T) {
	var missing *Ledger
	if got := missing.Snapshot(); got != nil {
		t.Fatalf("未装配 Snapshot = %+v, want nil", got)
	}
	if got := missing.SnapshotFor("s"); got != nil {
		t.Fatalf("未装配 SnapshotFor = %+v, want nil", got)
	}
	if got := missing.Changed(); got != nil {
		t.Fatalf("未装配 Changed 应为 nil channel")
	}
	if got := missing.BatchFor("s"); got != "" {
		t.Fatalf("未装配 BatchFor = %q, want 空", got)
	}
	missing.SwitchSession("s", nil)
	missing.SetBatch("s", "b")

	if _, _, err := missing.Add(dto.TaskSpec{}); err == nil || err.Error() != "task: registry unavailable" {
		t.Fatalf("未装配 Add 错误 = %v", err)
	}
	if _, err := missing.SetStatus("id", dto.TaskPending, ""); err == nil ||
		err.Error() != "task: registry unavailable" {
		t.Fatalf("未装配 SetStatus 错误 = %v", err)
	}
	if err := missing.SetTodoStatus(0, dto.TodoItemPending); err == nil ||
		err.Error() != "todolist: unavailable" {
		t.Fatalf("未装配 SetTodoStatus 错误 = %v", err)
	}
	if err := missing.ReplaceTodoFor("bg", nil); err == nil ||
		err.Error() != "todolist: unavailable" {
		t.Fatalf("未装配 ReplaceTodoFor 错误 = %v", err)
	}

	// 显式注入 nil 宿主同样走这条判据（NewLedger(nil)）。
	empty := NewLedger(nil)
	if got := empty.Snapshot(); got != nil {
		t.Fatalf("nil 宿主 Snapshot = %+v, want nil", got)
	}
	if _, _, err := empty.ResolveByKey("k"); err == nil || err.Error() != "task: registry unavailable" {
		t.Fatalf("nil 宿主 ResolveByKey 错误 = %v", err)
	}
}

// Changed 是注册表变更通道的透传（CSP：变更即投递）。
func TestLedgerChangedPassesThroughHostChannel(t *testing.T) {
	host := newFakeLedgerHost()
	ledger := NewLedger(host)
	want := dto.TaskRecord{ID: "task:1", Task: "变更行"}
	host.changed <- want
	if got := <-ledger.Changed(); got.ID != want.ID {
		t.Fatalf("Changed 透传 = %+v, want %+v", got, want)
	}
}
