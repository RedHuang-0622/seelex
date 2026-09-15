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
// 它，Set/SetFullAccess* 运行时原子更新。
//
// 全权（full access）是**会话级**的用户决定（application 侧归属进
// SessionUnit，G4）：执行面必须按工具调度 ctx 的会话归属解析，绝不能把
// 它放进程级布尔上——进程级布尔有两个真实后果：
//   - 污染：A 会话点全权 → B 会话的工具调用被静默放行（B 未同意）；
//   - 失灵：B 会话的 chat 起点同步（syncFullAccessFor）会把 A 的全权关掉，
//     表现为「点了全权仍弹审批/仍被拒」——多会话并行下必现（单飞时代只是
//     侥幸没暴露）。
type PermissionGate struct {
	mu      sync.RWMutex
	checker *toolspermission.PermissionChecker
	handler toolspermission.ApprovalHandler
	// sessionFullAccess 是会话级全权选择（键 = 会话 ID）。空串键是**进程级
	// 默认**（CLI -permission full_access / 未做会话级选择的回退面）；未选择
	// 的会话回退进程默认，不继承别的会话的开关。
	//
	// middleware **先读它**、再读 checker：用户点击全权后，即使 checker 实例
	// 被替换（Set 重建）或 checker 尚未装配，也不会出现「权限检查还走旧
	// manual 规则 / 落在 nil checker 上」的窗口。
	sessionFullAccess map[string]bool
	// SessionFromContext 从工具调度 ctx 提取会话归属（seelebridge 根包
	// 注入 seelebridge 会话路由键；nil = 权限审批保持进程级空归属回退）。
	SessionFromContext func(ctx context.Context) string
}

// Set 装配权限配置与审批处理器。checker 只承载 manual 规则：全权由
// sessionFullAccess 在 middleware 最前面短路，因此 Set 重建 checker 不会
// 把任何会话的全权吃掉（历史缺陷：全权只记一个进程级布尔，Set 后被退回
// manual）。
func (state *PermissionGate) Set(cfg toolspermission.PermissionConfig, handler toolspermission.ApprovalHandler) {
	state.mu.Lock()
	state.checker = toolspermission.NewPermissionChecker(cfg)
	state.handler = handler
	state.mu.Unlock()
}

// SetFullAccess 设置**进程级默认**全权（CLI -permission full_access 与
// 装配期基线用；等价 SetFullAccessFor("", on)）。
func (state *PermissionGate) SetFullAccess(on bool) {
	state.SetFullAccessFor("", on)
}

// SetFullAccessFor 设置指定会话的全权选择（空会话 ID = 进程级默认）。
// 只影响该会话自己的工具调度，不触碰其它会话的选择。
func (state *PermissionGate) SetFullAccessFor(sessionID string, on bool) {
	state.mu.Lock()
	if state.sessionFullAccess == nil {
		state.sessionFullAccess = make(map[string]bool)
	}
	state.sessionFullAccess[sessionID] = on
	state.mu.Unlock()
}

// FullAccess 返回进程级默认全权（装配期捕获面；未做会话级选择的回退值）。
func (state *PermissionGate) FullAccess() bool {
	state.mu.RLock()
	defer state.mu.RUnlock()
	return state.sessionFullAccess[""]
}

// FullAccessFor 返回指定会话生效的全权模式：会话级选择优先，未选择回退
// 进程级默认（与 middleware 同源解析，供探针/诊断读取）。
func (state *PermissionGate) FullAccessFor(sessionID string) bool {
	state.mu.RLock()
	defer state.mu.RUnlock()
	return state.effectiveFullAccessLocked(sessionID)
}

// effectiveFullAccessLocked 解析生效的全权模式（调用方持锁）。
func (state *PermissionGate) effectiveFullAccessLocked(sessionID string) bool {
	if on, ok := state.sessionFullAccess[sessionID]; ok {
		return on
	}
	return state.sessionFullAccess[""]
}

// Middleware 把 tools/permission 检查结果接入 framework tools.Registry 调度链。
// allow → 放行；deny → 拒绝；ask → 走 ApprovalHandler（human-in-the-loop）。
//
// 全权短路放在最前面，且**按会话归属解析**（A 会话的全权不替 B 会话放行；
// B 的起点同步也关不掉 A）：全权是用户的显式决定，不允许任何"还没轮到
// checker"的时序把它降级成询问或拒绝，也不允许它越会话传播。
func (state *PermissionGate) Middleware(approvalTimeout time.Duration) frameworktools.Middleware {
	return func(name string, next frameworktools.ToolHandler) frameworktools.ToolHandler {
		return frameworktools.HandlerFunc(func(ctx context.Context, argsJSON string) (string, error) {
			sessionID := state.sessionFromContext(ctx)
			state.mu.RLock()
			fullAccess := state.effectiveFullAccessLocked(sessionID)
			checker := state.checker
			handler := state.handler
			state.mu.RUnlock()
			if fullAccess {
				return next.Execute(ctx, argsJSON)
			}
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
					ID: fmt.Sprintf("perm-%d", time.Now().UnixNano()),
					// 波 4 approval 会话级归属：权限审批随调度 ctx 携带会话
					// ID（注入的会话路由键），不再以进程级空归属进入
					// 视图单格。
					SessionID: sessionID,
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

func (state *PermissionGate) sessionFromContext(ctx context.Context) string {
	if state == nil {
		return ""
	}
	state.mu.RLock()
	resolver := state.SessionFromContext
	state.mu.RUnlock()
	if resolver == nil {
		return ""
	}
	return resolver(ctx)
}

func previewArguments(argsJSON string) string {
	if len(argsJSON) <= 200 {
		return argsJSON
	}
	return argsJSON[:200] + "..."
}
