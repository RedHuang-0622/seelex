package seelebridge

import (
	"errors"
	"fmt"
	"hash/fnv"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ── task 注册表（worktable task 状态体系，Actor + Mailbox）────────────
// task 就是 worktable 的条目。单一 registry actor（mailbox，单消费者串行）
// 内部按 task 键隔离状态——保护粒度是 task 而不是整个 worktable；不采用
// 每 task 一个 goroutine。
//
// 触发方式：
//  - 主动：taskadd 工具（模型直接调用）→ Add（按幂等键去重）；
//  - 被动：plan/subagent 节点生命周期（启动前/进行时/结束后）经 CSP
//    channel 通知 → SetStatus / AttachParticipant / AppendTrace。
//
// todolist 直接内化为 worktable 的打点表：todolist_* 工具与 taskadd 共享
// 本注册表（kind=todo 的任务保持有序列表与索引语义），每次操作写入 trace。
// retry：status=retry 时 RetryCount 自增，重跑 running 保留计数。

// TaskStatus 是 task 的生命周期状态。
type TaskStatus string

const (
	TaskPending   TaskStatus = "pending"
	TaskQueued    TaskStatus = "queued"
	TaskRunning   TaskStatus = "running"
	TaskDoing     TaskStatus = "doing"
	TaskCompleted TaskStatus = "completed"
	TaskFailed    TaskStatus = "failed"
	TaskRetry     TaskStatus = "retry"
)

// TaskPhase* 是 worktable 条目阶段常量（前端筛选/渲染依赖这些字符串）。
const (
	TaskPhasePlan     = "plan"
	TaskPhaseTask     = "task"
	TaskPhaseTasklist = "tasklist"
	TaskPhaseSubagent = "subagent"
)

// TaskTracePoint 是 task 打点（与 worktable trace 同形；evidence 有界）。
type TaskTracePoint struct {
	At        time.Time `json:"at,omitempty"`
	Status    string    `json:"status"`
	Operation string    `json:"operation,omitempty"`
	Evidence  string    `json:"evidence,omitempty"`
	Duration  string    `json:"duration,omitempty"`
}

// TaskRecord 是 task 的只读快照 DTO（字段与 worktable WorkItem 同构）。
type TaskRecord struct {
	ID           string           `json:"id"`                     // plan:<node_id> | subagent:<id> | todo:<n> | task:<n>
	Key          string           `json:"key,omitempty"`          // 幂等键（归一化 goal hash / source id）
	Phase        string           `json:"phase"`                  // plan | tasklist | subagent | task
	Task         string           `json:"task"`                   // goal / label / todo text
	Description  string           `json:"description,omitempty"`  //
	Status       TaskStatus       `json:"status"`                 // pending/running/completed/failed/retry
	RetryCount   int              `json:"retry_count,omitempty"`  // 重试数字
	Assignee     string           `json:"assignee,omitempty"`     // main | 子代理 id | 执行节点
	Dependencies []string         `json:"dependencies,omitempty"` // 前置任务（WorkItem ID 引用）
	Attachments  []string         `json:"attachments,omitempty"`  // 可选附件路径
	Kind         string           `json:"kind"`                   // plan | todo | subagent | task
	SourceID     string           `json:"source_id,omitempty"`    // 原数据面 ID（详情溯源）
	Participants []string         `json:"participants,omitempty"` // 同一 task 的多个子代理
	StartedAt    time.Time        `json:"started_at,omitempty"`
	EndedAt      time.Time        `json:"ended_at,omitempty"`
	Elapsed      string           `json:"elapsed,omitempty"`
	Trace        []TaskTracePoint `json:"trace,omitempty"`
}

// TaskSpec 是 task 创建入参（主动 taskadd 或被动生命周期装配）。
type TaskSpec struct {
	ID           string   `json:"id,omitempty"` // 主动留空 → 后端生成 task:<n>
	Key          string   `json:"key,omitempty"`
	Phase        string   `json:"phase"`
	Task         string   `json:"task"`
	Description  string   `json:"description,omitempty"`
	Assignee     string   `json:"assignee,omitempty"`
	Kind         string   `json:"kind"`
	SourceID     string   `json:"source_id,omitempty"`
	Dependencies []string `json:"dependencies,omitempty"`
	Attachments  []string `json:"attachments,omitempty"`
}

var errTaskClosed = errors.New("task registry: actor closed")

type taskOp int

const (
	taskOpAdd taskOp = iota
	taskOpResolveByKey
	taskOpSetStatus
	taskOpAttachParticipant
	taskOpAppendTrace
	taskOpSnapshot
	taskOpRemove
	taskOpTodoSnapshot
	taskOpReplaceTodo
	taskOpAppendTodo
	taskOpTodoByIndex
	taskOpRestore
	taskOpReplaceAll
)

type taskCommand struct {
	op          taskOp
	spec        TaskSpec
	record      TaskRecord
	taskRecords []TaskRecord
	key         string
	id          string
	status      TaskStatus
	evidence    string
	index       int
	limit       int
	participant string
	point       TaskTracePoint
	items       []TodoItem
	reply       chan taskReply
}

type taskReply struct {
	task    TaskRecord
	tasks   []TaskRecord
	created bool
	err     error
}

// taskRecord 是 actor 内部可变状态（与 DTO 同构；快照时导出只读拷贝）。
type taskRecord struct {
	record TaskRecord
}

// taskRegistry 是 task 注册表 actor：单消费者串行处理命令（CSP 阻塞请求/
// 应答），外部读写只经门面方法，杜绝数据竞争。
type taskRegistry struct {
	mailbox        chan taskCommand
	changes        chan TaskRecord
	done           chan struct{}
	closeOnce      sync.Once
	droppedChanges atomic.Int64
}

func newTaskRegistry() *taskRegistry {
	registry := &taskRegistry{
		mailbox: make(chan taskCommand),
		changes: make(chan TaskRecord, 256),
		done:    make(chan struct{}),
	}
	go registry.loop()
	return registry
}

func (registry *taskRegistry) send(command taskCommand) (taskReply, error) {
	select {
	case registry.mailbox <- command:
		select {
		case reply := <-command.reply:
			return reply, nil
		case <-registry.done:
			return taskReply{}, errTaskClosed
		}
	case <-registry.done:
		return taskReply{}, errTaskClosed
	}
}

func (registry *taskRegistry) loop() {
	state := &taskRegistryState{
		tasks:  make(map[string]*taskRecord),
		byKey:  make(map[string]string),
		todo:   make([]string, 0),
		nextID: 1,
	}
	for {
		select {
		case command := <-registry.mailbox:
			registry.apply(command, state)
		case <-registry.done:
			return
		}
	}
}

// taskRegistryState 是 actor 持有的状态（按 task 键隔离；todo 有序列表）。
type taskRegistryState struct {
	tasks  map[string]*taskRecord
	byKey  map[string]string
	todo   []string // kind=todo 的有序 ID 列表（todolist 索引语义）
	nextID uint64
}

func (registry *taskRegistry) apply(command taskCommand, state *taskRegistryState) {
	var reply taskReply
	switch command.op {
	case taskOpAdd:
		reply.task, reply.created = addTaskLocked(command.spec, state)
	case taskOpResolveByKey:
		reply.task, _ = resolveTaskLocked(command.key, state)
	case taskOpSetStatus:
		reply.task, reply.err = setTaskStatusLocked(command.id, command.status, command.evidence, state)
	case taskOpAttachParticipant:
		reply.task, reply.err = attachParticipantLocked(command.id, command.participant, state)
	case taskOpAppendTrace:
		reply.task, reply.err = appendTaskTraceLocked(command.id, command.point, state)
	case taskOpSnapshot:
		reply.tasks = snapshotTasksLocked(state)
	case taskOpRemove:
		removeTaskLocked(command.id, state)
	case taskOpTodoSnapshot:
		reply.tasks = todoSnapshotLocked(state)
	case taskOpReplaceTodo:
		replaceTodoLocked(command.items, state)
	case taskOpAppendTodo:
		if len(state.todo) >= command.limit {
			reply.err = fmt.Errorf("todolist_add: list already at limit %d", command.limit)
			break
		}
		appendTodoLocked(command.items[0], state)
	case taskOpTodoByIndex:
		reply.task, reply.err = todoByIndexLocked(command.index, state)
	case taskOpRestore:
		reply.task, reply.err = restoreTaskLocked(command.id, command.record, state)
	case taskOpReplaceAll:
		replaceAllTasksLocked(command.taskRecords, state)
	}
	if reply.err == nil && reply.task.ID != "" && command.op != taskOpRestore && command.op != taskOpReplaceAll {
		registry.emitChange(reply.task)
	}
	command.reply <- reply
}

// emitChange 把变更后的 task 投递到输出 channel（CSP）：非阻塞；满则丢弃
// 并计数——task.changed 只是增量，worktable.changed 整表安全网兜底；绝不
// 让 actor 因消费者慢而阻塞（避免服务路径/子代理会话锁环路死锁）。
func (registry *taskRegistry) emitChange(record TaskRecord) {
	copyRecord := record
	copyRecord.Dependencies = append([]string(nil), record.Dependencies...)
	copyRecord.Attachments = append([]string(nil), record.Attachments...)
	copyRecord.Participants = append([]string(nil), record.Participants...)
	copyRecord.Trace = append([]TaskTracePoint(nil), record.Trace...)
	select {
	case registry.changes <- copyRecord:
	default:
		registry.droppedChanges.Add(1)
	}
}

// DroppedChanges 返回因 channel 满而丢弃的变更数（诊断/测试）。
func (registry *taskRegistry) DroppedChanges() int64 {
	if registry == nil {
		return 0
	}
	return registry.droppedChanges.Load()
}

func addTaskLocked(spec TaskSpec, state *taskRegistryState) (TaskRecord, bool) {
	key := spec.Key
	if key == "" {
		key = spec.ID
	}
	if key != "" {
		if id, ok := state.byKey[key]; ok {
			return state.tasks[id].record, false
		}
	}
	id := spec.ID
	if id == "" {
		id = fmt.Sprintf("task:%d", state.nextID)
		state.nextID++
	}
	if spec.Kind == "todo" {
		id = fmt.Sprintf("todo:%d", state.nextID)
		state.nextID++
	}
	record := &taskRecord{record: TaskRecord{
		ID: id, Key: key, Phase: spec.Phase, Task: spec.Task, Description: spec.Description,
		Status: TaskPending, Assignee: spec.Assignee, Kind: spec.Kind, SourceID: spec.SourceID,
		Dependencies: append([]string(nil), spec.Dependencies...),
		Attachments:  append([]string(nil), spec.Attachments...),
		StartedAt:    time.Now(),
		Trace: []TaskTracePoint{{
			At: time.Now(), Status: string(TaskPending), Operation: taskAddOperation(spec.Kind),
		}},
	}}
	state.tasks[id] = record
	if key != "" {
		state.byKey[key] = id
	}
	if spec.Kind == "todo" {
		state.todo = append(state.todo, id)
	}
	return record.record, true
}

func taskAddOperation(kind string) string {
	switch kind {
	case "todo":
		return "todo.add"
	case "plan":
		return "node.lifecycle"
	case "subagent":
		return "subagent.lifecycle"
	default:
		return "task.add"
	}
}

func resolveTaskLocked(key string, state *taskRegistryState) (TaskRecord, bool) {
	if key == "" {
		return TaskRecord{}, false
	}
	id, ok := state.byKey[key]
	if !ok {
		return TaskRecord{}, false
	}
	record, ok := state.tasks[id]
	if !ok {
		return TaskRecord{}, false
	}
	return record.record, true
}

func setTaskStatusLocked(id string, status TaskStatus, evidence string, state *taskRegistryState) (TaskRecord, error) {
	record, ok := state.tasks[id]
	if !ok {
		return TaskRecord{}, fmt.Errorf("task: %s not found", id)
	}
	if err := validateTaskTransition(record.record.Status, status); err != nil {
		return record.record, err
	}
	now := time.Now()
	record.record.Status = status
	if status == TaskRetry {
		record.record.RetryCount++
	}
	switch status {
	case TaskRunning, TaskDoing:
		if record.record.StartedAt.IsZero() {
			record.record.StartedAt = now
		}
		record.record.EndedAt = time.Time{}
	case TaskCompleted, TaskFailed:
		record.record.EndedAt = now
	}
	record.record.Trace = append(record.record.Trace, TaskTracePoint{At: now, Status: string(status), Evidence: evidence})
	if limit := taskTraceLimit; limit > 0 && len(record.record.Trace) > limit {
		record.record.Trace = record.record.Trace[len(record.record.Trace)-limit:]
	}
	return record.record, nil
}

func attachParticipantLocked(id, participant string, state *taskRegistryState) (TaskRecord, error) {
	record, ok := state.tasks[id]
	if !ok {
		return TaskRecord{}, fmt.Errorf("task: %s not found", id)
	}
	for _, existing := range record.record.Participants {
		if existing == participant {
			return record.record, nil
		}
	}
	record.record.Participants = append(record.record.Participants, participant)
	return record.record, nil
}

func appendTaskTraceLocked(id string, point TaskTracePoint, state *taskRegistryState) (TaskRecord, error) {
	record, ok := state.tasks[id]
	if !ok {
		return TaskRecord{}, fmt.Errorf("task: %s not found", id)
	}
	record.record.Trace = append(record.record.Trace, point)
	if limit := taskTraceLimit; limit > 0 && len(record.record.Trace) > limit {
		record.record.Trace = record.record.Trace[len(record.record.Trace)-limit:]
	}
	return record.record, nil
}

// validateTaskTransition 状态单调迁移：终态（completed/failed）不可回退，
// running/doing 不可退回 queued/pending；其余前向迁移允许。
func validateTaskTransition(current, next TaskStatus) error {
	if current == next {
		return nil
	}
	if current == TaskCompleted || current == TaskFailed {
		return fmt.Errorf("task: %s is terminal and cannot transition to %s", current, next)
	}
	if (current == TaskRunning || current == TaskDoing) && (next == TaskPending || next == TaskQueued) {
		return fmt.Errorf("task: cannot regress %s -> %s", current, next)
	}
	return nil
}

// taskTraceLimit 是单 task 打点上限（概览有界；详情弹窗另有完整时间线）。
const taskTraceLimit = 10

func snapshotTasksLocked(state *taskRegistryState) []TaskRecord {
	records := make([]TaskRecord, 0, len(state.tasks))
	for _, record := range state.tasks {
		copyRecord := record.record
		copyRecord.Dependencies = append([]string(nil), record.record.Dependencies...)
		copyRecord.Attachments = append([]string(nil), record.record.Attachments...)
		copyRecord.Participants = append([]string(nil), record.record.Participants...)
		copyRecord.Trace = append([]TaskTracePoint(nil), record.record.Trace...)
		records = append(records, copyRecord)
	}
	sort.SliceStable(records, func(left, right int) bool {
		return records[left].ID < records[right].ID
	})
	return records
}

func removeTaskLocked(id string, state *taskRegistryState) {
	record, ok := state.tasks[id]
	if !ok {
		return
	}
	if record.record.Key != "" {
		delete(state.byKey, record.record.Key)
	}
	if record.record.Kind == "todo" {
		for index, todoID := range state.todo {
			if todoID == id {
				state.todo = append(state.todo[:index], state.todo[index+1:]...)
				break
			}
		}
	}
	delete(state.tasks, id)
}

func replaceTodoLocked(items []TodoItem, state *taskRegistryState) {
	for _, id := range state.todo {
		removeTaskLocked(id, state)
	}
	for _, item := range items {
		spec := TaskSpec{Key: "todo:" + item.Text, Phase: TaskPhaseTasklist, Task: item.Text, Kind: "todo"}
		if item.Status == TodoItemDone {
			_, _ = addTaskLocked(spec, state)
			id := state.byKey[spec.Key]
			if record, ok := state.tasks[id]; ok {
				record.record.Status = TaskCompleted
			}
			continue
		}
		_, _ = addTaskLocked(spec, state)
	}
}

func appendTodoLocked(item TodoItem, state *taskRegistryState) {
	spec := TaskSpec{Key: "todo:" + item.Text, Phase: TaskPhaseTasklist, Task: item.Text, Kind: "todo"}
	_, _ = addTaskLocked(spec, state)
}

func todoByIndexLocked(index int, state *taskRegistryState) (TaskRecord, error) {
	if index < 0 || index >= len(state.todo) {
		return TaskRecord{}, fmt.Errorf("todolist: index %d out of range (0..%d)", index, len(state.todo)-1)
	}
	id := state.todo[index]
	record, ok := state.tasks[id]
	if !ok {
		return TaskRecord{}, fmt.Errorf("todolist: index %d missing", index)
	}
	return record.record, nil
}

// restoreTaskLocked 按原记录回填（幂等键冲突时保留既有，不覆盖）。
func restoreTaskLocked(id string, record TaskRecord, state *taskRegistryState) (TaskRecord, error) {
	if id == "" {
		return TaskRecord{}, fmt.Errorf("task: restore requires id")
	}
	key := record.Key
	if key == "" {
		key = id
	}
	if existingID, ok := state.byKey[key]; ok && existingID != id {
		return state.tasks[existingID].record, nil
	}
	restored := &taskRecord{record: record}
	restored.record.ID = id
	restored.record.Key = key
	restored.record.Dependencies = append([]string(nil), record.Dependencies...)
	restored.record.Attachments = append([]string(nil), record.Attachments...)
	restored.record.Participants = append([]string(nil), record.Participants...)
	restored.record.Trace = append([]TaskTracePoint(nil), record.Trace...)
	state.tasks[id] = restored
	if key != "" {
		state.byKey[key] = id
	}
	if record.Kind == "todo" {
		state.todo = append(state.todo, id)
	}
	return restored.record, nil
}

// replaceAllTasksLocked 整体替换注册表（会话级隔离：切换会话时清空并恢复
// 目标会话的 task；不发增量——前端走整表 resync）。
func replaceAllTasksLocked(records []TaskRecord, state *taskRegistryState) {
	state.tasks = make(map[string]*taskRecord, len(records))
	state.byKey = make(map[string]string, len(records))
	state.todo = make([]string, 0, len(records))
	for _, record := range records {
		_, _ = restoreTaskLocked(record.ID, record, state)
	}
}

func todoSnapshotLocked(state *taskRegistryState) []TaskRecord {
	records := make([]TaskRecord, 0, len(state.todo))
	for _, id := range state.todo {
		if record, ok := state.tasks[id]; ok {
			copyRecord := record.record
			copyRecord.Trace = append([]TaskTracePoint(nil), record.record.Trace...)
			records = append(records, copyRecord)
		}
	}
	return records
}

// ── 门面方法（CSP 阻塞请求/应答；actor 关闭后快速失败）────────────

// Add 创建 task（幂等：Key 命中返回既有记录）。
func (registry *taskRegistry) Add(spec TaskSpec) (TaskRecord, bool, error) {
	reply, err := registry.send(taskCommand{op: taskOpAdd, spec: spec, reply: make(chan taskReply, 1)})
	return reply.task, reply.created, err
}

// ResolveByKey 按幂等键查 task。
func (registry *taskRegistry) ResolveByKey(key string) (TaskRecord, bool, error) {
	reply, err := registry.send(taskCommand{op: taskOpResolveByKey, key: key, reply: make(chan taskReply, 1)})
	return reply.task, reply.task.ID != "", err
}

// SetStatus 更新 task 状态（retry 自增计数；running 保留计数）。
func (registry *taskRegistry) SetStatus(id string, status TaskStatus, evidence string) (TaskRecord, error) {
	reply, err := registry.send(taskCommand{op: taskOpSetStatus, id: id, status: status, evidence: evidence, reply: make(chan taskReply, 1)})
	if err != nil {
		return TaskRecord{}, err
	}
	return reply.task, reply.err
}

// AttachParticipant 把子代理挂为 task 参与者（幂等）。
func (registry *taskRegistry) AttachParticipant(id, participant string) (TaskRecord, error) {
	reply, err := registry.send(taskCommand{op: taskOpAttachParticipant, id: id, participant: participant, reply: make(chan taskReply, 1)})
	if err != nil {
		return TaskRecord{}, err
	}
	return reply.task, reply.err
}

// AppendTrace 追加打点。
func (registry *taskRegistry) AppendTrace(id string, point TaskTracePoint) (TaskRecord, error) {
	reply, err := registry.send(taskCommand{op: taskOpAppendTrace, id: id, point: point, reply: make(chan taskReply, 1)})
	if err != nil {
		return TaskRecord{}, err
	}
	return reply.task, reply.err
}

// Snapshot 返回只读拷贝。
func (registry *taskRegistry) Snapshot() []TaskRecord {
	reply, err := registry.send(taskCommand{op: taskOpSnapshot, reply: make(chan taskReply, 1)})
	if err != nil {
		return nil
	}
	return reply.tasks
}

// ReplaceTodo 整体替换 todolist（todolist_init；融合进 task 注册表）。
func (registry *taskRegistry) ReplaceTodo(items []TodoItem) error {
	reply, err := registry.send(taskCommand{op: taskOpReplaceTodo, items: items, reply: make(chan taskReply, 1)})
	if err != nil {
		return err
	}
	return reply.err
}

// TodoSnapshot 返回 todolist 有序只读快照（kind=todo 的 task 按列表序）。
func (registry *taskRegistry) TodoSnapshot() []TaskRecord {
	reply, err := registry.send(taskCommand{op: taskOpTodoSnapshot, reply: make(chan taskReply, 1)})
	if err != nil {
		return nil
	}
	return reply.tasks
}

// AppendTodo 追加一项（actor 内上限校验，防并发 check-then-act）。
func (registry *taskRegistry) AppendTodo(item TodoItem, limit int) error {
	reply, err := registry.send(taskCommand{op: taskOpAppendTodo, items: []TodoItem{item}, limit: limit, reply: make(chan taskReply, 1)})
	if err != nil {
		return err
	}
	return reply.err
}

// SetTodoStatusByIndex 按 todolist 索引设置状态（GUI 工作表格状态更新入口）。
func (registry *taskRegistry) SetTodoStatusByIndex(index int, status TaskStatus) (TaskRecord, error) {
	reply, err := registry.send(taskCommand{op: taskOpTodoByIndex, index: index, reply: make(chan taskReply, 1)})
	if err != nil {
		return TaskRecord{}, err
	}
	if reply.err != nil {
		return TaskRecord{}, reply.err
	}
	return registry.SetStatus(reply.task.ID, status, "")
}

// Close 优雅关闭 actor。
func (registry *taskRegistry) Close() {
	if registry != nil {
		registry.closeOnce.Do(func() { close(registry.done) })
	}
}

// TaskChanged 返回 task.changed 输出 channel（CSP：变更即投递，application
// 消费者直发增量，不拉脏、不丢事件）。
func (registry *taskRegistry) TaskChanged() <-chan TaskRecord {
	if registry == nil {
		return nil
	}
	return registry.changes
}

// Restore 会话恢复：按原记录原样回填（不追加打点、不校验迁移、不发增量）。
func (registry *taskRegistry) Restore(id string, record TaskRecord) error {
	reply, err := registry.send(taskCommand{op: taskOpRestore, id: id, record: record, reply: make(chan taskReply, 1)})
	if err != nil {
		return err
	}
	return reply.err
}

// ReplaceAll 整体替换注册表（会话级隔离：切换会话时清空并恢复目标会话
// 的 task；不发增量，前端走整表 resync）。
func (registry *taskRegistry) ReplaceAll(records []TaskRecord) error {
	reply, err := registry.send(taskCommand{op: taskOpReplaceAll, taskRecords: records, reply: make(chan taskReply, 1)})
	if err != nil {
		return err
	}
	return reply.err
}

// ── Runtime 门面（application 经端口消费；被动生命周期也经此处落库）────

// TaskSnapshot 返回 task 注册表只读快照（worktable 投影数据源）。
func (r *Runtime) TaskSnapshot() []TaskRecord {
	if r == nil || r.tasks == nil {
		return nil
	}
	return r.tasks.Snapshot()
}

// TaskAdd 主动登记 task（幂等：Key 命中返回既有记录，不重复建条目）。
func (r *Runtime) TaskAdd(spec TaskSpec) (TaskRecord, bool, error) {
	if r == nil || r.tasks == nil {
		return TaskRecord{}, false, errors.New("task: registry unavailable")
	}
	return r.tasks.Add(spec)
}

// ResolveTaskByKey 按幂等键查 task（B6 子代理装配：查重命中 → 绑定既有 id）。
func (r *Runtime) ResolveTaskByKey(key string) (TaskRecord, bool, error) {
	if r == nil || r.tasks == nil {
		return TaskRecord{}, false, errors.New("task: registry unavailable")
	}
	return r.tasks.ResolveByKey(key)
}

// TaskSetStatus 更新 task 状态（retry 自增计数；运行中保留计数）。
func (r *Runtime) TaskSetStatus(id string, status TaskStatus, evidence string) (TaskRecord, error) {
	if r == nil || r.tasks == nil {
		return TaskRecord{}, errors.New("task: registry unavailable")
	}
	return r.tasks.SetStatus(id, status, evidence)
}

// TaskAttachParticipant 把子代理挂为 task 参与者（幂等）。
func (r *Runtime) TaskAttachParticipant(id, participant string) (TaskRecord, error) {
	if r == nil || r.tasks == nil {
		return TaskRecord{}, errors.New("task: registry unavailable")
	}
	return r.tasks.AttachParticipant(id, participant)
}

// TaskAppendTrace 追加 task 打点。
func (r *Runtime) TaskAppendTrace(id string, point TaskTracePoint) (TaskRecord, error) {
	if r == nil || r.tasks == nil {
		return TaskRecord{}, errors.New("task: registry unavailable")
	}
	return r.tasks.AppendTrace(id, point)
}

// SwitchSessionTasks 会话级 task 隔离：切换会话时整体替换注册表
// （清空当前会话 task，恢复目标会话 task；T4 复用 session stack 存储）。
func (r *Runtime) SwitchSessionTasks(records []TaskRecord) {
	if r == nil || r.tasks == nil {
		return
	}
	_ = r.tasks.ReplaceAll(records)
}

// TaskChangedChannel 返回 task.changed 输出 channel（CSP：application 消费者
// 直发增量，不拉脏）。
func (r *Runtime) TaskChangedChannel() <-chan TaskRecord {
	if r == nil || r.tasks == nil {
		return nil
	}
	return r.tasks.TaskChanged()
}

// taskKeyForGoal 生成 task 的幂等键（归一化 goal 的 FNV-1a 哈希）。
func taskKeyForGoal(goal string) string {
	// 归一化：去全部空白并小写（中文分词空格不改变语义）。
	normalized := strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(goal)), ""))
	hasher := fnv.New64a()
	_, _ = hasher.Write([]byte(normalized))
	return fmt.Sprintf("goal:%x", hasher.Sum64())
}
