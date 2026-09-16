package main

import (
	"context"
	"strings"
	"testing"
	"time"

	frameworktools "github.com/RedHuang-0622/Seele/tools"
	toolspermission "github.com/RedHuang-0622/Seele/tools/permission"
	"github.com/RedHuang-0622/seelex/application"
	seeltools "github.com/RedHuang-0622/seelex/seelebridge/tools"
)

// permission_employee_panel_test.go — 跨层验收：员工（角色会话）越权提权的审批
// **落在现有审批面板上**。
//
// 口径（用户原话）：继承口径的会话继承全量工具，由中间件审查"他有没有使用这个工具
// 的权限"，需要越权使用的工具**用现有的**人为审批面板。
//
// 为什么这一条必须跨层验：中间件判定与"面板看不看得见"是两件事。审批请求由权限门
// 写进 ctx 的会话归属决定呈现位置——角色会话（goal-a2a-owner）不是用户视图里的会话，
// 若按角色会话号归属，既进不了视图单格 Interaction，也没有会话单元承载
// awaiting_approval（application/core 的 observeInteraction 只镜像视图会话/空归属），
// 于是对宿主完全不可见，只能等到审批超时被拒。折算到宿主主会话（RoleSessionOwner）
// 后，面板/目录/会话快照三条既有读面原样复用——这就是"直接用现有的"。

// TestEmployeeEscalationLandsInOwnerSessionPanel 走真实组合根桥接（newPermissionBridge）
// 验收：员工越权提权 → broker 待批 → 归属宿主主会话 → 放行后调用执行。
func TestEmployeeEscalationLandsInOwnerSessionPanel(t *testing.T) {
	const (
		roleSessionID = "goal-a2a-owner"
		mainSessionID = "sess-main"
	)
	cfg := mergedPermissionConfig(t)
	// 用真实事件 hub（NewApprovalBroker 接收 *EventHub；传 nil 会变成"非 nil 接口持
	// 空指针"的坑，生产装配也没有无 hub 的路径）。
	broker := application.NewApprovalBroker(application.NewEventHub())

	gate := &seeltools.PermissionGate{SessionFromContext: toolspermission.SessionIDFromContext}
	gate.Set(cfg, newPermissionBridge(broker))
	// 角色会话权责：空 = 继承宿主默认（工具面全量，逐次由中间件判定）。
	gate.SetRoleSessionPolicyResolver(func(sessionID string) (string, bool) {
		return "", sessionID == roleSessionID
	})
	// 审批归属折算：角色会话 → 宿主主会话（组合根注入同一份反查索引）。
	gate.SetRoleSessionOwnerResolver(func(sessionID string) (string, bool) {
		return mainSessionID, sessionID == roleSessionID
	})

	done := make(chan error, 1)
	go func() {
		_, err := gate.Middleware(0)("write_file", frameworktools.ToolMeta{},
			frameworktools.HandlerFunc(func(context.Context, string) (string, error) { return "ok", nil })).
			Execute(toolspermission.WithSessionID(context.Background(), roleSessionID), `{}`)
		done <- err
	}()

	var pendingSession, pendingTool, pendingID string
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		entries := broker.Pending()
		if len(entries) > 0 {
			pendingSession = entries[0].SessionID
			pendingTool = entries[0].Interaction.ToolName
			pendingID = entries[0].Interaction.ID
		}
		if len(entries) == 1 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if pendingID == "" {
		t.Fatalf("员工越权提权没有落到审批 broker：pending=%d", len(broker.Pending()))
	}
	if pendingSession != mainSessionID {
		t.Fatalf("审批归属 = %q, want %q（角色会话号没有面板承载）", pendingSession, mainSessionID)
	}
	// 面板读面：视图单格/目录 awaiting_approval 都按会话归属取待批。
	if got := broker.PendingBySession(mainSessionID); len(got) != 1 {
		t.Fatalf("宿主主会话的待批列表 = %d 条, want 1（面板读不出来等于没有面板）", len(got))
	}
	if got := broker.PendingBySession(roleSessionID); len(got) != 0 {
		t.Fatalf("角色会话号下不该再有待批：%d 条", len(got))
	}
	if !strings.Contains(pendingTool, "write_file") {
		t.Fatalf("审批工具名 = %q, want 含 write_file（人类要看见谁要干什么）", pendingTool)
	}

	if err := broker.Resolve(pendingID, application.ApprovalDecision{OptionID: "allow"}); err != nil {
		t.Fatalf("resolve approval: %v", err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("人类放行后工具调用应成功：%v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("工具调用没有在放行后返回（审批没有真正接到执行门上）")
	}
}
