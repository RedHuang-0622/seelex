package tools

import (
	"context"

	"github.com/RedHuang-0622/Seele/types"
	"github.com/RedHuang-0622/seelex/seelebridge/internal/model"
)

// PolicyDeps 是可见性策略的跨域闭包（goal skill 激活判定 + 插件过滤）。
type PolicyDeps struct {
	GoalSkillActive func() bool
	PluginFilter    func([]types.Tool) []types.Tool
}

// Policy 是 bridge.WithVisibilityPolicy 要求的函数类型策略的实现：
// 子代理（Plan kind:agent 节点）与主代理能力一致——完整工具面 + 插件
// include/exclude 过滤同等生效。唯一例外是操作全局状态的工具（plan 工具族、
// task 终态工具）——并发子代理调用会污染主代理的计划状态 / 错误终结任务。
// Dispatch 侧由 agent/bridge.RegistryRuntime 复核同一策略，隐藏工具返回
// ErrToolNotVisible。
type Policy struct {
	deps PolicyDeps
}

// NewPolicy 构造可见性策略。
func NewPolicy(deps PolicyDeps) *Policy {
	return &Policy{deps: deps}
}

// Filter 按节点作用域与插件配置过滤可见工具集。
func (p *Policy) Filter(ctx context.Context, tools []types.Tool) []types.Tool {
	if p == nil {
		return tools
	}
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
		if scope.Role != model.RoleSubAgent && isPlanTool(name) && !p.goalSkillActive() {
			continue
		}
		filtered = append(filtered, tool)
	}
	if p.deps.PluginFilter != nil {
		return p.deps.PluginFilter(filtered)
	}
	return filtered
}

func (p *Policy) goalSkillActive() bool {
	if p == nil || p.deps.GoalSkillActive == nil {
		return false
	}
	return p.deps.GoalSkillActive()
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
