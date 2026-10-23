package assemble

// runtime_transport_remote_root_test.go —— 连接配置 → 选择器端点的**取根**折算。
//
// 这一格是"远端根不再是恒空"的落点：model.WorkspaceSSHInfo.RemoteRoot 由本层的
// sshEndpoint 折进 registry.SSHEndpoint.RemoteRoot，路径门在远端面上才有一个真正的根
// 可比对（根内路径不需要 ssh.userspace）。折算丢了这一格不会报错，只会让判据悄悄退回
// 保守口径——所以钉一条。

import (
	"testing"

	"github.com/RedHuang-0622/seelex/middle/protocol/model"
)

func TestSSHEndpointCarriesRemoteRoot(t *testing.T) {
	connection := model.WorkspaceConnection{
		SSH: &model.WorkspaceSSHInfo{
			Host: "deploy.example", Port: 2200, User: "deploy", RemoteRoot: "/srv/app",
		},
	}
	endpoint, ok, err := sshEndpoint(connection)
	if err != nil || !ok {
		t.Fatalf("sshEndpoint: ok=%v err=%v", ok, err)
	}
	if endpoint.Host != "deploy.example" || endpoint.Port != 2200 || endpoint.User != "deploy" {
		t.Fatalf("端点 = %+v，want host/port/user 原样折出", endpoint)
	}
	if endpoint.RemoteRoot != "/srv/app" {
		t.Fatalf("远端项目根 = %q，want /srv/app（空 = 路径门退回保守口径，不是这里的行为）", endpoint.RemoteRoot)
	}

	// 未配连接 → (零值, false)：等价于"该项目没有远端"，路径门走本地面。
	if _, configured, err := sshEndpoint(model.WorkspaceConnection{}); err != nil || configured {
		t.Fatalf("未配连接 = configured:%v err:%v，want false/nil", configured, err)
	}
	// 配了 host 但没填远端工作目录 → 仍然配了，只是根未知（空）。
	unknownRoot, configured, err := sshEndpoint(model.WorkspaceConnection{
		SSH: &model.WorkspaceSSHInfo{Host: "deploy.example", User: "deploy"},
	})
	if err != nil || !configured {
		t.Fatalf("配了 host 应视为已配：configured=%v err=%v", configured, err)
	}
	if unknownRoot.RemoteRoot != "" {
		t.Fatalf("未填远端工作目录时 RemoteRoot = %q，want 空（未知 ≠ 猜一个根）", unknownRoot.RemoteRoot)
	}
}
