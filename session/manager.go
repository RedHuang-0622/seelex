// Package session 的 Manager 是会话装配薄壳（thin-wrapper 9.5 清理后）：
//   - Router 装配与 workspace 写作用域；
//   - SaveCurrent/Resume 回调注入（main.go 装配点）；
//   - 存储桥与 workspace 粒度旧口已删除——持久化一律经
//     sessionstore.SessionGranularStore（会话粒度五片）。
package session

import (
	"context"
	"fmt"
	"sync"

	"github.com/RedHuang-0622/seelex/sessionstore"
)

// Manager 是会话装配薄壳：持有 Router（物理布局）、workspace 写作用域与
// 保存/恢复回调。不再承担任何会话数据读写（存储归 SessionGranularStore）。
type Manager struct {
	router *sessionstore.Router
	mu     sync.Mutex
	saveFn func(sessionID string) error // 注入：保存当前会话到 store
	loadFn func(sessionID string) error // 注入：从 store 加载到 engine
}

// NewManager 构造空装配薄壳（Router 经 WithRouter 装配）。
func NewManager() *Manager {
	return &Manager{}
}

// WithRouter 安装会话存储路由（原子、可配置仓库；生产装配点）。
func (m *Manager) WithRouter(router *sessionstore.Router) *Manager {
	m.router = router
	return m
}

// Router 返回底层存储路由（context 模块装配等用途；未装配时 nil）。
func (m *Manager) Router() *sessionstore.Router {
	return m.router
}

// SetWorkspace 设置当前 workspace 写作用域。
func (m *Manager) SetWorkspace(workspaceID string) {
	if m.router != nil {
		m.router.SetWorkspace(workspaceID)
	}
}

// Workspace 返回当前 workspace 写作用域。
func (m *Manager) Workspace() string {
	if m.router != nil {
		return m.router.Workspace()
	}
	return ""
}

func (m *Manager) StorageConfig() (sessionstore.Config, error) {
	if m.router == nil {
		return sessionstore.Config{}, fmt.Errorf("session: configurable storage is unavailable")
	}
	return m.router.Config(), nil
}

func (m *Manager) TestStorage(ctx context.Context, config sessionstore.Config) error {
	if m.router == nil {
		return fmt.Errorf("session: configurable storage is unavailable")
	}
	return m.router.Test(ctx, config)
}

func (m *Manager) ConfigureStorage(ctx context.Context, config sessionstore.Config) error {
	if m.router == nil {
		return fmt.Errorf("session: configurable storage is unavailable")
	}
	return m.router.Configure(ctx, config)
}

// InjectSaveLoad 注入保存/加载回调（由 main.go 装配时传入）。
func (m *Manager) InjectSaveLoad(saveFn, loadFn func(sessionID string) error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.saveFn = saveFn
	m.loadFn = loadFn
}

// SaveCurrent 持久化当前会话。
func (m *Manager) SaveCurrent(sessionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.saveFn == nil {
		return fmt.Errorf("session: saveFn not injected")
	}
	return m.saveFn(sessionID)
}

// Resume 恢复历史会话。
func (m *Manager) Resume(sessionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.loadFn == nil {
		return fmt.Errorf("session: loadFn not injected")
	}
	return m.loadFn(sessionID)
}
