package seelebridge

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/RedHuang-0622/Seele/tools"
	"github.com/RedHuang-0622/Seele/tools/adapter"
)

// TestMCPFakeProviderState 验证 MCP 以 tools.ToolProvider 形态接入注册表后的
// 状态机：catalog 列举 → Refresh 建快照 → Dispatch 走 invoker（远端名）
// → 再次 Refresh 更新工具集。全程无真实 MCP 连接（fake catalog/invoker）。
func TestMCPFakeProviderState(t *testing.T) {
	var mu sync.Mutex
	invoked := make([]string, 0, 4)

	provider, err := adapter.NewMCPProvider("fake-mcp",
		adapter.CatalogFunc(func(context.Context) ([]adapter.Descriptor, error) {
			return []adapter.Descriptor{
				{Name: "search", RemoteName: "remote.search", Description: "搜索", InputSchema: map[string]interface{}{"type": "object"}},
				{Name: "fetch", RemoteName: "remote.fetch", Description: "抓取"},
			}, nil
		}),
		adapter.InvokeFunc(func(_ context.Context, name, arguments string) (string, error) {
			mu.Lock()
			invoked = append(invoked, name+":"+arguments)
			mu.Unlock()
			return "done", nil
		}),
		adapter.WithNamespace("mcp"),
	)
	if err != nil {
		t.Fatalf("NewMCPProvider: %v", err)
	}

	registry := tools.NewRegistry()
	if err := registry.Register(provider); err != nil {
		t.Fatalf("register: %v", err)
	}

	// Refresh 前无工具快照。
	if got := registry.Tools(); len(got) != 0 {
		t.Fatalf("tools before refresh = %v", got)
	}

	// Refresh 后暴露命名空间化的工具定义。
	if err := registry.Refresh(context.Background()); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	defs := registry.Tools()
	if len(defs) != 2 {
		t.Fatalf("tools after refresh = %d, want 2", len(defs))
	}
	names := map[string]string{}
	for _, def := range defs {
		names[def.Function.Name] = def.Function.Description
	}
	if names["mcp__search"] != "搜索" || names["mcp__fetch"] != "抓取" {
		t.Fatalf("unexpected tool definitions: %v", names)
	}

	// Dispatch 转发给 invoker，携带远端工具名。
	out, err := registry.Dispatch(context.Background(), tools.ToolCall{Name: "mcp__search", ArgumentsJSON: `{"q":"cad"}`})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if out != "done" {
		t.Fatalf("dispatch output = %q", out)
	}
	mu.Lock()
	calls := append([]string(nil), invoked...)
	mu.Unlock()
	if len(calls) != 1 || calls[0] != "remote.search:"+`{"q":"cad"}` {
		t.Fatalf("invoker calls = %v", calls)
	}

	// 未注册的工具仍然拒绝（tools.ErrToolNotFound 语义，与可见性无关）。
	if _, err := registry.Dispatch(context.Background(), tools.ToolCall{Name: "mcp__ghost", ArgumentsJSON: "{}"}); err == nil {
		t.Fatal("unknown tool dispatch should fail")
	}

	// 再次 Refresh 后工具集状态仍稳定（快照重建）。
	if err := registry.Refresh(context.Background()); err != nil {
		t.Fatalf("second refresh: %v", err)
	}
	if got := registry.Tools(); len(got) != 2 {
		t.Fatalf("tools after second refresh = %d, want 2", len(got))
	}
}

// TestMCPRuntimeLifecycleState 验证 seelebridge 侧的 MCP 生命周期状态：
// 空状态、attach 失败不破坏注册表、detach 幂等。
func TestMCPRuntimeLifecycleState(t *testing.T) {
	r := newTestRuntime(t)
	defer r.Shutdown()

	// 初始空状态。
	if names := r.MCPServerNames(); len(names) != 0 {
		t.Fatalf("initial servers = %v", names)
	}
	if alive, toolsCount, err := r.MCPServerStatus("missing"); err == nil || alive || toolsCount != 0 {
		t.Fatalf("status of missing server: alive=%v tools=%d err=%v", alive, toolsCount, err)
	}

	// attach 一个不存在的命令：失败但不破坏已有状态，注册表不被污染。
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := r.AttachMCP(ctx, MCPServer{Name: "ghost", Transport: "stdio", Command: "seelex-nonexistent-cmd"}); err == nil {
		t.Fatal("attach with nonexistent command should fail")
	}
	if names := r.MCPServerNames(); len(names) != 0 {
		t.Fatalf("servers after failed attach = %v", names)
	}
	if got := r.AllTools(); len(got) != 0 {
		t.Fatalf("registry tools after failed attach = %v", got)
	}

	// detach 幂等：不存在的 server 不报错。
	if err := r.DetachMCP("ghost"); err != nil {
		t.Fatalf("detach missing server: %v", err)
	}
}

// TestMCPLazyRegistrationDoesNotConnect 验证冷启动契约：登记配置不连接、
// 不注册工具（启动路径零 MCP 进程）；按名加载失败不破坏登记（可重试）；
// 未知 server / 无效配置显式报错。
func TestMCPLazyRegistrationDoesNotConnect(t *testing.T) {
	r := newTestRuntime(t)
	defer r.Shutdown()

	cfg := MCPServer{Name: "playwright", Transport: "stdio", Command: "seelex-nonexistent-cmd", Args: []string{"-v"}}
	if err := r.RegisterLazyMCP("playwright", cfg); err != nil {
		t.Fatal(err)
	}
	// 登记 ≠ 连接：无已附加服务器、无可见工具、未存活。
	if names := r.MCPServerNames(); len(names) != 0 {
		t.Fatalf("registered-but-cold servers = %v", names)
	}
	if got := r.AllTools(); len(got) != 0 {
		t.Fatalf("cold registry tools = %v", got)
	}
	if got := r.LazyMCPServerNames(); len(got) != 1 || got[0] != "playwright" {
		t.Fatalf("lazy names = %v", got)
	}
	// 按名加载：命令不存在 → 显式失败，且不破坏登记。
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := r.LoadMCP(ctx, "playwright"); err == nil {
		t.Fatal("load with nonexistent command should fail")
	}
	if names := r.MCPServerNames(); len(names) != 0 {
		t.Fatalf("servers after failed load = %v", names)
	}
	if got := r.LazyMCPServerNames(); len(got) != 1 {
		t.Fatalf("failed load dropped registration: %v", got)
	}
	// 未知 server 显式报错。
	if _, err := r.LoadMCP(ctx, "missing"); err == nil {
		t.Fatal("load unknown server must fail")
	}
	// 配置校验：空名 / 无效 transport 拒绝登记。
	if err := r.RegisterLazyMCP("", MCPServer{Transport: "stdio", Command: "x"}); err == nil {
		t.Fatal("register empty name must fail")
	}
	if err := r.RegisterLazyMCP("bad", MCPServer{Transport: "bogus"}); err == nil {
		t.Fatal("register invalid transport must fail")
	}
	// 重复登记覆盖配置（幂等语义）。
	if err := r.RegisterLazyMCP("playwright", cfg); err != nil {
		t.Fatal(err)
	}
	if got := r.LazyMCPServerNames(); len(got) != 1 {
		t.Fatalf("re-register duplicated entry: %v", got)
	}
}

// TestMCPProviderNameRegistered 验证 MCP provider 以固定名称 "mcp" 实现
// tools.ToolProvider（tools/mcp ProviderName），可注册进注册表且重注册幂等。
func TestMCPProviderNameRegistered(t *testing.T) {
	r := newTestRuntime(t)
	defer r.Shutdown()

	provider := r.mcpManager.Provider()
	if got := provider.ProviderName(); got != "mcp" {
		t.Fatalf("MCP provider name = %q, want mcp", got)
	}
	// RefreshTools 的注册/注销重挂载幂等。
	r.mcpManager.RefreshTools()
	r.mcpManager.RefreshTools()
	if got := r.AllTools(); len(got) != 0 {
		t.Fatalf("registry tools after re-register = %v", got)
	}
}
