package seelebridge

import "context"

// authorizePlanMutation 是 plan_load/plan_run/plan_clear 的变更授权钩子。
// 强制规划（RequirePlan/plan scope 机制）已于 2026-08-01 作为失败设计移除：
// 规划是模型的自愿决策，preflight 仅由显式 PrepareReplan 触发，因此这里
// 不再有"预检保留/权威拒绝"的阶段约束——计划变更由普通并发与产品工具
// 语义约束（每个请求独立 WorkPlan 装载）。
func (r *Runtime) authorizePlanMutation(ctx context.Context, toolName string) error {
	return nil
}
