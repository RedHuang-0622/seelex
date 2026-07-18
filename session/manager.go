// Package session 提供会话管理薄包装 — 直接使用 Seele 的 storage.Store
package session

import (
	"fmt"
	"sync"

	"github.com/RedHuang-0622/seelex/seelebridge"
)

type Store interface {
	List() []seelebridge.SessionMeta
	Delete(sessionID string) error
	Load(sessionID string) ([]seelebridge.Message, error)
	LoadRange(sessionID string, offset, limit int) ([]seelebridge.Message, int, error)
	MessageCount(sessionID string) (int, error)
}

// Manager 薄包装 Seele 的 storage.Store，提供 /new 和 /resume 能力
type Manager struct {
	store  Store
	mu     sync.Mutex
	saveFn func(sessionID string) error // 注入：保存当前会话到 store
	loadFn func(sessionID string) error // 注入：从 store 加载到 engine
}

func NewManager(store Store) *Manager {
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
func (m *Manager) List() []seelebridge.SessionMeta {
	return m.store.List()
}

// Delete 删除会话
func (m *Manager) Delete(sessionID string) error {
	return m.store.Delete(sessionID)
}

// LoadHistory 获取会话的全部历史消息（全量，用于 /resume 首次加载）。
func (m *Manager) LoadHistory(sessionID string) ([]seelebridge.Message, error) {
	return m.store.Load(sessionID)
}

// LoadHistoryRange 按偏移量窗口加载会话消息，返回 [offset, offset+limit) 范围内的消息和总数。
func (m *Manager) LoadHistoryRange(sessionID string, offset, limit int) ([]seelebridge.Message, int, error) {
	return m.store.LoadRange(sessionID, offset, limit)
}

// MessageCount 返回会话总消息数。
func (m *Manager) MessageCount(sessionID string) (int, error) {
	return m.store.MessageCount(sessionID)
}
