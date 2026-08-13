// Package mcp 承载 Seelex 的 MCP 服务器生命周期：provider 懒创建、breaker
// 事件通道、lazy 冷启动登记、attach/detach/refresh、工具重挂载。域内不依赖
// seelebridge 根包；工具注册表经 RegistryPort 接口注入。
package mcp

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/RedHuang-0622/Seele/tools"
	frameworkmcp "github.com/RedHuang-0622/Seele/tools/mcp"
	"github.com/RedHuang-0622/seelex/mcpstack"
)

// Server is the transport-neutral MCP configuration consumed by Seelex.
type Server struct {
	Name      string
	Transport string
	Command   string
	Args      []string
	Env       []string
	URL       string
}

// RegistryPort 是 Manager 需要的工具注册表表面（重挂载 provider 用）。
type RegistryPort interface {
	Unregister(name string) error
	Register(provider tools.ToolProvider) error
}

// Manager 持有 MCP provider 生命周期状态（懒创建、线程安全）。
type Manager struct {
	provider *frameworkmcp.Provider
	breaker  *breakerState
	stack    *mcpstack.MCPStack
	registry RegistryPort

	lazyMu sync.RWMutex
	lazy   map[string]Server

	closeMu sync.Mutex
	closed  bool
	wg      sync.WaitGroup // 熔断事件监听 goroutine
}

// breakerState 是 breaker 事件通道（instance-level）。
type breakerState struct {
	ch   chan frameworkmcp.BreakerEvent
	once sync.Once
}

// NewManager 构造 MCP 管理器。stack 记录 breaker 事件；registry 重挂载工具。
func NewManager(stack *mcpstack.MCPStack, registry RegistryPort) *Manager {
	return &Manager{stack: stack, registry: registry}
}

// Provider 返回 MCP 工具提供者（tools/mcp，实现 tools.ToolProvider），懒创建。
func (m *Manager) Provider() *frameworkmcp.Provider {
	if m.provider == nil {
		m.provider = frameworkmcp.NewProvider()
	}
	return m.provider
}

// BreakerEvents returns a read-only channel of breaker events.
// The consumer (mcpstack.ListenBreaker) runs automatically when Attach is called.
func (m *Manager) BreakerEvents() <-chan frameworkmcp.BreakerEvent {
	if m.breaker == nil {
		m.breaker = &breakerState{}
	}
	m.breaker.once.Do(func() {
		m.breaker.ch = make(chan frameworkmcp.BreakerEvent, 64)
		m.Provider().SetBreakerEventsChannel(m.breaker.ch)
	})
	return m.breaker.ch
}

// Attach connects and registers a new MCP server.
// Automatically:
//  1. Initializes the breaker events channel
//  2. Starts mcpstack.ListenBreaker to record breaker events into MCPStack
//  3. Refreshes tool list so new tools are visible
func (m *Manager) Attach(ctx context.Context, cfg Server) error {
	if m.isClosed() {
		return fmt.Errorf("seelebridge: MCP manager is closed")
	}
	provider := m.Provider()
	frameworkCfg, err := ToFramework(cfg)
	if err != nil {
		return err
	}

	// Ensure breaker events channel + listener are active
	ch := m.BreakerEvents()
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		mcpstack.ListenBreaker(m.stack, ch)
	}()

	if err := provider.Attach(ctx, frameworkCfg); err != nil {
		return fmt.Errorf("seelebridge: attach MCP %q: %w", cfg.Name, err)
	}
	m.refreshTools(provider)
	return nil
}

func (m *Manager) isClosed() bool {
	m.closeMu.Lock()
	defer m.closeMu.Unlock()
	return m.closed
}

// Close 关闭 MCP 管理器：停掉熔断事件监听（close(ch) 使 ListenBreaker
// range 退出）并等待 goroutine 结束。幂等。注意：Close 后 Attach 返回错误；
// breaker 通道关闭后 provider 若再有熔断事件发送会 panic——Close 只应在
// Runtime Shutdown 终局路径调用。
func (m *Manager) Close() {
	m.closeMu.Lock()
	if m.closed {
		m.closeMu.Unlock()
		return
	}
	m.closed = true
	ch := (*chan frameworkmcp.BreakerEvent)(nil)
	if m.breaker != nil {
		ch = &m.breaker.ch
	}
	m.closeMu.Unlock()
	if ch != nil && *ch != nil {
		close(*ch)
	}
	m.wg.Wait()
}

// AttachServer 是 plugin 域使用的展开入参版 Attach。
func (m *Manager) AttachServer(
	ctx context.Context,
	name, transport, command string,
	args, env []string,
	url string,
) error {
	return m.Attach(ctx, Server{
		Name: name, Transport: transport, Command: command,
		Args: args, Env: env, URL: url,
	})
}

// RegisterLazy 登记 MCP 服务器配置但不连接（冷启动：启动路径零 MCP 进程）。
// 配置校验与 Attach 一致（name/transport/command 契约）；首次需要时经
// Load 幂等连接并注册工具。重复登记同名 server 覆盖配置。
func (m *Manager) RegisterLazy(name string, cfg Server) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("seelebridge: MCP name is empty")
	}
	cfg.Name = name
	if _, err := ToFramework(cfg); err != nil {
		return err
	}
	m.lazyMu.Lock()
	defer m.lazyMu.Unlock()
	if m.lazy == nil {
		m.lazy = make(map[string]Server)
	}
	m.lazy[name] = cfg
	return nil
}

// LazyNames 返回已登记但尚未连接的 MCP 服务器名（按字典序）。
func (m *Manager) LazyNames() []string {
	m.lazyMu.RLock()
	defer m.lazyMu.RUnlock()
	names := make([]string, 0, len(m.lazy))
	for name := range m.lazy {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Load 按需连接已登记的 MCP 服务器（冷启动加载点）：
// 已附加 → 返回现有工具数（幂等）；未附加 → attach + tools/list 注册工具。
// 未知 server 返回显式错误；失败不破坏登记（可重试）。
func (m *Manager) Load(ctx context.Context, name string) (int, error) {
	name = strings.TrimSpace(name)
	m.lazyMu.RLock()
	cfg, ok := m.lazy[name]
	m.lazyMu.RUnlock()
	if !ok {
		return 0, fmt.Errorf("seelebridge: unknown MCP server %q (registered: %v)", name, m.LazyNames())
	}
	if alive, toolsCount, _ := m.Status(name); alive {
		return toolsCount, nil
	}
	if err := m.Attach(ctx, cfg); err != nil {
		return 0, err
	}
	_, toolsCount, _ := m.Status(name)
	return toolsCount, nil
}

// Detach 断开并注销 MCP 服务器。
func (m *Manager) Detach(name string) error {
	provider := m.Provider()
	provider.Detach(name)
	m.refreshTools(provider)
	return nil
}

// Refresh 刷新指定 MCP 服务器的工具列表。
func (m *Manager) Refresh(ctx context.Context, name string) error {
	provider := m.Provider()
	if err := provider.RefreshTools(ctx, name); err != nil {
		return fmt.Errorf("seelebridge: refresh MCP %q: %w", name, err)
	}
	m.refreshTools(provider)
	return nil
}

// Names 返回已连接 MCP 服务器名（按字典序）。
func (m *Manager) Names() []string {
	names := m.Provider().ServerNames()
	sort.Strings(names)
	return names
}

// IsAlive 轻量 ping 检查 MCP 服务器是否存活。
func (m *Manager) IsAlive(name string) bool {
	return m.Provider().IsAlive(name)
}

// Status 返回 MCP 服务器健康状态（alive + tool count + error）。
func (m *Manager) Status(name string) (alive bool, toolsCount int, err error) {
	return m.Provider().ServerStatus(name)
}

// RefreshTools 重新挂载 MCP provider 到注册表，重建可见工具快照（幂等）。
func (m *Manager) RefreshTools() {
	m.refreshTools(m.Provider())
}

// refreshTools 重新挂载 MCP provider 到注册表，重建可见工具快照。
func (m *Manager) refreshTools(provider *frameworkmcp.Provider) {
	if m.registry == nil {
		return
	}
	_ = m.registry.Unregister(provider.ProviderName())
	_ = m.registry.Register(provider)
}

// ToFramework 把 Seelex 的传输中立配置转换为框架 ServerConfig（校验契约）。
func ToFramework(cfg Server) (frameworkmcp.ServerConfig, error) {
	cfg.Name = strings.TrimSpace(cfg.Name)
	if cfg.Name == "" {
		return frameworkmcp.ServerConfig{}, fmt.Errorf("seelebridge: MCP name is empty")
	}
	transport := strings.ToLower(strings.TrimSpace(cfg.Transport))
	if transport == "" {
		switch {
		case cfg.Command != "":
			transport = "stdio"
		case cfg.URL != "":
			transport = "sse"
		}
	}
	if transport == "stdio" && strings.TrimSpace(cfg.Command) == "" {
		return frameworkmcp.ServerConfig{}, fmt.Errorf("seelebridge: MCP %q requires command", cfg.Name)
	}
	if transport == "sse" && strings.TrimSpace(cfg.URL) == "" {
		return frameworkmcp.ServerConfig{}, fmt.Errorf("seelebridge: MCP %q requires URL", cfg.Name)
	}
	if transport != "stdio" && transport != "sse" {
		return frameworkmcp.ServerConfig{}, fmt.Errorf("seelebridge: MCP %q unsupported transport %q", cfg.Name, transport)
	}
	return frameworkmcp.ServerConfig{
		Name:      cfg.Name,
		Transport: transport,
		Command:   cfg.Command,
		Args:      append([]string(nil), cfg.Args...),
		Env:       append([]string(nil), cfg.Env...),
		URL:       cfg.URL,
	}, nil
}
