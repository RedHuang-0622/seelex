package approval

import (
	"context"
	"testing"
	"time"
)

// 会话隔离探针（全权按钮的审批面）：A 会话打开全权，不得替 B 会话的
// 待批审批点头，也不得静默放行 B 会话需要用户确认的工具调用。

// waitApprovalPending 等待指定归属的待批审批出现（最多 2s）。
func waitApprovalPending(t *testing.T, broker *ApprovalBroker, sessionID string, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(broker.PendingBySession(sessionID)) == want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("session %q pending approvals = %d, want %d", sessionID, len(broker.PendingBySession(sessionID)), want)
}

// TestProbePermissionAutoApprovalIsPerSession：会话级自动放行只覆盖该会话。
func TestProbePermissionAutoApprovalIsPerSession(t *testing.T) {
	broker := NewApprovalBroker(nil)
	defer broker.Shutdown()
	broker.SetPermissionAutoApprovalFor("session-a", true)

	doneA := make(chan ApprovalDecision, 1)
	go func() {
		decision, err := broker.Request(context.Background(), ApprovalRequest{
			ID: "perm-a", SessionID: "session-a", PermissionRequest: true,
		})
		if err == nil {
			doneA <- decision
		}
	}()
	select {
	case decision := <-doneA:
		// 自动放行只放行本笔（"allow"），**不**留永久 allow 规则（"always"
		// 会被写成共享 checker 的规则，关掉全权后仍在别的会话生效）。
		if decision.OptionID != "allow" {
			t.Fatalf("A 自动放行 decision = %#v, want allow（不得写永久规则）", decision)
		}
	case <-time.After(time.Second):
		t.Fatal("A 开了全权，自己的权限请求应被放行")
	}

	// B 未开全权：权限请求必须等用户，不能被 A 的全权放行。
	go func() {
		_, _ = broker.Request(context.Background(), ApprovalRequest{
			ID: "perm-b", SessionID: "session-b", PermissionRequest: true,
		})
	}()
	waitApprovalPending(t, broker, "session-b", 1)

	// 无会话归属（legacy）的权限请求同样不受会话级全权影响。
	go func() {
		_, _ = broker.Request(context.Background(), ApprovalRequest{
			ID: "perm-legacy", PermissionRequest: true,
		})
	}()
	waitApprovalPending(t, broker, "", 1)

	if err := broker.Resolve("perm-b", ApprovalDecision{OptionID: "deny"}); err != nil {
		t.Fatalf("Resolve(perm-b): %v", err)
	}
	if err := broker.Resolve("perm-legacy", ApprovalDecision{OptionID: "deny"}); err != nil {
		t.Fatalf("Resolve(perm-legacy): %v", err)
	}
}

// TestProbeResolveAllForIsSessionScoped：全权放行只结本会话待批，别的会话
// 的待批保持等待（跨会话放行 = 替用户点头）。
func TestProbeResolveAllForIsSessionScoped(t *testing.T) {
	broker := NewApprovalBroker(nil)
	defer broker.Shutdown()
	results := make(chan ApprovalDecision, 2)
	for _, spec := range []struct{ id, sessionID string }{
		{id: "approval-a", sessionID: "session-a"},
		{id: "approval-b", sessionID: "session-b"},
	} {
		request := ApprovalRequest{ID: spec.id, SessionID: spec.sessionID, Question: "允许执行？"}
		go func() {
			decision, err := broker.Request(context.Background(), request)
			if err == nil {
				results <- decision
			}
		}()
	}
	waitApprovalPending(t, broker, "session-a", 1)
	waitApprovalPending(t, broker, "session-b", 1)

	if count := broker.ResolveAllFor("session-a", ApprovalDecision{OptionID: "allow"}); count != 1 {
		t.Fatalf("ResolveAllFor(session-a) = %d, want 1", count)
	}
	if remaining := broker.PendingBySession("session-b"); len(remaining) != 1 {
		t.Fatalf("B 的待批被 A 的全权结掉了：remaining = %d, want 1", len(remaining))
	}
	select {
	case decision := <-results:
		if decision.OptionID != "allow" {
			t.Fatalf("A 的待批 decision = %#v, want allow", decision)
		}
	case <-time.After(time.Second):
		t.Fatal("A 的待批未被结案")
	}
	if err := broker.Resolve("approval-b", ApprovalDecision{OptionID: "deny"}); err != nil {
		t.Fatalf("Resolve(approval-b): %v", err)
	}
	select {
	case decision := <-results:
		if decision.OptionID != "deny" {
			t.Fatalf("B 的待批 decision = %#v, want deny（用户显式拒绝）", decision)
		}
	case <-time.After(time.Second):
		t.Fatal("B 的待批未被结案")
	}
}
