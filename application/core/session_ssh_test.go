package core

import (
	"testing"

	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/middle/protocol/model"
)

// session_ssh_test.go 钉住两条不变式的判据（纯函数层）：
//  1. 放权集合必须是项目环境表的**子集**（写进不存在的名字要显式失败）；
//  2. 首次绑定的会话工作环境不可切换或清空。
//
// 端到端（会话端口 + 项目环境表）的路径由装配侧的测试覆盖；这两条是**规则本身**，
// 放在纯函数上跑得最快，也最难被后续重构悄悄改掉。

func knownEnvironments(names ...string) map[string]struct{} {
	set := make(map[string]struct{}, len(names))
	for _, name := range names {
		set[name] = struct{}{}
	}
	return set
}

func TestValidateAllowedEnvsRejectsNamesOutsideProject(t *testing.T) {
	known := knownEnvironments("dev", "test")

	if _, err := validateAllowedEnvs(known, []string{"prod"}); err == nil {
		t.Fatal("放权集合含项目里不存在的环境必须报错")
	}
	if _, err := validateAllowedEnvs(known, []string{"dev", "prod"}); err == nil {
		t.Fatal("只要有一个不在项目环境表里，整组就该拒绝（不做部分接受）")
	}

	got, err := validateAllowedEnvs(known, []string{" dev ", "", "test", "dev"})
	if err != nil {
		t.Fatalf("合法集合不该报错: %v", err)
	}
	if len(got) != 2 || got[0] != "dev" || got[1] != "test" {
		t.Fatalf("规整结果 = %#v, want [dev test]（去空白、去空项、保序去重）", got)
	}

	empty, err := validateAllowedEnvs(known, nil)
	if err != nil {
		t.Fatalf("空集合合法（= 什么都不放权）: %v", err)
	}
	if empty == nil || len(empty) != 0 {
		t.Fatalf("空集合 = %#v, want 非 nil 空切片", empty)
	}
}

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
	list, _, err := service.WorkspaceEnvironments(project.ID)
	if err != nil || len(list) != 2 {
		t.Fatalf("项目已存在但会话未绑定时仍须读到环境表: list=%v err=%v", list, err)
	}
	state, err := service.SetSessionSSHEnv("sess-fixed", project.ID, "test")
	if err != nil || state.CurrentEnv != "test" {
		t.Fatalf("bind = %+v err=%v", state, err)
	}
	if _, err := service.SetSessionSSHEnv("sess-fixed", project.ID, "test"); err != nil {
		t.Fatalf("重复固定同一环境应幂等: %v", err)
	}
	if _, err := service.SetSessionSSHEnv("sess-fixed", project.ID, "dev"); err == nil {
		t.Fatal("固定环境不得被 agent/工具切换")
	}
	if _, err := service.SetSessionSSHEnv("sess-fixed", project.ID, ""); err == nil {
		t.Fatal("固定环境不得被清空")
	}
	if _, err := service.SetSessionSSHEnv("sess-fixed", project.ID, "missing"); err == nil {
		t.Fatal("未知环境也不得替换固定值")
	}
}
