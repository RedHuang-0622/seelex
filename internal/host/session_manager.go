package host

// session_manager.go —— 会话装配薄壳（原 `session.Manager`，2026-10-17 从
// session 域移到装配层）：持有 Router（物理布局）、workspace 写作用域与
// 保存/恢复回调。会话域（`session/`）因此不再 import store，可迁入中间层。
//
// 存储桥与 workspace 粒度旧口已删除——持久化一律经
// store.SessionGranularStore（会话粒度五片）。

import (
	"fmt"
	"sync"

	"github.com/RedHuang-0622/seelex/store"
)

// SessionManager 是会话装配薄壳：持有 Router、workspace 写作用域与
// 保存/恢复回调。不承担任何会话数据读写（存储归 SessionGranularStore）。
type SessionManager struct {
	router *store.Router
	mu     sync.Mutex
	saveFn func(sessionID string) error // 注入：保存当前会话到 store
	loadFn func(sessionID string) error // 注入：从 store 加载到 engine
}

// NewSessionManager 构造空装配薄壳（Router 经 WithRouter 装配）。
func NewSessionManager() *SessionManager { return &SessionManager{} }

// WithRouter 安装会话存储路由（原子、可配置仓库；生产装配点）。
func (m *SessionManager) WithRouter(router *store.Router) *SessionManager {
	m.router = router
	return m
}

// Router 返回底层存储路由（context 模块装配等用途；未装配时 nil）。
func (m *SessionManager) Router() *store.Router { return m.router }

// SetWorkspace 设置当前 workspace 写作用域。
func (m *SessionManager) SetWorkspace(workspaceID string) {
	if m.router != nil {
		m.router.SetWorkspace(workspaceID)
	}
}

// Workspace 返回当前 workspace 写作用域。
func (m *SessionManager) Workspace() string {
	if m.router != nil {
		return m.router.Workspace()
	}
	return ""
}

// InjectSaveLoad 注入保存/加载回调（由 boot 装配时传入）。
func (m *SessionManager) InjectSaveLoad(saveFn, loadFn func(sessionID string) error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.saveFn = saveFn
	m.loadFn = loadFn
}

// SaveCommitWorkspace 在显式项目作用域下把一个 commit 追加进会话存储
// （append-only 通道；不改变 Router 的活跃写作用域）。
func (m *SessionManager) SaveCommitWorkspace(projectID, sessionID string, commit store.Commit) error {
	if m == nil || m.router == nil {
		return fmt.Errorf("session: commit storage is unavailable (router not attached)")
	}
	return m.router.SaveCommitWorkspace(projectID, sessionID, commit)
}

// SaveCurrent 持久化当前会话。
func (m *SessionManager) SaveCurrent(sessionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.saveFn == nil {
		return fmt.Errorf("session: saveFn not injected")
	}
	return m.saveFn(sessionID)
}

// Resume 恢复历史会话。
func (m *SessionManager) Resume(sessionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.loadFn == nil {
		return fmt.Errorf("session: loadFn not injected")
	}
	return m.loadFn(sessionID)
}
