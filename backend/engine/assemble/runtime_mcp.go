package assemble

// 这是 MCP 域的宿主转发面（R11-S2，契约 §2.3）。
// 连接与命令生命周期都在域包 `backend/engine/mcp`（`r.mcpManager`），
// `Runtime` 面上没有可下沉的状态，故只搬转发面（内容从 `ports.go` 原样搬来，
// nil 接收者与未装配的判据逐行未改）。

import (
	"context"

	frameworkmcp "github.com/RedHuang-0622/Seele/tools/mcp"

	"github.com/RedHuang-0622/seelex/backend/engine/mcp"
)

// ── MCP 端口 ──────────────────────────────────────────────────────

// MCPServer is the transport-neutral MCP configuration consumed by Seelex.
type MCPServer = mcp.Server

// BreakerEvents returns a read-only channel of breaker events.
// The consumer (mcpstack.ListenBreaker) runs automatically when AttachMCP is called.
func (r *Runtime) BreakerEvents() <-chan frameworkmcp.BreakerEvent {
	if r == nil || r.mcpManager == nil {
		return nil
	}
	return r.mcpManager.BreakerEvents()
}

// AttachMCP connects and registers a new MCP server.
// Automatically initializes the breaker channel, starts the trace listener,
// and refreshes the tool list.
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
