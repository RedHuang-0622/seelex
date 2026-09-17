package core

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
)

// input_team_test.go — `@` 手动召唤团队的验收。
//
// 口径（与 input_team.go 的边界一一对应）：
//   - `@<内置形态>` 真装配：在编角色进注册表、发言顺序进 lifecycle、入伙切点 =
//     装配那一刻的主会话消息尾 seq（与 goal 自动装配同一条判据）；
//   - `@<团队库条目>` 按 team_id **和**名字都能召唤（用户自己存的团队也走得通）；
//   - 空名 `@` 自述可用团队且不装配任何角色；未知名给可行动提示（列内置形态 +
//     库条目），旧写法 `@off` 指出新前缀；
//   - 旧前缀肌肉记忆：`#`/`$`/`@` 各自给出正确的迁移提示。

// summonService 造一个带团队存储面的服务，并把视图会话钉成固定 ID
// （召唤落在"当前会话"，断言必须指向确定的那个会话）。
func summonService(t *testing.T, sessions SessionPort) *Service {
	t.Helper()
	service := newTestService(t, &fakeEngine{}, withTestSessions(sessions))
	service.ViewMu.Lock()
	service.Core.Snapshot.Session.ID = "sess-summon"
	service.ViewMu.Unlock()
	return service
}

// noticesText 读可见会话里的系统通知（notice 的落地通道）。
func noticesText(service *Service) string {
	return strings.Join(conversationTexts(service.Snapshot().Conversation), "\n")
}

func TestSubmitTeamSummonsPresetTeam(t *testing.T) {
	sessions := &teamRecordingSessions{mainHeadSeq: 5}
	service := summonService(t, sessions)

	if err := service.Submit(context.Background(), "@goal-a2a"); err != nil {
		t.Fatalf("Submit(@goal-a2a): %v", err)
	}
	roles := sessions.ensuredRoles()
	// goal-a2a 的 user/main 是内建席位（JoinPolicy=builtin，不建角色会话），
	// 真正"新入职"的是 techlead——它必须出现在装配记录里。
	if !slices.Contains(roles, "tl") {
		t.Fatalf("内置形态没装配出 techlead 角色会话：%v", roles)
	}
	if joins := sessions.joinSeqSnapshot(); !slices.Contains(joins, "tl|5") {
		t.Fatalf("入伙切点必须是当前消息尾 seq=5：%v", joins)
	}
	policy, order := sessions.lifecycleSnapshot()
	if policy != dto.OrderPolicyGoalLoop || strings.Join(order, ",") != "user,main,tl" {
		t.Fatalf("发言顺序未写入：policy=%q order=%v", policy, order)
	}
	if text := noticesText(service); !strings.Contains(text, "已召唤团队 goal-a2a") {
		t.Fatalf("召唤回执缺失：%q", text)
	}
}

func TestSubmitTeamWithoutNameDescribesPresets(t *testing.T) {
	sessions := &teamRecordingSessions{}
	service := summonService(t, sessions)

	if err := service.Submit(context.Background(), "@"); err != nil {
		t.Fatalf("Submit(@): %v", err)
	}
	text := noticesText(service)
	for _, want := range []string{"goal-a2a", "review-team", "research-team"} {
		if !strings.Contains(text, want) {
			t.Fatalf("`@` 自述缺 %q：%q", want, text)
		}
	}
	if roles := sessions.ensuredRoles(); len(roles) != 0 {
		t.Fatalf("空名不该装配任何角色：%v", roles)
	}
}

func TestSubmitTeamSummonsLibraryEntryByName(t *testing.T) {
	sessions := newLibrarySessions()
	sessions.setRegistry(dto.TeamRegistry{
		TeamID: "audit-team", TeamKind: "audit-team", OrderPolicy: dto.OrderPolicyUserMainDecided,
		Roles: []dto.RoleSpec{
			{RoleName: "user", RoleKind: dto.RoleKindUser},
			{RoleName: "main", RoleKind: dto.RoleKindMain},
			{RoleName: "auditor", RoleKind: dto.RoleKindAgent, SystemPrompt: "审计员提示词", ToolsPolicy: dto.ToolPolicyReadonly},
		},
	})
	sessions.setLifecycle(dto.OrderPolicyUserMainDecided, []string{"user", "main", "auditor"})
	service := summonService(t, sessions)

	// 先把当前会话在编员工存成一条库条目（走既有"存进团队库"入口，不另造形态）。
	if _, err := service.AgentTeamSaveCurrentTeam("sess-summon", "审计小队", "audit-team"); err != nil {
		t.Fatalf("AgentTeamSaveCurrentTeam: %v", err)
	}
	// 清掉会话侧现场：之后装配回来的东西只可能来自库条目。
	sessions.setRegistry(dto.TeamRegistry{})
	sessions.setOrder(nil)

	// 用**用户起的名字**召唤（不是 team_id）：库条目的两种键都要能命中。
	if err := service.Submit(context.Background(), "@审计小队"); err != nil {
		t.Fatalf("Submit(@审计小队): %v", err)
	}
	if registry := sessions.registrySnapshot(); registry.TeamID != "audit-team" {
		t.Fatalf("库条目没装配回注册表：%+v", registry)
	}
	_, order := sessions.lifecycleSnapshot()
	if strings.Join(order, ",") != "user,main,auditor" {
		t.Fatalf("库条目的顺序没写回：%v", order)
	}
	if roles := sessions.ensuredRoles(); !slices.Contains(roles, "auditor") {
		t.Fatalf("库条目里的员工没进在编表：%v", roles)
	}
	if text := noticesText(service); !strings.Contains(text, "已召唤团队 审计小队") {
		t.Fatalf("召唤回执应显示库条目名字：%q", text)
	}
}

func TestSubmitTeamUnknownNameGivesActionableNotice(t *testing.T) {
	sessions := newLibrarySessions()
	sessions.setLibrary(dto.TeamLibrary{
		Teams:      []dto.TeamLibraryEntry{{TeamID: "audit-team", Name: "审计小队"}},
		Configured: true,
	})
	service := summonService(t, sessions)

	if err := service.Submit(context.Background(), "@nope"); err != nil {
		t.Fatalf("未知团队只补 notice，不该返回错误：%v", err)
	}
	text := noticesText(service)
	for _, want := range []string{"未知团队: nope", "goal-a2a", "audit-team"} {
		if !strings.Contains(text, want) {
			t.Fatalf("未知团队提示缺 %q：%q", want, text)
		}
	}
	if roles := sessions.ensuredRoles(); len(roles) != 0 {
		t.Fatalf("未知名不该装配任何角色：%v", roles)
	}

	// 旧写法 `@off`（前缀调整前 `@` 就是插件面）直接指出新前缀。
	if err := service.Submit(context.Background(), "@off"); err != nil {
		t.Fatalf("Submit(@off): %v", err)
	}
	if text := noticesText(service); !strings.Contains(text, "停用插件请用 #off") {
		t.Fatalf("@off 应指出新前缀：%q", text)
	}
}

func TestSigilMigrationHintsPointAtTheRightPrefix(t *testing.T) {
	sessions := &teamRecordingSessions{}
	service := summonService(t, sessions)

	// `#review`：`#` 现在是"切换插件"，review 是 Skill → 提示用 `$`。
	if err := service.Submit(context.Background(), "#review"); err == nil {
		t.Fatal("#review 应报插件不存在（插件切换失败要抛给调用方）")
	}
	if text := noticesText(service); !strings.Contains(text, "召回 Skill 用 $review") {
		t.Fatalf("#review 缺迁移提示：%q", text)
	}

	// `$default`：`$` 是"召回 Skill"，default 是 Plugin → 提示用 `#`。
	if err := service.Submit(context.Background(), "$default"); err != nil {
		t.Fatalf("未知 Skill 只补 notice，不该返回错误：%v", err)
	}
	if text := noticesText(service); !strings.Contains(text, "切换插件用 #default") {
		t.Fatalf("$default 缺迁移提示：%q", text)
	}

	// `@review`：`@` 是"召唤团队"，review 是 Skill → 提示用 `$`。
	if err := service.Submit(context.Background(), "@review"); err != nil {
		t.Fatalf("Submit(@review): %v", err)
	}
	if text := noticesText(service); !strings.Contains(text, "召回 Skill 用 $review") {
		t.Fatalf("@review 缺迁移提示：%q", text)
	}
}
