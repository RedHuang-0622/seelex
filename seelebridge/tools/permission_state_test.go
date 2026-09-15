package tools

import (
	"context"
	"strings"
	"testing"

	frameworktools "github.com/RedHuang-0622/Seele/tools"
	toolspermission "github.com/RedHuang-0622/Seele/tools/permission"
)

func TestPermissionGateReportsFullAccessState(t *testing.T) {
	state := &PermissionGate{}
	state.Set(toolspermission.PermissionConfig{Mode: toolspermission.ModeManual}, nil)
	if state.FullAccess() {
		t.Fatal("manual permission gate reported full access")
	}
	state.SetFullAccess(true)
	if !state.FullAccess() {
		t.Fatal("permission gate did not report enabled full access")
	}
	state.SetFullAccess(false)
	if state.FullAccess() {
		t.Fatal("permission gate did not restore manual mode")
	}
}

// TestPermissionGateFullAccessShortCircuitsBeforeChecker：全权开启后，权限
// 中间件必须**先**读全权意图——否则"点了全权仍有个别工具被拒/仍弹审批"
// 这类时序窗会复现（checker 尚未切换到 full_access、或恰好在 Set 重建的瞬间）。
func TestPermissionGateFullAccessShortCircuitsBeforeChecker(t *testing.T) {
	state := &PermissionGate{}
	// manual + 显式 deny：不开全权时该工具必然被拒。
	state.Set(toolspermission.PermissionConfig{
		Mode:  toolspermission.ModeManual,
		Rules: []toolspermission.PermissionRule{{ToolName: "bash", Action: toolspermission.ActionDeny}},
	}, nil)

	ran := 0
	handler := state.Middleware(0)("bash", frameworktools.HandlerFunc(func(context.Context, string) (string, error) {
		ran++
		return "ok", nil
	}))

	if _, err := handler.Execute(context.Background(), "{}"); err == nil {
		t.Fatal("manual + deny 规则下 bash 应被拒")
	}
	if ran != 0 {
		t.Fatalf("被拒的工具不应真正执行，ran=%d", ran)
	}

	// 全权开启：即使 manual 规则里写着 deny，也必须放行。
	state.SetFullAccess(true)
	if _, err := handler.Execute(context.Background(), "{}"); err != nil {
		t.Fatalf("全权开启后不应再被拒: %v", err)
	}
	if ran != 1 {
		t.Fatalf("全权开启后工具应真正执行，ran=%d", ran)
	}

	// 全权期间再 Set 一次（例如运行期重装权限配置）不得把全权悄悄退回 manual。
	state.Set(toolspermission.PermissionConfig{
		Mode:  toolspermission.ModeManual,
		Rules: []toolspermission.PermissionRule{{ToolName: "bash", Action: toolspermission.ActionDeny}},
	}, nil)
	if _, err := handler.Execute(context.Background(), "{}"); err != nil {
		t.Fatalf("Set 之后全权意图必须保持: %v", err)
	}
	if ran != 2 {
		t.Fatalf("ran=%d, want 2", ran)
	}

	// 关掉全权：manual 规则恢复生效。
	state.SetFullAccess(false)
	if _, err := handler.Execute(context.Background(), "{}"); err == nil {
		t.Fatal("关闭全权后 deny 规则应恢复生效")
	}
	if !strings.Contains("bash: permission denied by policy", "denied") {
		t.Fatal("sanity")
	}
}

// TestPermissionGateFullAccessBeforeSetIsNotLost：全权请求早于权限配置装配
// （Set）时不能被丢掉——否则宿主按 full_access 启动，实际仍在 manual 询问。
func TestPermissionGateFullAccessBeforeSetIsNotLost(t *testing.T) {
	state := &PermissionGate{}
	state.SetFullAccess(true)
	if !state.FullAccess() {
		t.Fatal("未装配 checker 时全权意图应被记住")
	}
	state.Set(toolspermission.PermissionConfig{
		Mode:  toolspermission.ModeManual,
		Rules: []toolspermission.PermissionRule{{ToolName: "bash", Action: toolspermission.ActionDeny}},
	}, nil)
	if !state.FullAccess() {
		t.Fatal("装配 checker 后全权意图应保持")
	}
	ran := 0
	handler := state.Middleware(0)("bash", frameworktools.HandlerFunc(func(context.Context, string) (string, error) {
		ran++
		return "ok", nil
	}))
	if _, err := handler.Execute(context.Background(), "{}"); err != nil {
		t.Fatalf("装配后全权应直接放行: %v", err)
	}
	if ran != 1 {
		t.Fatalf("ran=%d, want 1", ran)
	}
}
