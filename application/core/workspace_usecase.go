package core

import (
	"errors"
	"fmt"
	"time"

	"github.com/RedHuang-0622/seelex/application/contract"
	"github.com/RedHuang-0622/seelex/application/contract/dto"
)

func (service *Service) DeleteSession(sessionID string) error {
	// 会话粒度删除：项目绑定由存储层从会话 record 解析（旧 workspace
	// 粒度 DeleteWorkspace 口已删除）。
	if err := service.Deps.Sessions.Delete(sessionID); err != nil {
		return err
	}
	if service.Deps.Workspace != nil {
		service.Deps.Workspace.UnbindSession(sessionID)
		workspaceProjection := service.collectWorkspaceProjection()
		service.ViewMu.Lock()
		service.applyWorkspaceProjectionLocked(workspaceProjection)
		service.bumpLocked()
		service.ViewMu.Unlock()
		service.components.sessions.RequestCatalogRefresh()
	}
	return nil
}

func (service *Service) CreateWorkspace(name, rootPath, gitRemote string) error {
	if name == "" || rootPath == "" {
		return fmt.Errorf("workspace name and root path are required")
	}
	if gitRemote == "" {
		if detected := service.Deps.Workspace.DetectGitRemote(rootPath); detected != "" {
			gitRemote = detected
		}
	}
	workspace, err := service.Deps.Workspace.Create(name, rootPath, gitRemote)
	if err != nil {
		return err
	}
	return service.bindWorkspaceInfo(workspace)
}

func (service *Service) BindWorkspace(workspaceID string) error {
	workspace, err := service.Deps.Workspace.Get(workspaceID)
	if err != nil {
		return err
	}
	return service.bindWorkspaceInfo(workspace)
}

func (service *Service) bindWorkspaceInfo(workspace WorkspaceInfo) error {
	transition := service.transitionView()
	transition.Lock()
	defer transition.Unlock()

	service.ViewMu.RLock()
	if service.Core.Snapshot.Chat.Running {
		service.ViewMu.RUnlock()
		return ErrChatRunning
	}
	currentSessionID := service.Core.Snapshot.Session.ID
	draft := service.Core.Snapshot.Session.Draft
	currentWorkspaceID := ""
	if service.Core.Snapshot.CurrentWorkspace != nil {
		currentWorkspaceID = service.Core.Snapshot.CurrentWorkspace.ID
	}
	service.ViewMu.RUnlock()

	if draft {
		if err := service.Deps.Runtime.BindProjectRoot(workspace.RootPath); err != nil {
			return err
		}
		// G：工作区草稿的绑定**随 record 落盘**（按绑定项目写 record + 项目
		// 索引），不提前写 workspace.Repo 绑定——物化首次提交才 BindSession；
		// 冷启动恢复路径经项目枚举 + draft 槽恢复同一绑定（见 composer_draft.go）。
		service.Deps.Sessions.SetWorkspace(workspace.ID)
		workspaceProjection := service.collectWorkspaceProjection()
		service.ViewMu.Lock()
		service.Core.Snapshot.CurrentWorkspace = &WorkspaceInfo{
			ID: workspace.ID, Name: workspace.Name, RootPath: workspace.RootPath, GitRemote: workspace.GitRemote,
		}
		if service.draft != nil {
			service.draft.Workspace = &WorkspaceInfo{
				ID: workspace.ID, Name: workspace.Name, RootPath: workspace.RootPath, GitRemote: workspace.GitRemote,
			}
			service.draft.UpdatedAt = time.Now()
		}
		service.applyWorkspaceProjectionLocked(workspaceProjection)
		revision := service.bumpLocked()
		service.ViewMu.Unlock()
		service.publishSessionEvent(EventSnapshotChanged, revision, "", service.currentViewSessionID(), nil)
		service.components.sessions.RequestCatalogRefresh()
		return nil
	}

	history := service.Deps.Engine.History()
	startFreshSession := currentWorkspaceID != workspace.ID && len(history) > 0
	if startFreshSession {
		writeWorkspaceID := currentWorkspaceID
		if writeWorkspaceID == "" {
			writeWorkspaceID = service.components.sessions.LocateSession(currentSessionID).WorkspaceID
		}
		service.Deps.Sessions.SetWorkspace(writeWorkspaceID)
		if err := service.Deps.Sessions.SaveCurrent(currentSessionID); err != nil {
			return fmt.Errorf("save current session before switching project: %w", err)
		}
	}
	if err := service.Deps.Runtime.BindProjectRoot(workspace.RootPath); err != nil {
		return err
	}
	if startFreshSession {
		currentSessionID = service.Deps.Engine.StartSession()
		service.Deps.Engine.SetSystemPrompt(service.promptStack.Render())
	}
	service.Deps.Workspace.BindSession(currentSessionID, workspace.ID)
	// framework DurableHistory 按会话 workspace 显式键落盘（R3 键漂移收敛）。
	service.Deps.Runtime.SetSessionWorkspace(currentSessionID, workspace.ID)
	service.Deps.Sessions.SetWorkspace(workspace.ID)
	workspaceProjection := service.collectWorkspaceProjection()
	service.ViewMu.Lock()
	if startFreshSession {
		service.Core.Snapshot.Session.ID = currentSessionID
		service.Core.Snapshot.Session.Name = ""
		service.Core.Snapshot.Conversation = nil
		service.Core.Snapshot.HistoryOffset = 0
		service.Core.Snapshot.TotalMessages = 0
		service.Core.Snapshot.HasMoreHistory = false
		service.Core.Snapshot.Runtime.Plan = nil
		service.Core.Snapshot.Interaction = nil
		service.appendMessageLocked("system", fmt.Sprintf("已切换到项目 %s，新建独立会话", workspace.Name), nil)
	}
	service.Core.Snapshot.CurrentWorkspace = &WorkspaceInfo{
		ID: workspace.ID, Name: workspace.Name, RootPath: workspace.RootPath, GitRemote: workspace.GitRemote,
	}
	service.applyWorkspaceProjectionLocked(workspaceProjection)
	revision := service.bumpLocked()
	service.ViewMu.Unlock()
	service.publishSessionEvent(EventSnapshotChanged, revision, "", service.currentViewSessionID(), nil)
	service.components.sessions.RequestCatalogRefresh()
	return nil
}

func (service *Service) UnbindWorkspace() {
	transition := service.transitionView()
	transition.Lock()
	defer transition.Unlock()

	service.Deps.Runtime.UnbindProjectRoot()
	service.ViewMu.RLock()
	sessionID := service.Core.Snapshot.Session.ID
	draft := service.Core.Snapshot.Session.Draft
	service.ViewMu.RUnlock()
	if !draft && sessionID != "" {
		service.Deps.Workspace.UnbindSession(sessionID)
	}
	service.Deps.Sessions.SetWorkspace("")
	workspaceProjection := service.collectWorkspaceProjection()
	service.ViewMu.Lock()
	service.Core.Snapshot.CurrentWorkspace = nil
	if service.draft != nil && service.draft.ID == sessionID {
		service.draft.Workspace = nil
		service.draft.UpdatedAt = time.Now()
	}
	service.applyWorkspaceProjectionLocked(workspaceProjection)
	revision := service.bumpLocked()
	service.ViewMu.Unlock()
	service.publishSessionEvent(EventSnapshotChanged, revision, "", service.currentViewSessionID(), nil)
	service.components.sessions.RequestCatalogRefresh()
}

// WorkspaceTree 列出当前工作区某目录的子条目（GUI 工作树数据源；root 只
// 来自后端当前 workspace，客户端只能传相对路径，containment 由后端保证）。
func (service *Service) WorkspaceTree(relPath string, depth int) (dto.TreeListing, error) {
	port, root, err := service.workspaceTreePort()
	if err != nil {
		return dto.TreeListing{}, err
	}
	return port.ListTree(root, relPath, depth)
}

// WorkspaceFileCount 统计当前工作区文件/目录数（工作树文件数 badge 数据源）。
func (service *Service) WorkspaceFileCount() (dto.TreeCount, error) {
	port, root, err := service.workspaceTreePort()
	if err != nil {
		return dto.TreeCount{}, err
	}
	return port.CountFiles(root)
}

// WorkspaceGitLog 返回当前工作区最近 limit 条提交的拓扑树（GUI 提交记录树
// 数据源；只读元数据，root 只来自后端当前 workspace）。
func (service *Service) WorkspaceGitLog(limit int) (dto.GitLogResult, error) {
	port, root, err := service.workspaceTreePort()
	if err != nil {
		return dto.GitLogResult{}, err
	}
	return port.GitLog(root, limit)
}

// workspaceTreePort 读取当前工作区 root（锁内快照拷贝，锁外做文件 I/O）并
// 断言 WorkspacePort 实现 optional 树端口。
func (service *Service) workspaceTreePort() (contract.WorkspaceTreePort, string, error) {
	service.ViewMu.RLock()
	root := ""
	if service.Core.Snapshot.CurrentWorkspace != nil {
		root = service.Core.Snapshot.CurrentWorkspace.RootPath
	}
	service.ViewMu.RUnlock()
	if root == "" {
		return nil, "", errors.New("worktree: no workspace bound to current session")
	}
	port, ok := service.Deps.Workspace.(contract.WorkspaceTreePort)
	if !ok {
		return nil, "", errors.New("worktree: workspace backend does not support tree listing")
	}
	return port, root, nil
}

type workspaceStateProjection struct {
	workspaces []WorkspaceInfo
	bindings   map[string]string
}

// collectWorkspaceProjection 在获取 service.ViewMu 之前执行 WorkspacePort I/O。
// 应用时只拷贝已拥有的值进快照。
func (service *Service) collectWorkspaceProjection() workspaceStateProjection {
	if service.Deps.Workspace == nil {
		return workspaceStateProjection{}
	}
	all := service.Deps.Workspace.List()
	projection := workspaceStateProjection{workspaces: make([]WorkspaceInfo, len(all))}
	for index, workspace := range all {
		projection.workspaces[index] = WorkspaceInfo{
			ID: workspace.ID, Name: workspace.Name, RootPath: workspace.RootPath, GitRemote: workspace.GitRemote,
		}
	}
	projection.bindings = service.Deps.Workspace.AllBindings()
	return projection
}

func (service *Service) applyWorkspaceProjectionLocked(projection workspaceStateProjection) {
	service.Core.Snapshot.Workspaces = append([]WorkspaceInfo(nil), projection.workspaces...)
	service.Core.Snapshot.SessionWorkspaces = make(map[string]string, len(projection.bindings))
	for sessionID, workspaceID := range projection.bindings {
		service.Core.Snapshot.SessionWorkspaces[sessionID] = workspaceID
	}
}
