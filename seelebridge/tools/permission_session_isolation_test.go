package tools

import (
	"context"
	"testing"
	"time"

	frameworktools "github.com/RedHuang-0622/Seele/tools"
	toolspermission "github.com/RedHuang-0622/Seele/tools/permission"
)

// probeSessionKey 是探针用的会话路由键（生产由 seelebridge 根包注入
// telemetry 会话键；此处只验证「按归属会话解析」这一件事）。
type probeSessionKey struct{}

// probeCtx 构造带会话归属的调度 ctx（"" = 无归属/legacy 路径）。
func probeCtx(sessionID string) context.Context {
	return context.WithValue(context.Background(), probeSessionKey{}, sessionID)
}

// newFullAccessProbe 构造探针门：manual + 显式 deny 规则，SessionFromContext
// 从 ctx 读会话归属。deny 规则意味着「不放行就必然被拒」。
func newFullAccessProbe() (*PermissionGate, frameworktools.ToolHandler, *int) {
	state := &PermissionGate{SessionFromContext: func(ctx context.Context) string {
		sessionID, _ := ctx.Value(probeSessionKey{}).(string)
		return sessionID
	}}
	state.Set(toolspermission.PermissionConfig{
		Mode:  toolspermission.ModeManual,
		Rules: []toolspermission.PermissionRule{{ToolName: "bash", Action: toolspermission.ActionDeny}},
	}, nil)
	ran := new(int)
	handler := state.Middleware(time.Minute)("bash", frameworktools.ToolMeta{}, frameworktools.HandlerFunc(func(context.Context, string) (string, error) {
		*ran++
		return "ok", nil
	}))
	return state, handler, ran
}

// TestProbeFullAccessDoesNotLeakAcrossSessions：全权是「会话内的用户决定」，
// 不得泄漏到其它会话——A 打开全权后，B 的工具调用必须仍按 B 自己的模式
// （manual 规则）判定，否则等于替 B 会话静默放行（跨会话权限污染）。
func TestProbeFullAccessDoesNotLeakAcrossSessions(t *testing.T) {
	state, handler, ran := newFullAccessProbe()

	state.SetFullAccessFor("session-a", true)
	if _, err := handler.Execute(probeCtx("session-a"), "{}"); err != nil {
		t.Fatalf("会话 A 开全权后自己的工具调用应放行: %v", err)
	}
	if _, err := handler.Execute(probeCtx("session-b"), "{}"); err == nil {
		t.Fatal("污染：A 的全权放行了 B 会话的工具调用（B 未开全权，应被 deny 规则拦下）")
	}
	if _, err := handler.Execute(probeCtx(""), "{}"); err == nil {
		t.Fatal("污染：无会话归属的调用继承了某个会话的全权，应回退进程默认（manual）")
	}
	if *ran != 1 {
		t.Fatalf("真正执行的调用数 = %d, want 1（只有 A 放行）", *ran)
	}
}

// TestProbeFullAccessSurvivesOtherSessionSync：其它会话的 chat 起点同步
// （syncFullAccessFor 语义）不得把 A 的全权关掉——否则用户在 A 上点开的
// 全权会在 B 起跑/切会话后失效，表现为「点了全权仍弹审批」。
func TestProbeFullAccessSurvivesOtherSessionSync(t *testing.T) {
	state, handler, _ := newFullAccessProbe()

	state.SetFullAccessFor("session-a", true)
	// B 未选择全权：chat 起点按自己的模式同步（不变量：只影响 B）。
	state.SetFullAccessFor("session-b", false)

	if _, err := handler.Execute(probeCtx("session-a"), "{}"); err != nil {
		t.Fatalf("失灵：B 的起点同步关掉了 A 的全权: %v", err)
	}
	if _, err := handler.Execute(probeCtx("session-b"), "{}"); err == nil {
		t.Fatal("B 显式关闭全权后应恢复 manual 规则（deny）")
	}
	if got := state.FullAccessFor("session-a"); !got {
		t.Fatal("FullAccessFor(A) = false, want true")
	}
	if got := state.FullAccessFor("session-b"); got {
		t.Fatal("FullAccessFor(B) = true, want false")
	}
}

// TestProbeFullAccessPerSessionSurvivesSet：运行期重装权限配置（Set 重建
// checker）不得把会话级全权悄悄退回 manual（历史缺陷：粘性意图只记一个
// 进程级布尔，Set 后全权被吃掉）。
func TestProbeFullAccessPerSessionSurvivesSet(t *testing.T) {
	state, handler, _ := newFullAccessProbe()

	state.SetFullAccessFor("session-a", true)
	state.Set(toolspermission.PermissionConfig{
		Mode:  toolspermission.ModeManual,
		Rules: []toolspermission.PermissionRule{{ToolName: "bash", Action: toolspermission.ActionDeny}},
	}, nil)

	if _, err := handler.Execute(probeCtx("session-a"), "{}"); err != nil {
		t.Fatalf("Set 之后会话 A 的全权必须保持: %v", err)
	}
	if _, err := handler.Execute(probeCtx("session-b"), "{}"); err == nil {
		t.Fatal("Set 之后 B 仍须按 manual 规则（deny）")
	}
}

// TestProbeFullAccessDefaultStaysProcessScoped：进程级默认（CLI
// -permission full_access / 装配期捕获）仍是「未选择会话」的回退面，
// 且会话级显式选择优先于它。
func TestProbeFullAccessDefaultStaysProcessScoped(t *testing.T) {
	state, handler, _ := newFullAccessProbe()

	state.SetFullAccess(true) // 进程默认（legacy 面）
	if _, err := handler.Execute(probeCtx("session-any"), "{}"); err != nil {
		t.Fatalf("进程默认全权下未选择会话应放行: %v", err)
	}
	state.SetFullAccessFor("session-b", false)
	if _, err := handler.Execute(probeCtx("session-b"), "{}"); err == nil {
		t.Fatal("会话 b 显式关闭全权应优先于进程默认（deny）")
	}
	if _, err := handler.Execute(probeCtx("session-a"), "{}"); err != nil {
		t.Fatalf("其它未选择会话仍应跟随进程默认: %v", err)
	}
	if !state.FullAccess() {
		t.Fatal("FullAccess() 应返回进程默认（装配期捕获面）")
	}
	state.SetFullAccess(false)
	if state.FullAccess() {
		t.Fatal("FullAccess() = true, want false")
	}
	if _, err := handler.Execute(probeCtx("session-a"), "{}"); err == nil {
		t.Fatal("进程默认关掉后未选择会话应恢复 manual 规则（deny）")
	}
}
