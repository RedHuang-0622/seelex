package adapters

// session_workspace_connection_test.go —— **远端工作目录**在适配层的往返判据。
//
// 链路：store.SSHEndpoint.RemoteRoot → model.WorkspaceSSHInfo.RemoteRoot →
// registry.SSHEndpoint.RemoteRoot（由 assemble.sshEndpoint 折出）。任何一段丢掉这一格，
// 路径门在远端面上就退回"按本地根当筛子"的保守口径——不是报错，而是判据悄悄变粗。
// 所以每一段都要有用例。

import (
	"testing"

	"github.com/RedHuang-0622/seelex/middle/protocol/model"
	"github.com/RedHuang-0622/seelex/store/workspace"
)

// TestWorkspacePortCarriesRemoteRoot：读面、写面、摘要三条路都带着远端工作目录。
func TestWorkspacePortCarriesRemoteRoot(t *testing.T) {
	port := WorkspacePort{Repo: workspace.NewRepo()}
	created, err := port.Create("remote-target", t.TempDir(), "")
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}

	conn, err := port.SetWorkspaceConnection(created.ID, model.WorkspaceConnectionChange{
		SSH: &model.WorkspaceSSHInfo{
			Host: "deploy.example", Port: 22, User: "deploy", RemoteRoot: "/srv/app",
		},
	})
	if err != nil {
		t.Fatalf("set connection: %v", err)
	}
	if conn.SSH == nil || conn.SSH.RemoteRoot != "/srv/app" {
		t.Fatalf("写面读回 = %+v，want remote_root=/srv/app", conn.SSH)
	}

	read, err := port.WorkspaceConnection(created.ID)
	if err != nil {
		t.Fatalf("read connection: %v", err)
	}
	if read.SSH == nil || read.SSH.RemoteRoot != "/srv/app" {
		t.Fatalf("读面 = %+v，want remote_root=/srv/app（读回值必须与写入一致）", read.SSH)
	}

	// 摘要（工作区列表/快照走这一条）也要带上：装配层靠它折出选择器端点。
	info, err := port.Get(created.ID)
	if err != nil {
		t.Fatalf("get workspace: %v", err)
	}
	if info.SSH == nil || info.SSH.RemoteRoot != "/srv/app" {
		t.Fatalf("工作区摘要 = %+v，want ssh.remote_root=/srv/app", info.SSH)
	}
}
