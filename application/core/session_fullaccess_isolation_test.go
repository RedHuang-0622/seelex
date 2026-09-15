package core

import (
	"context"
	"testing"
	"time"
)

// 全权按钮的会话隔离探针（应用层）：选择归属视图会话，执行门（Runtime）
// 与审批面（ApprovalBroker）都必须按同一个会话生效——
//   - 污染：A 点全权不得放行 B 的权限请求/结掉 B 的待批审批；
//   - 失灵：B 会话的 chat 起点同步不得关掉 A 的全权。

// waitPendingApprovalsFor 等待指定会话的待批审批数达到 want。
func waitPendingApprovalsFor(t *testing.T, service *Service, sessionID string, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(service.Approval.PendingBySession(sessionID)) == want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("session %q pending approvals = %d, want %d",
		sessionID, len(service.Approval.PendingBySession(sessionID)), want)
}

// TestProbeFullAccessToggleStaysWithinViewSession：视图会话点全权只影响自己。
func TestProbeFullAccessToggleStaysWithinViewSession(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	runtime := service.Deps.Runtime.(*fakeRuntime)
	viewSession := service.Snapshot().Session.ID
	const backgroundSession = "session-background"

	// 后台会话 B 已有一笔等待用户确认的权限请求。
	go func() {
		_, _ = service.Approval.Request(context.Background(), ApprovalRequest{
			ID: "perm-b", SessionID: backgroundSession, PermissionRequest: true,
		})
	}()
	waitPendingApprovalsFor(t, service, backgroundSession, 1)

	if !service.SetFullAccess(true) {
		t.Fatal("SetFullAccess(true) = false, want true（视图会话生效值）")
	}

	// 1) 没替 B 点头：B 的待批仍在等待。
	if remaining := service.Approval.PendingBySession(backgroundSession); len(remaining) != 1 {
		t.Fatalf("污染：A 点全权结掉了 B 会话的待批审批（remaining=%d）", len(remaining))
	}
	// 2) 没替 B 放行新请求：B 的下一笔权限请求也必须等用户。
	go func() {
		_, _ = service.Approval.Request(context.Background(), ApprovalRequest{
			ID: "perm-b2", SessionID: backgroundSession, PermissionRequest: true,
		})
	}()
	waitPendingApprovalsFor(t, service, backgroundSession, 2)
	// 3) 执行门按会话解析：A 全权、B 不是。
	if !runtime.FullAccessFor(viewSession) {
		t.Fatal("视图会话的执行门未进入全权")
	}
	if runtime.FullAccessFor(backgroundSession) {
		t.Fatal("污染：后台会话继承了视图会话的全权")
	}
	// 4) A 自己的权限请求仍被放行（全权在本会话内生效）。
	decision, err := service.Approval.Request(context.Background(), ApprovalRequest{
		ID: "perm-a", SessionID: viewSession, PermissionRequest: true,
	})
	if err != nil || decision.OptionID != "allow" {
		t.Fatalf("视图会话的权限请求 = %#v, err=%v（应被全权放行，且不留永久规则）", decision, err)
	}

	// 收尾：显式结掉 B 的待批（不留给 Shutdown 之外的悬挂）。
	for _, id := range []string{"perm-b", "perm-b2"} {
		if err := service.Approval.Resolve(id, ApprovalDecision{OptionID: "deny"}); err != nil {
			t.Fatalf("Resolve(%s): %v", id, err)
		}
	}
}

// TestProbeFullAccessSurvivesBackgroundSessionStartSync：别的会话起跑
// （chat 起点的 syncFullAccessFor）不得把视图会话的全权关掉。
func TestProbeFullAccessSurvivesBackgroundSessionStartSync(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	runtime := service.Deps.Runtime.(*fakeRuntime)
	viewSession := service.Snapshot().Session.ID
	const backgroundSession = "session-background"

	if !service.SetFullAccess(true) {
		t.Fatal("SetFullAccess(true) = false, want true")
	}

	// B 起跑：chat 起点按自己的模式同步执行门（后台会话未选择 → 进程默认）。
	service.syncFullAccessFor(backgroundSession)

	if !runtime.FullAccessFor(viewSession) {
		t.Fatal("失灵：后台会话的起点同步关掉了视图会话的全权")
	}
	if runtime.FullAccessFor(backgroundSession) {
		t.Fatal("污染：后台会话起点同步继承到别的会话的全权")
	}

	// 视图会话切回自己：全权按自己的选择立即恢复（不需要等 B 结束）。
	service.syncFullAccessFor(viewSession)
	if !runtime.FullAccessFor(viewSession) {
		t.Fatal("视图会话的起点同步未按其选择恢复全权")
	}

	// 关掉全权只影响本会话。
	service.SetFullAccess(false)
	if runtime.FullAccessFor(viewSession) {
		t.Fatal("SetFullAccess(false) 后视图会话执行门仍为全权")
	}
}
