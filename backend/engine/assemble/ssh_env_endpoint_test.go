package assemble

import (
	"testing"

	toolreg "github.com/RedHuang-0622/seelex/backend/tool/registry"
	"github.com/RedHuang-0622/seelex/middle/protocol/model"
)

// ssh_env_endpoint_test.go 钉住"按环境名解析执行面"的判据：
//  1. 有端点 → ssh，并带 host/port/user/远端根；无端点 → local；
//  2. 环境名不在表里 → ok=false（决策段据此给 SSH_ENV_UNKNOWN，**不回落默认环境**）；
//  3. 半截端点（只有 host/user 之一）→ 查不到；
//  4. 没指名环境（env 为空）→ 根本不查环境表（走单条连接兜底）。
//
// 用本地 fake 端口而不是 adapters.WorkspacePort：assemble 不能反向依赖装配层
// （adapters 已经依赖 assemble 的 RuntimePort），那样会成环。

type fakeEnvironmentPort struct {
	environments []model.WorkspaceEnvironmentInfo
	calls        int
}

func (port *fakeEnvironmentPort) WorkspaceEnvironments(string) ([]model.WorkspaceEnvironmentInfo, string, error) {
	port.calls++
	return port.environments, "dev", nil
}

func (port *fakeEnvironmentPort) SetWorkspaceEnvironment(string, string, model.WorkspaceEnvironmentChange) (model.WorkspaceEnvironmentInfo, error) {
	return model.WorkspaceEnvironmentInfo{}, nil
}

func (port *fakeEnvironmentPort) DeleteWorkspaceEnvironment(string, string) error { return nil }

func (port *fakeEnvironmentPort) SetDefaultWorkspaceEnvironment(string, string) error { return nil }

func TestTransportEnvironmentForEnvResolvesNamedEnvironment(t *testing.T) {
	port := &fakeEnvironmentPort{environments: []model.WorkspaceEnvironmentInfo{
		{Name: "dev", Host: "dev.example.com", Port: 2222, User: "builder", RemoteRoot: "/srv/app"},
		{Name: "blank"},
	}}
	runtime := &Runtime{}
	runtime.SetWorkspaceEnvironmentPort(port)

	target, ok, err := runtime.transportEnvironmentForEnv("project-1", "dev")
	if err != nil || !ok {
		t.Fatalf("dev 该命中: ok=%v err=%v", ok, err)
	}
	if target.Kind != toolreg.TransportSSH || target.Name != "dev" {
		t.Fatalf("target = %#v, want ssh/dev", target)
	}
	if target.Endpoint.Host != "dev.example.com" || target.Endpoint.Port != 2222 || target.Endpoint.User != "builder" {
		t.Fatalf("端点 = %#v, want dev 的 host/port/user", target.Endpoint)
	}
	if target.Endpoint.RemoteRoot != "/srv/app" {
		t.Fatalf("远端根 = %q, want /srv/app（路径门的根内/越界靠它）", target.Endpoint.RemoteRoot)
	}
	local, ok, err := runtime.transportEnvironmentForEnv("project-1", "blank")
	if err != nil || !ok || local.Kind != toolreg.TransportLocal || local.Name != "blank" {
		t.Fatalf("空端点环境应为 local：%#v ok=%v err=%v", local, ok, err)
	}

	// 查不到 → ok=false（不回落）
	if _, ok, _ := runtime.transportEnvironmentForEnv("project-1", "prod"); ok {
		t.Fatal("表里没有 prod，不该命中（回落默认环境就是连错机器）")
	}
}

func TestTransportEnvironmentForEnvSkipsTableWhenEnvEmpty(t *testing.T) {
	port := &fakeEnvironmentPort{environments: []model.WorkspaceEnvironmentInfo{
		{Name: "dev", Host: "dev.example.com", Port: 22, User: "u"},
	}}
	runtime := &Runtime{}
	runtime.SetWorkspaceEnvironmentPort(port)

	if _, ok, _ := runtime.transportEnvironmentForEnv("project-1", ""); ok {
		t.Fatal("没指名环境时不该从环境表命中")
	}
	if port.calls != 0 {
		t.Fatalf("没指名环境时根本不该查环境表，实际查了 %d 次", port.calls)
	}
}

func TestTransportEnvironmentForEnvWithoutPortIsNotConfigured(t *testing.T) {
	runtime := &Runtime{}
	if _, ok, err := runtime.transportEnvironmentForEnv("project-1", "dev"); ok || err != nil {
		t.Fatalf("未装配环境表 = 查不到（ok=false），got ok=%v err=%v", ok, err)
	}
	// 单条连接同理：未装配 = 未配连接（本地执行），不许 panic。
	if _, ok, err := runtime.sshEndpointForProject("project-1"); ok || err != nil {
		t.Fatalf("未装配连接读面 = 未配连接，got ok=%v err=%v", ok, err)
	}
}
