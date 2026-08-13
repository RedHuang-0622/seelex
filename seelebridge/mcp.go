package seelebridge

import (
	"context"

	"github.com/RedHuang-0622/Seele/tools"
	frameworkmcp "github.com/RedHuang-0622/Seele/tools/mcp"
	"github.com/RedHuang-0622/seelex/seelebridge/mcp"
)

// MCP 服务器生命周期域已迁入 seelebridge/mcp（Manager：provider 懒创建、
// breaker 事件通道、lazy 冷启动登记、attach/detach/refresh、工具重挂载）。
// 本文件只保留类型别名与 Runtime 委托。

// MCPServer is the transport-neutral MCP configuration consumed by Seelex.
type MCPServer = mcp.Server

// mcpRegistryAdapter 把 Runtime 工具注册表适配为 mcp.RegistryPort
// （懒解析：注册表在 NewRuntime 中稍后才装配，调用时判空）。
type mcpRegistryAdapter struct{ runtime *Runtime }

func (a mcpRegistryAdapter) Unregister(name string) error {
	if a.runtime == nil || a.runtime.registry == nil || a.runtime.registry.registry == nil {
		return nil
	}
	return a.runtime.registry.registry.Unregister(name)
}

func (a mcpRegistryAdapter) Register(provider tools.ToolProvider) error {
	if a.runtime == nil || a.runtime.registry == nil || a.runtime.registry.registry == nil {
		return nil
	}
	return a.runtime.registry.registry.Register(provider)
}

// BreakerEvents returns a read-only channel of breaker events.
// The consumer (mcpstack.ListenBreaker) runs automatically when AttachMCP is called.
func (r *Runtime) BreakerEvents() <-chan frameworkmcp.BreakerEvent {
	if r == nil || r.mcpManager == nil {
		return nil
	}
	return r.mcpManager.BreakerEvents()
}

// AttachMCP connects and registers a new MCP server.
// Automatically:
//  1. Initializes the breaker events channel
//  2. Starts mcpstack.ListenBreaker to record breaker events into MCPStack
//  3. Refreshes tool list so new tools are visible
func (r *Runtime) AttachMCP(ctx context.Context, cfg MCPServer) error {
	if r == nil || r.mcpManager == nil {
		return nil
	}
	return r.mcpManager.Attach(ctx, cfg)
}

// AttachMCPServer 是 plugin 域使用的展开入参版 AttachMCP。
func (r *Runtime) AttachMCPServer(
	ctx context.Context,
	name, transport, command string,
	args, env []string,
	url string,
) error {
	if r == nil || r.mcpManager == nil {
		return nil
	}
	return r.mcpManager.AttachServer(ctx, name, transport, command, args, env, url)
}

// RegisterLazyMCP 登记 MCP 服务器配置但不连接（冷启动：启动路径零 MCP 进程）。
func (r *Runtime) RegisterLazyMCP(name string, cfg MCPServer) error {
	if r == nil || r.mcpManager == nil {
		return nil
	}
	return r.mcpManager.RegisterLazy(name, cfg)
}

// LazyMCPServerNames 返回已登记但尚未连接的 MCP 服务器名（按字典序）。
func (r *Runtime) LazyMCPServerNames() []string {
	if r == nil || r.mcpManager == nil {
		return nil
	}
	return r.mcpManager.LazyNames()
}

// LoadMCP 按需连接已登记的 MCP 服务器（冷启动加载点）。
func (r *Runtime) LoadMCP(ctx context.Context, name string) (int, error) {
	if r == nil || r.mcpManager == nil {
		return 0, nil
	}
	return r.mcpManager.Load(ctx, name)
}

// DetachMCP 断开并注销 MCP 服务器。
func (r *Runtime) DetachMCP(name string) error {
	if r == nil || r.mcpManager == nil {
		return nil
	}
	return r.mcpManager.Detach(name)
}

// RefreshMCP 刷新指定 MCP 服务器的工具列表。
func (r *Runtime) RefreshMCP(ctx context.Context, name string) error {
	if r == nil || r.mcpManager == nil {
		return nil
	}
	return r.mcpManager.Refresh(ctx, name)
}

// MCPServerNames 返回已连接 MCP 服务器名（按字典序）。
func (r *Runtime) MCPServerNames() []string {
	if r == nil || r.mcpManager == nil {
		return nil
	}
	return r.mcpManager.Names()
}

// IsMCPAlive 轻量 ping 检查 MCP 服务器是否存活（2s 超时）。
func (r *Runtime) IsMCPAlive(name string) bool {
	if r == nil || r.mcpManager == nil {
		return false
	}
	return r.mcpManager.IsAlive(name)
}

// MCPServerStatus 返回 MCP 服务器健康状态（alive + tool count + error）。
func (r *Runtime) MCPServerStatus(name string) (alive bool, tools int, err error) {
	if r == nil || r.mcpManager == nil {
		return false, 0, nil
	}
	return r.mcpManager.Status(name)
}
