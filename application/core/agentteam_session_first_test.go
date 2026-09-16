package core

// agentteam_session_first_test.go 钉住「先改会话设置，再通过同步（普及到全局）改整体配置」
// 的前半句：会话内的改角色 / 改序 / 入职**只写会话副本**（registry + lifecycle head），
// 母本（全局团队库 / 员工库 / 默认顺序）一行都不动。
//
// 后半句（普及才写母本）由 TestAgentTeamGlobalPublish 覆盖。两句合起来才是用户口径：
// 整体配置不会被一次误点的会话内编辑改掉，只有显式的「确认普及搭配到全局」才回写母本。
import (
	"testing"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
)

func TestAgentTeamSessionEditsNeverTouchGlobal(t *testing.T) {
	sessions := newLibrarySessions()
	sessions.setRegistry(dto.TeamRegistry{
		TeamID: "review-team", TeamKind: "review-team", OrderPolicy: dto.OrderPolicyUserMainDecided,
		Roles: []dto.RoleSpec{
			{RoleName: "user", RoleKind: dto.RoleKindUser},
			{RoleName: "main", RoleKind: dto.RoleKindMain},
			{RoleName: "auditor", RoleKind: dto.RoleKindAgent, ToolsPolicy: dto.ToolPolicyReadonly},
		},
	})
	sessions.setLifecycle(dto.OrderPolicyUserMainDecided, []string{"user", "main", "auditor"})
	service := newTestService(t, &fakeEngine{}, withTestSessions(sessions))

	writesBefore := sessions.globalWriteCount()
	if writesBefore != 0 {
		t.Fatalf("前置条件：新建夹具不该有母本写入，得到 %d", writesBefore)
	}

	// 1. 会话内改角色口径（readonly → readwrite）
	if _, err := service.AgentTeamPutRole("main-1", dto.RoleSpec{
		RoleName: "auditor", RoleKind: dto.RoleKindAgent, ToolsPolicy: dto.ToolPolicyReadWrite,
	}); err != nil {
		t.Fatalf("AgentTeamPutRole: %v", err)
	}
	// 2. 会话内改发言顺序（order_roles 必须以 user 开头，这是顺序规整的既有约束）
	if _, err := service.AgentTeamSetOrder("main-1", dto.OrderPolicyUserMainDecided, []string{"user", "auditor", "main"}); err != nil {
		t.Fatalf("AgentTeamSetOrder: %v", err)
	}
	// 3. 会话内入职一个新员工（该夹具未装角色实例化端口时跳过这一条，不影响主断言）
	if _, err := service.AgentTeamInstantiateRole("main-1", dto.RoleSpec{
		RoleName: "reviewer", RoleKind: dto.RoleKindAgent, ToolsPolicy: dto.ToolPolicyReadonly,
	}, 0); err != nil {
		t.Logf("夹具未装角色实例化端口，跳过入职断言: %v", err)
	}

	if writesAfter := sessions.globalWriteCount(); writesAfter != writesBefore {
		t.Fatalf("会话内的改角色/改序/入职写了母本 %d 次：整体配置只应由「普及到全局」改写", writesAfter-writesBefore)
	}

	// 母本仍是空的：普及之前整体配置不变。
	config, err := service.AgentTeamGlobalConfig("main-1")
	if err != nil {
		t.Fatalf("AgentTeamGlobalConfig: %v", err)
	}
	if len(config.Employees.Employees) != 0 {
		t.Fatalf("普及前全局员工库应为空，得到 %+v", config.Employees.Employees)
	}
	if len(config.Library.Teams) != 0 {
		t.Fatalf("普及前全局团队库应为空，得到 %+v", config.Library.Teams)
	}
	if len(config.Order.OrderRoles) != 0 {
		t.Fatalf("普及前全局默认顺序应为空，得到 %+v", config.Order.OrderRoles)
	}
	// 会话副本已经变了（证明上面三步真的生效，而不是被静默丢弃）
	auditor := dto.RoleSpec{}
	for _, role := range config.Composition.Employees {
		if role.RoleName == "auditor" {
			auditor = role
		}
	}
	if auditor.RoleName == "" || auditor.ToolsPolicy != dto.ToolPolicyReadWrite {
		t.Fatalf("会话副本应已按编辑更新，得到 %+v", config.Composition.Employees)
	}
	if len(config.Composition.OrderRoles) == 0 || config.Composition.OrderRoles[1] != "auditor" {
		t.Fatalf("会话副本顺序应已按编辑更新，得到 %+v", config.Composition.OrderRoles)
	}
}
