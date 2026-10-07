package core

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/RedHuang-0622/seelex/seelebridge"
)

// ── 定时周期任务服务面（GUI Bridge 变更入口）────────────────────────────
// 数据与执行都归 seelebridge 调度器（见 seelebridge/scheduler.go）；
// application 只提供变更入口 + 快照投影发布：变更成功后重新收集运行时投影
// 并发布 runtime.changed 增量，GUI 定时任务面板随之更新。
// 调度器运行中的状态变化（开始/完成/失败）经 main 装配的 observer 回调
// RefreshRuntimeSnapshot，同样走 runtime.changed 增量路径。

// ScheduleTask 创建并启动一个定时/周期任务（校验在 Runtime 调度器内完成）。
func (service *Service) ScheduleTask(ctx context.Context, spec seelebridge.ScheduledTaskSpec) (*seelebridge.ScheduledTaskStatus, error) {
	// 工作区存在性在**创建/编辑时**判：触发的会话要装配到哪个项目，用户在这
	// 一刻就看得见；留到触发时才发现"工作区没了"只会变成一次失败的运行记录。
	if err := service.validateScheduledWorkspace(spec.WorkspaceID); err != nil {
		return nil, err
	}
	created, err := service.Deps.Runtime.ScheduleTask(ctx, spec)
	if err != nil {
		return nil, err
	}
	service.RefreshRuntimeSnapshot()
	return created, nil
}

// UpdateScheduledTask 编辑既有定时/周期任务（ID 是操作键，定义整体替换）。
// 校验口径与创建完全一致：定义合法性在调度器内（与 Schedule 共用同一份判据），
// 工作区存在性在这里（与 ScheduleTask 共用同一份判据）。
func (service *Service) UpdateScheduledTask(ctx context.Context, id string, spec seelebridge.ScheduledTaskSpec) (*seelebridge.ScheduledTaskStatus, error) {
	if strings.TrimSpace(id) == "" {
		return nil, errors.New("任务 ID 不能为空")
	}
	if err := service.validateScheduledWorkspace(spec.WorkspaceID); err != nil {
		return nil, err
	}
	updated, err := service.Deps.Runtime.UpdateScheduledTask(ctx, id, spec)
	if err != nil {
		return nil, err
	}
	service.RefreshRuntimeSnapshot()
	return updated, nil
}

// validateScheduledWorkspace 校验任务指定的工作区可用（空 = 不绑项目，合法）。
// 创建与编辑共用这一份，避免"建的时候拦得住、改的时候漏得过"。
func (service *Service) validateScheduledWorkspace(workspaceID string) error {
	_, err := service.resolveScheduledWorkspace(workspaceID)
	return err
}

// resolveScheduledWorkspace 解析任务指定的工作区（空 ID = (nil, nil)，合法）。
// "工作区能力没装配 / 工作区查不到"这组判据与文案只有这一处：创建、编辑、
// 触发时解析都从这里走。
func (service *Service) resolveScheduledWorkspace(workspaceID string) (*WorkspaceInfo, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return nil, nil
	}
	if service.Deps.Workspace == nil {
		return nil, errors.New("指定了工作区，但工作区能力未装配")
	}
	info, err := service.Deps.Workspace.Get(workspaceID)
	if err != nil {
		return nil, fmt.Errorf("定时任务工作区 %q 不可用: %w", workspaceID, err)
	}
	return &info, nil
}

// CancelScheduledTask 取消并移除定时/周期任务。
func (service *Service) CancelScheduledTask(id string) error {
	if err := service.Deps.Runtime.CancelScheduledTask(id); err != nil {
		return err
	}
	service.RefreshRuntimeSnapshot()
	return nil
}

// RefreshRuntimeSnapshot 重新收集运行时投影（含定时/周期任务快照）并发布
// runtime.changed 增量。供 Runtime 侧状态变化通知（调度器 observer）与
// 定时/周期任务变更入口复用；与 SelectAccount 等既有路径内联逻辑一致。
func (service *Service) RefreshRuntimeSnapshot() {
	projection := service.collectRuntimeProjection(context.Background())
	service.ViewMu.Lock()
	service.applyRuntimeProjectionLocked(projection)
	revision := service.bumpLocked()
	service.ViewMu.Unlock()
	service.publishSessionEvent(EventRuntimeChanged, revision, "", service.currentViewSessionID(), service.Snapshot().Runtime)
	service.publishTaskDeltas()
}

// StartScheduledSession 为一次定时触发**新建会话**并发起提示词，返回新会话 ID。
//
// 三件事，一件都不少、也不多做：
//   - 新会话：早分配一个会话 ID 并装好引擎 bundle / context store（与草稿
//     物化共用 openSessionEngine）；
//   - 工作区装配：workspaceID 非空时按该工作区绑定项目（会话级绑定 + 项目根），
//     于是这个会话的工具面与它的**会话记录落盘作用域**都归到那个项目；
//   - 发起：把提示词提交到新会话（后台执行，不切用户的视图指针）。
//
// 会话记录仍然按会话自己的存储与读写纪律落盘（项目作用域由绑定决定），本方法
// 不碰任何存储实现——它只是"新建 + 装配 + 提交"。
func (service *Service) StartScheduledSession(ctx context.Context, prompt, workspaceID string) (string, error) {
	input := strings.TrimSpace(prompt)
	if input == "" {
		return "", errors.New("提示词内容不能为空")
	}
	service.ViewMu.RLock()
	closed, draining := service.closed, service.draining
	service.ViewMu.RUnlock()
	if closed {
		return "", errors.New("application is shut down")
	}
	if draining {
		return "", ErrApplicationDraining
	}
	workspace, err := service.resolveScheduledWorkspace(workspaceID)
	if err != nil {
		return "", err
	}
	sessionID := service.newGeneratedSessionID("sched")
	transition := service.transitionForSession(sessionID)
	transition.Lock()
	newID, err := service.openSessionEngine(sessionID, workspace)
	transition.Unlock()
	if err != nil {
		return "", err
	}
	// 叫醒一次目录刷新（与草稿物化同序）：新会话仍在内存里，要等它的回合落盘
	// 才会被目录枚举看见；这里先把刷新排上，避免用户等到下一次目录轮询才看到
	// "多了一个会跑的会话"。驻留 LRU 同步记账（引擎 bundle 已建）。
	service.touchResident(newID)
	service.components.sessions.RequestCatalogRefresh()
	if err := service.submitConversationFor(ctx, newID, input); err != nil {
		return newID, err
	}
	return newID, nil
}

// ScheduledTaskSpec 是定时/周期任务创建入参的类型别名（GUI Bridge 直接使用）。
type ScheduledTaskSpec = seelebridge.ScheduledTaskSpec

// ScheduledTaskStatus 是定时/周期任务快照的类型别名（GUI 面板消费）。
type ScheduledTaskStatus = seelebridge.ScheduledTaskStatus
