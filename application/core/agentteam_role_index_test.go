package core

// agentteam_role_index_test.go — 角色会话归属的反向索引（权限门的读面）。
//
// 修复前的行为：main.go 的解析器锚在"当前视图会话"上（app.Snapshot().Session.ID），
// 于是**后台会话**的角色会话解析不到 → 落回 root = 不拦。权限面的正确性不该取决于
// 用户此刻看着哪个会话。
//
// 同时钉住歧义口径：角色会话号是 `teamID-roleName`，不含主会话身份；两个会话若用了
// 同一个 team_id 就会重号，此时必须取**最严**的一个（fail-closed），而不是落回 root
// （fail-open）。

import (
	"context"
	"testing"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
	goaldomain "github.com/RedHuang-0622/seelex/application/core/goal"
)

// TestRoleSessionPolicyResolvesOwningSessionNotCurrentView：角色会话被读取过归属注册表
// 之后，任何会话都能按角色会话号问出权责（与"当前看着谁"无关）。
func TestRoleSessionPolicyResolvesOwningSessionNotCurrentView(t *testing.T) {
	sessions := &teamRecordingSessions{}
	service := newTestService(t, &fakeEngine{}, withTestSessions(sessions))

	if _, err := service.GoalBeginFor(context.Background(), "sess-bg", goaldomain.BeginRequest{Title: "后台会话的目标"}); err != nil {
		t.Skipf("goal 装配不可用于该夹具: %v", err)
	}
	view, err := service.AgentTeamView("sess-bg")
	if err != nil {
		t.Fatalf("AgentTeamView: %v", err)
	}
	if !view.Configured {
		t.Skip("该夹具未物化团队注册表（无角色会话可查）")
	}
	var roleSessionID, wantPolicy string
	for _, member := range view.Members {
		if member.RoleKind == dto.RoleKindTechlead && member.RoleSessionID != "" {
			roleSessionID, wantPolicy = member.RoleSessionID, member.ToolsPolicy
			break
		}
	}
	if roleSessionID == "" {
		t.Skip("该夹具未物化 TL 角色会话")
	}

	// 读另一个会话的注册表（模拟"用户切到别的会话"）——不该影响按角色会话号的解析。
	if _, err := service.AgentTeamView("sess-other"); err != nil {
		t.Logf("读另一个会话视图失败（不影响本断言）: %v", err)
	}

	policy, found := service.RoleSessionToolsPolicy(roleSessionID)
	if !found {
		t.Fatalf("必须能按角色会话号 %q 解析归属（后台会话的角色也必须拦得住）", roleSessionID)
	}
	if policy != wantPolicy {
		t.Fatalf("解析出的权责 = %q, want %q（与注册表口径一致）", policy, wantPolicy)
	}
	if owner, ok := service.RoleSessionOwner(roleSessionID); !ok || owner != "sess-bg" {
		t.Fatalf("归属主会话 = %q (ok=%v), want sess-bg", owner, ok)
	}

	// 没登记过的角色会话号：不该"猜"出一份权责（found=false → 调用方按无角色处理）。
	if _, found := service.RoleSessionToolsPolicy("ghost-team-ghostrole"); found {
		t.Fatal("未登记的角色会话不该有归属（猜出来的权责就是越权的入口）")
	}
}

// TestRoleSessionIndexAmbiguityTakesMostRestrictive：角色会话号重号时必须取最严口径。
func TestRoleSessionIndexAmbiguityTakesMostRestrictive(t *testing.T) {
	index := roleSessionIndex{}
	const roleSessionID = "goal-a2a-tl"
	index.remember("sess-a", dto.TeamView{
		Configured: true,
		Members: []dto.TeamMember{
			{RoleName: "tl", RoleKind: dto.RoleKindTechlead, RoleSessionID: roleSessionID, ToolsPolicy: "readwrite"},
		},
	})
	index.remember("sess-b", dto.TeamView{
		Configured: true,
		Members: []dto.TeamMember{
			{RoleName: "tl", RoleKind: dto.RoleKindTechlead, RoleSessionID: roleSessionID, ToolsPolicy: "readonly"},
		},
	})
	policy, found := index.policy(roleSessionID)
	if !found {
		t.Fatal("重号也必须能解析出权责（取最严）")
	}
	if policy != "readonly" {
		t.Fatalf("重号时的权责 = %q, want readonly（歧义必须 fail-closed，不能 fail-open）", policy)
	}

	// 同一 (主会话, 角色) 更新口径：以最新读到的为准（角色改权责后立即生效）。
	index.remember("sess-b", dto.TeamView{
		Configured: true,
		Members: []dto.TeamMember{
			{RoleName: "tl", RoleKind: dto.RoleKindTechlead, RoleSessionID: roleSessionID, ToolsPolicy: "readwrite"},
		},
	})
	if policy, _ := index.policy(roleSessionID); policy != "readwrite" {
		t.Fatalf("同一归属更新后权责 = %q, want readwrite", policy)
	}

	// 未配置的视图不登记（空注册表不该污染索引）。
	index.remember("sess-c", dto.TeamView{Members: []dto.TeamMember{{RoleName: "tl", RoleSessionID: "ghost", ToolsPolicy: "readonly"}}})
	if _, found := index.policy("ghost"); found {
		t.Fatal("未配置的注册表不该登记角色会话")
	}
}
