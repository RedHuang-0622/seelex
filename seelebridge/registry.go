package seelebridge

import (
	"context"
	"fmt"
	"path"
	"sync"
	"time"

	"github.com/RedHuang-0622/Seele/tools"
	toolspermission "github.com/RedHuang-0622/Seele/tools/permission"
	"github.com/RedHuang-0622/Seele/types"
	"github.com/RedHuang-0622/seelex/seelebridge/internal/model"
)

// toolsRegistryState 包装 tools.Registry：内联工具 provider 由 RegisterTool
// 累积维护，注册表快照在每次增删后重建。
type toolsRegistryState struct {
	registry *tools.Registry
	inline   *inlineToolProvider
}

func newToolsRegistry(timeout time.Duration, permission *permissionGateState, approvalTimeout time.Duration, eventMiddleware, diagnosticMiddleware tools.Middleware) *tools.Registry {
	return tools.NewRegistry(
		tools.WithCallTimeout(timeout),
		tools.WithMiddleware(eventMiddleware, permission.middleware(approvalTimeout), diagnosticMiddleware),
	)
}

// bashDiagnosticMiddleware marks entry to and exit from the framework tool
// registry. Together with scopedBash's process stages, it distinguishes a
// stalled handler from a stall in the registry/framework after the handler
// has already returned. It is no-op unless a diagnostic observer is installed.
func (r *Runtime) bashDiagnosticMiddleware() tools.Middleware {
	return func(name string, next tools.ToolHandler) tools.ToolHandler {
		if name != "bash" {
			return next
		}
		return tools.HandlerFunc(func(ctx context.Context, argsJSON string) (string, error) {
			r.observeBash(BashDiagnosticEvent{Stage: "bash.registry.dispatch.start"})
			result, err := next.Execute(ctx, argsJSON)
			if err != nil {
				r.observeBash(BashDiagnosticEvent{Stage: "bash.registry.dispatch.error", Err: err})
				return result, err
			}
			r.observeBash(BashDiagnosticEvent{Stage: "bash.registry.dispatch.done"})
			return result, nil
		})
	}
}

// inlineToolProvider 累积 RegisterTool 注册的普通产品工具。
// tools.Registry 不允许同名工具重复，RegisterTool 在添加前按名称去重。
type inlineToolProvider struct {
	mu      sync.Mutex
	entries []tools.ToolEntry
}

func (p *inlineToolProvider) ProviderName() string { return "seelex-inline" }

func (p *inlineToolProvider) Tools() []tools.ToolEntry {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]tools.ToolEntry(nil), p.entries...)
}

func (p *inlineToolProvider) upsert(entry tools.ToolEntry) {
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

// addInlineTool 注册一个普通产品工具（等价旧 holder.RegisterInline，重名覆盖）。
func (r *Runtime) addInlineTool(
	name, description string,
	inputSchema map[string]interface{},
	handler func(context.Context, string) (string, error),
) {
	state := r.registry
	if state == nil || state.registry == nil {
		return
	}
	if state.inline == nil {
		state.inline = &inlineToolProvider{}
		_ = state.registry.Register(state.inline)
	}
	state.inline.upsert(tools.ToolEntry{
		Definition: types.Tool{
			Type: "function",
			Function: types.ToolFunction{
				Name: name, Description: description, Parameters: inputSchema,
			},
		},
		Handler: tools.HandlerFunc(handler),
	})
	// 重建快照使新工具立即可见（注册表只读锁调度，快照重建线程安全）。
	_ = state.registry.Unregister(state.inline.ProviderName())
	_ = state.registry.Register(state.inline)
}

// seelexVisibilityPolicy 是 bridge.WithVisibilityPolicy 要求的函数类型策略。
// 子代理（Plan kind:agent 节点）与主代理能力一致：完整工具面 + 插件
// include/exclude 过滤同等生效。唯一例外是操作全局状态的工具（plan 工具族、
// task 终态工具）——并发子代理调用会污染主代理的计划状态 / 错误终结任务，
// 见 nodeScopeExcludedTool。
//
// 并发共享时策略闭包只读 Runtime 的加锁字段，安全。
// Dispatch 侧由 agent/bridge.RegistryRuntime 复核同一策略，隐藏工具返回
// ErrToolNotVisible。
func (r *Runtime) seelexVisibilityPolicy(ctx context.Context, tools []types.Tool) []types.Tool {
	scope := nodeScopeFromContextOrEmpty(ctx)
	filtered := make([]types.Tool, 0, len(tools))
	for _, tool := range tools {
		name := tool.Function.Name
		if scope.NodeID != "" && scope.Role == model.RoleSubAgent && nodeScopeExcludedTool(name) {
			continue
		}
		// plan 工具面归位（plan.md §6）：主代理与 entry 节点的 plan 工具族
		// 仅在 goal skill 激活时可见（模型自由层默认面 = todolist + fork，
		// 不暴露 plan DAG；entry 节点同主代理语义，避免 DAG 内递归 plan）。
		if scope.Role != model.RoleSubAgent && isPlanTool(name) && !r.goalSkillActive() {
			continue
		}
		filtered = append(filtered, tool)
	}
	r.pluginMu.RLock()
	active := r.activePlugin
	def, ok := r.pluginDefs[active]
	r.pluginMu.RUnlock()
	if !ok || active == "" {
		return filtered
	}
	pluginFiltered := make([]types.Tool, 0, len(filtered))
	for _, tool := range filtered {
		name := tool.Function.Name
		if len(def.Include) > 0 && !matchesAnyPattern(name, def.Include) {
			continue
		}
		if matchesAnyPattern(name, def.Exclude) {
			continue
		}
		pluginFiltered = append(pluginFiltered, tool)
	}
	return pluginFiltered
}

// isPlanTool 判断 plan 工具族（goal skill 激活时对主代理可见）。
func isPlanTool(name string) bool {
	switch name {
	case "plan_load", "plan_clear", "plan_run", "plan_status", "plan_export", "plan_validate":
		return true
	default:
		return false
	}
}

// nodeScopeExcludedTool 判断子代理不可见的全局状态工具：这些工具操作
// runtime/会话级单例状态（plan.planToolProvider.loaded、TaskStack 终态），
// 并行子代理调用会造成语义冲突（子代理 plan_run 递归嵌套 DAG、
// task_complete 错误终结主任务）。其余工具与主代理一致可见。
func nodeScopeExcludedTool(name string) bool {
	switch name {
	case "plan_load", "plan_clear", "plan_run", "plan_status", "plan_export", "plan_validate",
		"task_complete", "task_failed", "task_needs_user_decision",
		"fork_subagents": // fork 会递归派生子代理（无深度控制），同 plan 工具族理由
		return true
	default:
		return false
	}
}

func matchesAnyPattern(name string, patterns []string) bool {
	for _, pattern := range patterns {
		if matchToolPattern(pattern, name) {
			return true
		}
	}
	return false
}

// matchToolPattern 支持 "*" 通配的简单模式匹配（与旧 holder 插件过滤语义一致）。
func matchToolPattern(pattern, name string) bool {
	if pattern == "" {
		return name == ""
	}
	ok, err := path.Match(pattern, name)
	if err != nil {
		return pattern == name
	}
	return ok
}

// permissionGateState 是权限门控的可变状态：middleware 在注册表构造时
// 闭包捕获它，SetPermissionConfig/SetFullAccess 运行时原子更新。
type permissionGateState struct {
	mu      sync.RWMutex
	checker *toolspermission.PermissionChecker
	handler toolspermission.ApprovalHandler
	manual  *toolspermission.PermissionConfig
}

func (state *permissionGateState) set(cfg toolspermission.PermissionConfig, handler toolspermission.ApprovalHandler) {
	state.mu.Lock()
	state.checker = toolspermission.NewPermissionChecker(cfg)
	state.handler = handler
	manual := cfg
	state.manual = &manual
	state.mu.Unlock()
}

func (state *permissionGateState) setFullAccess(on bool) {
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

func (state *permissionGateState) fullAccess() bool {
	state.mu.RLock()
	checker := state.checker
	state.mu.RUnlock()
	return checker != nil && checker.Mode() == toolspermission.ModeFullAccess
}

// middleware 把 tools/permission 检查结果接入 tools.Registry 调度链。
// allow → 放行；deny → 拒绝；ask → 走 ApprovalHandler（human-in-the-loop）。
func (state *permissionGateState) middleware(approvalTimeout time.Duration) tools.Middleware {
	return func(name string, next tools.ToolHandler) tools.ToolHandler {
		return tools.HandlerFunc(func(ctx context.Context, argsJSON string) (string, error) {
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
