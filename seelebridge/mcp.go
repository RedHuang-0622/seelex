package seelebridge

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	frameworkmcp "github.com/RedHuang-0622/Seele/agent/core/tool/mcp"
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

// BreakerEvents returns a read-only channel of breaker events.
// The consumer (mcpstack.ListenBreaker) runs automatically when AttachMCP is called.
func (r *Runtime) BreakerEvents() <-chan frameworkmcp.BreakerEvent {
	if r.breaker == nil {
		r.breaker = &breakerState{}
	}
	r.breaker.once.Do(func() {
		r.breaker.ch = make(chan frameworkmcp.BreakerEvent, 64)
		r.agent.MCP().SetBreakerEventsChannel(r.breaker.ch)
	})
	return r.breaker.ch
}

// AttachMCP connects and registers a new MCP server.
// Automatically:
//  1. Initializes the breaker events channel
//  2. Starts mcpstack.ListenBreaker to record breaker events into MCPStack
//  3. Refreshes tool list so new tools are visible
func (r *Runtime) AttachMCP(ctx context.Context, cfg MCPServer) error {
	provider := r.agent.MCP()
	if provider == nil {
		return fmt.Errorf("seelebridge: MCP provider is unavailable")
	}
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

func (r *Runtime) DetachMCP(name string) error {
	provider := r.agent.MCP()
	if provider == nil {
		return fmt.Errorf("seelebridge: MCP provider is unavailable")
	}
	provider.Detach(name)
	r.refreshMCPTools(provider)
	return nil
}

func (r *Runtime) RefreshMCP(ctx context.Context, name string) error {
	provider := r.agent.MCP()
	if provider == nil {
		return fmt.Errorf("seelebridge: MCP provider is unavailable")
	}
	if err := provider.RefreshTools(ctx, name); err != nil {
		return fmt.Errorf("seelebridge: refresh MCP %q: %w", name, err)
	}
	r.refreshMCPTools(provider)
	return nil
}

func (r *Runtime) MCPServerNames() []string {
	provider := r.agent.MCP()
	if provider == nil {
		return nil
	}
	names := provider.ServerNames()
	sort.Strings(names)
	return names
}

// IsMCPAlive 轻量 ping 检查 MCP 服务器是否存活（2s 超时）。
func (r *Runtime) IsMCPAlive(name string) bool {
	provider := r.agent.MCP()
	if provider == nil {
		return false
	}
	return provider.IsAlive(name)
}

// MCPServerStatus 返回 MCP 服务器健康状态（alive + tool count + error）。
func (r *Runtime) MCPServerStatus(name string) (alive bool, tools int, err error) {
	provider := r.agent.MCP()
	if provider == nil {
		return false, 0, fmt.Errorf("seelebridge: MCP provider is unavailable")
	}
	return provider.ServerStatus(name)
}

func (r *Runtime) refreshMCPTools(provider *frameworkmcp.Provider) {
	tools := r.agent.Tools()
	tools.Unregister(provider.ProviderName())
	tools.Register(provider)
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
