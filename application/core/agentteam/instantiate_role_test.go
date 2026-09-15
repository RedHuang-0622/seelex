package agentteam

import (
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
)

// TestInstantiateRoleOneStepHiresAnEmployee：一步"员工入职"——会话、注册表配置、
// 工作顺序位、执行者绑定结论在一次调用里全部拿到，调用方不必自己串
// "PutRole + Materialize + SetOrder"。
func TestInstantiateRoleOneStepHiresAnEmployee(t *testing.T) {
	port := newFakePort()
	port.policy = dto.OrderPolicyUserMainDecided
	port.order = []string{string(dto.RoleKindUser), string(dto.RoleKindMain)}
	factory, err := NewFactory(port)
	if err != nil {
		t.Fatal(err)
	}

	result, err := factory.InstantiateRole("sess-main", dto.RoleSpec{
		RoleName:    "reviewer",
		RoleKind:    dto.RoleKindAgent,
		JoinPolicy:  "on_team_create",
		ToolsPolicy: "read-only",
		ModelPolicy: "same-as-exec",
	}, 7)
	if err != nil {
		t.Fatalf("InstantiateRole: %v", err)
	}
	if !result.Session.Created || strings.TrimSpace(result.Session.RoleSessionID) == "" {
		t.Fatalf("角色会话未创建: %+v", result.Session)
	}
	if !result.InOrder || result.OrderIndex != 2 {
		t.Fatalf("on_team_create 应自动排入顺序末尾: %+v", result)
	}
	if len(port.registry.Roles) != 1 {
		t.Fatalf("注册表角色数 = %d, want 1", len(port.registry.Roles))
	}
	role := port.registry.Roles[0]
	if role.RoleName != "reviewer" || role.ToolsPolicy != "read-only" || role.ModelPolicy != "same-as-exec" {
		t.Fatalf("注册表未落角色配置: %+v", role)
	}
	if result.Executor != "" || !strings.Contains(strings.Join(result.Notice, "；"), "没有运行时执行者") {
		t.Fatalf("没有执行者的角色必须显式说明: executor=%q notice=%v", result.Executor, result.Notice)
	}
	if port.policy != dto.OrderPolicyUserMainDecided {
		t.Fatalf("实例化不得改写既有顺序策略: %q", port.policy)
	}

	// 幂等：同角色再来一次 → 不重建会话、顺序不重复、配置按新值覆盖。
	updated, err := factory.InstantiateRole("sess-main", dto.RoleSpec{
		RoleName:    "reviewer",
		RoleKind:    dto.RoleKindAgent,
		JoinPolicy:  "on_team_create",
		ToolsPolicy: "read-write",
	}, 9)
	if err != nil {
		t.Fatalf("重复实例化: %v", err)
	}
	if updated.Session.Created {
		t.Fatal("重复实例化不应重建角色会话")
	}
	if len(updated.OrderRoles) != 3 {
		t.Fatalf("重复实例化不应重复入顺序: %v", updated.OrderRoles)
	}
	if len(port.registry.Roles) != 1 || port.registry.Roles[0].ToolsPolicy != "read-write" {
		t.Fatalf("修改应覆盖既有配置: %+v", port.registry.Roles)
	}
}

// TestInstantiateRoleRespectsJoinPolicyAndTimer：join_policy 决定是否自动排入；
// 定时角色不进工作顺序（由调度器触发），且两种"未排入"都给出可读提示。
func TestInstantiateRoleRespectsJoinPolicyAndTimer(t *testing.T) {
	factory, err := NewFactory(newFakePort())
	if err != nil {
		t.Fatal(err)
	}

	deferred, err := factory.InstantiateRole("sess-main", dto.RoleSpec{
		RoleName: "advisor", RoleKind: dto.RoleKindAgent, JoinPolicy: "on_goal_create",
	}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if deferred.InOrder || len(deferred.Notice) == 0 || !strings.Contains(strings.Join(deferred.Notice, "；"), "on_goal_create") {
		t.Fatalf("on_goal_create 不应自动排入且应给出提示: %+v", deferred)
	}

	timer, err := factory.InstantiateRole("sess-main", dto.RoleSpec{
		RoleName: "digest", RoleKind: dto.RoleKindTimer, JoinPolicy: "scheduled",
	}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if timer.InOrder || timer.Executor != "scheduler" {
		t.Fatalf("定时角色不应进工作顺序且执行者应为调度器: %+v", timer)
	}
	if !strings.Contains(strings.Join(timer.Notice, "；"), "调度器") {
		t.Fatalf("定时角色的归属应说明清楚: %v", timer.Notice)
	}
}

// TestInstantiateRoleRejectsBuiltinAndBadInput：内置角色（user/main）由会话本身
// 提供，不能"入职"；role_name 缺失显式报错（不静默补默认名）。
func TestInstantiateRoleRejectsBuiltinAndBadInput(t *testing.T) {
	factory, err := NewFactory(newFakePort())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := factory.InstantiateRole("sess-main", dto.RoleSpec{RoleName: string(dto.RoleKindMain)}, 1); err == nil {
		t.Fatal("main 是内置角色，不应可实例化")
	}
	if _, err := factory.InstantiateRole("sess-main", dto.RoleSpec{RoleName: "  "}, 1); err == nil {
		t.Fatal("role_name 缺失必须报错")
	}
	if _, err := factory.InstantiateRole("  ", dto.RoleSpec{RoleName: "reviewer"}, 1); err == nil {
		t.Fatal("主会话号缺失必须报错")
	}
}

// TestInstantiateRoleReportsExecutorForTechlead：tl 有真实执行者（goal 治理的
// ADVISOR 回合），实例化回执必须把执行者标出来——UI 据此区分"有人干活的角色"
// 与"只读成员"。
func TestInstantiateRoleReportsExecutorForTechlead(t *testing.T) {
	factory, err := NewFactory(newFakePort())
	if err != nil {
		t.Fatal(err)
	}
	result, err := factory.InstantiateRole("sess-main", dto.RoleSpec{
		RoleName: RoleTechlead, RoleKind: dto.RoleKindTechlead, JoinPolicy: "on_goal_create",
	}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if result.Executor != RoleTechlead {
		t.Fatalf("tl 的执行者应为 %q，得到 %q", RoleTechlead, result.Executor)
	}
}
