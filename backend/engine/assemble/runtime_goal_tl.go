package assemble

// runtime_goal_tl.go 是 **goal 域 TL 评估器的宿主转发面**。
//
// 落点沿革（P21-13「域方法随类型走」）：评估器本体（提示词分段与输出契约追加、工具边界
// 与实际执行面同源的判定、裁决解析与域校验、只读权限口径）整份搬进
// [`backend/multiagent/goal`](../../multiagent/goal/README.md) 的
// `advisor_evaluator.go`（plan §2.5：`seelebridge/runtime_goal_tl.go` → `backend/multiagent/goal/`
// "搬 + 反向依赖改端口"）。这里只剩**三格宿主读数**：
//
//	Complete       无工具兜底（一次 completer 调用）
//	PromptProvider 已登记的员工提示词读面（`SetRolePromptProvider` 注入）
//	Round          角色回合执行原语（`runRoleRound`：评审有工作区时带只读工具跑一轮）
//
// 组合根在首次会话启动前经 `app.SetGoalTLEvaluator` 注入本方法返回的评估器。

import (
	"context"

	"github.com/RedHuang-0622/Seele/types"

	goaldomain "github.com/RedHuang-0622/seelex/backend/multiagent/goal"
)

// GoalTLEvaluator 返回真实 TL 评估器（用 Runtime 主 completer；未装配 completer 时
// 返回 nil → Supervisor 按 TL 未启用处理）。
//
// ADVISOR 的角色提示词取自 `SetRolePromptProvider` 注入的读面（装配根从 Agent Team
// 角色注册表读）：用户登记了提示词就用它，否则用内置角色设定；工具边界与输出契约
// 永远追加（判据在域包）。
func (r *Runtime) GoalTLEvaluator() goaldomain.TLEvaluator {
	if r == nil || r.completer == nil {
		return nil
	}
	return goaldomain.NewAdvisorEvaluator(goaldomain.AdvisorEvaluatorDeps{
		Complete:       r.completeAdvisorOnce,
		PromptProvider: r.rolePromptFor,
		Round:          r.advisorRound,
	})
}

// completeAdvisorOnce 是无工具兜底的一格读数：一次 completer 调用（system + user）。
func (r *Runtime) completeAdvisorOnce(ctx context.Context, systemPrompt, userContent string) (string, error) {
	message, err := r.completer.Complete(ctx, []types.Message{
		{Role: "system", Content: stringPtr(systemPrompt)},
		{Role: "user", Content: stringPtr(userContent)},
	}, nil)
	if err != nil {
		return "", err
	}
	if message.Content == nil {
		return "", nil
	}
	return *message.Content, nil
}

// advisorRound 把域包的回合请求翻译成宿主角色回合原语（**唯一**一处翻译；判定与
// 提示词都在域包，本层只搬运字段）。
func (r *Runtime) advisorRound(ctx context.Context, request goaldomain.AdvisorRoundRequest) (string, error) {
	return r.runRoleRound(ctx, roleRoundSpec{
		MainSessionID: request.MainSessionID,
		RoleName:      request.RoleName,
		RoleSessionID: request.RoleSessionID,
		ToolsPolicy:   request.ToolsPolicy,
		SystemPrompt:  request.SystemPrompt,
		Input:         request.Input,
		MaxLoops:      request.MaxLoops,
		OnDelta:       request.OnDelta,
		FreshContext:  request.FreshContext,
	})
}
