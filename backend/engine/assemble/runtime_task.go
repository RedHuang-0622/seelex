package assemble

// 这是 task 台账域的**薄转发面**（R11-S5，契约 §2.3）：判据与状态都在
// [`kernel/task`](../../engine/task/README.md) 的 `Ledger`（跨会话台账合并、会话 scope 分区、
// 会话级批次、清单面）；nil 接收者与"未装配"的判据由域内**一处**给出，
// 本层不写第二份。内容从 `ports.go` 原样搬来。

import (
	"github.com/RedHuang-0622/seelex/backend/tool/task"
	"github.com/RedHuang-0622/seelex/contract/dto"
	task_context "github.com/RedHuang-0622/seelex/kernel/task"
)

// ── task 台账域端口（薄转发）────────────────────────────────────────
//
// 判据与状态都在 kernel/task.Ledger：跨会话台账合并（Snapshot）、
// 会话 scope 分区读写、会话级批次、清单面。本层只把 Runtime 的公开面接到域
// 实现上——nil 接收者与"注册表/清单未装配"的判据由域内**一处**给出，这里不
// 写第二份。

// ledger 返回台账域句柄：nil 接收者与"未装配"都返回 nil 句柄，判据由域自己给
// （见 Ledger 的 nil 防护）。
func (r *Runtime) ledger() *task_context.Ledger {
	if r == nil {
		return nil
	}
	return r.taskLedger
}

// TaskSnapshot 返回**项目/全局** task 表只读快照（worktable 投影数据源）。
func (r *Runtime) TaskSnapshot() []dto.TaskRecord {
	return r.ledger().Snapshot()
}

// TaskSnapshotFor 返回指定会话的 task 注册表快照（会话级读面：持久化落盘
// 与请求尾部打点块用）。
func (r *Runtime) TaskSnapshotFor(sessionID string) []dto.TaskRecord {
	return r.ledger().SnapshotFor(sessionID)
}

// TaskAdd 主动登记 task（幂等：Key 命中返回既有记录，不重复建条目）。
func (r *Runtime) TaskAdd(spec dto.TaskSpec) (dto.TaskRecord, bool, error) {
	return r.ledger().Add(spec)
}

// TaskAddFor 按归属会话登记 task：当前任务会话写实时注册表，后台会话写自身
// scope 分区（R6/P2 收口：写自有域，后台任务不再污染当前注册表）。
func (r *Runtime) TaskAddFor(sessionID string, spec dto.TaskSpec) (dto.TaskRecord, bool, error) {
	return r.ledger().AddFor(sessionID, spec)
}

// ResolveTaskByKey 按幂等键查 task（B6 子代理装配：查重命中 → 绑定既有 id）。
func (r *Runtime) ResolveTaskByKey(key string) (dto.TaskRecord, bool, error) {
	return r.ledger().ResolveByKey(key)
}

// ResolveTaskByKeyFor 按归属会话查 task。
func (r *Runtime) ResolveTaskByKeyFor(sessionID, key string) (dto.TaskRecord, bool, error) {
	return r.ledger().ResolveByKeyFor(sessionID, key)
}

// TaskSetStatus 更新 task 状态（生命周期打点）。
func (r *Runtime) TaskSetStatus(id string, status dto.TaskStatus, evidence string) (dto.TaskRecord, error) {
	return r.ledger().SetStatus(id, status, evidence)
}

// TaskSetStatusFor 按归属会话更新 task 状态。
func (r *Runtime) TaskSetStatusFor(sessionID, id string, status dto.TaskStatus, evidence string) (dto.TaskRecord, error) {
	return r.ledger().SetStatusFor(sessionID, id, status, evidence)
}

// TaskAttachParticipant 记录参与节点（并行执行证明）。
func (r *Runtime) TaskAttachParticipant(id, participant string) (dto.TaskRecord, error) {
	return r.ledger().AttachParticipant(id, participant)
}

// TaskAppendTrace 追加任务生命周期 trace 点。
func (r *Runtime) TaskAppendTrace(id string, point dto.TaskTracePoint) (dto.TaskRecord, error) {
	return r.ledger().AppendTrace(id, point)
}

// SwitchSessionTasks 会话切换：保存旧会话注册表快照到其 scope 分区，装载
// 目标会话分区记录到当前注册表（空会话 ID = 进入草稿）。工作表格走全局读面，
// 切走/新建都不丢行。
func (r *Runtime) SwitchSessionTasks(sessionID string, records []dto.TaskRecord) {
	r.ledger().SwitchSession(sessionID, records)
}

// TaskChangedChannel 返回 task.changed 输出 channel（CSP：变更即投递）。
func (r *Runtime) TaskChangedChannel() <-chan dto.TaskRecord {
	return r.ledger().Changed()
}

// SetCurrentTaskBatch 设置会话级默认批次（application startChat 按会话调用；
// 后台会话不覆盖活跃注册表默认批次——L3 写隔离）。
func (r *Runtime) SetCurrentTaskBatch(sessionID, batchID string) {
	r.ledger().SetBatch(sessionID, batchID)
}

// CurrentTaskBatchFor 返回指定会话的默认批次（TaskAdd 盖章辅助；空 = 未启动）。
func (r *Runtime) CurrentTaskBatchFor(sessionID string) string {
	return r.ledger().BatchFor(sessionID)
}

// TodoSnapshot 返回当前清单只读拷贝（application 快照投影数据源；主代理
// 每次 todolist_* 工具完成经 runtime.changed 增量带到 GUI）。
func (r *Runtime) TodoSnapshot() []dto.TodoItem {
	return r.ledger().TodoSnapshot()
}

// SetTodoStatus 设置指定待办项的三态（GUI 工作表格人工状态更新入口；
// 只改状态，不新增工具族——todolist 仍是 harness 默认工具族）。
func (r *Runtime) SetTodoStatus(index int, status dto.TodoItemStatus) error {
	return r.ledger().SetTodoStatus(index, status)
}

// TodoSnapshotFor 按归属会话读清单（工具族按调用会话取数）。
func (r *Runtime) TodoSnapshotFor(sessionID string) []dto.TodoItem {
	return r.ledger().TodoSnapshotFor(sessionID)
}

// ReplaceTodoFor 按归属会话整体替换清单（todo_init）。
func (r *Runtime) ReplaceTodoFor(sessionID string, items []dto.TodoItem) error {
	return r.ledger().ReplaceTodoFor(sessionID, items)
}

// AppendTodoFor 按归属会话追加清单项（todo_add；上限与注册表同一口径）。
func (r *Runtime) AppendTodoFor(sessionID string, item dto.TodoItem, limit int) error {
	return r.ledger().AppendTodoFor(sessionID, item, limit)
}

// SetTodoStatusFor 按归属会话设置清单项三态（todo_done）。
func (r *Runtime) SetTodoStatusFor(sessionID string, index int, status task.TaskStatus) (dto.TaskRecord, error) {
	return r.ledger().SetTodoStatusFor(sessionID, index, status)
}

// SetSessionWorkspace 记录会话绑定的 workspace ID（application 在恢复/
// 物化/绑定工作区时通知；framework DurableHistory 按显式键落盘，R3 键
// 漂移收敛）。会话绑定事实，不属 task 台账域。
func (r *Runtime) SetSessionWorkspace(sessionID, workspaceID string) {
	if r == nil {
		return
	}
	r.sessionWorkspacesMu.Lock()
	r.sessionWorkspaces[sessionID] = workspaceID
	r.sessionWorkspacesMu.Unlock()
}

// RegisterTaskTerminalTools 把终态工具 provider 注册进工具注册表（幂等）。
// 状态是**工具注册表**（不属 task 台账域），故留在装配层。
func (r *Runtime) RegisterTaskTerminalTools(handler task.TaskTerminalHandler) {
	state := r.registry
	if state == nil || state.Registry == nil {
		return
	}
	_ = state.Registry.Unregister("seelex-task-terminal")
	if err := state.Registry.Register(task.NewTaskTerminalProvider(handler)); err != nil {
		return
	}
}
