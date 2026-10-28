package teamwork

// plan_query_test.go 是计划查询两条判据的域级用例（P21-33：随实现从
// `backend/engine/assemble/runtime_teamwork.go` 的两个遍历点搬进本域）。

import (
	storeteamwork "github.com/RedHuang-0622/seelex/store/teamwork"
	"testing"
)

func planQueryFixture() storeteamwork.Plan {
	return storeteamwork.Plan{
		TeamID: "team-1",
		Members: []storeteamwork.Member{
			{Role: "pm", ToolsPolicy: "readonly"},
			{Role: "exec", ToolsPolicy: "readwrite"},
		},
		Milestones: []storeteamwork.Milestone{{ID: "m1", Items: []storeteamwork.WorkItem{
			{ID: "wi-1", Role: "exec"},
			{ID: "wi-2", Role: "pm"},
		}}},
	}
}

// TestItemRoleFindsTheOwner：按 id 找执行角色（入参 trim 后比较）；找不到 / 空 id 回空串。
func TestItemRoleFindsTheOwner(t *testing.T) {
	plan := planQueryFixture()
	if got := ItemRole(plan, "wi-1"); got != "exec" {
		t.Fatalf("wi-1 的执行角色 = %q, want exec", got)
	}
	if got := ItemRole(plan, "  wi-2  "); got != "pm" {
		t.Fatalf("入参应当 trim 后比较：%q", got)
	}
	for _, missing := range []string{"wi-9", "", "   "} {
		if got := ItemRole(plan, missing); got != "" {
			t.Fatalf("查不到（%q）应当回空串，得到 %q", missing, got)
		}
	}
	// 空计划安全。
	if got := ItemRole(storeteamwork.Plan{}, "wi-1"); got != "" {
		t.Fatalf("空计划应当回空串：%q", got)
	}
}

// TestMemberOfFindsTheMember：按角色名找在编成员（trim 后比较）；找不到 / 空角色回 false。
func TestMemberOfFindsTheMember(t *testing.T) {
	plan := planQueryFixture()
	member, ok := MemberOf(plan, " exec ")
	if !ok || member.Role != "exec" || member.ToolsPolicy != "readwrite" {
		t.Fatalf("找成员 = %+v ok=%v", member, ok)
	}
	for _, missing := range []string{"ghost", "", "  "} {
		if _, ok := MemberOf(plan, missing); ok {
			t.Fatalf("查不到（%q）应当回 false", missing)
		}
	}
}
