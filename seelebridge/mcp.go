package seelebridge

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	frameworkmcp "github.com/RedHuang-0622/Seele/tools/mcp"
	"github.com/RedHuang-0622/seelex/mcpstack"
)

// MCPServer is the transport-neutral MCP configuration consumed by Seelex.
type MCPServer struct {
	Name      string
	Transport string
	Command   string
	Args      []string
	Env       []string
	URL       string
}

// breaker state (instance-level)
type breakerState struct {
	ch   chan frameworkmcp.BreakerEvent
	once sync.Once
}

// mcp 返回 MCP 工具提供者（tools/mcp，实现 tools.ToolProvider）。
func (r *Runtime) mcp() *frameworkmcp.Provider {
	if r.mcpProvider == nil {
		r.mcpProvider = frameworkmcp.NewProvider()
	}
	return r.mcpProvider
}

// BreakerEvents returns a read-only channel of breaker events.
// The consumer (mcpstack.ListenBreaker) runs automatically when AttachMCP is called.
func (r *Runtime) BreakerEvents() <-chan frameworkmcp.BreakerEvent {
	if r.breaker == nil {
		r.breaker = &breakerState{}
	}
	r.breaker.once.Do(func() {
		r.breaker.ch = make(chan frameworkmcp.BreakerEvent, 64)
		r.mcp().SetBreakerEventsChannel(r.breaker.ch)
	})
	return r.breaker.ch
}

// AttachMCP connects and registers a new MCP server.
// Automatically:
//  1. Initializes the breaker events channel
//  2. Starts mcpstack.ListenBreaker to record breaker events into MCPStack
//  3. Refreshes tool list so new tools are visible
func (r *Runtime) AttachMCP(ctx context.Context, cfg MCPServer) error {
	provider := r.mcp()
	frameworkCfg, err := toFrameworkMCP(cfg)
	if err != nil {
		return err
	}

	// Ensure breaker events channel + listener are active
	ch := r.BreakerEvents()
	go mcpstack.ListenBreaker(r.MCPStack, ch)

	if err := provider.Attach(ctx, frameworkCfg); err != nil {
		return fmt.Errorf("seelebridge: attach MCP %q: %w", cfg.Name, err)
	}
	r.refreshMCPTools(provider)
	return nil
}

func (r *Runtime) AttachMCPServer(
	ctx context.Context,
	name, transport, command string,
	args, env []string,
	url string,
) error {
	return r.AttachMCP(ctx, MCPServer{
		Name: name, Transport: transport, Command: command,
		Args: args, Env: env, URL: url,
	})
}

// RegisterLazyMCP 登记 MCP 服务器配置但不连接（冷启动：启动路径零 MCP 进程）。
// 配置校验与 AttachMCP 一致（name/transport/command 契约）；首次需要时经
// LoadMCP 幂等连接并注册工具。重复登记同名 server 覆盖配置。
func (r *Runtime) RegisterLazyMCP(name string, cfg MCPServer) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("seelebridge: MCP name is empty")
	}
	cfg.Name = name
	if _, err := toFrameworkMCP(cfg); err != nil {
		return err
	}
	r.lazyMCPServerMu.Lock()
	defer r.lazyMCPServerMu.Unlock()
	if r.lazyMCPServers == nil {
		r.lazyMCPServers = make(map[string]MCPServer)
	}
	r.lazyMCPServers[name] = cfg
	return nil
}

// LazyMCPServerNames 返回已登记但尚未连接的 MCP 服务器名（按字典序）。
func (r *Runtime) LazyMCPServerNames() []string {
	r.lazyMCPServerMu.RLock()
	defer r.lazyMCPServerMu.RUnlock()
	names := make([]string, 0, len(r.lazyMCPServers))
	for name := range r.lazyMCPServers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// LoadMCP 按需连接已登记的 MCP 服务器（冷启动加载点）：
// 已附加 → 返回现有工具数（幂等）；未附加 → attach + tools/list 注册工具。
// 未知 server 返回显式错误；失败不破坏登记（可重试）。
func (r *Runtime) LoadMCP(ctx context.Context, name string) (int, error) {
	name = strings.TrimSpace(name)
	r.lazyMCPServerMu.RLock()
	cfg, ok := r.lazyMCPServers[name]
	r.lazyMCPServerMu.RUnlock()
	if !ok {
		return 0, fmt.Errorf("seelebridge: unknown MCP server %q (registered: %v)", name, r.LazyMCPServerNames())
	}
	if alive, tools, _ := r.MCPServerStatus(name); alive {
		return tools, nil
	}
	if err := r.AttachMCP(ctx, cfg); err != nil {
		return 0, err
	}
	_, tools, _ := r.MCPServerStatus(name)
	return tools, nil
}

func (r *Runtime) DetachMCP(name string) error {
	provider := r.mcp()
	provider.Detach(name)
	r.refreshMCPTools(provider)
	return nil
}

func (r *Runtime) RefreshMCP(ctx context.Context, name string) error {
	provider := r.mcp()
	if err := provider.RefreshTools(ctx, name); err != nil {
		return fmt.Errorf("seelebridge: refresh MCP %q: %w", name, err)
	}
	r.refreshMCPTools(provider)
	return nil
}

func (r *Runtime) MCPServerNames() []string {
	names := r.mcp().ServerNames()
	sort.Strings(names)
	return names
}

// IsMCPAlive 轻量 ping 检查 MCP 服务器是否存活（2s 超时）。
func (r *Runtime) IsMCPAlive(name string) bool {
	return r.mcp().IsAlive(name)
}

// MCPServerStatus 返回 MCP 服务器健康状态（alive + tool count + error）。
func (r *Runtime) MCPServerStatus(name string) (alive bool, tools int, err error) {
	return r.mcp().ServerStatus(name)
}

// refreshMCPTools 重新挂载 MCP provider 到注册表，重建可见工具快照。
func (r *Runtime) refreshMCPTools(provider *frameworkmcp.Provider) {
	if r.registry == nil || r.registry.registry == nil {
		return
	}
	_ = r.registry.registry.Unregister(provider.ProviderName())
	_ = r.registry.registry.Register(provider)
}

func toFrameworkMCP(cfg MCPServer) (frameworkmcp.ServerConfig, error) {
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
		return frameworkmcp.ServerConfig{}, fmt.Errorf("seelebridge: MCP %q has invalid transport %q", cfg.Name, transport)
	}
	return frameworkmcp.ServerConfig{
		Name: cfg.Name, Transport: transport, Command: cfg.Command,
		Args: append([]string(nil), cfg.Args...), Env: append([]string(nil), cfg.Env...), URL: cfg.URL,
	}, nil
}

// Compile-time check: ensure *Runtime is used for the unexported breaker field.
var _ = (*Runtime).BreakerEvents
