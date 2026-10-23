package assemble

import (
	"testing"

	"github.com/RedHuang-0622/seelex/middle/protocol/model"
)

// ssh_env_endpoint_test.go 钉住"按环境名解析端点"的判据：
//  1. 命中环境 → 端点带着 host/port/user/远端根一起出来（远端根决定路径门的根内/越界）；
//  2. 环境名不在表里 → ok=false（决策段据此给 SSH_ENV_UNKNOWN，**不回落默认环境**）；
//  3. 表里有行但没填 host → 也算查不到（"这个环境今天不可用"，不是"用默认顶上"）；
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

func TestSSHEndpointForEnvResolvesNamedEnvironment(t *testing.T) {
	port := &fakeEnvironmentPort{environments: []model.WorkspaceEnvironmentInfo{
		{Name: "dev", Host: "dev.example.com", Port: 2222, User: "builder", RemoteRoot: "/srv/app"},
		{Name: "blank"},
	}}
	runtime := &Runtime{}
	runtime.SetWorkspaceEnvironmentPort(port)

	endpoint, ok, err := runtime.sshEndpointForEnv("project-1", "dev")
	if err != nil || !ok {
		t.Fatalf("dev 该命中: ok=%v err=%v", ok, err)
	}
	if endpoint.Host != "dev.example.com" || endpoint.Port != 2222 || endpoint.User != "builder" {
		t.Fatalf("端点 = %#v, want dev 的 host/port/user", endpoint)
	}
	if endpoint.RemoteRoot != "/srv/app" {
		t.Fatalf("远端根 = %q, want /srv/app（路径门的根内/越界靠它）", endpoint.RemoteRoot)
	}

	// 查不到 → ok=false（不回落）
	if _, ok, _ := runtime.sshEndpointForEnv("project-1", "prod"); ok {
		t.Fatal("表里没有 prod，不该命中（回落默认环境就是连错机器）")
	}
	// 表里有行但没填 host → 也算查不到
	if _, ok, _ := runtime.sshEndpointForEnv("project-1", "blank"); ok {
		t.Fatal("没填 host 的环境不该命中")
	}
}

func TestSSHEndpointForEnvSkipsTableWhenEnvEmpty(t *testing.T) {
	port := &fakeEnvironmentPort{environments: []model.WorkspaceEnvironmentInfo{
		{Name: "dev", Host: "dev.example.com", Port: 22, User: "u"},
	}}
	runtime := &Runtime{}
	runtime.SetWorkspaceEnvironmentPort(port)

	if _, ok, _ := runtime.sshEndpointForEnv("project-1", ""); ok {
		t.Fatal("没指名环境时不该从环境表命中")
	}
	if port.calls != 0 {
		t.Fatalf("没指名环境时根本不该查环境表，实际查了 %d 次", port.calls)
	}
}

func TestSSHEndpointForEnvWithoutPortIsNotConfigured(t *testing.T) {
	runtime := &Runtime{}
	if _, ok, err := runtime.sshEndpointForEnv("project-1", "dev"); ok || err != nil {
		t.Fatalf("未装配环境表 = 查不到（ok=false），got ok=%v err=%v", ok, err)
	}
	// 单条连接同理：未装配 = 未配连接（本地执行），不许 panic。
	if _, ok, err := runtime.sshEndpointForProject("project-1"); ok || err != nil {
		t.Fatalf("未装配连接读面 = 未配连接，got ok=%v err=%v", ok, err)
	}
}
