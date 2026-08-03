package core

import "fmt"

func (service *Service) DeleteSession(sessionID string) error {
	location := service.components.sessions.locateSession(sessionID)
	if scoped, ok := service.deps.Sessions.(scopedSessionPort); ok {
		if err := scoped.DeleteWorkspace(location.workspaceID, sessionID); err != nil {
			return err
		}
	} else if err := service.deps.Sessions.Delete(sessionID); err != nil {
		return err
	}
	if service.deps.Workspace != nil {
		service.deps.Workspace.UnbindSession(sessionID)
		service.mu.Lock()
		service.refreshWorkspaceLocked()
		service.bumpLocked()
		service.mu.Unlock()
	}
	service.components.sessions.invalidateSessionName(sessionID)
	return nil
}

func (service *Service) CreateWorkspace(name, rootPath, gitRemote string) error {
	if name == "" || rootPath == "" {
		return fmt.Errorf("workspace name and root path are required")
	}
	if gitRemote == "" {
		if detected := service.deps.Workspace.DetectGitRemote(rootPath); detected != "" {
			gitRemote = detected
		}
	}
	workspace, err := service.deps.Workspace.Create(name, rootPath, gitRemote)
	if err != nil {
		return err
	}
	return service.bindWorkspaceInfo(workspace)
}

func (service *Service) BindWorkspace(workspaceID string) error {
	workspace, err := service.deps.Workspace.Get(workspaceID)
	if err != nil {
		return err
	}
	return service.bindWorkspaceInfo(workspace)
}

func (service *Service) bindWorkspaceInfo(workspace WorkspaceInfo) error {
	service.sessionTransitionMu.Lock()
	defer service.sessionTransitionMu.Unlock()

	service.mu.RLock()
	if service.snapshot.Chat.Running {
		service.mu.RUnlock()
		return ErrChatRunning
	}
	currentSessionID := service.snapshot.Session.ID
	draft := service.snapshot.Session.Draft
	currentWorkspaceID := ""
	if service.snapshot.CurrentWorkspace != nil {
		currentWorkspaceID = service.snapshot.CurrentWorkspace.ID
	}
	service.mu.RUnlock()

	if draft {
		if err := service.deps.Runtime.BindProjectRoot(workspace.RootPath); err != nil {
			return err
		}
		service.deps.Sessions.SetWorkspace(workspace.ID)
		service.mu.Lock()
		service.snapshot.CurrentWorkspace = &WorkspaceInfo{
			ID: workspace.ID, Name: workspace.Name, RootPath: workspace.RootPath, GitRemote: workspace.GitRemote,
		}
		service.refreshWorkspaceLocked()
		revision := service.bumpLocked()
		service.mu.Unlock()
		service.events.Publish(EventSnapshotChanged, revision, "", nil)
		return nil
	}

	history := service.deps.Engine.History()
	startFreshSession := currentWorkspaceID != workspace.ID && len(history) > 0
	if startFreshSession {
		writeWorkspaceID := currentWorkspaceID
		if writeWorkspaceID == "" {
			writeWorkspaceID = service.components.sessions.locateSession(currentSessionID).workspaceID
		}
		service.deps.Sessions.SetWorkspace(writeWorkspaceID)
		if err := service.deps.Sessions.SaveCurrent(currentSessionID); err != nil {
			return fmt.Errorf("save current session before switching project: %w", err)
		}
	}
	if err := service.deps.Runtime.BindProjectRoot(workspace.RootPath); err != nil {
		return err
	}
	if startFreshSession {
		currentSessionID = service.deps.Engine.StartSession()
		service.deps.Engine.SetSystemPrompt(service.promptStack.Render())
	}
	service.deps.Workspace.BindSession(currentSessionID, workspace.ID)
	service.deps.Sessions.SetWorkspace(workspace.ID)
	service.mu.Lock()
	if startFreshSession {
		service.snapshot.Session.ID = currentSessionID
		service.snapshot.Session.Name = ""
		service.snapshot.Conversation = nil
		service.snapshot.HistoryOffset = 0
		service.snapshot.TotalMessages = 0
		service.snapshot.HasMoreHistory = false
		service.snapshot.Runtime.Plan = nil
		service.snapshot.Interaction = nil
		service.appendMessageLocked("system", fmt.Sprintf("已切换到项目 %s，新建独立会话", workspace.Name), nil)
	}
	service.snapshot.CurrentWorkspace = &WorkspaceInfo{
		ID: workspace.ID, Name: workspace.Name, RootPath: workspace.RootPath, GitRemote: workspace.GitRemote,
	}
	service.refreshWorkspaceLocked()
	revision := service.bumpLocked()
	service.mu.Unlock()
	service.events.Publish(EventSnapshotChanged, revision, "", nil)
	return nil
}

func (service *Service) UnbindWorkspace() {
	service.sessionTransitionMu.Lock()
	defer service.sessionTransitionMu.Unlock()

	service.deps.Runtime.UnbindProjectRoot()
	service.mu.RLock()
	sessionID := service.snapshot.Session.ID
	draft := service.snapshot.Session.Draft
	service.mu.RUnlock()
	if !draft && sessionID != "" {
		service.deps.Workspace.UnbindSession(sessionID)
	}
	service.deps.Sessions.SetWorkspace("")
	service.mu.Lock()
	service.snapshot.CurrentWorkspace = nil
	service.refreshWorkspaceLocked()
	revision := service.bumpLocked()
	service.mu.Unlock()
	service.events.Publish(EventSnapshotChanged, revision, "", nil)
}

func (service *Service) refreshWorkspaceLocked() {
	all := service.deps.Workspace.List()
	service.snapshot.Workspaces = make([]WorkspaceInfo, len(all))
	for index, workspace := range all {
		service.snapshot.Workspaces[index] = WorkspaceInfo{
			ID: workspace.ID, Name: workspace.Name, RootPath: workspace.RootPath, GitRemote: workspace.GitRemote,
		}
	}
	service.snapshot.SessionWorkspaces = service.deps.Workspace.AllBindings()
}
