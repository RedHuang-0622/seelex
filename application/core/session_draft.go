package core

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

const draftSessionName = "新会话"

// BeginNewSession enters an idempotent, unpersisted draft state. The engine
// session is created only when the first real conversation request is sent.
func (service *Service) BeginNewSession() error {
	service.sessionTransitionMu.Lock()
	defer service.sessionTransitionMu.Unlock()

	service.mu.RLock()
	closed := service.closed
	draining := service.draining
	running := service.snapshot.Chat.Running
	draft := service.snapshot.Session.Draft
	sessionID := service.snapshot.Session.ID
	currentWorkspaceID := workspaceID(service.snapshot.CurrentWorkspace)
	service.mu.RUnlock()
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

	if len(service.deps.Engine.History()) > 0 {
		service.deps.Sessions.SetWorkspace(currentWorkspaceID)
		if err := service.components.sessions.persistCurrentSession(sessionID); err != nil {
			return fmt.Errorf("save current session before drafting a new one: %w", err)
		}
	}
	service.deps.Engine.ClearHistory()
	service.promptStack.ClearKind("skill")
	// 离开当前会话：解绑 context 模块，防止四栈串到新会话。
	if store, ok := service.deps.Sessions.(sessionContextPort); ok {
		store.DetachSessionContext()
	}

	service.mu.Lock()
	service.snapshot.Session = SessionState{Name: draftSessionName, Draft: true}
	service.snapshot.Conversation = nil
	service.snapshot.Chat = ChatState{}
	service.snapshot.HistoryOffset = 0
	service.snapshot.TotalMessages = 0
	service.snapshot.HasMoreHistory = false
	service.snapshot.Runtime.Plan = nil
	service.snapshot.Interaction = nil
	service.sessionTitle = SessionTitle{}
	service.planStack = nil
	service.activePlanID = ""
	service.planSequence = 0
	service.inputQueue = nil
	service.deferredInputQueue = nil
	service.taskExecution = nil
	service.taskService = nil
	service.components.tasks.syncGoalSkillActiveLocked()
	service.transcript = nil
	service.transcriptSeq = 0
	service.pendingProviderCalls = nil
	service.pendingToolResults = nil
	service.toolResultRefs = nil
	service.resultRefsByToolCallID = make(map[string]string)
	service.taskCheckpoints = nil
	revision := service.bumpLocked()
	service.mu.Unlock()
	service.publishRuntimeProjections()
	// 会话级工作台隔离：新会话清空 task 注册表与子代理树，避免旧会话
	// 数据污染新会话工作台，并发布空工作表格。
	service.deps.Runtime.SwitchSessionTasks(nil)
	_ = service.deps.Runtime.ClearSubagentTree()
	service.refreshWorkTableFromSources()
	service.events.Publish(EventSnapshotChanged, revision, "", nil)
	return nil
}

// materializeDraftSession creates the engine session and project binding for
// the first request. The caller must hold sessionTransitionMu.
func (service *Service) materializeDraftSession(firstQuestion string) error {
	service.mu.RLock()
	draft := service.snapshot.Session.Draft
	var workspace *WorkspaceInfo
	if service.snapshot.CurrentWorkspace != nil {
		item := *service.snapshot.CurrentWorkspace
		workspace = &item
	}
	service.mu.RUnlock()
	if !draft {
		return nil
	}

	if workspace != nil {
		if err := service.deps.Runtime.BindProjectRoot(workspace.RootPath); err != nil {
			return fmt.Errorf("bind project root for new session: %w", err)
		}
		service.deps.Sessions.SetWorkspace(workspace.ID)
	} else {
		service.deps.Runtime.UnbindProjectRoot()
		service.deps.Sessions.SetWorkspace("")
	}
	newID := strings.TrimSpace(service.deps.Engine.StartSession())
	if newID == "" {
		return errors.New("engine returned an empty session ID")
	}
	// 新会话无既有 context：保持解绑（Runtime 退回内存态，与 draft 一致）。
	if store, ok := service.deps.Sessions.(sessionContextPort); ok {
		store.DetachSessionContext()
	}
	service.deps.Engine.SetSystemPrompt(service.promptStack.Render())
	if workspace != nil && service.deps.Workspace != nil {
		service.deps.Workspace.BindSession(newID, workspace.ID)
	}
	workspaceProjection := service.collectWorkspaceProjection()

	service.mu.Lock()
	title := SessionTitle{Value: sessionTitle(firstQuestion), Source: "first_request", FinalizedAt: time.Now()}
	service.snapshot.Session = SessionState{ID: newID, Name: title.Value}
	service.sessionTitle = title
	service.planStack = nil
	service.activePlanID = ""
	service.planSequence = 0
	service.applyWorkspaceProjectionLocked(workspaceProjection)
	revision := service.bumpLocked()
	service.mu.Unlock()
	service.publishRuntimeProjections()
	service.events.Publish(EventSnapshotChanged, revision, "", nil)
	service.requestSessionCatalogRefresh()
	return nil
}
