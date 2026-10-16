package task_context

import (
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/RedHuang-0622/seelex/backend/tool/task"
	"github.com/RedHuang-0622/seelex/contract/dto"
)

// ledger.go 拥有 task **台账域**：跨会话工作表格的合并读面（Snapshot）、
// 会话 scope 分区写面（AddFor/SetStatusFor/…）与会话级默认批次（SetBatch）。
//
// 为什么落在这一层：工作表格是一条**跨会话台账**——会话只是条目的产生地，
// 不是表格的作用域。切走/新建会话都不应让先前会话的条目从表里消失（用户看的
// 是"这个项目在办什么"，不是"这个会话在办什么"）；同时"写自有域"要求后台会话
// 的 task 写进它自己的 scope 分区，不能污染当前（视图）注册表。两条判据共用
// 同一份会话归属状态（分区 + 当前任务会话），所以同处一个类型。
//
// 宿主只提供"注册表怎么读写"（见 [LedgerHost]），台账自己持有分区与批次：
// 域不 import `backend/engine/assemble`，装配点（Runtime 构造器）把注册表注入
// 即可。零值不可用，用 [NewLedger] 构造；方法一律带 nil 防护。
type Ledger struct {
	host LedgerHost

	// partitions 是会话切换时保存的 task 注册表快照（阶段 0：后台会话持久化
	// 按会话取快照，不读活跃注册表；对应 R6/P2）。mu 同时保护 currentSessionID。
	mu               sync.Mutex
	partitions       map[string][]dto.TaskRecord
	currentSessionID string
	// batches 是会话级默认批次（阶段 1 L3：后台会话不覆盖活跃注册表默认批次）。
	batchesMu sync.Mutex
	batches   map[string]string
}

// 台账域的两条"没有可用宿主"判据（各只有一处定义；宿主未注入 = 语义与
// 迁移前的"注册表未装配"完全一致）。
var (
	errTaskRegistryUnavailable = errors.New("task: registry unavailable")
	errTodolistUnavailable     = errors.New("todolist: unavailable")
)

// NewLedger 构造台账域。宿主是提供实时注册表读写面的实现（生产装配点是
// `*backend/tool/task.TaskRegistry`）。
func NewLedger(host LedgerHost) *Ledger {
	return &Ledger{
		host:       host,
		partitions: make(map[string][]dto.TaskRecord),
		batches:    make(map[string]string),
	}
}

// Snapshot 返回**项目/全局** task 表只读快照（worktable 投影数据源）：
// 实时注册表（当前会话）与各会话 scope 分区合并，按跨会话身份去重（幂等键
// 优先，否则行 ID）、稳定排序。自动条目的行 ID（`task:<n>`/`todo:<n>`）由
// 进程级分配器给出，进程内唯一（见 task.NextAutoID）——同 ID 在台账里是同一
// 行，重号会丢行。
//
// 每条记录带**归属会话**（SessionID：实时注册表 = 当前会话，分区 = 分区键）：
// 台账默认全量展示，前端会话筛选轴（"仅本会话"）据此过滤——全局口径不牺牲
// "这行是谁在办的"的可判定性。
func (l *Ledger) Snapshot() []dto.TaskRecord {
	if l == nil || l.host == nil {
		return nil
	}
	return l.snapshotAll()
}

// snapshotAll 合并实时注册表与所有会话分区（跨会话身份去重：有幂等键
// 按 Key、否则按 ID；注册表记录优先）。每条记录标注**归属会话**：实时注册表
// 的记录归当前会话，分区记录归分区键——工作表格的会话筛选轴（默认全部、可
// 切「仅本会话」）按此判定，否则合并后无法回答「这行是谁在办的」。
func (l *Ledger) snapshotAll() []dto.TaskRecord {
	l.mu.Lock()
	defer l.mu.Unlock()
	live := l.host.Snapshot()
	records := make([]dto.TaskRecord, 0, len(live))
	seen := make(map[string]struct{}, len(live))
	for _, record := range live {
		record.SessionID = l.currentSessionID
		seen[ledgerIdentity(record)] = struct{}{}
		records = append(records, record)
	}
	for sessionID, partition := range l.partitions {
		for _, record := range partition {
			identity := ledgerIdentity(record)
			if _, exists := seen[identity]; exists {
				continue
			}
			seen[identity] = struct{}{}
			record.SessionID = sessionID
			records = append(records, record)
		}
	}
	sort.SliceStable(records, func(left, right int) bool { return records[left].ID < records[right].ID })
	return records
}

// ledgerIdentity 是跨会话台账的去重身份：幂等键优先（注册表按 Key 去重，
// plan/subagent/todo 与 task_add 都有稳定 Key），缺失时才回退行 ID。优先级
// 不能反过来——同一 plan 节点/子代理/待办文本会在会话切换、恢复、跨进程冷读
// 时以不同来源出现，Key 才是它们的同一性；而回退行 ID 的自动号现在由进程级
// 分配器保证唯一（见 task.NextAutoID），不会把两个会话的条目并成一行、也不
// 会重号丢行（外部分配/跨版本数据仍按 ObserveAutoID 抬高水位）。
func ledgerIdentity(record dto.TaskRecord) string {
	if record.Key != "" {
		return "key:" + record.Key
	}
	return "id:" + record.ID
}

// SnapshotFor 返回指定会话的 task 注册表快照（会话级读面：持久化落盘
// 与请求尾部打点块用）。后台会话（非当前 task 会话）返回切换时保存的分片
// 快照；当前会话返回注册表实时快照；空会话 ID 视为当前。
//
// 注意：工作表格本体取全局读面（Snapshot），不是这里——本方法刻意保持
// 会话粒度，避免把别的会话的活动任务注入本会话上下文；也刻意**不**标注
// SessionID（调用方本来就知道是哪个会话，且本方法服务于落盘——归属会话由
// 容器 `SessionRecord` 表达，不必写进每条记录）。
func (l *Ledger) SnapshotFor(sessionID string) []dto.TaskRecord {
	if l == nil || l.host == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if sessionID == "" || sessionID == l.currentSessionID {
		return l.host.Snapshot()
	}
	return append([]dto.TaskRecord(nil), l.partitions[sessionID]...)
}

// Add 主动登记 task（幂等：Key 命中返回既有记录，不重复建条目）。
func (l *Ledger) Add(spec dto.TaskSpec) (dto.TaskRecord, bool, error) {
	if l == nil || l.host == nil {
		return dto.TaskRecord{}, false, errTaskRegistryUnavailable
	}
	return l.host.Add(spec)
}

// AddFor 按归属会话登记 task：当前任务会话写实时注册表，后台会话写自身
// scope 分区（R6/P2 收口：写自有域，后台任务不再污染当前注册表）。
func (l *Ledger) AddFor(sessionID string, spec dto.TaskSpec) (dto.TaskRecord, bool, error) {
	if l == nil || l.host == nil {
		return dto.TaskRecord{}, false, errTaskRegistryUnavailable
	}
	l.mu.Lock()
	current := l.currentSessionID
	l.mu.Unlock()
	if sessionID == "" || sessionID == current {
		return l.host.Add(spec)
	}
	return l.addPartition(sessionID, spec)
}

// addPartition 把 task 写入指定会话的 scope 分区（幂等：Key 命中返回既有记录）。
//
// 自动 ID 取**进程级**分配器（见 task.NextAutoID）：分区里"每个会话各自从 1
// 数"会让两个会话的无显式 ID 条目同号——跨会话台账按行 ID 去重时丢行，前端
// keyed reconciliation 也只认一行。
func (l *Ledger) addPartition(sessionID string, spec dto.TaskSpec) (dto.TaskRecord, bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.partitions == nil {
		l.partitions = make(map[string][]dto.TaskRecord)
	}
	records := l.partitions[sessionID]
	if spec.Key != "" {
		for _, record := range records {
			if record.Key == spec.Key {
				return record, false, nil
			}
		}
	}
	id := spec.ID
	if id == "" {
		id = task.NextAutoID("task")
		for partitionOccupied(records, id) {
			id = task.NextAutoID("task")
		}
	} else {
		task.ObserveAutoID(id)
	}
	record := dto.TaskRecord{
		ID: id, Key: spec.Key, Phase: spec.Phase, Task: spec.Task, Description: spec.Description,
		Status: dto.TaskPending, Assignee: spec.Assignee, Kind: spec.Kind,
		Dependencies: append([]string(nil), spec.Dependencies...),
		Attachments:  append([]string(nil), spec.Attachments...),
	}
	l.partitions[sessionID] = append(records, record)
	return record, true, nil
}

// partitionOccupied 判断分区里是否已有同 ID 行（分区行键同样是 ID）。
func partitionOccupied(records []dto.TaskRecord, id string) bool {
	for _, record := range records {
		if record.ID == id {
			return true
		}
	}
	return false
}

// ResolveByKey 按幂等键查 task（B6 子代理装配：查重命中 → 绑定既有 id）。
func (l *Ledger) ResolveByKey(key string) (dto.TaskRecord, bool, error) {
	if l == nil || l.host == nil {
		return dto.TaskRecord{}, false, errTaskRegistryUnavailable
	}
	return l.host.ResolveByKey(key)
}

// ResolveByKeyFor 按归属会话查 task。
func (l *Ledger) ResolveByKeyFor(sessionID, key string) (dto.TaskRecord, bool, error) {
	if l == nil || l.host == nil {
		return dto.TaskRecord{}, false, errTaskRegistryUnavailable
	}
	l.mu.Lock()
	current := l.currentSessionID
	l.mu.Unlock()
	if sessionID == "" || sessionID == current {
		return l.host.ResolveByKey(key)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, record := range l.partitions[sessionID] {
		if record.Key == key {
			return record, true, nil
		}
	}
	return dto.TaskRecord{}, false, nil
}

// SetStatus 更新 task 状态（生命周期打点）。
func (l *Ledger) SetStatus(id string, status dto.TaskStatus, evidence string) (dto.TaskRecord, error) {
	if l == nil || l.host == nil {
		return dto.TaskRecord{}, errTaskRegistryUnavailable
	}
	return l.host.SetStatus(id, status, evidence)
}

// SetStatusFor 按归属会话更新 task 状态。
func (l *Ledger) SetStatusFor(sessionID, id string, status dto.TaskStatus, evidence string) (dto.TaskRecord, error) {
	if l == nil || l.host == nil {
		return dto.TaskRecord{}, errTaskRegistryUnavailable
	}
	l.mu.Lock()
	current := l.currentSessionID
	l.mu.Unlock()
	if sessionID == "" || sessionID == current {
		return l.host.SetStatus(id, status, evidence)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	records := l.partitions[sessionID]
	for index := range records {
		if records[index].ID != id {
			continue
		}
		record := records[index]
		record.Status = status
		if status == dto.TaskRetry {
			record.RetryCount++
		}
		records[index] = record
		l.partitions[sessionID] = records
		return record, nil
	}
	return dto.TaskRecord{}, fmt.Errorf("task %q not found in session %q scope", id, sessionID)
}

// AttachParticipant 记录参与节点（并行执行证明）。
func (l *Ledger) AttachParticipant(id, participant string) (dto.TaskRecord, error) {
	if l == nil || l.host == nil {
		return dto.TaskRecord{}, errTaskRegistryUnavailable
	}
	return l.host.AttachParticipant(id, participant)
}

// AppendTrace 追加任务生命周期 trace 点。
func (l *Ledger) AppendTrace(id string, point dto.TaskTracePoint) (dto.TaskRecord, error) {
	if l == nil || l.host == nil {
		return dto.TaskRecord{}, errTaskRegistryUnavailable
	}
	return l.host.AppendTrace(id, point)
}

// SwitchSession 会话切换：保存旧会话注册表快照到其 scope 分区，装载
// 目标会话分区记录到当前注册表。sessionID 为空 = 进入草稿（无会话归属）。
// 与旧语义的关键差异：后台会话的 AddFor 分区写不再被切换覆盖（换血
// 只作用于当前注册表本身，分区是写自有域的权威存储）。
//
// 注意：本方法只改变**当前会话**的实时注册表；工作表格走全局读面
// （Snapshot = 实时注册表 + 全部分区），切走/新建都不丢行。
func (l *Ledger) SwitchSession(sessionID string, records []dto.TaskRecord) {
	if l == nil || l.host == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	current := l.host.Snapshot()
	if l.currentSessionID != "" && l.currentSessionID != sessionID {
		l.partitions[l.currentSessionID] = current
	}
	l.currentSessionID = sessionID
	// 目标会话已装载进当前注册表后，其分区即过期（实时读当前注册表）；
	// 删除避免后续 SnapshotFor 读到陈旧分片。
	if sessionID != "" {
		delete(l.partitions, sessionID)
	}
	_ = l.host.ReplaceAll(records)
}

// SetBatch 设置会话级默认批次（application startChat 按会话调用；后台会话
// 不覆盖活跃注册表默认批次——L3 写隔离）。
func (l *Ledger) SetBatch(sessionID, batchID string) {
	if l == nil || l.host == nil {
		return
	}
	l.mu.Lock()
	current := l.currentSessionID
	l.mu.Unlock()
	l.batchesMu.Lock()
	if l.batches == nil {
		l.batches = make(map[string]string)
	}
	l.batches[sessionID] = batchID
	l.batchesMu.Unlock()
	if sessionID == current {
		_ = l.host.SetDefaultBatch(batchID)
	}
}

// BatchFor 返回指定会话的默认批次（Add 盖章辅助；空 = 未启动）。
func (l *Ledger) BatchFor(sessionID string) string {
	if l == nil {
		return ""
	}
	l.batchesMu.Lock()
	defer l.batchesMu.Unlock()
	return l.batches[sessionID]
}

// Changed 返回 task.changed 输出 channel（CSP：变更即投递）。
func (l *Ledger) Changed() <-chan dto.TaskRecord {
	if l == nil || l.host == nil {
		return nil
	}
	return l.host.TaskChanged()
}

// ── 清单面（todolist 工具族）──────────────────────────────────────
//
// 清单项在注册表里就是 kind=todo 的 task 行，因此会话级读面与 task 分区同构：
// 分区里按 Kind=="todo" 的**顺序**表达清单顺序。

// TodoSnapshot 返回当前清单只读拷贝（application 快照投影数据源；主代理
// 每次 todolist_* 工具完成经 runtime.changed 增量带到 GUI）。
func (l *Ledger) TodoSnapshot() []dto.TodoItem {
	if l == nil || l.host == nil {
		return nil
	}
	records := l.host.TodoSnapshot()
	items := make([]dto.TodoItem, 0, len(records))
	for _, record := range records {
		items = append(items, task.TaskToTodoItem(record))
	}
	return items
}

// SetTodoStatus 设置指定待办项的三态（GUI 工作表格人工状态更新入口；
// 只改状态，不新增工具族——todolist 仍是 harness 默认工具族）。
func (l *Ledger) SetTodoStatus(index int, status dto.TodoItemStatus) error {
	if l == nil || l.host == nil {
		return errTodolistUnavailable
	}
	if !task.ValidTodoStatus(status) {
		return fmt.Errorf("todolist: invalid status %q (want pending|doing|done)", status)
	}
	_, err := l.host.SetTodoStatusByIndex(index, task.TodoToTaskStatus(status))
	return err
}

// TodoSnapshotFor 按归属会话读清单（工具族按调用会话取数）。
func (l *Ledger) TodoSnapshotFor(sessionID string) []dto.TodoItem {
	if l == nil || l.host == nil {
		return nil
	}
	l.mu.Lock()
	current := l.currentSessionID
	records := append([]dto.TaskRecord(nil), l.partitions[sessionID]...)
	l.mu.Unlock()
	if sessionID == "" || sessionID == current {
		return l.TodoSnapshot()
	}
	return todoItemsFromRecords(records)
}

// ReplaceTodoFor 按归属会话整体替换清单（todo_init）。
func (l *Ledger) ReplaceTodoFor(sessionID string, items []dto.TodoItem) error {
	if l == nil || l.host == nil {
		return errTodolistUnavailable
	}
	l.mu.Lock()
	current := l.currentSessionID
	l.mu.Unlock()
	if sessionID == "" || sessionID == current {
		return l.host.ReplaceTodo(items)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	records := partitionWithoutTodo(l.partitions[sessionID])
	for _, item := range items {
		records = append(records, todoRecordFromItem(item))
	}
	l.partitions[sessionID] = records
	return nil
}

// AppendTodoFor 按归属会话追加清单项（todo_add；上限与注册表同一口径）。
func (l *Ledger) AppendTodoFor(sessionID string, item dto.TodoItem, limit int) error {
	if l == nil || l.host == nil {
		return errTodolistUnavailable
	}
	l.mu.Lock()
	current := l.currentSessionID
	l.mu.Unlock()
	if sessionID == "" || sessionID == current {
		return l.host.AppendTodo(item, limit)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	records := l.partitions[sessionID]
	if limit > 0 && len(todoRecordsIn(records)) >= limit {
		return fmt.Errorf("todo_add: list already at limit %d", limit)
	}
	l.partitions[sessionID] = append(records, todoRecordFromItem(item))
	return nil
}

// SetTodoStatusFor 按归属会话设置清单项三态（todo_done）。
func (l *Ledger) SetTodoStatusFor(sessionID string, index int, status task.TaskStatus) (dto.TaskRecord, error) {
	if l == nil || l.host == nil {
		return dto.TaskRecord{}, errTodolistUnavailable
	}
	l.mu.Lock()
	current := l.currentSessionID
	l.mu.Unlock()
	if sessionID == "" || sessionID == current {
		return l.host.SetTodoStatusByIndex(index, status)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	records := l.partitions[sessionID]
	positions := todoRecordPositions(records)
	if index < 0 || index >= len(positions) {
		return dto.TaskRecord{}, fmt.Errorf("todo_done: index %d out of range (0..%d)", index, len(positions)-1)
	}
	position := positions[index]
	if status != task.TaskPending && status != task.TaskDoing && status != task.TaskCompleted {
		return dto.TaskRecord{}, fmt.Errorf("task: todo 只支持三态 pending/doing/completed，不能迁移到 %s", status)
	}
	records[position].Status = status
	l.partitions[sessionID] = records
	return records[position], nil
}

// todoItemsFromRecords 还原分区里的清单（按行顺序 = 清单顺序）。
func todoItemsFromRecords(records []dto.TaskRecord) []dto.TodoItem {
	items := make([]dto.TodoItem, 0, len(records))
	for _, record := range records {
		if record.Kind != "todo" {
			continue
		}
		items = append(items, task.TaskToTodoItem(record))
	}
	return items
}

// todoRecordsIn 返回分区里的清单行（保持顺序）。
func todoRecordsIn(records []dto.TaskRecord) []dto.TaskRecord {
	todos := make([]dto.TaskRecord, 0, len(records))
	for _, record := range records {
		if record.Kind == "todo" {
			todos = append(todos, record)
		}
	}
	return todos
}

// todoRecordPositions 返回清单行在分区里的下标（顺序即清单顺序）。
func todoRecordPositions(records []dto.TaskRecord) []int {
	positions := make([]int, 0, len(records))
	for index, record := range records {
		if record.Kind == "todo" {
			positions = append(positions, index)
		}
	}
	return positions
}

// partitionWithoutTodo 去掉分区里的清单行（整体替换清单用；任务行原样保留）。
func partitionWithoutTodo(records []dto.TaskRecord) []dto.TaskRecord {
	kept := make([]dto.TaskRecord, 0, len(records))
	for _, record := range records {
		if record.Kind == "todo" {
			continue
		}
		kept = append(kept, record)
	}
	return kept
}

// todoRecordFromItem 由清单项构造 kind=todo 的分区行：形状与注册表建的行同构
// （Phase=tasklist、Key=todo:<文本>、状态三态映射），ID 取**进程级**分配器——
// 分区里"每个会话各自从 1 数"会让跨会话台账同号去重丢行（见 addPartition）。
func todoRecordFromItem(item dto.TodoItem) dto.TaskRecord {
	id := task.NextAutoID("todo")
	return dto.TaskRecord{
		ID: id, Key: "todo:" + item.Text, Phase: dto.TaskPhaseTasklist,
		Task: item.Text, Status: task.TodoToTaskStatus(item.Status), Kind: "todo",
	}
}

// 编译期钉子：生产宿主（task 注册表 actor）结构上满足台账域的宿主面，
// 因此装配点直接注入注册表，不需要适配层（AGENTS §8.1）。
var _ LedgerHost = (*task.TaskRegistry)(nil)
