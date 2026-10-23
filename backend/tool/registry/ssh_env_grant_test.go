package registry

import (
	"context"
	"testing"
)

// ssh_env_grant_test.go 钉住"环境放权"这道闸门的三格：
//  1. 环境在项目表里、但**没在本会话放权** → SSH_ENV_NOT_GRANTED（不是 UNKNOWN：码不同，
//     下一步动作也不同——一个要放权，一个要去项目里加环境）；
//  2. 放权了 → 不因放权被拦（后续可行性交给探针/建连段）；
//  3. 放权面没装配 → 一律拒绝（fail-closed，不把"不知道"当成"都放行"）。
//
// 为什么这条闸门必须在决策层而不是只在写入侧：显式 `env:` 参数是绕过会话状态的一条
// 直路，只在 SetSessionSSHAllowed 校验的话，任何能写工具参数的调用方都能指到未放权的
// 环境上。

func grantDeps(granted func(context.Context, string, string) bool) TransportSelectorDeps {
	return TransportSelectorDeps{
		Enabled:    true,
		SessionKey: func(context.Context) string { return "session-1" },
		Project:    func(context.Context, string) string { return "project-1" },
		EndpointFor: func(string, string) (SSHEndpoint, bool, error) {
			return SSHEndpoint{Host: "dev.example.com", Port: 22, User: "builder"}, true, nil
		},
		EnvGranted: granted,
	}
}

func TestExplicitEnvWithoutGrantIsRejected(t *testing.T) {
	deps := grantDeps(func(context.Context, string, string) bool { return false })
	decision := resolveTransportDecision(context.Background(), deps, "bash", true, TransportSSH, "prod")

	if decision.FailureCode != CodeSSHEnvNotGranted {
		t.Fatalf("失败码 = %q, want %q（未放权≠未配置，两个码不能混）", decision.FailureCode, CodeSSHEnvNotGranted)
	}
	if decision.Env != "prod" {
		t.Fatalf("决策里的环境名 = %q, want prod（回执要如实写这次指名了什么）", decision.Env)
	}
}

func TestExplicitEnvWithGrantPassesGate(t *testing.T) {
	deps := grantDeps(func(_ context.Context, sessionID, env string) bool {
		return sessionID == "session-1" && env == "dev"
	})
	decision := resolveTransportDecision(context.Background(), deps, "bash", true, TransportSSH, "dev")

	if decision.FailureCode != "" {
		t.Fatalf("已放权的环境不该被拦，got %q（%s）", decision.FailureCode, decision.FailureMsg)
	}
	if !decision.Remote() {
		t.Fatal("指名 ssh + 有端点，命名空间该在远端")
	}
}

func TestEnvGateWithoutGrantPortIsFailClosed(t *testing.T) {
	deps := grantDeps(nil) // 放权面未装配
	decision := resolveTransportDecision(context.Background(), deps, "bash", true, TransportSSH, "dev")

	if decision.FailureCode != CodeSSHEnvNotGranted {
		t.Fatalf("放权面未装配时该拒绝，got %q", decision.FailureCode)
	}
}
