package store

import (
	storeteamwork "github.com/RedHuang-0622/seelex/store/teamwork"
	"strings"
	"testing"
)

// pluginPlan 造一份最小合法计划（存储层的校验要求至少一份里程碑），成员带插件清单。
func pluginPlan(members ...storeteamwork.Member) storeteamwork.Plan {
	return storeteamwork.Plan{
		TeamID:     "t",
		Version:    1,
		Members:    members,
		Milestones: []storeteamwork.Milestone{{ID: "m-1", Name: "build"}},
	}
}

// TestValidateTeamworkPlanPluginStructure 钉住存储层的**结构**关口：插件清单可被
// leader 改写，所以落盘那一步也要挡脏数据（重复 / 空项显式拒绝），但**不**比对插件
// 目录（那是插件域的事实，存储层不该 import 插件域），也不重复声明数量上限（那是
// limits.plugins.per_teammate，在编排入口拦）。
func TestValidateTeamworkPlanPluginStructure(t *testing.T) {
	valid := pluginPlan(storeteamwork.Member{Role: "exec", RoleSessionID: "s1", Plugins: []string{"cad", "docs"}})
	if err := storeteamwork.ValidatePlan(valid, 0); err != nil {
		t.Fatalf("合法清单不得被拒：%v", err)
	}

	inherit := pluginPlan(storeteamwork.Member{Role: "exec", RoleSessionID: "s1"})
	if err := storeteamwork.ValidatePlan(inherit, 0); err != nil {
		t.Fatalf("空/缺失插件 = 不覆盖，必须合法：%v", err)
	}
	if inherit.Members[0].Plugins != nil {
		t.Fatal("校验不得凭空造出空切片")
	}

	duplicated := pluginPlan(storeteamwork.Member{Role: "exec", RoleSessionID: "s1", Plugins: []string{"cad", "cad"}})
	if err := storeteamwork.ValidatePlan(duplicated, 0); err == nil || !strings.Contains(err.Error(), "重复") {
		t.Fatalf("重复插件名必须显式拒绝，得 %v", err)
	}

	blank := pluginPlan(storeteamwork.Member{Role: "exec", RoleSessionID: "s1", Plugins: []string{"cad", "  "}})
	if err := storeteamwork.ValidatePlan(blank, 0); err == nil || !strings.Contains(err.Error(), "为空") {
		t.Fatalf("空项必须显式拒绝，得 %v", err)
	}
}
