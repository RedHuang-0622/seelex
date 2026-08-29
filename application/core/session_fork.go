package core

import (
	"errors"
	"fmt"
	"strings"

	"github.com/RedHuang-0622/seelex/application/core/session_runtime"
	"github.com/RedHuang-0622/seelex/application/model"
)

// ForkSession 从父会话的指定切断点创建独立子会话，并切换到子会话继续。
// 一期决策契约：深拷贝（含 tool-results 物理复制）+ 血缘 meta（子会话侧
// forked_from 事实源）；切断点锚定 EventSeq + 段落边界；运行中拒绝 fork；
// 跨项目 fork 一期禁止（子会话恒落在父会话所在项目）。
func (service *Service) ForkSession(parentID string, request model.ForkRequest) (string, error) {
	parentID = strings.TrimSpace(parentID)
	if parentID == "" {
		return "", errors.New("session ID is required")
	}
	transition := service.components.sessions.TransitionLock()
	transition.Lock()
	childID, err := service.forkSessionLocked(parentID, request)
	transition.Unlock()
	if err != nil {
		return "", err
	}
	if err := service.resumeSession(childID); err != nil {
		return childID, fmt.Errorf("fork created session %q but switching failed: %w", childID, err)
	}
	return childID, nil
}

// ForkSessionLatest 从父会话最新完整段落边界创建独立子会话并切换到子会话
// 继续（GUI/TUI 默认 fork 入口；精细切断点用 ForkSession + ForkRequest）。
func (service *Service) ForkSessionLatest(parentID string) (string, error) {
	parentID = strings.TrimSpace(parentID)
	if parentID == "" {
		return "", errors.New("session ID is required")
	}
	transition := service.components.sessions.TransitionLock()
	transition.Lock()
	location := service.components.sessions.LocateSession(parentID)
	cut, err := service.components.sessions.LatestForkCut(location, parentID)
	var childID string
	if err == nil {
		childID, err = service.forkSessionLocked(parentID, model.ForkRequest{EventSeq: cut})
	}
	transition.Unlock()
	if err != nil {
		return "", err
	}
	if err := service.resumeSession(childID); err != nil {
		return childID, fmt.Errorf("fork created session %q but switching failed: %w", childID, err)
	}
	return childID, nil
}

// forkSessionLocked 在持有会话切换锁时执行 fork 落盘：解析切断点 → 构建
// 截断后的子会话快照 → 写入子会话键 → 绑定项目并切换写作用域。
// 只检查父会话自身是否运行中（并行会话的其它会话执行不影响 fork）。
func (service *Service) forkSessionLocked(parentID string, request model.ForkRequest) (string, error) {
	service.Mu.RLock()
	closed := service.closed
	draining := service.draining
	running := false
	if runtime := service.sessionChat[parentID]; runtime != nil {
		running = runtime.chat.Running
	}
	service.Mu.RUnlock()
	if closed {
		return "", errors.New("application is shut down")
	}
	if draining {
		return "", ErrApplicationDraining
	}
	if running {
		return "", ErrChatRunning
	}
	forkPort, ok := service.Deps.Sessions.(session_runtime.SessionForkPort)
	if !ok {
		return "", errors.New("session fork: durable storage is unavailable")
	}
	location := service.components.sessions.LocateSession(parentID)
	childID := strings.TrimSpace(service.Deps.Engine.StartSession())
	if childID == "" {
		return "", errors.New("engine returned an empty session ID")
	}
	forkContext, err := service.components.sessions.PrepareFork(location, childID, parentID, request)
	if err != nil {
		return "", err
	}
	// ProviderHistory 是可重建缓存：子会话冷恢复由事件流/record 重建，
	// 不继承父的 provider 缓存（避免消息↔事件坐标映射的不确定性）。
	// ToolResults 为父通道全量物理复制（含 compressed:<segment_id> 原文）。
	if err := forkPort.SaveSessionSnapshotWorkspace(location.WorkspaceID, childID, nil, forkContext.Record, forkContext.Events, forkContext.ToolResults); err != nil {
		return "", fmt.Errorf("write forked session snapshot: %w", err)
	}
	if err := forkPort.SaveContextStateWorkspace(location.WorkspaceID, childID, forkContext.Context); err != nil {
		return "", fmt.Errorf("write forked session context: %w", err)
	}
	// 注册子会话：绑定项目 + 切换写作用域（resumeSession 依赖 binding 定位
	// 目标会话，不能等目录刷新）。
	if service.Deps.Workspace != nil && location.Workspace != nil {
		service.Deps.Workspace.BindSession(childID, location.WorkspaceID)
	}
	service.Deps.Sessions.SetWorkspace(location.WorkspaceID)
	service.components.sessions.RequestCatalogRefresh()
	return childID, nil
}
