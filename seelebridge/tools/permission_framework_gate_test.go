package tools

import (
	"context"
	"errors"
	"testing"
	"time"

	frameworktools "github.com/RedHuang-0622/Seele/tools"
	toolspermission "github.com/RedHuang-0622/Seele/tools/permission"
	"github.com/RedHuang-0622/seelex/seelebridge/internal/model"
)

// sessionScopedGate 造一个按调用 ctx 解析会话归属的门，模拟 seelebridge 的装配。
func sessionScopedGate(sessionID string, cfg toolspermission.PermissionConfig, approval toolspermission.ApprovalHandler) *PermissionGate {
	state := &PermissionGate{}
	if sessionID != "" {
		state.SessionFromContext = func(context.Context) string { return sessionID }
	}
	state.Set(cfg, approval)
	return state
}

// TestPermissionGateDeniesWithFrameworkError 钉住接入后的拒绝口径：策略拒绝走
// DenyWithoutPrompt，直接返回可 errors.Is 归类的英文错误，**不再**是
// "permission denied by policy" 这类自定义文案，也不再需要模板式拼串。
func TestPermissionGateDeniesWithFrameworkError(t *testing.T) {
	approvals := 0
	state := sessionScopedGate("sess-deny", toolspermission.PermissionConfig{
		Mode:  toolspermission.ModeManual,
		Rules: []toolspermission.PermissionRule{{ToolName: "bash", Action: toolspermission.ActionDeny}},
	}, func(*toolspermission.ApprovalContext) (*toolspermission.ApprovalResponse, error) {
		approvals++
		return &toolspermission.ApprovalResponse{Choice: "allow"}, nil
	})

	ran := 0
	handler := state.Middleware(0)("bash", frameworktools.ToolMeta{},
		frameworktools.HandlerFunc(func(context.Context, string) (string, error) {
			ran++
			return "ok", nil
		}))

	_, err := handler.Execute(context.Background(), `{}`)
	if err == nil {
		t.Fatal("manual + deny 的 bash 应被拒绝")
	}
	if !errors.Is(err, frameworktools.ErrPermissionDenied) {
		t.Fatalf("err = %v, want errors.Is(..., ErrPermissionDenied)", err)
	}
	if errors.Is(err, frameworktools.ErrToolNotVisible) {
		t.Fatalf("策略拒绝不应被当成断位：%v", err)
	}
	if approvals != 0 {
		t.Fatalf("DenyWithoutPrompt 语义下拒绝不应弹选择页面，approvals=%d", approvals)
	}
	if ran != 0 {
		t.Fatalf("被拒绝的工具不应执行，ran=%d", ran)
	}
}

// TestPermissionGateRoutesByToolDeclaredClusters 钉住接入后真正拿到的新能力：
// 工具自带簇属（ToolMeta.Groups/Bits）优先于按名字路由，主体持有的位决定可见性。
//
// 主体口径（2026-09-16 起）：主体 = 主体类（root / sub / emp_ro / emp_rw），不再
// 等于会话 ID（见 permission_policy.go）。这里用子代理主体（有 ro/rw 位、无
// ctl/adm 位）覆盖"位齐 → 放行"与"位缺 → 不可见"两侧。
func TestPermissionGateRoutesByToolDeclaredClusters(t *testing.T) {
	cfg := DefaultPermissionConfig()
	subCtx := model.WithNodeScope(context.Background(), model.NodeScope{
		NodeID: "node-1", Role: model.RoleSubAgent,
	})

	cases := []struct {
		name    string
		meta    frameworktools.ToolMeta
		wantErr error
		wantRan int
	}{
		{
			name:    "有位则放行",
			meta:    frameworktools.ToolMeta{Kind: frameworktools.ToolKindRead, Groups: []string{GroupRO}, Bits: bitRead},
			wantRan: 1,
		},
		{
			name:    "缺位即不可见",
			meta:    frameworktools.ToolMeta{Kind: frameworktools.ToolKindWrite, Groups: []string{GroupCTL}, Bits: bitExecute},
			wantErr: frameworktools.ErrToolNotVisible,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			state := sessionScopedGate("sess-bits", cfg, nil)
			ran := 0
			handler := state.Middleware(0)("read_doc", tc.meta,
				frameworktools.HandlerFunc(func(context.Context, string) (string, error) {
					ran++
					return "ok", nil
				}))
			_, err := handler.Execute(subCtx, `{}`)
			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("err = %v, want nil", err)
				}
			} else if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want errors.Is(..., %v)", err, tc.wantErr)
			}
			if ran != tc.wantRan {
				t.Fatalf("ran = %d, want %d", ran, tc.wantRan)
			}
		})
	}
}

// TestPermissionGateApprovalRequestFilledByFramework 钉住接入后审批请求由框架
// 填齐：ID 唯一、SessionID 来自调用 ctx、Risk/Preview 就绪、Timeout 透传产品值。
// 这几项正是接入前 seelex 手工拼装的部分。
func TestPermissionGateApprovalRequestFilledByFramework(t *testing.T) {
	var got toolspermission.ApprovalRequest
	state := sessionScopedGate("sess-perm", toolspermission.PermissionConfig{Mode: toolspermission.ModeManual},
		func(ctx *toolspermission.ApprovalContext) (*toolspermission.ApprovalResponse, error) {
			got = ctx.Request
			return &toolspermission.ApprovalResponse{RequestID: ctx.Request.ID, Choice: "allow"}, nil
		})

	handler := state.Middleware(time.Second)("bash", frameworktools.ToolMeta{},
		frameworktools.HandlerFunc(func(context.Context, string) (string, error) { return "ok", nil }))
	if _, err := handler.Execute(context.Background(), `{"cmd":"pwd"}`); err != nil {
		t.Fatalf("handler: %v", err)
	}

	if got.ID == "" {
		t.Fatal("审批请求 ID 未由框架填充")
	}
	if got.SessionID != "sess-perm" {
		t.Fatalf("SessionID = %q, want sess-perm（来自调用 ctx）", got.SessionID)
	}
	if got.ToolName != "bash" {
		t.Fatalf("ToolName = %q, want bash", got.ToolName)
	}
	if got.Timeout != time.Second {
		t.Fatalf("Timeout = %v, want 1s（透传产品配置）", got.Timeout)
	}
	if got.Preview == "" {
		t.Fatal("Preview 未由框架填充")
	}
	if len(got.Options) == 0 {
		t.Fatal("Options 未由框架填充")
	}
}
