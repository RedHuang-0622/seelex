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
	sessionNameMu       sync.Mutex
	sessionTransitionMu sync.Mutex
	sessionNames        map[string]sessionNameCacheEntry
	// sessionTitles 是会话级标题表（阶段 0：标题 per-session，后台会话
	// 收尾不得读活跃会话标题；对应 code-review 5.4）。
	sessionTitles      map[string]model.SessionTitle
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

type sessionNameCacheEntry struct {
	updatedAt time.Time
	name      string
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
			sessionNames:       make(map[string]sessionNameCacheEntry),
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

// TransitionLock 返回会话切换互斥锁（BeginNewSession/ResumeSession/
// BindWorkspace 等根包跨域事务共用；锁所有权在会话域）。
func (c *Coordinator) TransitionLock() sync.Locker {
	return &c.sessionTransitionMu
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
				c.refreshCatalogCache()
			case <-c.sessionCatalogStop:
				return
			}
		}
	}()
	c.RequestCatalogRefresh()
}

// RequestCatalogRefresh 非阻塞唤醒目录刷新 worker。
func (c *Coordinator) RequestCatalogRefresh() {
	select {
	case c.sessionCatalogWake <- struct{}{}:
	default:
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
	c.Core.Mu.Unlock()
	c.Core.Events.Publish(event.EventSnapshotChanged, revision, "", nil)
}
