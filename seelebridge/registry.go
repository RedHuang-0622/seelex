package seelebridge

import (
	"context"

	"github.com/RedHuang-0622/Seele/tools"
	"github.com/RedHuang-0622/Seele/types"
	"github.com/RedHuang-0622/seelex/seelebridge/internal/model"
)

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
