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
	// 内容 LRU 回读：目标会话的可见正文可能已被卸载（视图未切换到的空闲会话
	// 会释放正文）。热挂载只换视图指针、不重建历史，正文必须在这里从磁盘补回
	// ——否则用户切回来看到的是空会话（磁盘为准，不丢数据）。
	if err := service.ensureSessionContent(sessionID); err != nil {
		return err
	}
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
	// G5 出临界区化：workspace 查询（外部 WorkspacePort，可能含磁盘索引读）
	// 在锁外完成一次并保存拷贝，ViewMu 临界区内不再调用外部端口。
	var attachWorkspace *WorkspaceInfo
	if service.Deps.Workspace != nil {
		workspace, ok := service.Deps.Workspace.SessionWorkspace(sessionID)
		if ok {
			attachWorkspace = &workspace
			// 热加载 = 只换视图指针，不得触碰执行作用域：有其它会话运行中
			// 时跳过全局项目根/写作用域重绑（P3/G5），per-session 绑定照记。
			if service.bindProjectRootIfSafe(sessionID, workspace.RootPath) {
				service.setWorkspaceWriteScope(workspace.ID)
			}
			service.Deps.Runtime.SetSessionWorkspace(sessionID, workspace.ID)
		}
	}
	// 工作台投影：目标会话任务快照（只影响 view，不触碰执行态）。
	service.Deps.Runtime.SwitchSessionTasks(sessionID, service.Deps.Runtime.TaskSnapshotFor(sessionID))
	_ = service.Deps.Runtime.RestoreSubagentAnchors(sessionID)
	workspaceProjection := service.collectWorkspaceProjection()
	// 需求变更（P1-1）：热切换读回目标会话的权限档位（含磁盘读，在锁外完成；
	// 内存态落地在下面拿到会话单元之后）。
	storedTier := service.readStoredPermissionTier(sessionID)

	service.ViewMu.Lock()
	name := service.components.sessions.SessionTitleFor(sessionID).Value
	service.Core.Snapshot.Session = SessionState{ID: sessionID, Name: name}
	service.sessions.SetActive(sessionID)
	resumedRuntime := service.sessionUnitLocked(sessionID)
	service.applyStoredPermissionTier(sessionID, storedTier)
	service.setSessionChatLockedFor(sessionID, resumedRuntime.ChatState())
	service.mirrorActiveViewLocked()
	if task := service.components.tasks.VisibleTaskStateFor(sessionID); task != nil {
		service.Core.Snapshot.Task = task
	} else {
		service.Core.Snapshot.Task = nil
	}
	service.mirrorPlanProjectionForSessionLocked(sessionID, func() *PlanState {
		return task_context.ActivePlanFromStack(
			service.components.tasks.PlanStackFor(sessionID),
			service.components.tasks.ActivePlanIDFor(sessionID),
		)
	})
	service.Core.Snapshot.Interaction = nil
	// 波 4 approval 会话级归属：热切换后单格只镜像目标会话的待批（后台
	// 会话卡在审批时切回即见，不依赖「审批发生在视图会话」的旧假设）。
	service.mirrorPendingApprovalsLocked(sessionID)
	if service.Deps.Workspace != nil {
		if attachWorkspace != nil {
			service.Core.Snapshot.CurrentWorkspace = attachWorkspace
		}
		service.applyWorkspaceProjectionLocked(workspaceProjection)
	}
	// *Locked 方法必须在持锁段内取值：引擎写入留到解锁之后。
	systemPrompt := service.components.prompts.SystemPromptForActiveTaskLocked()
	revision := service.bumpLocked()
	service.ViewMu.Unlock()
	if !targetRunning {
		// 会话路由引擎的 prompt 已在上方按目标会话经 SetSystemPromptFor
		// 写入（缓存同步在 EnginePort 内完成）；此处若再写全局活跃别名引擎
		// （port.engine），而该别名恰指向另一个正在运行的会话，会被其
		// framework Session 锁（ChatStream 全程持有）阻塞到它跑完——用户
		// 视角的“切换要等被切走的会话结束才开始”。非路由单会话引擎无跨
		// 会话别名面，仍走全局写。
		if _, ok := service.Deps.Engine.(interface{ SetSystemPromptFor(string, string) }); !ok {
			service.Deps.Engine.SetSystemPrompt(systemPrompt)
		}
	}
	// 会话运行原件的**重建**（2026-10-04）：热挂载换了视图指针，但目标会话的 runtime
	// 槽可能已被卸载路径清空（或从未采过）——必须在发布快照**之前**重采一次，否则
	// 切回来第一帧就缺 teamwork_board（前端整块退场），要等下一轮才回来。
	service.refreshRuntimeProjectionForSession(sessionID)
	service.publishSessionEvent(EventSnapshotChanged, revision, "", sessionID, nil)
	service.publishRuntimeProjections()
	service.components.sessions.RequestCatalogRefresh()
	// G6 驻留 LRU：热切换 = 一次引擎使用（移动到使用序最前）。
	service.touchResident(sessionID)
	// 内容 LRU：热挂载的会话正文刚被使用（切换回来即最近使用）。
	service.touchContent(sessionID)
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
	transition := service.transitionForSession(sessionID)
	transition.Lock()
	defer transition.Unlock()

	service.ViewMu.RLock()
	restoring := service.isRestoringLocked(sessionID)
	service.ViewMu.RUnlock()
	if restoring {
		return ErrChatRunning
	}

	service.ViewMu.RLock()
	running := false
	if unit := service.sessions.Unit(sessionID); unit != nil {
		running = unit.ChatState().Running
	}
	active := sessionID == service.Core.Snapshot.Session.ID
	service.ViewMu.RUnlock()
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
	service.ViewMu.Lock()
	service.sessions.Remove(sessionID)
	service.components.tasks.DropPlanProjection(sessionID)
	service.components.tasks.UnloadSessionState(sessionID)
	service.components.sessions.UnloadSessionTitle(sessionID)
	if active {
		// 视图单例一致性：卸载活跃会话后进入新的早分配 SID 草稿单元
		// （HasSession=false，不建引擎 bundle；不写槽位——卸载后的空白草稿
		// 不进入会话树，与卸载前语义一致）。
		service.nextViewEpochLocked()
		draftID := service.newDraftSessionIDLocked()
		service.sessions.SetActive(draftID)
		draftUnit := service.sessionUnitLocked(draftID)
		draftUnit.SetChatState(ChatState{}, nil)
		draftUnit.SetCancel(nil)
		draftUnit.SetRequests(nil)
		service.Core.Snapshot.Session = SessionState{ID: draftID, Name: draftSessionName, Draft: true, Status: SessionStatusDraft}
		service.Core.Snapshot.Conversation = nil
		service.Core.Snapshot.Chat = draftUnit.ChatState()
		service.Core.Snapshot.Task = nil
		service.Core.Snapshot.Runtime.Plan = nil
		service.Core.Snapshot.ReadFiles = nil
		// 卸载活跃会话后进入的是**就地新建**的草稿（ID 全新，没有任何持久设置），
		// 档位同样必须在同一临界区里重算：否则卸载后留在快照里的是刚被卸载会话的
		// 档位，chip 与实际生效档位不符（与 BeginNewSession 同一口径）。
		service.syncViewPermissionTierLocked(draftID)
		service.components.tasks.ResetForNewSessionLocked()
	}
	revision := service.bumpLocked()
	service.ViewMu.Unlock()
	service.publishSessionEvent(EventSnapshotChanged, revision, "", sessionID, nil)
	service.components.sessions.RequestCatalogRefresh()
	return nil
}
