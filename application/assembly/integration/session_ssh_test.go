package integration

import (
	. "github.com/RedHuang-0622/seelex/application/assembly"
	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/contract/protocol"
	"testing"
)

// session_ssh_test.go 钉住两条不变式的判据（纯函数层）：
//  1. 放权集合必须是项目环境表的**子集**（写进不存在的名字要显式失败）；
//  2. 首次绑定的会话工作环境不可切换或清空。
//
// 端到端（会话端口 + 项目环境表）的路径由装配侧的测试覆盖；这两条是**规则本身**，
// 放在纯函数上跑得最快，也最难被后续重构悄悄改掉。

func TestSessionEnvironmentCanBeFixedOnceAndNeverChanged(t *testing.T) {
	workspace := &environmentTraceWorkspace{
		fakeWorkspace: newFakeWorkspace(),
		environments: []model.WorkspaceEnvironmentInfo{
			{Name: "dev"},
			{Name: "test", Host: "test.example.com", User: "qa"},
		},
	}
	project, err := workspace.Create("app", "/workspace/app", "")
	if err != nil {
		t.Fatal(err)
	}
	sessions := &environmentTraceSessions{}
	service := newTestService(t, &fakeEngine{},
		func(deps *contract.Dependencies) { deps.Workspace = workspace },
		withTestSessions(sessions),
	)
	list, _, err := ComponentsFor(service).Workspace.Projects.WorkspaceEnvironments(project.ID)
	if err != nil || len(list) != 2 {
		t.Fatalf("项目已存在但会话未绑定时仍须读到环境表: list=%v err=%v", list, err)
	}
	state, err := ComponentsFor(service).Session.SSH.SetEnv("sess-fixed", project.ID, "test")
	if err != nil || state.CurrentEnv != "test" {
		t.Fatalf("bind = %+v err=%v", state, err)
	}
	if _, err := ComponentsFor(service).Session.SSH.SetEnv("sess-fixed", project.ID, "test"); err != nil {
		t.Fatalf("重复固定同一环境应幂等: %v", err)
	}
	if _, err := ComponentsFor(service).Session.SSH.SetEnv("sess-fixed", project.ID, "dev"); err == nil {
		t.Fatal("固定环境不得被 agent/工具切换")
	}
	if _, err := ComponentsFor(service).Session.SSH.SetEnv("sess-fixed", project.ID, ""); err == nil {
		t.Fatal("固定环境不得被清空")
	}
	if _, err := ComponentsFor(service).Session.SSH.SetEnv("sess-fixed", project.ID, "missing"); err == nil {
		t.Fatal("未知环境也不得替换固定值")
	}
}
