// Package session 提供会话管理薄包装 — 直接使用 Seele 的 storage.Store
package session

import (
	"fmt"
	"sync"

	"github.com/RedHuang-0622/Seele/seelectx/storage"
)

// Manager 薄包装 Seele 的 storage.Store，提供 /new 和 /resume 能力
type Manager struct {
	store   *storage.Store
	mu      sync.Mutex
	saveFn  func(sessionID string) error // 注入：保存当前会话到 store
	loadFn  func(sessionID string) error // 注入：从 store 加载到 engine
}

func NewManager(store *storage.Store) *Manager {
	return &Manager{store: store}
}

// InjectSaveLoad 注入保存/加载回调（由 main.go 装配时传入）
func (m *Manager) InjectSaveLoad(saveFn, loadFn func(sessionID string) error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.saveFn = saveFn
	m.loadFn = loadFn
}

// SaveCurrent 持久化当前会话
func (m *Manager) SaveCurrent(sessionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.saveFn == nil {
		return fmt.Errorf("session: saveFn not injected")
	}
	return m.saveFn(sessionID)
}

// Resume 恢复历史会话
func (m *Manager) Resume(sessionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.loadFn == nil {
		return fmt.Errorf("session: loadFn not injected")
	}
	return m.loadFn(sessionID)
}

// List 列出所有持久化会话
func (m *Manager) List() []storage.SessionMeta {
	return m.store.List()
}

// Delete 删除会话
func (m *Manager) Delete(sessionID string) error {
	return m.store.Delete(sessionID)
}
