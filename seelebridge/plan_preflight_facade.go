package seelebridge

import (
	"context"
	"fmt"
)

// PrepareReplan atomically replaces a failed WorkPlan with a recovery plan.
// It only plans; it never invokes plan_run or retries side effects. The
// existing plan_load handler still applies the current effort policy.
// 实现归属 seelebridge/plan（Executor.PrepareReplan），本文件只保留
// Runtime 公开委托（application/contract 端口面）。
func (r *Runtime) PrepareReplan(ctx context.Context, request ReplanRequest) (PlanPreflight, error) {
	if r == nil || r.planExecutor == nil {
		return PlanPreflight{}, fmt.Errorf("plan replan: plan executor is unavailable")
	}
	return r.planExecutor.PrepareReplan(ctx, request)
}
