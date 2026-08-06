package core

import (
	"context"

	"github.com/RedHuang-0622/seelex/seelebridge"
)

// ── 定时周期任务服务面（GUI Bridge 变更入口）────────────────────────────
// 数据与执行都归 seelebridge 调度器（见 seelebridge/scheduler.go）；
// application 只提供变更入口 + 快照投影发布：变更成功后重新收集运行时投影
// 并发布 runtime.changed 增量，GUI 定时任务面板随之更新。
// 调度器运行中的状态变化（开始/完成/失败）经 main 装配的 observer 回调
// RefreshRuntimeSnapshot，同样走 runtime.changed 增量路径。

// ScheduleTask 创建并启动一个周期任务（校验在 Runtime 调度器内完成）。
func (service *Service) ScheduleTask(ctx context.Context, spec seelebridge.ScheduledTaskSpec) (*seelebridge.ScheduledTaskStatus, error) {
	created, err := service.deps.Runtime.ScheduleTask(ctx, spec)
	if err != nil {
		return nil, err
	}
	service.RefreshRuntimeSnapshot()
	return created, nil
}

// CancelScheduledTask 取消并移除周期任务。
func (service *Service) CancelScheduledTask(id string) error {
	if err := service.deps.Runtime.CancelScheduledTask(id); err != nil {
		return err
	}
	service.RefreshRuntimeSnapshot()
	return nil
}

// RefreshRuntimeSnapshot 重新收集运行时投影（含周期任务快照）并发布
// runtime.changed 增量。供 Runtime 侧状态变化通知（调度器 observer）与
// 周期任务变更入口复用；与 SelectAccount 等既有路径内联逻辑一致。
func (service *Service) RefreshRuntimeSnapshot() {
	projection := service.collectRuntimeProjection(context.Background())
	service.mu.Lock()
	service.applyRuntimeProjectionLocked(projection)
	revision := service.bumpLocked()
	service.mu.Unlock()
	service.events.Publish(EventRuntimeChanged, revision, "", service.Snapshot().Runtime)
}

// ScheduledTaskSpec 是周期任务创建入参的类型别名（GUI Bridge 直接使用）。
type ScheduledTaskSpec = seelebridge.ScheduledTaskSpec

// ScheduledTaskStatus 是周期任务快照的类型别名（GUI 面板消费）。
type ScheduledTaskStatus = seelebridge.ScheduledTaskStatus
