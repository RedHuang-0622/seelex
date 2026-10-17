package assemble

// runtime_schedule.go — 定时/周期任务域的**宿主转发面**。
//
// 状态与判据早已下沉 `backend/engine/schedule`（`State` 承载调度器 actor：
// 白名单命令、存储粒度、触发与恢复）；本文件只把 `Runtime` 的公开面接到域实现上，
// 内容从 `ports.go` 原样搬来（含 7 个 DTO 别名），口径逐字未改。
//
// 为什么不是“域包 + 构造注入”形状（契约 §2.2）：该域的**状态**不在 `Runtime` 上
// （`Runtime.scheduler` 只是持有 `*schedule.State` 的装配点引用），没有可下沉的状态，因此按任务书
// S4/S5 条只搬转发面（参见 `docs/2026-10-15-architecture-reshape/r10-r11-p13-contract.md` §2.3）。
//
// 文案（P19 已裁决并统一）：`seelex: scheduler unavailable`。历史前缀 `seelebridge:` 是旧包名，
// 2026-10-17 按用户裁决把全仓用户可见的错误/log 前缀统一改成 `seelex: `（本片只改文案，语义与
// 触发条件一字未动）。

import (
	"context"
	"errors"

	"github.com/RedHuang-0622/seelex/backend/engine/schedule"
	"github.com/RedHuang-0622/seelex/contract/dto"
)

// ScheduledTaskKind 定时/周期任务类型（DTO 别名）。
type ScheduledTaskKind = dto.ScheduledTaskKind

const (
	ScheduledTaskCommand = dto.ScheduledTaskCommand
	ScheduledTaskPrompt  = dto.ScheduledTaskPrompt
)

// ScheduledCommand 白名单命令描述（登记即信任；argv 固定直传，不解析用户文本）。
type ScheduledCommand = dto.ScheduledCommand

// ScheduledCommandInfo 白名单命令展示信息（GUI 新建弹窗下拉数据源）。
type ScheduledCommandInfo = dto.ScheduledCommandInfo

// ScheduledTaskSpec 创建任务入参（GUI Bridge 输入；RunAt 非零 = 一次性定时任务）。
type ScheduledTaskSpec = dto.ScheduledTaskSpec

// ScheduledTaskStatus 任务快照 DTO（GUI 定时任务面板消费）。
type ScheduledTaskStatus = dto.ScheduledTaskStatus

// ScheduledPromptOutcome 是一次定时提示词触发的落点（展示文本 + 本次会话号）。
type ScheduledPromptOutcome = schedule.PromptOutcome

// ScheduledPromptExecutor 提示词任务执行器（main 装配注入；nil = prompt 任务
// 不可创建）。落点口径：sessionID 非空 → 投递该会话；空（默认）→ **新建会话
// 发起**，workspaceID 非空时把新会话装配到该工作区。
type ScheduledPromptExecutor = schedule.PromptExecutor

// RestoreScheduledTasks 从全局 JSONL 读回定时任务定义（冷启动重建）：返回
// （恢复数, 跳过数）。跳过 = 记录已不适用（一次性已过期/命令不在白名单/
// 周期非法），逐条跳过而不是一条坏记录挡住全部。必须在执行器与观察者注入
// 之后调用——恢复出来的任务会按排期立刻触发。
//
// 存储粒度是**全局**的（`<store>/scheduled-tasks.jsonl`，见 RuntimeConfig
// .ScheduledTasksPath）：任务不按项目分区、不按会话分片；触发产生的会话记录
// 才走会话自己的存储与读写纪律。
func (r *Runtime) RestoreScheduledTasks() (int, int, error) {
	if r == nil || r.scheduler == nil {
		return 0, 0, nil
	}
	return r.scheduler.Restore()
}

// RegisterScheduledCommand 登记白名单命令（重复键拒绝；main 装配调用）。
func (r *Runtime) RegisterScheduledCommand(command ScheduledCommand) error {
	if r == nil || r.scheduler == nil {
		return errors.New("seelex: scheduler unavailable")
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

// ScheduleTask 创建并启动一个定时/周期任务（校验入参；返回创建后的快照）。
func (r *Runtime) ScheduleTask(ctx context.Context, spec ScheduledTaskSpec) (*ScheduledTaskStatus, error) {
	if r == nil || r.scheduler == nil {
		return nil, errors.New("seelex: scheduler unavailable")
	}
	return r.scheduler.Schedule(ctx, spec)
}

// UpdateScheduledTask 用一份新定义覆盖既有定时/周期任务（编辑入口；校验与创建
// 同一份判据，运行账目保留）。
func (r *Runtime) UpdateScheduledTask(ctx context.Context, id string, spec ScheduledTaskSpec) (*ScheduledTaskStatus, error) {
	if r == nil || r.scheduler == nil {
		return nil, errors.New("seelex: scheduler unavailable")
	}
	return r.scheduler.Update(ctx, id, spec)
}

// CancelScheduledTask 取消并移除定时/周期任务。
func (r *Runtime) CancelScheduledTask(id string) error {
	if r == nil || r.scheduler == nil {
		return errors.New("seelex: scheduler unavailable")
	}
	return r.scheduler.CancelTask(id)
}

// ScheduledTasksSnapshot 返回定时/周期任务只读快照（application 快照投影数据源）。
func (r *Runtime) ScheduledTasksSnapshot() []ScheduledTaskStatus {
	if r == nil || r.scheduler == nil {
		return nil
	}
	return r.scheduler.Snapshot()
}

// SetScheduledPromptExecutor 注入提示词任务执行器（nil = 禁用 prompt 任务）。
func (r *Runtime) SetScheduledPromptExecutor(executor ScheduledPromptExecutor) {
	if r == nil || r.scheduler == nil {
		return
	}
	r.scheduler.SetPromptExecutor(executor)
}

// SetSchedulerObserver 注入调度器状态变化通知（main 接 application 投影发布）。
func (r *Runtime) SetSchedulerObserver(observer func()) {
	if r == nil || r.scheduler == nil {
		return
	}
	r.scheduler.SetObserver(observer)
}
