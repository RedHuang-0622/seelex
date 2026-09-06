package core

import (
	"errors"
	"fmt"
	"strings"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
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
	transition := service.transitionForKey(parentID)
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
	transition := service.transitionForKey(parentID)
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
	service.ViewMu.RLock()
	closed := service.closed
	draining := service.draining
	running := false
	if unit := service.sessions.Unit(parentID); unit != nil {
		running = unit.ChatState().Running
	}
	service.ViewMu.RUnlock()
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
	var childID string
	if service.perSessionExecution() {
		childID = service.newGeneratedSessionID("fork")
		if activator, ok := service.Deps.Engine.(interface{ ActivateSession(string) error }); ok {
			if err := activator.ActivateSession(childID); err != nil {
				return "", fmt.Errorf("activate forked session %q: %w", childID, err)
			}
		}
	} else {
		childID = strings.TrimSpace(service.Deps.Engine.StartSession())
		if childID == "" {
			return "", errors.New("engine returned an empty session ID")
		}
	}
	forkContext, err := service.components.sessions.PrepareFork(location, childID, parentID, request)
	if err != nil {
		return "", err
	}
	// 会话粒度深拷贝（thin-wrapper §5）：子会话 record 前缀 + 上下文栈
	// 引用与父不相交（T2.7/B4）；改子不影响父。
	forkContext.Record = deepCopyForkRecord(forkContext.Record)
	forkContext.Events = append([]model.TranscriptEvent(nil), forkContext.Events...)
	forkContext.ToolResults = append([]model.StoredToolResult(nil), forkContext.ToolResults...)
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
	service.setWorkspaceWriteScope(location.WorkspaceID)
	// F-4：fork 子引擎只是“占位登记”（逐会话宿主显式建 bundle，legacy
	// 宿主经 StartSession 分配 ID），登记完成后一律卸载，让后续 resume
	// 走 cold_load 从 fork 快照装载正文。legacy 宿主若不卸载，resume 会
	// 把空引擎判为已加载 → 热挂载空视图（子会话 record 有正文但
	// SnapshotOf 恒 total=0）；不支持 UnloadSession 的引擎跳过，行为与
	// 修复前一致（其 resume 本就无热挂载语义）。
	if unloader, ok := service.Deps.Engine.(interface{ UnloadSession(string) error }); ok {
		_ = unloader.UnloadSession(childID)
	}
	service.components.sessions.RequestCatalogRefresh()
	return childID, nil
}

// deepCopyForkRecord 深拷贝 fork 子会话 record：Conversation 消息（含
// Tool 引用）、Tasks、Checkpoints、ToolResults、ReadFiles、PlanStack 全部
// 新建切片/对象，父子引用不相交。
func deepCopyForkRecord(record model.SessionRecord) model.SessionRecord {
	copy := record
	copy.Conversation.Messages = make([]model.Message, len(record.Conversation.Messages))
	for index, message := range record.Conversation.Messages {
		cloned := message
		if message.Tool != nil {
			tool := *message.Tool
			cloned.Tool = &tool
		}
		copy.Conversation.Messages[index] = cloned
	}
	copy.Tasks = append([]dto.TaskRecord(nil), record.Tasks...)
	copy.Checkpoints = append([]model.TaskCheckpoint(nil), record.Checkpoints...)
	copy.ToolResults = append([]model.ToolResultRef(nil), record.ToolResults...)
	copy.PlanStack = session_runtime.CloneSessionPlanStack(record.PlanStack)
	copy.Execution.ReadFiles = append([]model.ReadFileRef(nil), record.Execution.ReadFiles...)
	return copy
}
