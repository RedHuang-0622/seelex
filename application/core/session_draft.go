package core

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/RedHuang-0622/seelex/application/core/session_runtime"
)

const draftSessionName = "新会话"

// BeginNewSession 进入幂等、未持久化的草稿状态。引擎会话只在第一条真实
// conversation 请求发出时创建。
func (service *Service) BeginNewSession() error {
	transition := service.components.sessions.TransitionLock()
	transition.Lock()
	defer transition.Unlock()

	service.Mu.RLock()
	closed := service.closed
	draining := service.draining
	draft := service.Core.Snapshot.Session.Draft
	sessionID := service.Core.Snapshot.Session.ID
	currentRunning := false
	if unit := service.sessions.Unit(sessionID); unit != nil {
		currentRunning = unit.ChatState().Running
	}
	currentWorkspaceID := session_runtime.WorkspaceID(service.Core.Snapshot.CurrentWorkspace)
	service.Mu.RUnlock()
	if closed {
		return errors.New("application is shut down")
	}
	if draining {
		return ErrApplicationDraining
	}
	if draft {
		return nil
	}

	if !currentRunning && len(service.Deps.Engine.History()) > 0 {
		service.Deps.Sessions.SetWorkspace(currentWorkspaceID)
		location := service.components.sessions.LocateSession(sessionID)
		if err := service.components.sessions.PersistCurrentSession(location, sessionID); err != nil {
			return fmt.Errorf("save current session before drafting a new one: %w", err)
		}
	}
	// 离开当前会话：清空活跃引擎历史（历史已持久化；会话引擎缓存按需
	// 由 ResumeSession 重建），防止 draft 状态串入旧会话内容。
	if !currentRunning {
		service.Deps.Engine.ClearHistory()
	}
	service.promptStack.ClearKind("skill")
	// 离开当前会话：解绑 context 模块，防止四栈串到新会话。
	if store, ok := service.Deps.Sessions.(session_runtime.SessionContextPort); ok {
		store.DetachSessionContext()
	}
	// 新会话是「任务会话」——必须真正未关联工作区：清空上一个会话继承的
	// 项目绑定（CurrentWorkspace / Runtime project root / session store
	// workspace），防止上个对话的项目信息（项目地址、资源管理器文件树与
	// 提交记录、工作台投影）污染新会话。旧会话的 workspace binding 保留
	// （上面已按 currentWorkspaceID 持久化，会话树仍归入原工作区分组）。
	// 需要项目上下文的「工作区会话」由调用方在草稿上显式 BindWorkspace，
	// 再在首次请求物化时绑定。
	// 草稿槽位：恢复已保留的草稿（含工作区会话绑定）或新建。
	service.Mu.Lock()
	slot := service.draft
	if slot == nil {
		slot = &draftSlot{CreatedAt: time.Now()}
		service.draft = slot
	}
	slot.UpdatedAt = time.Now()
	var restoredWorkspace *WorkspaceInfo
	if slot.Workspace != nil {
		item := *slot.Workspace
		restoredWorkspace = &item
	}
	service.Mu.Unlock()

	if restoredWorkspace == nil {
		// 任务会话草稿：必须真正未关联工作区（清空上个会话继承的项目绑定）。
		if service.Deps.Runtime != nil {
			service.Deps.Runtime.UnbindProjectRoot()
		}
		service.Deps.Sessions.SetWorkspace("")
	}
	// 恢复"工作区会话"草稿：只恢复展示绑定，不在此处切换全局工程根 / store
	// 写作用域（若其它会话运行中，切换会串写；首次提交物化时再绑定）。

	service.Mu.Lock()
	service.Core.Snapshot.Session = SessionState{Name: draftSessionName, Draft: true, Status: SessionStatusDraft}
	service.sessions.SetActive("")
	service.Core.Snapshot.CurrentWorkspace = restoredWorkspace
	service.Core.Snapshot.Conversation = nil
	service.Core.Snapshot.HistoryOffset = 0
	service.Core.Snapshot.TotalMessages = 0
	service.Core.Snapshot.HasMoreHistory = false
	service.Core.Snapshot.Runtime.Plan = nil
	service.Core.Snapshot.Interaction = nil
	draftRuntime := service.sessionUnitLocked("")
	draftRuntime.SetChatState(ChatState{}, nil)
	draftRuntime.SetCancel(nil)
	draftRuntime.SetRequests(nil)
	service.Core.Snapshot.Chat = draftRuntime.ChatState()
	service.components.sessions.SetSessionTitleLocked("", SessionTitle{})
	service.components.tasks.ResetForNewSessionLocked()
	revision := service.bumpLocked()
	service.Mu.Unlock()
	service.publishRuntimeProjections()
	// 会话级工作台隔离：新会话清空 task 注册表与子代理树，避免旧会话
	// 数据污染新会话工作台，并发布空工作表格。
	service.Deps.Runtime.SwitchSessionTasks("", nil)
	_ = service.Deps.Runtime.ClearSubagentTree()
	service.refreshWorkTableFromSources()
	service.publishSessionEvent(EventSnapshotChanged, revision, "", "", nil)
	return nil
}

// materializeDraftSession 为首条请求创建引擎会话与项目绑定。调用方必须持有
// sessionTransitionMu。
func (service *Service) materializeDraftSession(firstQuestion string) error {
	service.Mu.RLock()
	draft := service.Core.Snapshot.Session.Draft
	var workspace *WorkspaceInfo
	if service.Core.Snapshot.CurrentWorkspace != nil {
		item := *service.Core.Snapshot.CurrentWorkspace
		workspace = &item
	}
	service.Mu.RUnlock()
	if !draft {
		return nil
	}

	if workspace != nil {
		if err := service.Deps.Runtime.BindProjectRoot(workspace.RootPath); err != nil {
			return fmt.Errorf("bind project root for new session: %w", err)
		}
		service.Deps.Sessions.SetWorkspace(workspace.ID)
	} else {
		service.Deps.Runtime.UnbindProjectRoot()
		service.Deps.Sessions.SetWorkspace("")
	}
	newID := strings.TrimSpace(service.Deps.Engine.StartSession())
	if newID == "" {
		return errors.New("engine returned an empty session ID")
	}
	// framework DurableHistory 按会话 workspace 显式键落盘（R3 键漂移收敛）。
	if workspace != nil {
		service.Deps.Runtime.SetSessionWorkspace(newID, workspace.ID)
	} else {
		service.Deps.Runtime.SetSessionWorkspace(newID, "")
	}
	// 新会话无既有 context：保持解绑（Runtime 退回内存态，与 draft 一致）。
	if store, ok := service.Deps.Sessions.(session_runtime.SessionContextPort); ok {
		store.DetachSessionContext()
	}
	service.Deps.Engine.SetSystemPrompt(service.promptStack.Render())
	if workspace != nil && service.Deps.Workspace != nil {
		service.Deps.Workspace.BindSession(newID, workspace.ID)
	}
	workspaceProjection := service.collectWorkspaceProjection()

	service.Mu.Lock()
	service.draft = nil // 草稿已物化为真实会话，消费槽位
	title := SessionTitle{Value: session_runtime.SessionTitle(firstQuestion), Source: "first_request", FinalizedAt: time.Now()}
	service.Core.Snapshot.Session = SessionState{ID: newID, Name: title.Value}
	// 视图单例一致性：V 的唯一镜像随物化切到新会话（与 hot_attach/resume 同
	// 步路径），否则流式增量会按 ActiveID="" 误判为后台，Snapshot 收不到增量。
	service.sessions.SetActive(newID)
	service.components.sessions.SetSessionTitleLocked(newID, title)
	service.components.tasks.ResetPlanStateLocked()
	service.applyWorkspaceProjectionLocked(workspaceProjection)
	revision := service.bumpLocked()
	service.Mu.Unlock()
	service.publishRuntimeProjections()
	service.Events.Publish(EventSnapshotChanged, revision, "", nil)
	service.components.sessions.RequestCatalogRefresh()
	return nil
}
