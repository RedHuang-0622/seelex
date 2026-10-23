package main

// middleware.go —— 权限判定作为**中间件**（与框架 MetaMiddleware 同形状）。
//
// 产品语义就是 middleware：它包在 handler 外面，拒就是拒、问就是问，不进 handler
// 就不可能产生副作用。位置仍是既有那一摞的第 3 层（event → pathGate → permission
// → diagnostic），新增的 ssh / ssh.userspace 判定发生在**同一个中间件内部**，
// 不新增链层——链层多了没有收益，只会让顺序更难讲清。

import (
	"context"
	"fmt"
)

// Call 是一次工具调用（框架里是 ToolCall）。
type Call struct {
	Name string
	Args map[string]string
}

// Result 是中间件观测到的结果（MVP 只关心"走到没走到 handler"）。
type Result struct {
	Reached  bool     `json:"reached"`
	Approved bool     `json:"approved"`
	Decision Decision `json:"decision"`
}

// Handler 是链上的处理器。
type Handler func(ctx context.Context, call Call) (Result, error)

// Middleware 与框架 MetaMiddleware 同形状。
type Middleware func(name string, next Handler) Handler

// GroupResolver 从调用 ctx 解析用户组（生产实现在 permission_policy.go：
// root / sub / emp_* 与将来的自定义组）。
type GroupResolver func(ctx context.Context) GroupID

// RequirementFunc 组装 Requirements。生产实现由**上游两处**喂数据：
// 传输选择器给出 Remote，pathGate 给出 OutOfRoot —— 本层不重复解析。
type RequirementFunc func(ctx context.Context, call Call) Requirement

// Approver 是审批面（生产是 ApprovalHandler）。
type Approver func(ctx context.Context, decision Decision) (bool, error)

// ErrDenied 是权限拒绝的稳定错误（可用错误码分流）。
type ErrDenied struct {
	Decision Decision
}

func (e *ErrDenied) Error() string {
	return fmt.Sprintf("permission: %s（%s）", e.Decision.Effect, e.Decision.Reason)
}

// ErrApprovalRejected 是审批被拒。
type ErrApprovalRejected struct {
	Decision Decision
}

func (e *ErrApprovalRejected) Error() string { return "permission: 审批被拒" }

// PermissionMiddleware 是矩阵判定中间件。
func PermissionMiddleware(gate Gate, groupOf GroupResolver, reqOf RequirementFunc, approve Approver) Middleware {
	return func(name string, next Handler) Handler {
		return func(ctx context.Context, call Call) (Result, error) {
			group := groupOf(ctx)
			requirement := reqOf(ctx, call)
			requirement.Tool = name
			decision, err := gate.Decide(ctx, group, requirement)
			if err != nil {
				return Result{}, err
			}
			switch decision.Effect {
			case EffectDeny:
				return Result{Decision: decision}, &ErrDenied{Decision: decision}
			case EffectAsk:
				approved, approveErr := approve(ctx, decision)
				if approveErr != nil {
					return Result{Decision: decision}, approveErr
				}
				if !approved {
					return Result{Decision: decision}, &ErrApprovalRejected{Decision: decision}
				}
				res, callErr := next(ctx, call)
				res.Approved = true
				res.Decision = decision
				return res, callErr
			default:
				res, callErr := next(ctx, call)
				res.Decision = decision
				return res, callErr
			}
		}
	}
}
