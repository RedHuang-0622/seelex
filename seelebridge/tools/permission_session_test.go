package tools

import (
	"context"
	"testing"
	"time"

	frameworktools "github.com/RedHuang-0622/Seele/tools"
	toolspermission "github.com/RedHuang-0622/Seele/tools/permission"
)

// TestPermissionMiddlewareAttachesSessionFromContext 波 4 approval 会话级
// 归属：权限审批请求随调度 ctx 携带会话 ID（注入的会话路由键），不再以
// 进程级空归属进入视图单格。
func TestPermissionMiddlewareAttachesSessionFromContext(t *testing.T) {
	state := &PermissionGate{}
	state.SessionFromContext = func(ctx context.Context) string { return "sess-perm" }
	var gotRequest toolspermission.ApprovalRequest
	state.Set(toolspermission.PermissionConfig{Mode: toolspermission.ModeManual},
		func(ctx *toolspermission.ApprovalContext) (*toolspermission.ApprovalResponse, error) {
			gotRequest = ctx.Request
			return &toolspermission.ApprovalResponse{
				RequestID: ctx.Request.ID, Choice: "allow",
			}, nil
		})

	middleware := state.Middleware(time.Second)
	next := frameworktools.HandlerFunc(func(ctx context.Context, argsJSON string) (string, error) {
		return "ok", nil
	})
	handler := middleware("bash", next)
	out, err := handler.Execute(context.Background(), `{"cmd":"pwd"}`)
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if out != "ok" {
		t.Fatalf("output = %q, want ok", out)
	}
	if gotRequest.SessionID != "sess-perm" {
		t.Fatalf("permission request session id = %q, want sess-perm", gotRequest.SessionID)
	}
	if gotRequest.ToolName != "bash" {
		t.Fatalf("permission request tool = %q, want bash", gotRequest.ToolName)
	}
}

// TestPermissionMiddlewareLeavesEmptySessionWithoutResolver 未注入会话解析
// 器（legacy/测试桩）时权限审批保持空归属，不 panic。
func TestPermissionMiddlewareLeavesEmptySessionWithoutResolver(t *testing.T) {
	state := &PermissionGate{}
	var gotRequest toolspermission.ApprovalRequest
	state.Set(toolspermission.PermissionConfig{Mode: toolspermission.ModeManual},
		func(ctx *toolspermission.ApprovalContext) (*toolspermission.ApprovalResponse, error) {
			gotRequest = ctx.Request
			return &toolspermission.ApprovalResponse{
				RequestID: ctx.Request.ID, Choice: "allow",
			}, nil
		})
	handler := state.Middleware(time.Second)("bash", frameworktools.HandlerFunc(func(ctx context.Context, argsJSON string) (string, error) {
		return "ok", nil
	}))
	if _, err := handler.Execute(context.Background(), `{}`); err != nil {
		t.Fatalf("handler: %v", err)
	}
	if gotRequest.SessionID != "" {
		t.Fatalf("permission request session id = %q, want empty", gotRequest.SessionID)
	}
}
