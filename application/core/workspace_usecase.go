package core

import (
	"errors"
	"fmt"
	"time"

	"github.com/RedHuang-0622/seelex/application/contract"
	"github.com/RedHuang-0622/seelex/application/contract/dto"
)

// releaseSessionAsync 把"这个会话没有了"转告执行域：杀掉它名下所有在途后台命令。
// 与 releaseTeamRuntime 同一时点、同一语义（会话这一段生命结束）。
func (service *Service) releaseSessionAsync(sessionID string) {
	if service == nil || service.Deps.Runtime == nil || sessionID == "" {
		return
	}
	service.Deps.Runtime.ReleaseSessionAsync(sessionID)
}

func (service *Service) DeleteSession(sessionID string) error {
	// 会话粒度删除：项目绑定由存储层从会话 record 解析（旧 workspace
	// 粒度 DeleteWorkspace 口已删除）。
	if err := service.Deps.Sessions.Delete(sessionID); err != nil {
		return err
	}
	// 会话没了就不该留着它的发言调度记账（环是派生状态，重开按落盘事实重建）。
	service.releaseTeamRuntime(sessionID)
	// 同一时点终止它名下还在跑的后台命令：句柄表按会话持有执行体，会话删了就没有
	// 任何入口能再取回或杀掉它们。
	service.releaseSessionAsync(sessionID)
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
		if err := service.bindGlobalProjectRoot(workspace.RootPath); err != nil {
			return err
		}
		// G：工作区草稿的绑定**随 record 落盘**（按绑定项目写 record + 项目
		// 索引），不提前写 workspace.Repo 绑定——物化首次提交才 BindSession；
		// 冷启动恢复路径经项目枚举 + draft 槽恢复同一绑定（见 composer_draft.go）。
		service.setWorkspaceWriteScope(workspace.ID)
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
		service.setWorkspaceWriteScope(writeWorkspaceID)
		if err := service.Deps.Sessions.SaveCurrent(currentSessionID); err != nil {
			return fmt.Errorf("save current session before switching project: %w", err)
		}
	}
	if err := service.bindGlobalProjectRoot(workspace.RootPath); err != nil {
		return err
	}
	if startFreshSession {
		if service.perSessionExecution() {
			currentSessionID = service.newGeneratedSessionID("ws")
			if activator, ok := service.Deps.Engine.(interface{ ActivateSession(string) error }); ok {
				if err := activator.ActivateSession(currentSessionID); err != nil {
					return fmt.Errorf("activate new workspace session: %w", err)
				}
			}
		} else {
			currentSessionID = service.Deps.Engine.StartSession()
		}
		service.Deps.Engine.SetSystemPrompt(service.promptStack.Render())
	}
	service.Deps.Workspace.BindSession(currentSessionID, workspace.ID)
	// framework DurableHistory 按会话 workspace 显式键落盘（R3 键漂移收敛）。
	service.Deps.Runtime.SetSessionWorkspace(currentSessionID, workspace.ID)
	// 另起的独立会话同样要绑定它自己的 context store（与 /new 的草稿物化、
	// resume 共用一条挂接路径；只建引擎不挂接 = 整段活跃期未绑定，见
	// attachSessionContextFor 的注释）。
	if startFreshSession {
		if err := service.attachSessionContextFor(workspace.ID, currentSessionID); err != nil {
			return err
		}
	}
	service.setWorkspaceWriteScope(workspace.ID)
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
		// 会话域活跃指针跟着换（与 /new、热挂载、冷恢复、冷加载同序）：只改快照
		// 里的 Session.ID 而不动这一格，新项目的回合会按旧指针登记到**上一个项目
		// 的会话**上（RequestID→会话反查落空 → 任务纪元/plan/transcript 全落在旧
		// 会话），见 work_table_project_scope_test.go。
		service.sessions.SetActive(currentSessionID)
		service.appendMessageLocked("system", fmt.Sprintf("已切换到项目 %s，新建独立会话", workspace.Name), nil)
	}
	service.Core.Snapshot.CurrentWorkspace = &WorkspaceInfo{
		ID: workspace.ID, Name: workspace.Name, RootPath: workspace.RootPath, GitRemote: workspace.GitRemote,
	}
	service.applyWorkspaceProjectionLocked(workspaceProjection)
	revision := service.bumpLocked()
	service.ViewMu.Unlock()
	// 会话切换（新建独立会话）：换当前会话指针、清子代理锚、把**新会话自己的**
	// task 账装回实时注册表，再重投影工作表格——与 /new（session_draft.go）、
	// 热挂载（session_lifecycle.go）、冷恢复（session_history.go）同一套协议。
	//
	// 缺这一步时注册表指针停在**上一个项目**的会话上，而打点块对"视图会话"走
	// 「实时注册表」那条读面（workTableTraceBlockFor），于是新项目首轮请求的尾部
	// 打点表会把旧项目的活动任务前置进 currentInput：跨项目上下文污染（且按轮
	// 重拼，压缩也洗不掉）。旧项目的行不丢：SwitchSessionTasks 把它们搬进该会话
	// 自己的 scope 分区，全局台账（工作表格）照旧看得见它——工作表格是项目/全局
	// 台账、打点块是会话级实发面，两件事不能混（docs/devlog/2026-09-19-worktable-global-scope.md）。
	if startFreshSession {
		service.Deps.Runtime.SwitchSessionTasks(currentSessionID, service.Deps.Runtime.TaskSnapshotFor(currentSessionID))
		_ = service.Deps.Runtime.ClearSubagentTree()
		service.refreshWorkTableFromSources()
	}
	service.publishSessionEvent(EventSnapshotChanged, revision, "", service.currentViewSessionID(), nil)
	service.components.sessions.RequestCatalogRefresh()
	return nil
}

func (service *Service) UnbindWorkspace() {
	transition := service.transitionView()
	transition.Lock()
	defer transition.Unlock()

	service.unbindGlobalProjectRoot()
	service.ViewMu.RLock()
	sessionID := service.Core.Snapshot.Session.ID
	draft := service.Core.Snapshot.Session.Draft
	service.ViewMu.RUnlock()
	if !draft && sessionID != "" {
		service.Deps.Workspace.UnbindSession(sessionID)
		if service.Deps.Runtime != nil {
			// 该会话的项目根随绑定一起失效：留着会让归档后的会话仍解析到旧项目。
			service.Deps.Runtime.UnbindProjectRootFor(sessionID)
		}
	}
	service.setWorkspaceWriteScope("")
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

// WorkspaceGitLog 返回当前工作区最近 limit 条提交（含父提交拓扑；GUI
// 提交记录树数据源；只读元数据，root 只来自后端当前 workspace）。
func (service *Service) WorkspaceGitLog(limit int) (dto.GitLogResult, error) {
	port, root, err := service.workspaceTreePort()
	if err != nil {
		return dto.GitLogResult{}, err
	}
	return port.GitLog(root, limit)
}

// WorkspaceChanges 返回当前工作区未提交的改动（GUI 工作区更改面板数据源；
// 只读元数据——路径与状态字符，不含文件内容或 diff；root 只来自后端当前
// workspace，路径基准归一化/敏感过滤/上限在 workspace 层保证）。
func (service *Service) WorkspaceChanges(limit int) (dto.WorkspaceChangesResult, error) {
	port, root, err := service.workspaceTreePort()
	if err != nil {
		return dto.WorkspaceChangesResult{}, err
	}
	return port.GitChanges(root, limit)
}

// WorkspaceGitCommitDetail 返回当前工作区某个提交改了哪些文件（GUI「提交记录 →
// 点开某一条」数据源；只读元数据——状态/路径/重命名原路径/±行数，不含补丁或
// 文件内容）。root 只来自后端当前 workspace，hash 由前端从提交列表原样带回；
// hash 形状校验、路径基准与敏感过滤都在 workspace 层保证。
func (service *Service) WorkspaceGitCommitDetail(hash string, limit int) (dto.GitCommitDetail, error) {
	port, root, err := service.workspaceTreePort()
	if err != nil {
		return dto.GitCommitDetail{}, err
	}
	return port.GitCommitDetail(root, hash, limit)
}

// WorkspaceFileContent 读取当前工作区某文件的前 limit 字节（GUI 文件预览
// 数据源；root 只来自后端当前 workspace，containment/敏感过滤/上限在
// workspace 层保证；limit ≤ 0 用默认上限）。只读受控字节，不落快照。
func (service *Service) WorkspaceFileContent(relPath string, limit int64) (dto.FileContent, error) {
	port, root, err := service.workspaceFilePort()
	if err != nil {
		return dto.FileContent{}, err
	}
	return port.ReadFile(root, relPath, limit)
}

// WorkspaceGitCommitFileContent 读取当前工作区某文件在某个提交时的内容（GUI
// 「提交记录 → 点开某个文件」数据源）。与 WorkspaceFileContent 同一条可见性
// 边界与同一个返回形状，区别只在字节来源是 git 对象库而不是工作区磁盘：这个
// 提交那一刻的那一份，工作区怎么改都影响不到它；这条通道也只有读。
func (service *Service) WorkspaceGitCommitFileContent(hash, relPath string, limit int64) (dto.FileContent, error) {
	port, root, err := service.workspaceFilePort()
	if err != nil {
		return dto.FileContent{}, err
	}
	return port.GitCommitFileContent(root, hash, relPath, limit)
}

// WorkspaceWriteFile 用 content 覆盖当前工作区某文件的全部内容（「资源管理器 →
// 文件详情」面板的编辑保存数据源）。root 只来自后端当前 workspace，客户端只能传
// 相对路径；可见性边界（containment / 忽略目录 / 敏感文件名 / 符号链接 / 二进制 /
// 上限）在 workspace 层保证，且写入是原子发布——因此保存成功后调用方读回同一路径
// 就能把编辑器基线同步到实际文件。
//
// 权限口径：这是**用户自己**在面板里点下的保存（用户即动作主体，点击就是同意），
// 不走主代理权限档位与执行选择页面——档位管的是"主 agent 能不能动你的文件"，不是
// "你能不能改自己的文件"。工具面（write_file/edit_file）的档位判定一字未动。
func (service *Service) WorkspaceWriteFile(relPath, content string) (dto.FileWriteResult, error) {
	service.ViewMu.RLock()
	root := ""
	if service.Core.Snapshot.CurrentWorkspace != nil {
		root = service.Core.Snapshot.CurrentWorkspace.RootPath
	}
	service.ViewMu.RUnlock()
	if root == "" {
		return dto.FileWriteResult{}, errors.New("worktree: no workspace bound to current session")
	}
	port, ok := service.Deps.Workspace.(contract.WorkspaceFileWritePort)
	if !ok {
		return dto.FileWriteResult{}, errors.New("worktree: workspace backend does not support file editing")
	}
	return port.WriteFile(root, relPath, []byte(content))
}

// workspaceTreePort 读取当前工作区 root（锁内快照拷贝，锁外做文件 I/O）并
// 断言 WorkspacePort 实现 optional 树端口。
func (service *Service) workspaceTreePort() (contract.WorkspaceTreePort, string, error) {
	port, root, err := service.workspaceRootPort("worktree: workspace backend does not support tree listing")
	if err != nil {
		return nil, "", err
	}
	treePort, ok := port.(contract.WorkspaceTreePort)
	if !ok {
		return nil, "", errors.New("worktree: workspace backend does not support tree listing")
	}
	return treePort, root, nil
}

// workspaceFilePort 读取当前工作区 root（同一套锁内快照拷贝）并断言
// WorkspacePort 实现 optional 文件端口。预览读取与"提交内文件内容"读取共用它：
// root 的来源只有一处，两条读取通道不可能解析到不同的工作区。
func (service *Service) workspaceFilePort() (contract.WorkspaceFilePort, string, error) {
	port, root, err := service.workspaceRootPort("worktree: workspace backend does not support file preview")
	if err != nil {
		return nil, "", err
	}
	filePort, ok := port.(contract.WorkspaceFilePort)
	if !ok {
		return nil, "", errors.New("worktree: workspace backend does not support file preview")
	}
	return filePort, root, nil
}

// workspaceRootPort 解析"当前会话绑定的工作区根"与后端本身：没有绑定工作区时
// 报错（调用方各自给出能力面的说明）。
func (service *Service) workspaceRootPort(missingPortMessage string) (contract.WorkspacePort, string, error) {
	service.ViewMu.RLock()
	root := ""
	if service.Core.Snapshot.CurrentWorkspace != nil {
		root = service.Core.Snapshot.CurrentWorkspace.RootPath
	}
	service.ViewMu.RUnlock()
	if root == "" {
		return nil, "", errors.New("worktree: no workspace bound to current session")
	}
	if service.Deps.Workspace == nil {
		return nil, "", errors.New(missingPortMessage)
	}
	return service.Deps.Workspace, root, nil
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
