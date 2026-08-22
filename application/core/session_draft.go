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
	running := service.Core.Snapshot.Chat.Running
	draft := service.Core.Snapshot.Session.Draft
	sessionID := service.Core.Snapshot.Session.ID
	currentWorkspaceID := session_runtime.WorkspaceID(service.Core.Snapshot.CurrentWorkspace)
	service.Mu.RUnlock()
	if closed {
		return errors.New("application is shut down")
	}
	if draining {
		return ErrApplicationDraining
	}
	if running {
		return ErrChatRunning
	}
	if draft {
		return nil
	}

	if len(service.Deps.Engine.History()) > 0 {
		service.Deps.Sessions.SetWorkspace(currentWorkspaceID)
		if err := service.components.sessions.PersistCurrentSession(sessionID); err != nil {
			return fmt.Errorf("save current session before drafting a new one: %w", err)
		}
	}
	service.Deps.Engine.ClearHistory()
	service.promptStack.ClearKind("skill")
	// 离开当前会话：解绑 context 模块，防止四栈串到新会话。
	if store, ok := service.Deps.Sessions.(session_runtime.SessionContextPort); ok {
		store.DetachSessionContext()
	}

	service.Mu.Lock()
	service.Core.Snapshot.Session = SessionState{Name: draftSessionName, Draft: true}
	service.Core.Snapshot.Conversation = nil
	service.Core.Snapshot.Chat = ChatState{}
	service.Core.Snapshot.HistoryOffset = 0
	service.Core.Snapshot.TotalMessages = 0
	service.Core.Snapshot.HasMoreHistory = false
	service.Core.Snapshot.Runtime.Plan = nil
	service.Core.Snapshot.Interaction = nil
	service.components.sessions.SetSessionTitleLocked(SessionTitle{})
	service.inputQueue = nil
	service.components.tasks.ResetForNewSessionLocked()
	revision := service.bumpLocked()
	service.Mu.Unlock()
	service.publishRuntimeProjections()
	// 会话级工作台隔离：新会话清空 task 注册表与子代理树，避免旧会话
	// 数据污染新会话工作台，并发布空工作表格。
	service.Deps.Runtime.SwitchSessionTasks(nil)
	_ = service.Deps.Runtime.ClearSubagentTree()
	service.refreshWorkTableFromSources()
	service.Events.Publish(EventSnapshotChanged, revision, "", nil)
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
	title := SessionTitle{Value: session_runtime.SessionTitle(firstQuestion), Source: "first_request", FinalizedAt: time.Now()}
	service.Core.Snapshot.Session = SessionState{ID: newID, Name: title.Value}
	service.components.sessions.SetSessionTitleLocked(title)
	service.components.tasks.ResetPlanStateLocked()
	service.applyWorkspaceProjectionLocked(workspaceProjection)
	revision := service.bumpLocked()
	service.Mu.Unlock()
	service.publishRuntimeProjections()
	service.Events.Publish(EventSnapshotChanged, revision, "", nil)
	service.components.sessions.RequestCatalogRefresh()
	return nil
}
