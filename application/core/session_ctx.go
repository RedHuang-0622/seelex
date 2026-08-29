package core

import (
	"context"

	"github.com/RedHuang-0622/seelex/application/core/task_context"
)

// withSessionID 把会话 ID 注入 ctx（runChat 执行路径）。Seele ReActLoop 会
// 把该 ctx 原样传给 LoopHooks 回调与工具 handler（session/loop.go 行为），
// 因此 hook 与工具可据此路由到目标会话的独立状态（多会话并行执行）。
// ctx 键与 task_context 共享。
func withSessionID(ctx context.Context, sessionID string) context.Context {
	return task_context.WithSessionID(ctx, sessionID)
}

// sessionIDFromContext 返回 ctx 中的会话 ID；未注入时返回 ""。
func sessionIDFromContext(ctx context.Context) string {
	return task_context.SessionIDFromContext(ctx)
}
