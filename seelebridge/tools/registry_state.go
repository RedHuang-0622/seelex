package tools

import (
	"context"
	"fmt"
	"sync"
	"time"

	frameworktools "github.com/RedHuang-0622/Seele/tools"
	toolspermission "github.com/RedHuang-0622/Seele/tools/permission"
	"github.com/RedHuang-0622/Seele/types"
)

// RegistryState 包装 framework tools.Registry：内联工具 provider 由
// AddInline 累积维护，注册表快照在每次增删后重建。
type RegistryState struct {
	Registry *frameworktools.Registry
	inline   *InlineProvider
}

// NewRegistryState 构造带超时/权限门/事件与诊断中间件的工具注册表。
func NewRegistryState(timeout time.Duration, permission *PermissionGate, approvalTimeout time.Duration, eventMiddleware, diagnosticMiddleware frameworktools.Middleware) *RegistryState {
	return &RegistryState{Registry: frameworktools.NewRegistry(
		frameworktools.WithCallTimeout(timeout),
		frameworktools.WithMiddleware(eventMiddleware, permission.Middleware(approvalTimeout), diagnosticMiddleware),
	)}
}

// AddInline 注册一个普通产品工具（等价旧 holder.RegisterInline，重名覆盖）。
func (s *RegistryState) AddInline(
	name, description string,
	inputSchema map[string]interface{},
	handler func(context.Context, string) (string, error),
) {
	if s == nil || s.Registry == nil {
		return
	}
	if s.inline == nil {
		s.inline = &InlineProvider{}
		_ = s.Registry.Register(s.inline)
	}
	s.inline.upsert(frameworktools.ToolEntry{
		Definition: types.Tool{
			Type: "function",
			Function: types.ToolFunction{
				Name: name, Description: description, Parameters: inputSchema,
			},
		},
		Handler: frameworktools.HandlerFunc(handler),
	})
	// 重建快照使新工具立即可见（注册表只读锁调度，快照重建线程安全）。
	_ = s.Registry.Unregister(s.inline.ProviderName())
	_ = s.Registry.Register(s.inline)
}

// FindTool 按名称在注册表中查找工具（plan 工具面读取）。
func (s *RegistryState) FindTool(name string) (types.Tool, bool) {
	if s == nil || s.Registry == nil {
		return types.Tool{}, false
	}
	for _, tool := range s.Registry.Tools() {
		if tool.Function.Name == name {
			return tool, true
		}
	}
	return types.Tool{}, false
}

// InlineProvider 累积 RegisterTool 注册的普通产品工具。
// framework tools.Registry 不允许同名工具重复，AddInline 在添加前按名称去重。
type InlineProvider struct {
	mu      sync.Mutex
	entries []frameworktools.ToolEntry
}

// ProviderName 返回内联 provider 名称。
func (p *InlineProvider) ProviderName() string { return "seelex-inline" }

// Tools 返回全部内联工具（只读拷贝）。
func (p *InlineProvider) Tools() []frameworktools.ToolEntry {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]frameworktools.ToolEntry(nil), p.entries...)
}

func (p *InlineProvider) upsert(entry frameworktools.ToolEntry) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for index := range p.entries {
		if p.entries[index].Definition.Function.Name == entry.Definition.Function.Name {
			p.entries[index] = entry
			return
		}
	}
	p.entries = append(p.entries, entry)
}

// PermissionGate 是权限门控的可变状态：middleware 在注册表构造时闭包捕获
// 它，Set/SetFullAccess 运行时原子更新。
type PermissionGate struct {
	mu      sync.RWMutex
	checker *toolspermission.PermissionChecker
	handler toolspermission.ApprovalHandler
	manual  *toolspermission.PermissionConfig
}

// Set 装配权限配置与审批处理器。
func (state *PermissionGate) Set(cfg toolspermission.PermissionConfig, handler toolspermission.ApprovalHandler) {
	state.mu.Lock()
	state.checker = toolspermission.NewPermissionChecker(cfg)
	state.handler = handler
	manual := cfg
	state.manual = &manual
	state.mu.Unlock()
}

// SetFullAccess 切换全放行模式（关闭时回退手动配置）。
func (state *PermissionGate) SetFullAccess(on bool) {
	state.mu.Lock()
	if state.checker == nil {
		state.mu.Unlock()
		return
	}
	if on {
		state.checker.SetMode(toolspermission.ModeFullAccess)
	} else if state.manual != nil {
		state.checker = toolspermission.NewPermissionChecker(*state.manual)
	}
	state.mu.Unlock()
}

// FullAccess 返回当前是否全放行。
func (state *PermissionGate) FullAccess() bool {
	state.mu.RLock()
	checker := state.checker
	state.mu.RUnlock()
	return checker != nil && checker.Mode() == toolspermission.ModeFullAccess
}

// Middleware 把 tools/permission 检查结果接入 framework tools.Registry 调度链。
// allow → 放行；deny → 拒绝；ask → 走 ApprovalHandler（human-in-the-loop）。
func (state *PermissionGate) Middleware(approvalTimeout time.Duration) frameworktools.Middleware {
	return func(name string, next frameworktools.ToolHandler) frameworktools.ToolHandler {
		return frameworktools.HandlerFunc(func(ctx context.Context, argsJSON string) (string, error) {
			state.mu.RLock()
			checker := state.checker
			handler := state.handler
			state.mu.RUnlock()
			if checker == nil {
				return next.Execute(ctx, argsJSON)
			}
			switch checker.Check(name, argsJSON) {
			case toolspermission.ResultAllow:
				return next.Execute(ctx, argsJSON)
			case toolspermission.ResultDeny:
				return "", fmt.Errorf("%s: permission denied by policy", name)
			default:
				if handler == nil {
					return next.Execute(ctx, argsJSON)
				}
				request := toolspermission.ApprovalRequest{
					ID:        fmt.Sprintf("perm-%d", time.Now().UnixNano()),
					ToolName:  name,
					Arguments: argsJSON,
					Preview:   previewArguments(argsJSON),
					Options:   toolspermission.DefaultApproveOptions(),
					Timeout:   approvalTimeout, // limits.approval_timeout（默认 10 分钟，等待用户审批）
				}
				response, err := handler(&toolspermission.ApprovalContext{Request: request})
				if err != nil {
					return "", fmt.Errorf("%s: approval unavailable: %w", name, err)
				}
				if response != nil && (response.Choice == "allow" || response.Choice == "always") {
					if response.Remember {
						checker.AddAllowRule(name, argsJSON)
					}
					return next.Execute(ctx, argsJSON)
				}
				return "", fmt.Errorf("%s: approval denied by user", name)
			}
		})
	}
}

func previewArguments(argsJSON string) string {
	if len(argsJSON) <= 200 {
		return argsJSON
	}
	return argsJSON[:200] + "..."
}
