// Package workspace 承载「当前会话 ↔ 工作区」的用例编排与投影收集。
//
// 生态位（plan §2.4）：工作区存储与 Git/文件能力在 store/workspace，工作区
// 用例（创建 / 绑定 / 解绑 / 当前工作区读面）的唯一编排点在这里；应用视图
// （application/assembly 的 Snapshot）仍是工作区投影的 owner，本包只产出
// Projection 并通过 ViewPort 交给宿主写入。
//
// 依赖方向：contract + middle/protocol/model + 标准库。禁止 import
// application/*、store（具体类型）、wiring、backend/*。
package workspace

import (
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/contract/dto"
	model "github.com/RedHuang-0622/seelex/contract/protocol"
)

// Projection 是工作区用例交给应用视图的窄投影面：工作区清单与
// 会话 → 工作区绑定。owner 仍是应用视图，本包只负责在锁外从存储收集。
type Projection struct {
	Workspaces []model.WorkspaceInfo
	Bindings   map[string]string
}

// ViewState 是工作区用例在视图过渡锁内读取的一份事实快照。
type ViewState struct {
	SessionID            string
	Draft                bool
	Running              bool
	CurrentWorkspaceID   string
	CurrentWorkspaceRoot string
}

// ViewPort 是应用视图的窄投影写面（实现：application/assembly 的 workspaceHost）。
//
// 每个 Apply* 都在宿主的视图锁内完成写入并返回推进后的 revision；
// PublishSnapshotChanged / RequestCatalogRefresh / ClearReviewRequest 由宿主
// 在锁外发布。视图状态的所有权、锁次序（ViewMu → 组件锁）都在宿主，
// 本包不持锁、不复制 Snapshot。
type ViewPort interface {
	// LockTransition 获取视图过渡锁，返回解锁函数。
	LockTransition() (unlock func())
	// State 在视图锁内读取当前视图事实。
	State() ViewState
	// ApplyDraftBinding 提交草稿会话的工作区绑定（快照 CurrentWorkspace +
	// 草稿槽位 + 投影），返回推进后的 revision。
	ApplyDraftBinding(info model.WorkspaceInfo, projection Projection) uint64
	// ApplyBinding 提交普通会话的工作区绑定；freshSessionID 非空时在同一临界区
	// 内重置会话视图（会话 ID/正文/游标/运行时单格）、切换活跃指针并追加 notice。
	ApplyBinding(info model.WorkspaceInfo, projection Projection, freshSessionID, notice string) uint64
	// ApplyUnbind 清空当前工作区与（命中的）草稿绑定。
	ApplyUnbind(sessionID string, projection Projection) uint64
	// PublishSnapshotChanged 发布一次 snapshot.changed（会话取当前视图会话）。
	PublishSnapshotChanged(revision uint64)
	// RequestCatalogRefresh 唤醒会话目录刷新。
	RequestCatalogRefresh()
	// ClearReviewRequest 清空视图会话的待审阅请求（换根后相对路径失效）。
	ClearReviewRequest()
}

// Sessions 是会话目录/绑定的窄写面（*wiring/adapters.SessionManager 结构满足）。
type Sessions interface {
	// SetWorkspace 切换会话存储的写作用域（legacy Router 面）。
	SetWorkspace(workspaceID string)
	// SaveCurrent 在切换项目前把当前会话落盘。
	SaveCurrent(sessionID string) error
}

// Runtime 是工作区绑定涉及的运行时窄端口（contract.RuntimePort 结构满足）。
type Runtime interface {
	BindProjectRoot(rootPath string) error
	UnbindProjectRoot()
	UnbindProjectRootFor(sessionID string)
	SetSessionWorkspace(sessionID, workspaceID string)
	TaskSnapshotFor(sessionID string) []dto.TaskRecord
	SwitchSessionTasks(sessionID string, records []dto.TaskRecord)
	ClearSubagentTree() error
}

// Engine 是工作区切换涉及的引擎窄端口（contract.ChatEngine 结构满足）。
type Engine interface {
	History() []contract.EngineMessage
	StartSession() string
	SetSystemPrompt(prompt string)
}

// sessionActivator 是「按显式 ID 建引擎 bundle」的窄可选端口。
type sessionActivator interface {
	ActivateSession(sessionID string) error
}

// Deps 是工作区用例的装配入参。
//
// 四组跨层依赖各用窄面：Store（工作区存储/绑定）、Sessions（会话存储写作用域）、
// Runtime（项目根与任务账）、View（视图投影写面）。其余小能力用函数值注入，
// 避免为单一调用点扩大上述接口。
type Deps struct {
	Store    contract.WorkspacePort
	Sessions Sessions
	Runtime  Runtime
	Engine   Engine
	View     ViewPort

	// Prompt 返回当前 system prompt（切项目另起会话时写入新引擎）。
	Prompt func() string
	// PerSessionExecution 报告宿主是否具备逐会话执行能力。
	PerSessionExecution func() bool
	// NewSessionID 生成显式会话 ID（切项目另起会话用）。
	NewSessionID func(prefix string) string
	// AttachSessionContext 把目标会话自己的 context 模块挂到引擎。
	AttachSessionContext func(workspaceID, sessionID string) error
	// RefreshWorkTable 重投影工作表格（新会话的 task 账装回后）。
	RefreshWorkTable func()
	// WorkspaceIDForSession 解析会话当前所在项目（切项目前保存旧项目会话用）。
	WorkspaceIDForSession func(sessionID string) string
	// ChatRunningError 是宿主的「会话在跑」错误值（application/assembly.ErrChatRunning），
	// 注入而不是另定义一份，保证 errors.Is 身份不变。
	ChatRunningError error
}

// UseCase 是工作区用例的唯一编排点（无状态，全部依赖在 Deps）。
type UseCase struct {
	deps Deps
}

// New 装配工作区用例。
func New(deps Deps) *UseCase {
	return &UseCase{deps: deps}
}

// CreateWorkspace 建工作区（空 gitRemote 时探测 origin），成功后立即绑定到当前视图。
func (usecase *UseCase) CreateWorkspace(name, rootPath, gitRemote string) error {
	info, err := usecase.createWorkspace(name, rootPath, gitRemote)
	if err != nil {
		return err
	}
	return usecase.bindInfo(info)
}

// CreateWorkspaces 批量登记项目，不绑定当前视图；调用方创建后再按 ID 绑定。
// 所有输入先整体校验，重复根路径不会产生半批写入。
func (usecase *UseCase) CreateWorkspaces(requests []dto.WorkspaceCreateRequest) (dto.WorkspaceCreateBatchResult, error) {
	prepared, err := normalizeWorkspaceCreateRequests(requests)
	if err != nil {
		return dto.WorkspaceCreateBatchResult{}, err
	}
	if usecase.deps.Store == nil {
		return dto.WorkspaceCreateBatchResult{}, errors.New("workspace store is not available")
	}
	existing := make(map[string]struct{})
	for _, info := range usecase.deps.Store.List() {
		existing[workspaceRootKey(info.RootPath)] = struct{}{}
	}
	for _, request := range prepared {
		if _, found := existing[workspaceRootKey(request.RootPath)]; found {
			return dto.WorkspaceCreateBatchResult{}, fmt.Errorf("workspace root is already registered: %s", request.RootPath)
		}
	}
	result := dto.WorkspaceCreateBatchResult{Workspaces: make([]dto.WorkspaceCreateResult, 0, len(prepared))}
	for index, request := range prepared {
		info, createErr := usecase.createWorkspace(request.Name, request.RootPath, request.GitRemote)
		if createErr != nil {
			return result, fmt.Errorf("create workspace %q (#%d): %w", request.Name, index+1, createErr)
		}
		entry := dto.WorkspaceCreateResult{
			ID: info.ID, Name: info.Name, RootPath: info.RootPath, GitRemote: info.GitRemote,
		}
		// 连接配置是**附带项**：写失败不回滚项目，也不中断整批，只把原因带回给调用方。
		if change, hasChange := connectionChangeForCreate(request); hasChange {
			conn, connErr := usecase.SetConnection(info.ID, change)
			if connErr != nil {
				entry.ConnectionError = connErr.Error()
			} else {
				applyConnectionToResult(&entry, conn)
			}
		}
		result.Workspaces = append(result.Workspaces, entry)
	}
	return result, nil
}

// SetConnection 写项目级连接配置（SSH 端点 / git 访问），返回权威读回值。
func (usecase *UseCase) SetConnection(id string, change model.WorkspaceConnectionChange) (model.WorkspaceConnection, error) {
	port, err := usecase.connectionPort()
	if err != nil {
		return model.WorkspaceConnection{}, err
	}
	return port.SetWorkspaceConnection(id, change)
}

// Connection 读项目级连接配置（口令只以布尔出现）。
func (usecase *UseCase) Connection(id string) (model.WorkspaceConnection, error) {
	port, err := usecase.connectionPort()
	if err != nil {
		return model.WorkspaceConnection{}, err
	}
	return port.WorkspaceConnection(id)
}

// connectionPort 解析连接配置的窄可选端口（存储没实现就显式报错，不静默降级）。
func (usecase *UseCase) connectionPort() (contract.WorkspaceConnectionPort, error) {
	if usecase.deps.Store == nil {
		return nil, errors.New("workspace store is not available")
	}
	port, ok := usecase.deps.Store.(contract.WorkspaceConnectionPort)
	if !ok {
		return nil, errors.New("workspace store does not support connection settings")
	}
	return port, nil
}

// connectionChangeForCreate 把新建请求里的连接字段翻成写面变更。
func connectionChangeForCreate(request dto.WorkspaceCreateRequest) (model.WorkspaceConnectionChange, bool) {
	change := model.WorkspaceConnectionChange{}
	has := false
	if request.SSH != nil {
		change.SSH = &model.WorkspaceSSHInfo{
			Host: strings.TrimSpace(request.SSH.Host),
			Port: request.SSH.Port,
			User: strings.TrimSpace(request.SSH.User),
			// 远端工作目录（空 = 未知）原样带下去：校验（POSIX 绝对路径）在存储层，
			// 本层不替人补前缀（猜一个根会让"越界"判错方向）。
			RemoteRoot: strings.TrimSpace(request.SSH.RemoteRoot),
		}
		change.SSHPassword = request.SSH.Password
		has = true
	}
	if request.Git != nil {
		change.Git = &model.WorkspaceGitInfo{
			Username: strings.TrimSpace(request.Git.Username),
			Kind:     strings.TrimSpace(request.Git.Kind),
		}
		change.GitPassword = request.Git.Password
		has = true
	}
	return change, has
}

// applyConnectionToResult 把读回值填进新建结果（只填摘要与布尔）。
func applyConnectionToResult(entry *dto.WorkspaceCreateResult, conn model.WorkspaceConnection) {
	if conn.SSH != nil {
		entry.SSH = &dto.WorkspaceSSHSummary{
			Host: conn.SSH.Host, Port: conn.SSH.Port, User: conn.SSH.User,
			RemoteRoot: conn.SSH.RemoteRoot,
		}
	}
	if conn.Git != nil {
		entry.Git = &dto.WorkspaceGitSummary{Username: conn.Git.Username, Kind: conn.Git.Kind}
	}
	entry.SSHPasswordSet = conn.SSHPasswordSet
	entry.GitPasswordSet = conn.GitPasswordSet
}

func (usecase *UseCase) createWorkspace(name, rootPath, gitRemote string) (model.WorkspaceInfo, error) {
	if name == "" || rootPath == "" {
		return model.WorkspaceInfo{}, fmt.Errorf("workspace name and root path are required")
	}
	if gitRemote == "" {
		if detected := usecase.deps.Store.DetectGitRemote(rootPath); detected != "" {
			gitRemote = detected
		}
	}
	info, err := usecase.deps.Store.Create(name, rootPath, gitRemote)
	if err != nil {
		return model.WorkspaceInfo{}, err
	}
	return info, nil
}

func normalizeWorkspaceCreateRequests(requests []dto.WorkspaceCreateRequest) ([]dto.WorkspaceCreateRequest, error) {
	if len(requests) == 0 {
		return nil, errors.New("workspace batch requires at least one project")
	}
	out := make([]dto.WorkspaceCreateRequest, len(requests))
	roots := make(map[string]struct{}, len(requests))
	for index, request := range requests {
		name := strings.TrimSpace(request.Name)
		rootPath := strings.TrimSpace(request.RootPath)
		if name == "" || rootPath == "" {
			return nil, fmt.Errorf("workspace name and root path are required (#%d)", index+1)
		}
		key := workspaceRootKey(rootPath)
		if _, exists := roots[key]; exists {
			return nil, fmt.Errorf("duplicate workspace root path: %s", rootPath)
		}
		roots[key] = struct{}{}
		out[index] = dto.WorkspaceCreateRequest{
			Name: name, RootPath: rootPath, GitRemote: strings.TrimSpace(request.GitRemote),
			// 连接配置原样透传（口令的校验交给存储层：它才知道自己有没有密文库）。
			SSH: request.SSH,
			Git: request.Git,
		}
	}
	return out, nil
}

func workspaceRootKey(rootPath string) string {
	cleaned := filepath.Clean(rootPath)
	if runtime.GOOS == "windows" {
		return strings.ToLower(cleaned)
	}
	return cleaned
}

// BindWorkspace 把已存在的工作区绑定到当前视图会话/草稿。
func (usecase *UseCase) BindWorkspace(workspaceID string) error {
	info, err := usecase.deps.Store.Get(workspaceID)
	if err != nil {
		return err
	}
	return usecase.bindInfo(info)
}

// bindInfo 是「绑定一个工作区」的唯一编排：
// 草稿只记在视图/草稿槽位；普通会话在换项目且已有历史时另起独立会话。
func (usecase *UseCase) bindInfo(info model.WorkspaceInfo) error {
	unlock := usecase.deps.View.LockTransition()
	defer unlock()

	state := usecase.deps.View.State()
	if state.Running {
		return usecase.deps.ChatRunningError
	}

	if state.Draft {
		if err := usecase.deps.Runtime.BindProjectRoot(info.RootPath); err != nil {
			return err
		}
		// G：工作区草稿的绑定随 record 落盘，不提前写 workspace.Repo 绑定——
		// 物化首次提交才 BindSession；冷启动经项目枚举 + draft 槽恢复。
		usecase.deps.Sessions.SetWorkspace(info.ID)
		revision := usecase.deps.View.ApplyDraftBinding(info, usecase.Projection())
		usecase.publish(revision)
		return nil
	}

	history := usecase.deps.Engine.History()
	startFreshSession := state.CurrentWorkspaceID != info.ID && len(history) > 0
	currentSessionID := state.SessionID
	if startFreshSession {
		writeWorkspaceID := state.CurrentWorkspaceID
		if writeWorkspaceID == "" {
			writeWorkspaceID = usecase.deps.WorkspaceIDForSession(currentSessionID)
		}
		usecase.deps.Sessions.SetWorkspace(writeWorkspaceID)
		if err := usecase.deps.Sessions.SaveCurrent(currentSessionID); err != nil {
			return fmt.Errorf("save current session before switching project: %w", err)
		}
	}
	if err := usecase.deps.Runtime.BindProjectRoot(info.RootPath); err != nil {
		return err
	}
	if startFreshSession {
		if usecase.deps.PerSessionExecution() {
			currentSessionID = usecase.deps.NewSessionID("ws")
			if activator, ok := usecase.deps.Engine.(sessionActivator); ok {
				if err := activator.ActivateSession(currentSessionID); err != nil {
					return fmt.Errorf("activate new workspace session: %w", err)
				}
			}
		} else {
			currentSessionID = usecase.deps.Engine.StartSession()
		}
		usecase.deps.Engine.SetSystemPrompt(usecase.prompt())
	}
	usecase.deps.Store.BindSession(currentSessionID, info.ID)
	// framework DurableHistory 按会话 workspace 显式键落盘（R3 键漂移收敛）。
	usecase.deps.Runtime.SetSessionWorkspace(currentSessionID, info.ID)
	// 另起的独立会话同样要绑定它自己的 context store（与 /new 物化、resume
	// 共用一条挂接路径）。
	if startFreshSession {
		if err := usecase.deps.AttachSessionContext(info.ID, currentSessionID); err != nil {
			return err
		}
	}
	usecase.deps.Sessions.SetWorkspace(info.ID)

	freshSessionID := ""
	notice := ""
	if startFreshSession {
		freshSessionID = currentSessionID
		notice = fmt.Sprintf("已切换到项目 %s，新建独立会话", info.Name)
	}
	revision := usecase.deps.View.ApplyBinding(info, usecase.Projection(), freshSessionID, notice)
	// 新会话的 task 账装回实时注册表，再重投影工作表格（与 /new、热挂载、
	// 冷恢复同一套协议；缺这一步会把旧项目的活动任务前置进新会话）。
	if startFreshSession {
		usecase.deps.Runtime.SwitchSessionTasks(currentSessionID, usecase.deps.Runtime.TaskSnapshotFor(currentSessionID))
		_ = usecase.deps.Runtime.ClearSubagentTree()
		usecase.deps.RefreshWorkTable()
	}
	usecase.publish(revision)
	return nil
}

// UnbindWorkspace 解绑当前视图会话/草稿的工作区。
func (usecase *UseCase) UnbindWorkspace() {
	unlock := usecase.deps.View.LockTransition()
	defer unlock()

	usecase.deps.Runtime.UnbindProjectRoot()
	state := usecase.deps.View.State()
	if !state.Draft && state.SessionID != "" {
		usecase.deps.Store.UnbindSession(state.SessionID)
		// 该会话的项目根随绑定一起失效：留着会让归档后的会话仍解析到旧项目。
		usecase.deps.Runtime.UnbindProjectRootFor(state.SessionID)
	}
	usecase.deps.Sessions.SetWorkspace("")
	revision := usecase.deps.View.ApplyUnbind(state.SessionID, usecase.Projection())
	usecase.publish(revision)
}

// publish 在视图过渡锁内完成投影后统一做三件锁外动作（与迁移前同序）。
func (usecase *UseCase) publish(revision uint64) {
	usecase.deps.View.PublishSnapshotChanged(revision)
	usecase.deps.View.RequestCatalogRefresh()
	// 工作区切换/解绑 = 待审阅请求失效（清单里是工作区相对路径，换根后不再
	// 指向同一份文件）：抽屉随树清空。
	usecase.deps.View.ClearReviewRequest()
}

func (usecase *UseCase) prompt() string {
	if usecase.deps.Prompt == nil {
		return ""
	}
	return usecase.deps.Prompt()
}

// Projection 在视图锁之外收集工作区清单与会话绑定（可能含磁盘索引读）。
func (usecase *UseCase) Projection() Projection {
	if usecase.deps.Store == nil {
		return Projection{}
	}
	all := usecase.deps.Store.List()
	projection := Projection{Workspaces: make([]model.WorkspaceInfo, len(all))}
	copy(projection.Workspaces, all)
	bindings := usecase.deps.Store.AllBindings()
	projection.Bindings = make(map[string]string, len(bindings))
	for sessionID, workspaceID := range bindings {
		projection.Bindings[sessionID] = workspaceID
	}
	return projection
}

// Tree 列出当前工作区某目录的子条目（root 只来自宿主视图）。
func (usecase *UseCase) Tree(relPath string, depth int) (dto.TreeListing, error) {
	port, root, err := usecase.treePort()
	if err != nil {
		return dto.TreeListing{}, err
	}
	return port.ListTree(root, relPath, depth)
}

// FileCount 统计当前工作区文件/目录数。
func (usecase *UseCase) FileCount() (dto.TreeCount, error) {
	port, root, err := usecase.treePort()
	if err != nil {
		return dto.TreeCount{}, err
	}
	return port.CountFiles(root)
}

// GitLog 返回当前工作区最近 limit 条提交（只读元数据）。
func (usecase *UseCase) GitLog(limit int) (dto.GitLogResult, error) {
	port, root, err := usecase.treePort()
	if err != nil {
		return dto.GitLogResult{}, err
	}
	return port.GitLog(root, limit)
}

// Changes 返回当前工作区未提交改动（只读元数据）。
func (usecase *UseCase) Changes(limit int) (dto.WorkspaceChangesResult, error) {
	port, root, err := usecase.treePort()
	if err != nil {
		return dto.WorkspaceChangesResult{}, err
	}
	return port.GitChanges(root, limit)
}

// GitCommitDetail 返回当前工作区某提交改了哪些文件（只读元数据）。
func (usecase *UseCase) GitCommitDetail(hash string, limit int) (dto.GitCommitDetail, error) {
	port, root, err := usecase.treePort()
	if err != nil {
		return dto.GitCommitDetail{}, err
	}
	return port.GitCommitDetail(root, hash, limit)
}

// FileContent 读取当前工作区某文件的前 limit 字节（只读受控字节）。
func (usecase *UseCase) FileContent(relPath string, limit int64) (dto.FileContent, error) {
	port, root, err := usecase.FilePort()
	if err != nil {
		return dto.FileContent{}, err
	}
	return port.ReadFile(root, relPath, limit)
}

// GitCommitFileContent 读取某文件在某个提交时的内容（git 对象库，不是磁盘）。
func (usecase *UseCase) GitCommitFileContent(hash, relPath string, limit int64) (dto.FileContent, error) {
	port, root, err := usecase.FilePort()
	if err != nil {
		return dto.FileContent{}, err
	}
	return port.GitCommitFileContent(root, hash, relPath, limit)
}

// WriteFile 覆盖当前工作区某文件的全部内容（GUI 文件详情面板的保存面）。
func (usecase *UseCase) WriteFile(relPath, content string) (dto.FileWriteResult, error) {
	root := usecase.deps.View.State().CurrentWorkspaceRoot
	if root == "" {
		return dto.FileWriteResult{}, errors.New("worktree: no workspace bound to current session")
	}
	port, ok := usecase.deps.Store.(contract.WorkspaceFileWritePort)
	if !ok {
		return dto.FileWriteResult{}, errors.New("worktree: workspace backend does not support file editing")
	}
	return port.WriteFile(root, relPath, []byte(content))
}

// FilePort 解析当前工作区 root 与可选文件端口；预览读取、提交内文件内容与
// 待审阅请求共用它（root 来源只有一处）。
func (usecase *UseCase) FilePort() (contract.WorkspaceFilePort, string, error) {
	port, root, err := usecase.workspaceRootPort("worktree: workspace backend does not support file preview")
	if err != nil {
		return nil, "", err
	}
	filePort, ok := port.(contract.WorkspaceFilePort)
	if !ok {
		return nil, "", errors.New("worktree: workspace backend does not support file preview")
	}
	return filePort, root, nil
}

func (usecase *UseCase) treePort() (contract.WorkspaceTreePort, string, error) {
	port, root, err := usecase.workspaceRootPort("worktree: workspace backend does not support tree listing")
	if err != nil {
		return nil, "", err
	}
	treePort, ok := port.(contract.WorkspaceTreePort)
	if !ok {
		return nil, "", errors.New("worktree: workspace backend does not support tree listing")
	}
	return treePort, root, nil
}

// WorkspaceRootPort 解析「当前视图绑定的工作区根」与后端本身（锁内快照拷贝、
// 锁外做 I/O）。
//
// 供应用层做「按仓库根查询」的编排：工作区根只能从这一处取（只有本包持视图
// 依赖），而可选端口的断言（WorkspaceGitReposPort 之类）留在应用层——本包的
// 职责是「根从哪来」，不是替应用层决定用哪个端口。
func (usecase *UseCase) WorkspaceRootPort() (contract.WorkspacePort, string, error) {
	return usecase.workspaceRootPort("worktree: workspace backend is not available")
}

// workspaceRootPort 解析「当前视图绑定的工作区根」与后端本身（锁内快照拷贝，
// 锁外做文件 I/O）；没有绑定工作区时报统一的缺绑定错误。
func (usecase *UseCase) workspaceRootPort(missingPortMessage string) (contract.WorkspacePort, string, error) {
	root := usecase.deps.View.State().CurrentWorkspaceRoot
	if root == "" {
		return nil, "", errors.New("worktree: no workspace bound to current session")
	}
	if usecase.deps.Store == nil {
		return nil, "", errors.New(missingPortMessage)
	}
	return usecase.deps.Store, root, nil
}
