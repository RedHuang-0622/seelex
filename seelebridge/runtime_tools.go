package seelebridge

import (
	"context"

	frameworktools "github.com/RedHuang-0622/Seele/tools"
	toolspermission "github.com/RedHuang-0622/Seele/tools/permission"
	"github.com/RedHuang-0622/Seele/types"

	"github.com/RedHuang-0622/seelex/seelebridge/internal/docker"
	"github.com/RedHuang-0622/seelex/seelebridge/internal/model"
	"github.com/RedHuang-0622/seelex/seelebridge/task"
	seeltools "github.com/RedHuang-0622/seelex/seelebridge/tools"
)

// mcpRegistryAdapter 把 Runtime 工具注册表适配为 mcp.RegistryPort
// （懒解析：注册表在 NewRuntime 中稍后才装配，调用时判空）。
type mcpRegistryAdapter struct{ runtime *Runtime }

func (a mcpRegistryAdapter) Unregister(name string) error {
	if a.runtime == nil || a.runtime.registry == nil || a.runtime.registry.Registry == nil {
		return nil
	}
	return a.runtime.registry.Registry.Unregister(name)
}

func (a mcpRegistryAdapter) Register(provider frameworktools.ToolProvider) error {
	if a.runtime == nil || a.runtime.registry == nil || a.runtime.registry.Registry == nil {
		return nil
	}
	return a.runtime.registry.Registry.Register(provider)
}

// SetPermissionConfig 安装权限门控：Mode + Rules + ApprovalHandler。
// 门控作为 tools.Registry middleware 在每次工具调度前生效。
func (r *Runtime) SetPermissionConfig(cfg toolspermission.PermissionConfig, handler toolspermission.ApprovalHandler) {
	if r.permission != nil {
		r.permission.Set(cfg, handler)
	}
}

// bashDiagnosticMiddleware marks entry to and exit from the framework tool
// registry. Together with scopedBash's process stages, it distinguishes a
// stalled handler from a stall in the registry/framework after the handler
// has already returned. It is no-op unless a diagnostic observer is installed.
func (r *Runtime) bashDiagnosticMiddleware() frameworktools.Middleware {
	return func(name string, next frameworktools.ToolHandler) frameworktools.ToolHandler {
		if name != "bash" {
			return next
		}
		return frameworktools.HandlerFunc(func(ctx context.Context, argsJSON string) (string, error) {
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

// ensureDockerForRuntime 是 tools 域的接线面：按 limits 配置执行自动恢复
// （disable_docker_auto_start 关闭时返回 nil 表示"不处理"）。
func (r *Runtime) ensureDockerForRuntime(ctx context.Context) error {
	if r == nil {
		return nil
	}
	return docker.EnsureForRuntime(ctx, r.limits.DisableDockerAutoStart, r.limits.DockerStartTimeoutSec, r.dockerProbe)
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
// runtime/会话级单例状态，并行子代理调用会造成语义冲突。其余工具与主代理
// 一致可见。
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

// observeBash 投递 scoped bash 诊断事件（工具调用不可被诊断改变；观察者
// 意外 panic 也不影响工具调用）。
func (r *Runtime) observeBash(event BashDiagnosticEvent) {
	if r == nil {
		return
	}
	r.bashObserverMu.RLock()
	observer := r.bashObserver
	r.bashObserverMu.RUnlock()
	if observer == nil {
		return
	}
	defer func() { _ = recover() }()
	observer(event)
}

// registerProjectScopedTools overrides the Seele builtin filesystem tools
// （委托 tools.Router；RegisterBuiltins 内调用）。
func (r *Runtime) registerProjectScopedTools() {
	seeltools.NewRouter(r.scopedToolsDeps()).Register()
}

// registerTaskTools 注册主动任务工具 taskadd（同上委托）。
func (r *Runtime) registerTaskTools() {
	task.NewTools(r.taskToolsDeps()).RegisterTaskTools()
}

// registerTodoTools 注册 todolist 工具族（委托 task.Tools；RegisterBuiltins 内调用）。
func (r *Runtime) registerTodoTools() {
	task.NewTools(r.taskToolsDeps()).RegisterTodoTools()
}

// scopedToolsDeps 把 Runtime 能力面注入 tools 域（Deps 全部为闭包）。
func (r *Runtime) scopedToolsDeps() seeltools.Deps {
	return seeltools.Deps{
		RegisterTool:           r.RegisterTool,
		ProjectScope:           r.projectScope,
		FileSystem:             r.filesystem,
		GrepMaxResults:         r.limits.GrepMaxResults,
		WalkTimeoutSec:         r.limits.WalkTimeoutSec,
		ToolCallTimeout:        r.toolCallTimeout,
		ToolCallTimeoutSec:     r.limits.ToolCallTimeoutSec,
		DisableDockerAutoStart: r.limits.DisableDockerAutoStart,
		ObserveBash:            r.observeBash,
		EnsureDocker:           r.ensureDockerForRuntime,
		DockerDaemonDown:       docker.IsDaemonDown,
		DockerCLIPath:          docker.CLIPath,
		DockerHint:             docker.Hint,
	}
}

// seelexVisibilityPolicy 是 bridge.WithVisibilityPolicy 要求的函数类型策略。
// 子代理（Plan kind:agent 节点）与主代理能力一致：完整工具面 + 插件
// include/exclude 过滤同等生效。唯一例外是操作全局状态的工具（plan 工具族、
// task 终态工具）——并发子代理调用会污染主代理的计划状态 / 错误终结任务。
// Dispatch 侧由 agent/bridge.RegistryRuntime 复核同一策略，隐藏工具返回
// ErrToolNotVisible。
func (r *Runtime) seelexVisibilityPolicy(ctx context.Context, tools []types.Tool) []types.Tool {
	scope := model.NodeScopeFromContextOrEmpty(ctx)
	filtered := make([]types.Tool, 0, len(tools))
	for _, tool := range tools {
		name := tool.Function.Name
		if scope.NodeID != "" && scope.Role == model.RoleSubAgent && nodeScopeExcludedTool(name) {
			continue
		}
		// plan 工具面归位（plan.md §6）：主代理与 entry 节点的 plan 工具族
		// 仅在 goal skill 激活时可见（模型自由层默认面 = todolist + fork，
		// 不暴露 plan DAG；entry 节点同主代理语义，避免 DAG 内递归 plan）。
		if scope.Role != model.RoleSubAgent && isPlanTool(name) && !r.node.GoalSkillActive() {
			continue
		}
		filtered = append(filtered, tool)
	}
	return r.plugins.Filter(filtered)
}

// taskToolsDeps 把 Runtime 能力面注入 task 工具族（Deps 全部为闭包）。
func (r *Runtime) taskToolsDeps() task.Deps {
	return task.Deps{
		RegisterTool:         r.RegisterTool,
		ReplaceTodo:          r.tasks.ReplaceTodo,
		AppendTodo:           r.tasks.AppendTodo,
		SetTodoStatusByIndex: r.tasks.SetTodoStatusByIndex,
		TodoSnapshot:         r.TodoSnapshot,
		TaskAdd:              r.TaskAdd,
		TodoMaxItems:         r.limits.TodoMaxItems,
	}
}
func (r *Runtime) AllTools() []Tool {
	return summarizeTools(r.registry.Registry.Tools())
}
func (r *Runtime) FullAccess() bool {
	return r.permission != nil && r.permission.FullAccess()
}
func (r *Runtime) RegisterBuiltins() {
	r.registerProjectScopedTools()
	r.registerForkTool()
	r.registerTodoTools()
	r.registerTaskTools()
	r.scopedToolsReady = true
	// plan 工具（seelex-workplan provider）：plan_load/plan_clear/plan_validate/
	// plan_status/plan_export；plan_run 的执行内核在 seele-v2 slice 4 迁移后恢复。
	if r.planExecutor != nil {
		if err := r.registry.Registry.Register(r.planExecutor.Provider()); err != nil {
			return
		}
	}
}
func (r *Runtime) RegisterTool(
	name, description string,
	inputSchema map[string]interface{},
	handler func(context.Context, string) (string, error),
) {
	if r.scopedToolsReady && isProjectScopedTool(name) {
		return
	}
	r.registry.AddInline(name, description, inputSchema, handler)
}
func (r *Runtime) SetFullAccess(on bool) {
	if r.permission != nil {
		r.permission.SetFullAccess(on)
	}
}
func (r *Runtime) VisibleTools(ctx context.Context) []Tool {
	return summarizeTools(r.agt.VisibleTools(ctx))
}
func isProjectScopedTool(name string) bool {
	switch name {
	case "read_file", "grep_search", "glob", "write_file", "edit_file", "bash":
		return true
	default:
		return false
	}
}
