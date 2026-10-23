package core

import (
	"testing"
)

// session_ssh_test.go 钉住两条不变式的判据（纯函数层）：
//  1. 放权集合必须是项目环境表的**子集**（写进不存在的名字要显式失败）；
//  2. 收回放权时当前环境自动回落到"没有限定"，不留悬空值。
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

func TestResolveCurrentEnvFallsBackWhenRevoked(t *testing.T) {
	cases := []struct {
		name    string
		allowed []string
		current string
		want    string
	}{
		{"仍被放权", []string{"dev", "test"}, "test", "test"},
		{"已被收回", []string{"dev"}, "test", ""},
		{"本来没限定", []string{"dev"}, "", ""},
		{"放权清空", nil, "dev", ""},
		{"空白当前环境", []string{"dev"}, "   ", ""},
	}
	for _, item := range cases {
		if got := resolveCurrentEnv(item.allowed, item.current); got != item.want {
			t.Fatalf("%s: resolveCurrentEnv = %q, want %q", item.name, got, item.want)
		}
	}
}
