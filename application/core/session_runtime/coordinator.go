package session_runtime

import (
	"sort"
	"sync"
	"time"

	"github.com/RedHuang-0622/seelex/application/contract"
	"github.com/RedHuang-0622/seelex/application/core/internal/state"
	"github.com/RedHuang-0622/seelex/application/event"
	"github.com/RedHuang-0622/seelex/application/model"
	"github.com/RedHuang-0622/seelex/seelexctx"
)

const sessionCatalogShutdownWait = 100 * time.Millisecond

// Coordinator 是会话域协调器：持久化、目录与标题恢复、项目 binding、
// storage 设置、会话三读（record/history/transcript）。跨域事务
// （BeginNewSession/ResumeSession/BindWorkspace 编排）仍由根包 Service
// 承担，本包只提供原子域操作。
type Coordinator struct {
	Core  *state.Core
	tasks TaskPersistencePort

	view                 ViewPort
	closed               func() bool
	transcriptTailBudget func(any) int
	isInternalContent    func(string) bool
	tailHistory          func([]model.TranscriptEvent, int, int) []contract.EngineMessage
	oversizedWarning     func(name, resultRef string) string
	contentWarning       func(resultRef string) string
	limits               func() seelexctx.Limits
	displayUserInput     func(string) string

	sessionRuntimeState
}

// sessionRuntimeState 是会话域自持状态（Catalog worker 的 channel 生命周期
// 由 Start/StopCatalogRefresh 管理）。
type sessionRuntimeState struct {
	// transition 是"会话过渡互斥"的 per-session keyed 注册表（G5：同会话
	// 串行、跨会话并行；视图命令共用 view key）。每个 key 一个显式 actor
	// （无锁化：单 goroutine 持有 inFlight 状态，channel 命令；取代原
	// 进程级单把 mutex，见 transition_actor.go / transition_manager.go）。
	transition *SessionTransitionManager
	// catalogMu 保护会话目录 worker 的三类内存态：目录缓存（最近一轮枚举
	// 结果——按 projectID 分格）、会话标题表与刷新回执队列。G5：目录枚举/
	// 标题恢复走外部 SessionPort/WorkspacePort（阻塞 I/O），一律在锁外完成；
	// catalogMu 只护"换内存态"，发布镜像到 Snapshot 时另取 ViewMu 短临界区。
	// 锁序：ViewMu → catalogMu（目录 worker 从不反向持有）。
	catalogMu sync.Mutex
	// catalogGrid 是目录缓存的按项目分格存储：projectID（含默认项目 "" =
	// 未关联会话）→ 该项目最近一轮枚举结果。G6：目录枚举源头已按
	// projectID（SessionsOf），分格缓存使"刷新项目 A"不会重算或覆盖项目 B
	// 的格子；跨项目归档/删除/状态过滤互不串扰。快照对外镜像仍是联合形状
	// （mergedCatalogLocked 逐格组合 + 按会话 ID 去重），内部不再存在单一
	// 全局数组。
	catalogGrid map[string][]model.SessionInfo
	// catalogWorkspaces / catalogTitles 以全局唯一 sessionID 为键：会话
	// 身份唯一，这两种映射天然不构成"合并数组"，不存在跨项目串写面；分格
	// 需求针对的是列表类缓存（catalogGrid）与其逐项目刷新。
	catalogWorkspaces  map[string]string
	catalogTitles      map[string]model.SessionTitle
	catalogWaiters     []catalogWaiter
	catalogStopped     bool
	sessionCatalogWake chan struct{}
	sessionCatalogStop chan struct{}
	sessionCatalogDone chan struct{}
	sessionCatalogOnce sync.Once
}

// catalogWaiter 是一次目录刷新请求的回执登记：done 在"覆盖本次请求项目
// 范围"的一轮刷新完成后关闭。projects == nil 表示全部项目（默认请求语义：
// 任何一轮全量刷新都覆盖它）。
type catalogWaiter struct {
	done     chan struct{}
	projects map[string]struct{}
}

// Location 是一次会话定位结果：workspace 绑定 + 目录元信息。
type Location struct {
	WorkspaceID string
	Workspace   *model.WorkspaceInfo
	Meta        model.SessionInfo
}

// NewCoordinator 构造会话域协调器；Tasks 由装配根注入
// （task_context.Coordinator 满足 TaskPersistencePort）。
func NewCoordinator(deps Deps) *Coordinator {
	return &Coordinator{
		Core:                 deps.Core,
		tasks:                deps.Tasks,
		view:                 deps.View,
		closed:               deps.Closed,
		transcriptTailBudget: deps.TranscriptTailBudget,
		isInternalContent:    deps.IsInternalContent,
		tailHistory:          deps.TailHistory,
		oversizedWarning:     deps.OversizedToolResultWarning,
		contentWarning:       deps.ContentReferenceWarning,
		limits:               deps.Limits,
		displayUserInput:     deps.DisplayUserInput,
		sessionRuntimeState: sessionRuntimeState{
			transition:         NewSessionTransitionManager(),
			catalogGrid:        make(map[string][]model.SessionInfo),
			catalogWorkspaces:  make(map[string]string),
			catalogTitles:      make(map[string]model.SessionTitle),
			sessionCatalogWake: make(chan struct{}, 1),
			sessionCatalogStop: make(chan struct{}),
			sessionCatalogDone: make(chan struct{}),
		},
	}
}

// SessionTitleFor 返回指定会话标题（G5：标题表由 catalogMu 保护，调用方
// 无需再为标题持 ViewMu；缺省回退活跃 Snapshot 名称——该回退读发生在
// 调用方的 ViewMu 上下文中，锁序 ViewMu → catalogMu，不反向）。
func (c *Coordinator) SessionTitleFor(sessionID string) model.SessionTitle {
	c.catalogMu.Lock()
	defer c.catalogMu.Unlock()
	if title, ok := c.catalogTitles[sessionID]; ok {
		return title
	}
	return model.SessionTitle{Value: c.Core.Snapshot.Session.Name, Source: "first_request"}
}

// SetSessionTitleLocked 设置指定会话标题（标题表由 catalogMu 保护；调用方
// 持有 ViewMu 时同样安全——锁序 ViewMu → catalogMu）。
func (c *Coordinator) SetSessionTitleLocked(sessionID string, title model.SessionTitle) {
	c.catalogMu.Lock()
	defer c.catalogMu.Unlock()
	if c.catalogTitles == nil {
		c.catalogTitles = make(map[string]model.SessionTitle)
	}
	c.catalogTitles[sessionID] = title
}

// UnloadSessionTitle 释放指定会话的标题（阶段 2 生命周期：unload 后重开走
// cold_load，标题由 record 重新装载）。
func (c *Coordinator) UnloadSessionTitle(sessionID string) {
	c.catalogMu.Lock()
	defer c.catalogMu.Unlock()
	delete(c.catalogTitles, sessionID)
}

// catalogTitleOf 返回标题表原始值（不回退活跃会话名；存档 record 用——
// 后台会话没有标题时不得借用视图会话名落盘）。
func (c *Coordinator) catalogTitleOf(sessionID string) model.SessionTitle {
	c.catalogMu.Lock()
	defer c.catalogMu.Unlock()
	return c.catalogTitles[sessionID]
}

// CatalogCache 返回目录 worker 最近一轮枚举结果的拷贝（catalogMu 保护；
// 观察/测试用，渲染数据仍以 Snapshot 镜像为准）。返回值为逐项目网格组合
// 后的联合视图：跨项目同 ID 重复条目（迁移/旧布局）取最近更新。
func (c *Coordinator) CatalogCache() ([]model.SessionInfo, map[string]string) {
	c.catalogMu.Lock()
	defer c.catalogMu.Unlock()
	sessions := mergedCatalogLocked(c.catalogGrid)
	workspaces := make(map[string]string, len(c.catalogWorkspaces))
	for sessionID, workspaceID := range c.catalogWorkspaces {
		workspaces[sessionID] = workspaceID
	}
	return sessions, workspaces
}

// mergedCatalogLocked 从分格网格组合联合目录视图（调用方持 catalogMu）：
// 逐项目展开 → 按会话 ID 去重（保留最近更新）→ 按 UpdatedAt 倒序。
func mergedCatalogLocked(grid map[string][]model.SessionInfo) []model.SessionInfo {
	selected := map[string]model.SessionInfo{}
	for _, projectID := range catalogProjectOrder(grid) {
		for _, info := range grid[projectID] {
			current, exists := selected[info.ID]
			if !exists || preferSessionInfo(info, current) {
				selected[info.ID] = info
			}
		}
	}
	sessions := make([]model.SessionInfo, 0, len(selected))
	for _, info := range selected {
		sessions = append(sessions, info)
	}
	sort.Slice(sessions, func(i, j int) bool {
		if sessions[i].UpdatedAt.Equal(sessions[j].UpdatedAt) {
			return sessions[i].ID < sessions[j].ID
		}
		return sessions[i].UpdatedAt.After(sessions[j].UpdatedAt)
	})
	return sessions
}

// catalogProjectOrder 返回网格的全部项目键（稳定顺序，避免测试/镜像抖动）。
func catalogProjectOrder(grid map[string][]model.SessionInfo) []string {
	projects := make([]string, 0, len(grid))
	for projectID := range grid {
		projects = append(projects, projectID)
	}
	sort.Strings(projects)
	return projects
}

// TransitionLock 返回指定 key 的会话过渡互斥（key=会话 ID：该会话生命
// 周期命令串行、不同会话并行；key=""：视图过渡保留 key，BeginNewSession/
// ResumeSession/BindWorkspace 等影响视图指针的根包跨域事务共用）。实现为
// per-key 显式 actor（无 mutex），见 transition_manager.go。
func (c *Coordinator) TransitionLock(key string) sync.Locker {
	return c.transition.Lock(key)
}

// BindView 注入 Snapshot revision bump 端口（装配根在 view 构造完成后调用；
// StartCatalogRefresh 之前必须绑定）。
func (c *Coordinator) BindView(view ViewPort) {
	c.view = view
}

// StartCatalogRefresh 启动会话目录刷新 worker：目录发现与标题恢复离开
// Snapshot 热路径，外部 SessionPort/WorkspacePort 调用在 worker 内完成，
// 只向快照发布内存态拷贝。
func (c *Coordinator) StartCatalogRefresh() {
	go func() {
		defer close(c.sessionCatalogDone)
		for {
			select {
			case <-c.sessionCatalogWake:
				c.runCatalogPasses()
			case <-c.sessionCatalogStop:
				c.stopCatalogWaiters()
				return
			}
		}
	}()
	c.RequestCatalogRefresh()
}

// RequestCatalogRefresh 非阻塞唤醒目录刷新 worker，并返回完成回执：某一轮
// 刷新（覆盖全部项目）在该请求登记之后开始并收尾（发布或因服务已关闭而
// 跳过）时回执关闭。
//
// 回执先登记再唤醒，所以唤醒撞上"槽位已满"被丢弃也不会丢请求：正在排空批次的
// 那一轮收尾时会看到新登记的回执并再刷一轮；已在等待的 worker 则被这次唤醒
// 直接唤起。worker 已停止时返回已关闭回执——关闭路径上不得有人因等目录而卡住。
func (c *Coordinator) RequestCatalogRefresh() <-chan struct{} {
	return c.registerCatalogRefresh(nil)
}

// RequestCatalogRefreshProject 请求只刷新指定项目（projectID="" = 默认/
// 未关联项目）并返回完成回执。G6 分格语义：其它项目的格子保留最近一轮结果，
// 不受本请求影响（跨项目枚举/状态过滤不串）；回执在该项目完成一轮覆盖本请求
// 的刷新后关闭。停止/回执语义与 RequestCatalogRefresh 相同。
func (c *Coordinator) RequestCatalogRefreshProject(projectID string) <-chan struct{} {
	return c.registerCatalogRefresh(map[string]struct{}{projectID: {}})
}

func (c *Coordinator) registerCatalogRefresh(projects map[string]struct{}) <-chan struct{} {
	c.catalogMu.Lock()
	if c.catalogStopped {
		c.catalogMu.Unlock()
		done := make(chan struct{})
		close(done)
		return done
	}
	waiter := catalogWaiter{done: make(chan struct{}), projects: projects}
	c.catalogWaiters = append(c.catalogWaiters, waiter)
	c.catalogMu.Unlock()
	select {
	case c.sessionCatalogWake <- struct{}{}:
	default:
	}
	return waiter.done
}

// runCatalogPasses 逐批排空回执：每批跑一轮刷新并在发布后关闭回执，批次为空才
// 回到等待。刷新期间登记的请求因此在同一次唤醒里继续被处理。
func (c *Coordinator) runCatalogPasses() {
	for {
		c.catalogMu.Lock()
		batch := c.catalogWaiters
		c.catalogWaiters = nil
		c.catalogMu.Unlock()
		if len(batch) == 0 {
			return
		}
		scope := unionCatalogScope(batch)
		c.refreshCatalogProjects(scope)
		for _, waiter := range batch {
			close(waiter.done)
		}
	}
}

// unionCatalogScope 合并一批回执的项目范围：任一请求覆盖全部项目（nil）
// → 整批按全量刷新（覆盖所有请求）；否则按各项目并集刷新一次即可覆盖全部
// 项目级请求。
func unionCatalogScope(batch []catalogWaiter) map[string]struct{} {
	scope := map[string]struct{}{}
	for _, waiter := range batch {
		if waiter.projects == nil {
			return nil
		}
		for projectID := range waiter.projects {
			scope[projectID] = struct{}{}
		}
	}
	if len(scope) == 0 {
		return nil
	}
	return scope
}

// stopCatalogWaiters 在 worker 退出路径上释放仍等待的回执：目录不再刷新，
// 等待者应立即收敛而不是等各自的超时。
func (c *Coordinator) stopCatalogWaiters() {
	c.catalogMu.Lock()
	batch := c.catalogWaiters
	c.catalogWaiters = nil
	c.catalogStopped = true
	c.catalogMu.Unlock()
	for _, waiter := range batch {
		close(waiter.done)
	}
}

// StopCatalogRefresh 关闭目录刷新 worker（有限等待，避免慢端口拖垮退出）。
func (c *Coordinator) StopCatalogRefresh() {
	c.sessionCatalogOnce.Do(func() {
		close(c.sessionCatalogStop)
		// SessionPort deliberately predates context-aware catalog reads. Do not
		// turn a slow external catalog operation into a GUI shutdown hang; the
		// worker checks c.closed before publishing and exits once the port
		// returns. Normal workers are still drained eagerly.
		select {
		case <-c.sessionCatalogDone:
		case <-time.After(sessionCatalogShutdownWait):
		}
		// 切换互斥 actor 一并收尾（契约：调用方保证无活跃持有者；
		// Shutdown 已取消运行中会话并等待收敛）。
		c.transition.Close()
	})
}

// CatalogRefreshDone 返回目录 worker 退出信号（测试/生命周期钩子：worker
// 停止后 channel 关闭）。
func (c *Coordinator) CatalogRefreshDone() <-chan struct{} {
	return c.sessionCatalogDone
}

// refreshCatalogProjects 按项目范围刷新目录：scope == nil 表示全部项目
// （整表替换）；否则只替换 scope 内项目的格子，其它项目保留上一轮结果。
// 枚举（外部 SessionPort/WorkspacePort I/O）在锁外完成；结果先落 catalogMu
// 保护的目录缓存（只换内存态），再取 ViewMu 短临界区发布 Snapshot 镜像。
func (c *Coordinator) refreshCatalogProjects(scope map[string]struct{}) {
	grid := make(map[string][]model.SessionInfo)
	discoveredBindings := map[string]string{}
	if granular, ok := c.Core.Deps.Sessions.(SessionGranularPort); ok {
		for _, projectID := range c.scopedProjectIDs(scope) {
			rows, bindings := c.sessionCatalogProject(granular, projectID)
			grid[projectID] = rows
			for sessionID, workspaceID := range bindings {
				discoveredBindings[sessionID] = workspaceID
			}
		}
	} else {
		// legacy 单列表端口没有项目维度：整体归入默认项目格（全量刷新语义）。
		grid[""] = c.Core.Deps.Sessions.List()
	}

	c.catalogMu.Lock()
	if c.catalogGrid == nil {
		c.catalogGrid = make(map[string][]model.SessionInfo)
	}
	if scope == nil {
		c.catalogGrid = grid
	} else {
		for projectID, rows := range grid {
			c.catalogGrid[projectID] = append([]model.SessionInfo(nil), rows...)
		}
	}
	if c.catalogWorkspaces == nil {
		c.catalogWorkspaces = make(map[string]string, len(discoveredBindings))
	}
	for sessionID, workspaceID := range discoveredBindings {
		c.catalogWorkspaces[sessionID] = workspaceID
	}
	c.catalogMu.Unlock()

	c.publishCatalogMirror()
}

// scopedProjectIDs 返回本次刷新的项目集合：scope == nil → 全部已知项目；
// 否则只返回 scope 内的项目（目录 worker 只为请求的项目做枚举 I/O）。
func (c *Coordinator) scopedProjectIDs(scope map[string]struct{}) []string {
	if scope == nil {
		return c.allProjectIDs()
	}
	projects := make([]string, 0, len(scope))
	for projectID := range scope {
		projects = append(projects, projectID)
	}
	sort.Strings(projects)
	return projects
}

// publishCatalogMirror 把分格目录组合成联合镜像发布进内核（锁内 bump →
// 锁外 Publish；与 refreshCatalogProjects 同调用栈，目录 worker 专用）。
func (c *Coordinator) publishCatalogMirror() {
	c.catalogMu.Lock()
	sessions := mergedCatalogLocked(c.catalogGrid)
	discoveredBindings := make(map[string]string, len(c.catalogWorkspaces))
	for sessionID, workspaceID := range c.catalogWorkspaces {
		discoveredBindings[sessionID] = workspaceID
	}
	c.catalogMu.Unlock()

	c.Core.ViewMu.Lock()
	if c.closed() {
		c.Core.ViewMu.Unlock()
		return
	}
	c.Core.Snapshot.Sessions = append([]model.SessionInfo(nil), sessions...)
	bindings := make(map[string]string, len(c.Core.Snapshot.SessionWorkspaces)+len(discoveredBindings))
	for sessionID, workspaceID := range c.Core.Snapshot.SessionWorkspaces {
		bindings[sessionID] = workspaceID
	}
	for sessionID, workspaceID := range discoveredBindings {
		bindings[sessionID] = workspaceID
	}
	c.Core.Snapshot.SessionWorkspaces = bindings
	if c.Core.Snapshot.Session.Name == "" {
		for _, session := range sessions {
			if session.ID == c.Core.Snapshot.Session.ID {
				c.Core.Snapshot.Session.Name = session.Name
				break
			}
		}
	}
	revision := c.view.BumpLocked()
	sessionID := c.Core.Snapshot.Session.ID
	c.Core.ViewMu.Unlock()
	if hub, ok := c.Core.Events.(event.SessionAwareHub); ok {
		hub.PublishSession(event.EventSnapshotChanged, revision, "", sessionID, nil)
	} else {
		c.Core.Events.Publish(event.EventSnapshotChanged, revision, "", nil)
	}
}
