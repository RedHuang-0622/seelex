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
	// sessionTitles 是会话级标题表（阶段 0：标题 per-session，后台会话
	// 收尾不得读活跃会话标题；对应 code-review 5.4）。
	sessionTitles      map[string]model.SessionTitle
	sessionCatalogWake chan struct{}
	sessionCatalogStop chan struct{}
	sessionCatalogDone chan struct{}
	sessionCatalogOnce sync.Once
	// catalogMu 保护"等待某轮目录刷新完成"的回执队列（C3）：worker 每轮开始时
	// 取走当前批次，发布后统一关闭，因此登记方无需持有 worker 的调度权。
	catalogMu      sync.Mutex
	catalogWaiters []chan struct{}
	catalogStopped bool
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
			sessionTitles:      make(map[string]model.SessionTitle),
			sessionCatalogWake: make(chan struct{}, 1),
			sessionCatalogStop: make(chan struct{}),
			sessionCatalogDone: make(chan struct{}),
		},
	}
}

// SessionTitleFor 返回指定会话标题（调用方持有 Core.Mu；缺省回退活跃
// Snapshot 名称）。
func (c *Coordinator) SessionTitleFor(sessionID string) model.SessionTitle {
	if title, ok := c.sessionTitles[sessionID]; ok {
		return title
	}
	return model.SessionTitle{Value: c.Core.Snapshot.Session.Name, Source: "first_request"}
}

// SetSessionTitleLocked 设置指定会话标题（调用方持有 Core.Mu）。
func (c *Coordinator) SetSessionTitleLocked(sessionID string, title model.SessionTitle) {
	if c.sessionTitles == nil {
		c.sessionTitles = make(map[string]model.SessionTitle)
	}
	c.sessionTitles[sessionID] = title
}

// UnloadSessionTitle 释放指定会话的标题（阶段 2 生命周期：unload 后重开走
// cold_load，标题由 record 重新装载）。
func (c *Coordinator) UnloadSessionTitle(sessionID string) {
	delete(c.sessionTitles, sessionID)
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
	c.Core.Mu.Lock()
	if c.closed() {
		c.Core.Mu.Unlock()
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
	c.Core.Mu.Unlock()
	if hub, ok := c.Core.Events.(event.SessionAwareHub); ok {
		hub.PublishSession(event.EventSnapshotChanged, revision, "", sessionID, nil)
	} else {
		c.Core.Events.Publish(event.EventSnapshotChanged, revision, "", nil)
	}
}
