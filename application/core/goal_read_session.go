package core

import (
	"context"

	"github.com/RedHuang-0622/seelex/seelebridge"
)

// goalReadSessionID 是「读 goal 看板」该用的会话号。
//
// 普通回合 = ctx 里那把 task_context 会话键（与既有一致）。
//
// 员工做工回合（teammate / 评审者）没有那把键：role 回合只按 telemetry 会话 +
// roleWorkScope 组织 ctx（见 seelebridge/runtime_role_turn.go 的 runRoleRound），
// 而 worker 作业的 ctx 由框架 jobs manager 从 Background 派生，也不经 chat.go 的
// withSessionID。此时若照旧读，只会拿到空串，goal_status 直接报
// "goal: session ID is required"（见 goal_service.go 的 goalCoordinatorFor）。
//
// 看板属 leader，所以员工回合按**宿主主会话**读——宿主会话由 roleWorkScope 按构造
// 带在 ctx 上（seelebridge.HostSessionForToolCall），不必反查角色会话归属表。
// 这是只读路径：员工据此看得到 leader 的看板，但 goal 的写/收口工具仍在 ctl 组，
// 员工面上没有位（越权由权限门在执行判定上拒，不靠这里）。
func goalReadSessionID(ctx context.Context) string {
	if sessionID := sessionIDFromContext(ctx); sessionID != "" {
		return sessionID
	}
	return seelebridge.HostSessionForToolCall(ctx)
}
