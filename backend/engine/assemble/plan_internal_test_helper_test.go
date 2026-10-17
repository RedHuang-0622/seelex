package assemble

import (
	"context"
	"fmt"
)

// runPlanToolForTest 直接调用 plan provider 的 handler，绕过模型可见面。
//
// plan_* 已从模型工具面退役（2026-10-17：teamwork 足够），但内部 Plan 执行器
// 仍被一组历史用例用来钉节点/worktree/事件链。这些用例改为直接调 handler，
// 不再依赖"plan 工具对模型可见"。
func runPlanToolForTest(runtime *Runtime, ctx context.Context, name, args string) (string, error) {
	if runtime == nil || runtime.planExecutor == nil {
		return "", fmt.Errorf("%s: plan executor unavailable", name)
	}
	for _, entry := range runtime.planExecutor.Provider().Tools() {
		if entry.Definition.Function.Name != name {
			continue
		}
		return entry.Handler.Execute(ctx, args)
	}
	return "", fmt.Errorf("%s: internal plan tool not found", name)
}
