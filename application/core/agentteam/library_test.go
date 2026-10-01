package agentteam

import (
	"errors"
	"testing"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
)

// fakeLibraryPort 是团队库的读写面桩：只记录写入的整份库，不复制存储语义
// （落盘/规整由 sessionstore 测试覆盖）。
type fakeLibraryPort struct {
	library dto.TeamLibrary
	writes  int
}

func (port *fakeLibraryPort) ReadTeamLibrary() (dto.TeamLibrary, error) {
	return port.library, nil
}

func (port *fakeLibraryPort) WriteTeamLibrary(library dto.TeamLibrary) error {
	port.writes++
	library.Configured = true
	port.library = library
	return nil
}

func TestLibrarySaveIsIdempotentByTeamID(t *testing.T) {
	port := &fakeLibraryPort{}
	library, err := NewLibrary(port)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := library.SaveTeam(dto.TeamLibraryEntry{
		TeamID: "review-team", Name: "评审", OrderPolicy: dto.OrderPolicyUserMainDecided,
		OrderRoles: []string{"user", "main", "reviewer"},
		Roles:      []dto.RoleSpec{{RoleName: "reviewer", RoleKind: dto.RoleKindAgent, SystemPrompt: "v1"}},
	}); err != nil {
		t.Fatal(err)
	}
	view, err := library.SaveTeam(dto.TeamLibraryEntry{
		TeamID: "review-team", Name: "评审 v2", OrderPolicy: dto.OrderPolicyUserMainDecided,
		OrderRoles: []string{"user", "main", "reviewer"},
		Roles:      []dto.RoleSpec{{RoleName: "reviewer", RoleKind: dto.RoleKindAgent, SystemPrompt: "v2", ToolsPolicy: dto.ToolPolicyReadonly}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Teams) != 1 {
		t.Fatalf("同 team_id 必须就地覆盖，库 = %+v", view.Teams)
	}
	if view.Teams[0].Name != "评审 v2" || view.Teams[0].Roles[0].SystemPrompt != "v2" || view.Teams[0].Roles[0].ToolsPolicy != "readonly" {
		t.Fatalf("覆盖后的条目 = %+v", view.Teams[0])
	}

	after, err := library.DeleteTeam("review-team")
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Teams) != 0 {
		t.Fatalf("删除后库 = %+v", after.Teams)
	}
	// 幂等：删不存在的条目不报错。
	if _, err := library.DeleteTeam("review-team"); err != nil {
		t.Fatalf("重复删除应幂等：%v", err)
	}
}

func TestLibraryEntryUnknownTeam(t *testing.T) {
	library, err := NewLibrary(&fakeLibraryPort{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := library.Entry("nope"); !errors.Is(err, ErrUnknownTeam) {
		t.Fatalf("未知团队应返回 ErrUnknownTeam，实际 %v", err)
	}
}

// TestSpecOfEntryKeepsRolesAndOrder：库条目 → TeamSpec 必须原样带入顺序与角色
// 配置（提示词/权限不丢），由既有 Normalize 再校验一次。
func TestSpecOfEntryKeepsRolesAndOrder(t *testing.T) {
	spec := SpecOfEntry(dto.TeamLibraryEntry{
		TeamID: "custom", TeamKind: "custom", OrderPolicy: dto.OrderPolicyGoalLoop,
		OrderRoles: []string{"user", "main", "auditor"},
		Roles:      []dto.RoleSpec{{RoleName: "auditor", RoleKind: dto.RoleKindAgent, SystemPrompt: "审计", ToolsPolicy: "readonly"}},
	})
	normalized, err := Normalize(spec)
	if err != nil {
		t.Fatal(err)
	}
	if len(normalized.OrderRoles) != 3 || normalized.OrderRoles[2] != "auditor" {
		t.Fatalf("顺序 = %v", normalized.OrderRoles)
	}
	if len(normalized.Roles) != 1 || normalized.Roles[0].SystemPrompt != "审计" || normalized.Roles[0].ToolsPolicy != "readonly" {
		t.Fatalf("角色 = %+v", normalized.Roles)
	}
}

// TestEntryFromRegistryDropsBuiltinsAndKeepsPrompts：把会话在编员工存进团队库时，
// user/main（会话自带）不进库条目，其余角色的提示词与权限原样保留。
func TestEntryFromRegistryDropsBuiltinsAndKeepsPrompts(t *testing.T) {
	entry, err := EntryFromRegistry(dto.TeamRegistry{
		TeamID: "review-team", TeamKind: "review-team", OrderPolicy: dto.OrderPolicyUserMainDecided,
		Roles: []dto.RoleSpec{
			{RoleName: "user", RoleKind: dto.RoleKindUser},
			{RoleName: "main", RoleKind: dto.RoleKindMain},
			{RoleName: "auditor", RoleKind: dto.RoleKindAgent, SystemPrompt: "审计员", ToolsPolicy: "readonly"},
		},
	}, []string{"user", "main", "auditor"}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(entry.Roles) != 1 || entry.Roles[0].RoleName != "auditor" {
		t.Fatalf("库条目角色 = %+v", entry.Roles)
	}
	if entry.Roles[0].SystemPrompt != "审计员" || entry.Roles[0].ToolsPolicy != "readonly" {
		t.Fatalf("提示词/权限必须保留：%+v", entry.Roles[0])
	}
	if entry.Name != "review-team" || len(entry.OrderRoles) != 3 {
		t.Fatalf("条目身份/顺序 = %+v", entry)
	}
}

// TestNormalizeLibraryEntryRejectsBadInput：缺 team_id / 非法顺序策略显式报错。
func TestNormalizeLibraryEntryRejectsBadInput(t *testing.T) {
	if _, err := NormalizeLibraryEntry(dto.TeamLibraryEntry{}); err == nil {
		t.Fatal("缺 team_id 必须报错")
	}
	if _, err := NormalizeLibraryEntry(dto.TeamLibraryEntry{TeamID: "t", OrderPolicy: "nope"}); err == nil {
		t.Fatal("非法顺序策略必须报错")
	}
	entry, err := NormalizeLibraryEntry(dto.TeamLibraryEntry{TeamID: "t", OrderRoles: []string{"user", "ghost", "user"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(entry.OrderRoles) != 2 || entry.OrderRoles[0] != "user" || entry.OrderRoles[1] != "main" {
		t.Fatalf("顺序表应过滤未登记角色并保序，实际 %v", entry.OrderRoles)
	}
	if entry.Name != "t" || entry.OrderPolicy != dto.DefaultOrderPolicy {
		t.Fatalf("缺省值 = %+v", entry)
	}
}

// TestEntryFromSpecCopiesShape：一份 TeamSpec 可复制成库条目（"以现有团队新建一支"）。
func TestEntryFromSpecCopiesShape(t *testing.T) {
	spec := testReviewSpec()
	entry, err := EntryFromSpec(spec, "我的评审队", "custom")
	if err != nil {
		t.Fatal(err)
	}
	if entry.TeamKind != "review-team" || entry.Name != "我的评审队" || entry.Origin != "custom" {
		t.Fatalf("库条目 = %+v", entry)
	}
	if len(entry.Roles) != 3 || entry.OrderPolicy != dto.OrderPolicyUserMainDecided {
		t.Fatalf("角色集/策略必须带入：%+v", entry)
	}
}
