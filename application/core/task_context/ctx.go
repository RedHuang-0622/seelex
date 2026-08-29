package task_context

import "context"

// sessionIDKey 是上下文中的会话路由键（M2 多会话并行）。runChat 执行路径
// 把会话 ID 注入 ctx；Seele ReActLoop 会把该 ctx 原样传给 LoopHooks 回调
// 与工具 handler，因此 task 域可据此路由到目标会话的独立状态。
// core 根包（tool_hooks/session_ctx）与本包共享同一键。
type sessionIDKey struct{}

// WithSessionID 返回携带会话 ID 的上下文（幂等覆盖）。
func WithSessionID(ctx context.Context, sessionID string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, sessionIDKey{}, sessionID)
}

// SessionIDFromContext 返回 ctx 中的会话 ID；未注入时返回 ""。
func SessionIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if sessionID, ok := ctx.Value(sessionIDKey{}).(string); ok {
		return sessionID
	}
	return ""
}
