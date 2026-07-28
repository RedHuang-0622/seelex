// Package session 提供会话管理薄包装 — 直接使用 Seele 的 storage.Store
package session

import (
	"context"
	"fmt"
	"sync"

	"github.com/RedHuang-0622/seelex/seelebridge"
	"github.com/RedHuang-0622/seelex/sessionstore"
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
	store       Store
	nestedStore *seelebridge.NestedSessionStore // optional workspace-aware store
	router      *sessionstore.Router
	mu          sync.Mutex
	saveFn      func(sessionID string) error // 注入：保存当前会话到 store
	loadFn      func(sessionID string) error // 注入：从 store 加载到 engine
}

func NewManager(store Store) *Manager {
	return &Manager{store: store}
}

// WithNestedStore attaches a workspace-aware nested store for routing.
func (m *Manager) WithNestedStore(ns *seelebridge.NestedSessionStore) {
	m.nestedStore = ns
}

// WithRouter installs the atomic, configurable repository used by production.
// The legacy nested store remains supported for compatibility with old callers.
func (m *Manager) WithRouter(router *sessionstore.Router) {
	m.router = router
	m.store = router
}

// SetWorkspace sets the active workspace for session routing.
func (m *Manager) SetWorkspace(workspaceID string) {
	if m.router != nil {
		m.router.SetWorkspace(workspaceID)
		return
	}
	if m.nestedStore != nil {
		m.nestedStore.SetWorkspace(workspaceID)
	}
}

// Workspace returns the currently active workspace ID.
func (m *Manager) Workspace() string {
	if m.router != nil {
		return m.router.Workspace()
	}
	if m.nestedStore != nil {
		return m.nestedStore.Workspace()
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

// ListByWorkspace lists sessions stored under a specific workspace.
func (m *Manager) ListByWorkspace(workspaceID string) []seelebridge.SessionMeta {
	if m.router != nil {
		return m.router.ListWorkspace(workspaceID)
	}
	if m.nestedStore != nil {
		return m.nestedStore.ListByWorkspace(workspaceID)
	}
	if workspaceID == m.Workspace() {
		return m.store.List()
	}
	return []seelebridge.SessionMeta{}
}

// LoadHistoryByWorkspace reads a session from an explicit workspace without
// changing the active workspace used by subsequent writes.
func (m *Manager) LoadHistoryByWorkspace(workspaceID, sessionID string) ([]seelebridge.Message, error) {
	if m.router != nil {
		return m.router.LoadWorkspace(workspaceID, sessionID)
	}
	if workspaceID != m.Workspace() {
		return nil, fmt.Errorf("session: explicit workspace reads require the configurable router")
	}
	return m.store.Load(sessionID)
}

// LoadHistoryRangeByWorkspace reads a history window from an explicit
// workspace without changing the active workspace used by subsequent writes.
func (m *Manager) LoadHistoryRangeByWorkspace(workspaceID, sessionID string, offset, limit int) ([]seelebridge.Message, int, error) {
	if m.router != nil {
		return m.router.LoadRangeWorkspace(workspaceID, sessionID, offset, limit)
	}
	if workspaceID != m.Workspace() {
		return nil, 0, fmt.Errorf("session: explicit workspace reads require the configurable router")
	}
	return m.store.LoadRange(sessionID, offset, limit)
}

// DeleteByWorkspace deletes a session from an explicit workspace without
// changing the active workspace used by subsequent writes.
func (m *Manager) DeleteByWorkspace(workspaceID, sessionID string) error {
	if m.router != nil {
		return m.router.DeleteWorkspace(workspaceID, sessionID)
	}
	if workspaceID != m.Workspace() {
		return fmt.Errorf("session: explicit workspace deletes require the configurable router")
	}
	return m.store.Delete(sessionID)
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
