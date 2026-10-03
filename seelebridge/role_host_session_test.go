package seelebridge

import (
	"context"
	"testing"
)

// TestHostSessionForToolCall 钉住「员工做工回合里，工具 handler 能按构造拿到宿主
// 主会话」这条契约。
//
// 它是 goal 看板对 teammate 可读的**唯一依据**：role 回合的 ctx 上只有 telemetry
// 会话与 roleWorkScope，没有 application 侧工具 handler 读的 task_context 会话键
// （见 application/core/goal_read_session.go）。这条契约一旦漂移，teammate 的
// goal_status 会重新退化成 "goal: session ID is required"。
func TestHostSessionForToolCall(t *testing.T) {
	plain := context.Background()
	if got := HostSessionForToolCall(plain); got != "" {
		t.Fatalf("普通回合（无 roleWorkScope）应返回空串，got %q", got)
	}

	scoped := withRoleWorkScope(plain, roleWorkScope{
		MainSessionID: "s-main",
		RoleSessionID: "s-main-t-exec",
		RoleName:      "exec",
	})
	if got := HostSessionForToolCall(scoped); got != "s-main" {
		t.Fatalf("员工做工回合应返回宿主主会话 s-main，got %q", got)
	}
}
