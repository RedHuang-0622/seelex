package seelebridge

import (
	"context"
	"errors"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
	"github.com/RedHuang-0622/seelex/seelebridge/scheduler"
)

// 定时周期任务域已迁入 seelebridge/scheduler（State actor：ticker 循环、
// 白名单命令、prompt 任务、状态快照）。本文件只保留类型别名与 Runtime 委托。

// ScheduledTaskKind 周期任务类型（DTO 别名）。
type ScheduledTaskKind = dto.ScheduledTaskKind

const (
	ScheduledTaskCommand = dto.ScheduledTaskCommand
	ScheduledTaskPrompt  = dto.ScheduledTaskPrompt
)

// ScheduledCommand 白名单命令描述（登记即信任；argv 固定直传，不解析用户文本）。
type ScheduledCommand = dto.ScheduledCommand

// ScheduledCommandInfo 白名单命令展示信息（GUI 新建弹窗下拉数据源）。
type ScheduledCommandInfo = dto.ScheduledCommandInfo

// ScheduledTaskSpec 创建任务入参（GUI Bridge 输入）。
type ScheduledTaskSpec = dto.ScheduledTaskSpec

// ScheduledTaskStatus 任务快照 DTO（GUI 定时任务面板消费）。
type ScheduledTaskStatus = dto.ScheduledTaskStatus

// ScheduledPromptExecutor 周期提示词任务执行器（main 装配注入：application
// Submit 复用当前主会话；nil = prompt 任务不可创建）。
type ScheduledPromptExecutor = scheduler.PromptExecutor

// RegisterScheduledCommand 登记白名单命令（重复键拒绝；main 装配调用）。
func (r *Runtime) RegisterScheduledCommand(command ScheduledCommand) error {
	if r == nil || r.scheduler == nil {
		return errors.New("seelebridge: scheduler unavailable")
	}
	return r.scheduler.RegisterCommand(command)
}

// ScheduledCommands 返回白名单命令展示信息（GUI 新建弹窗数据源）。
func (r *Runtime) ScheduledCommands() []ScheduledCommandInfo {
	if r == nil || r.scheduler == nil {
		return nil
	}
	return r.scheduler.CommandInfos()
}

// ScheduleTask 创建并启动一个周期任务（校验入参；返回创建后的快照）。
func (r *Runtime) ScheduleTask(ctx context.Context, spec ScheduledTaskSpec) (*ScheduledTaskStatus, error) {
	if r == nil || r.scheduler == nil {
		return nil, errors.New("seelebridge: scheduler unavailable")
	}
	return r.scheduler.Schedule(ctx, spec)
}

// CancelScheduledTask 取消并移除周期任务。
func (r *Runtime) CancelScheduledTask(id string) error {
	if r == nil || r.scheduler == nil {
		return errors.New("seelebridge: scheduler unavailable")
	}
	return r.scheduler.CancelTask(id)
}

// ScheduledTasksSnapshot 返回周期任务只读快照（application 快照投影数据源；
// 状态变化经 observer 通知后由 runtime.changed 增量带到 GUI）。
func (r *Runtime) ScheduledTasksSnapshot() []ScheduledTaskStatus {
	if r == nil || r.scheduler == nil {
		return nil
	}
	return r.scheduler.Snapshot()
}

// SetScheduledPromptExecutor 注入周期提示词任务执行器（nil = 禁用 prompt 任务）。
func (r *Runtime) SetScheduledPromptExecutor(executor ScheduledPromptExecutor) {
	if r == nil || r.scheduler == nil {
		return
	}
	r.scheduler.SetPromptExecutor(executor)
}

// SetSchedulerObserver 注入调度器状态变化通知（main 接 application 的
// 快照投影发布，使 GUI 经 runtime.changed 增量更新定时任务面板）。
func (r *Runtime) SetSchedulerObserver(observer func()) {
	if r == nil || r.scheduler == nil {
		return
	}
	r.scheduler.SetObserver(observer)
}
