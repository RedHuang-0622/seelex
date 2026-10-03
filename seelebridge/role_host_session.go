package seelebridge

import (
	"context"
	"strings"
)

// HostSessionForToolCall 返回「这次工具调用在为哪个宿主会话干活」：员工做工回合
// （ctx 带 roleWorkScope，见 runtime_role_turn.go 的 withRoleWorkScope）返回该回合
// 的 MainSessionID；普通回合没有这层作用域，返回 ""。
//
// 为什么需要它：员工角色回合的 ctx 上只有 telemetry 会话（角色会话）与
// roleWorkScope，**没有** application 侧工具 handler 读的那把 task_context 会话键
// （见 application/core/session_ctx.go）。于是「看 leader 的看板」这类需要宿主会话的
// 只读工具，在员工回合里只会拿到空串而直接失败（例如 goal_status 报
// "goal: session ID is required"，见 application/core/goal_service.go 的
// goalCoordinatorFor）。
//
// 取法是**按构造**的：roleWorkScope 是本轮开工时写进 ctx 的（不依赖任何反查），
// 所以不需要查 role session → owner 注册表（那张索引可能还没热）。
// 只读语义：本函数只读取 ctx，不产生任何副作用。
func HostSessionForToolCall(ctx context.Context) string {
	scope, ok := roleWorkScopeFrom(ctx)
	if !ok {
		return ""
	}
	return strings.TrimSpace(scope.MainSessionID)
}
