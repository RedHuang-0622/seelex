package core

// 阶段 2：会话生命周期（design-model 1.2/1.4/1.5）。
// cold_load = resumeSession 未驻留分支；hot_attach = 驻留会话只换视图指针；
// unload = persist + 释放内存态（scope/引擎/任务运行时）。

import (
	"errors"
	"fmt"
	"strings"

	"github.com/RedHuang-0622/seelex/application/core/task_context"
)

// hotAttachSession 热加载：目标会话已驻留（引擎实例在内存，含运行中），
// 只换视图指针 + 投影会话 scope；不重建历史、不触碰 X/M/R（不变量 Ⅱ）。
// 运行中会话也允许回看（design-model 第 5 节决策：热加载支持运行中查看）。
func (service *Service) hotAttachSession(sessionID string) error {
	// 目标会话运行中：其 framework Session 锁被 ChatStream 全程持有，
	// SetSystemPromptFor 会阻塞到该会话跑完（用户视角：切换后应用冻结、
	// 消息发不出、也切不走）。运行中会话的 prompt 在下一轮上下文装配时
	// 自然生效，热挂载只移视图指针，不得触碰引擎锁。
	targetRunning := false
	if unit := service.sessions.Unit(sessionID); unit != nil {
		targetRunning = unit.ChatState().Running
	}
	if !targetRunning {
		if promptPort, ok := service.Deps.Engine.(interface{ SetSystemPromptFor(string, string) }); ok {
			promptPort.SetSystemPromptFor(sessionID, service.promptStack.Render())
		} else {
			service.Deps.Engine.SetSystemPrompt(service.promptStack.Render())
		}
	}
	if service.Deps.Workspace != nil {
		workspace, ok := service.Deps.Workspace.SessionWorkspace(sessionID)
		if ok {
			// 热加载 = 只换视图指针，不得触碰执行作用域：有其它会话运行中
			// 时跳过全局项目根/写作用域重绑（P3/G5），per-session 绑定照记。
			if service.bindProjectRootIfSafe(sessionID, workspace.RootPath) {
				service.Deps.Sessions.SetWorkspace(workspace.ID)
			}
			service.Deps.Runtime.SetSessionWorkspace(sessionID, workspace.ID)
		}
	}
	// 工作台投影：目标会话任务快照（只影响 view，不触碰执行态）。
	service.Deps.Runtime.SwitchSessionTasks(sessionID, service.Deps.Runtime.TaskSnapshotFor(sessionID))
	_ = service.Deps.Runtime.RestoreSubagentAnchors(sessionID)
	workspaceProjection := service.collectWorkspaceProjection()

	service.Mu.Lock()
	name := service.components.sessions.SessionTitleFor(sessionID).Value
	service.Core.Snapshot.Session = SessionState{ID: sessionID, Name: name}
	service.sessions.SetActive(sessionID)
	resumedRuntime := service.sessionUnitLocked(sessionID)
	service.setSessionChatLockedFor(sessionID, resumedRuntime.ChatState())
	service.mirrorActiveViewLocked()
	if task := service.components.tasks.TaskStateFor(sessionID); task != nil {
		service.Core.Snapshot.Task = task
	} else {
		service.Core.Snapshot.Task = nil
	}
	service.Core.Snapshot.Runtime.Plan = task_context.ActivePlanFromStack(
		service.components.tasks.PlanStackFor(sessionID),
		service.components.tasks.ActivePlanIDFor(sessionID),
	)
	service.Core.Snapshot.Interaction = nil
	if service.Deps.Workspace != nil {
		if workspace, ok := service.Deps.Workspace.SessionWorkspace(sessionID); ok {
			service.Core.Snapshot.CurrentWorkspace = &workspace
		}
		service.applyWorkspaceProjectionLocked(workspaceProjection)
	}
	revision := service.bumpLocked()
	service.Mu.Unlock()
	if !targetRunning {
		service.Deps.Engine.SetSystemPrompt(service.components.prompts.SystemPromptForActiveTaskLocked())
	}
	service.publishSessionEvent(EventSnapshotChanged, revision, "", sessionID, nil)
	service.publishRuntimeProjections()
	service.components.sessions.RequestCatalogRefresh()
	return nil
}

// UnloadSession 卸载会话：先持久化（非活跃时），再释放内存态（引擎实例、
// 任务运行时、会话 view scope）；registry 保留 COLD 元数据，重开走
// cold_load。运行中会话拒绝卸载。
func (service *Service) UnloadSession(sessionID string) error {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return errors.New("session ID is required")
	}
	transition := service.components.sessions.TransitionLock()
	transition.Lock()
	defer transition.Unlock()

	service.Mu.RLock()
	running := false
	if unit := service.sessions.Unit(sessionID); unit != nil {
		running = unit.ChatState().Running
	}
	active := sessionID == service.Core.Snapshot.Session.ID
	service.Mu.RUnlock()
	if running {
		return ErrChatRunning
	}
	location := service.components.sessions.LocateSession(sessionID)
	if !active {
		if err := service.components.sessions.PersistCurrentSession(location, sessionID); err != nil {
			return fmt.Errorf("unload persist %q: %w", sessionID, err)
		}
	}
	// 释放内存态。
	if routed, ok := service.Deps.Engine.(interface{ UnloadSession(string) error }); ok {
		if err := routed.UnloadSession(sessionID); err != nil {
			return fmt.Errorf("unload engine %q: %w", sessionID, err)
		}
	}
	service.Mu.Lock()
	service.sessions.Remove(sessionID)
	if service.planProjections != nil {
		delete(service.planProjections, sessionID)
	}
	service.components.tasks.UnloadSessionState(sessionID)
	service.components.sessions.UnloadSessionTitle(sessionID)
	if active {
		// 视图单例一致性：V 的唯一镜像同步到空（draft），避免 Domain.ActiveID
		// 与 Snapshot.Session.ID 分叉（否则后台/活跃判定误判新会话）。
		service.sessions.SetActive("")
		service.Core.Snapshot.Session = SessionState{Draft: true, Name: draftSessionName}
		service.Core.Snapshot.Conversation = nil
		service.Core.Snapshot.Chat = ChatState{}
		service.Core.Snapshot.Task = nil
		service.Core.Snapshot.Runtime.Plan = nil
		service.Core.Snapshot.ReadFiles = nil
	}
	revision := service.bumpLocked()
	service.Mu.Unlock()
	service.publishSessionEvent(EventSnapshotChanged, revision, "", sessionID, nil)
	service.components.sessions.RequestCatalogRefresh()
	return nil
}
