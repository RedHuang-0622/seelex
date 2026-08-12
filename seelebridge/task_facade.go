package seelebridge

import (
	"errors"

	"github.com/RedHuang-0622/seelex/seelebridge/task"
)

// ── Runtime task 门面（application 经端口消费；被动生命周期也经此处落库）────
// task 注册表 actor 本体已迁入 seelebridge/task（TaskRegistry）；本文件只保留
// *Runtime 门面方法，委托 task 包实现。

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

// RegisterTaskTerminalTools 把终态工具 provider 注册进 Runtime 的工具注册表
// （幂等：同 provider 重注册先注销再注册，保持快照一致）。
func (r *Runtime) RegisterTaskTerminalTools(handler TaskTerminalHandler) {
	state := r.registry
	if state == nil || state.registry == nil {
		return
	}
	_ = state.registry.Unregister("seelex-task-terminal")
	if err := state.registry.Register(task.NewTaskTerminalProvider(handler)); err != nil {
		return
	}
}
