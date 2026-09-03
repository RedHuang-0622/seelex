package session_runtime

import (
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
	// transition 是"会话切换互斥"的显式 actor（无锁化：单 goroutine 持有
	// inFlight 状态，channel 命令；取代原 sync.Mutex，见 transition_actor.go）。
	transition *SessionTransitionActor
	// catalogMu 保护会话目录 worker 的三类内存态：目录缓存（最近一轮枚举
	// 结果）、会话标题表与刷新回执队列。G5：目录枚举/标题恢复走外部
	// SessionPort/WorkspacePort（阻塞 I/O），一律在锁外完成；catalogMu
	// 只护"换内存态"，发布镜像到 Snapshot 时另取 ViewMu 短临界区。
	// 锁序：ViewMu → catalogMu（目录 worker 从不反向持有）。
	catalogMu         sync.Mutex
	catalogSessions   []model.SessionInfo
	catalogWorkspaces map[string]string
	catalogTitles     map[string]model.SessionTitle
	catalogWaiters    []chan struct{}
	catalogStopped    bool
	sessionCatalogWake chan struct{}
	sessionCatalogStop chan struct{}
	sessionCatalogDone chan struct{}
	sessionCatalogOnce sync.Once
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
			transition:         NewSessionTransitionActor(),
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
// 观察/测试用，渲染数据仍以 Snapshot 镜像为准）。
func (c *Coordinator) CatalogCache() ([]model.SessionInfo, map[string]string) {
	c.catalogMu.Lock()
	defer c.catalogMu.Unlock()
	sessions := append([]model.SessionInfo(nil), c.catalogSessions...)
	workspaces := make(map[string]string, len(c.catalogWorkspaces))
	for sessionID, workspaceID := range c.catalogWorkspaces {
		workspaces[sessionID] = workspaceID
	}
	return sessions, workspaces
}

// TransitionLock 返回会话切换互斥（BeginNewSession/ResumeSession/
// BindWorkspace 等根包跨域事务共用）。实现为显式 actor（无 mutex）：
// inFlight 状态由单一 goroutine 持有，Acquire/Release 经 channel 命令。
func (c *Coordinator) TransitionLock() sync.Locker {
	return transitionLocker{actor: c.transition}
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
// 刷新在该请求登记之后开始并收尾（发布或因服务已关闭而跳过）时回执关闭。
//
// 回执先登记再唤醒，所以唤醒撞上"槽位已满"被丢弃也不会丢请求：正在排空批次的
// 那一轮收尾时会看到新登记的回执并再刷一轮；已在等待的 worker 则被这次唤醒
// 直接唤起。worker 已停止时返回已关闭回执——关闭路径上不得有人因等目录而卡住。
func (c *Coordinator) RequestCatalogRefresh() <-chan struct{} {
	c.catalogMu.Lock()
	if c.catalogStopped {
		c.catalogMu.Unlock()
		done := make(chan struct{})
		close(done)
		return done
	}
	done := make(chan struct{})
	c.catalogWaiters = append(c.catalogWaiters, done)
	c.catalogMu.Unlock()
	select {
	case c.sessionCatalogWake <- struct{}{}:
	default:
	}
	return done
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
		c.refreshCatalogCache()
		for _, done := range batch {
			close(done)
		}
	}
}

// stopCatalogWaiters 在 worker 退出路径上释放仍等待的回执：目录不再刷新，
// 等待者应立即收敛而不是等各自的超时。
func (c *Coordinator) stopCatalogWaiters() {
	c.catalogMu.Lock()
	batch := c.catalogWaiters
	c.catalogWaiters = nil
	c.catalogStopped = true
	c.catalogMu.Unlock()
	for _, done := range batch {
		close(done)
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

// refreshCatalogCache 把目录快照发布进内核（锁内 bump → 锁外 Publish）。
func (c *Coordinator) refreshCatalogCache() {
	sessions, discoveredBindings := c.sessionCatalog()
	// G5：worker 枚举（外部 SessionPort/WorkspacePort I/O）在锁外完成；结果
	// 先落 catalogMu 保护的目录缓存（只换内存态），再取 ViewMu 短临界区
	// 发布 Snapshot 镜像——目录刷新不再持有视图锁做 I/O，也不与视图写路径
	// 争用同一把锁。
	c.catalogMu.Lock()
	c.catalogSessions = append([]model.SessionInfo(nil), sessions...)
	if c.catalogWorkspaces == nil {
		c.catalogWorkspaces = make(map[string]string, len(discoveredBindings))
	}
	for sessionID, workspaceID := range discoveredBindings {
		c.catalogWorkspaces[sessionID] = workspaceID
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
