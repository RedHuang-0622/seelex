package agentteam

import (
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
)

// fakePort 是工厂/注册表的装配面桩：只记录"建了哪些角色会话、顺序写成了什么"，
// 不复制存储语义（存储语义由 sessionstore 测试覆盖）。
type fakePort struct {
	sessions map[string]bool // role_session_id
	created  []string
	registry dto.TeamRegistry
	policy   string
	order    []string
}

func newFakePort() *fakePort {
	return &fakePort{sessions: map[string]bool{}}
}

func (port *fakePort) EnsureRoleSession(mainSessionID, roleName, roleSessionID string, joinSeq uint64) (bool, error) {
	if port.sessions[roleSessionID] {
		return false, nil
	}
	port.sessions[roleSessionID] = true
	port.created = append(port.created, roleName+"="+roleSessionID)
	return true, nil
}

func (port *fakePort) ReadLifecycleOrder(string) (string, []string, error) {
	return port.policy, append([]string(nil), port.order...), nil
}

func (port *fakePort) SetLifecycleOrder(_ string, policy string, roles []string) error {
	port.policy = policy
	port.order = append([]string(nil), roles...)
	return nil
}

func (port *fakePort) ReadTeamRegistry(string) (dto.TeamRegistry, error) {
	return port.registry, nil
}

func (port *fakePort) WriteTeamRegistry(_ string, registry dto.TeamRegistry) error {
	registry.Configured = true
	port.registry = registry
	return nil
}

// TestNormalizeDerivesOrderAndExcludesScheduledRoles：未给 order_roles 时按
// user→main→其余角色推导；定时角色单独分区、不入工作顺序。
func TestNormalizeDerivesOrderAndExcludesScheduledRoles(t *testing.T) {
	spec, err := Normalize(dto.TeamSpec{
		Roles: []dto.RoleSpec{
			{RoleName: "digest", RoleKind: dto.RoleKindTimer, OrderPriority: 0},
			{RoleName: "reviewer", RoleKind: dto.RoleKindAgent, OrderPriority: 2},
			{RoleName: "planner", RoleKind: dto.RoleKindAgent, OrderPriority: 1},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"user", "main", "planner", "reviewer"}
	if strings.Join(spec.OrderRoles, ",") != strings.Join(want, ",") {
		t.Fatalf("order_roles = %v, want %v", spec.OrderRoles, want)
	}
	if spec.TeamKind != dto.DefaultTeamKind || spec.OrderPolicy != dto.DefaultOrderPolicy {
		t.Fatalf("defaults = %+v", spec)
	}
}

// TestNormalizeRejectsBrokenOrder：定时角色不得进顺序、顺序角色必须已注册、
// user/main 必须在列。
func TestNormalizeRejectsBrokenOrder(t *testing.T) {
	cases := []struct {
		name string
		spec dto.TeamSpec
	}{
		{"scheduled in order", dto.TeamSpec{
			OrderRoles: []string{"user", "main", "digest"},
			Roles:      []dto.RoleSpec{{RoleName: "digest", RoleKind: dto.RoleKindTimer}},
		}},
		{"unknown role", dto.TeamSpec{OrderRoles: []string{"user", "main", "ghost"}}},
		{"missing main", dto.TeamSpec{
			OrderRoles: []string{"user", "reviewer"},
			Roles:      []dto.RoleSpec{{RoleName: "reviewer", RoleKind: dto.RoleKindAgent}},
		}},
	}
	for _, testCase := range cases {
		if _, err := Normalize(testCase.spec); err == nil {
			t.Fatalf("%s: want error", testCase.name)
		}
	}
}

// TestGoalPresetMaterializeIsIdempotent：goal preset 装配建 TL 角色会话、写顺序策略；
// 重复装配不产生第二个会话（AT6 幂等的工厂侧证据）。
func TestGoalPresetMaterializeIsIdempotent(t *testing.T) {
	port := newFakePort()
	factory, err := NewFactory(port)
	if err != nil {
		t.Fatal(err)
	}
	spec, err := Preset(string(dto.TeamKindGoalA2A))
	if err != nil {
		t.Fatal(err)
	}
	first, err := factory.Materialize("main-1", spec, 7)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Sessions) != 1 || first.Sessions[0].RoleName != "tl" || !first.Sessions[0].Created {
		t.Fatalf("sessions = %+v", first.Sessions)
	}
	if first.Sessions[0].RoleSessionID != "goal-a2a-tl" {
		t.Fatalf("role session id = %q", first.Sessions[0].RoleSessionID)
	}
	if port.policy != dto.OrderPolicyGoalLoop || strings.Join(port.order, ",") != "user,main,tl" {
		t.Fatalf("order = %q %v", port.policy, port.order)
	}
	second, err := factory.Materialize("main-1", spec, 7)
	if err != nil {
		t.Fatal(err)
	}
	if second.Sessions[0].Created {
		t.Fatal("second materialize must not create a new role session")
	}
	if len(port.created) != 1 {
		t.Fatalf("created = %v", port.created)
	}

	// 成员表：user/main/tl 都在序，tl 带角色会话号。
	if len(first.View.Members) != 3 {
		t.Fatalf("members = %+v", first.View.Members)
	}
	if first.View.Members[2].RoleName != "tl" || !first.View.Members[2].InOrder ||
		first.View.Members[2].RoleSessionID != "goal-a2a-tl" {
		t.Fatalf("tl member = %+v", first.View.Members[2])
	}
	if len(first.View.DesignNotice) != 0 {
		t.Fatalf("design notices = %v", first.View.DesignNotice)
	}
}

// TestSecondTeamThroughSameFactory（AT8）：同一个工厂实例化 goal 之外的第二个团队，
// 换的只是 TeamSpec（角色集 + 顺序策略），不复制代码路径。
func TestSecondTeamThroughSameFactory(t *testing.T) {
	port := newFakePort()
	factory, err := NewFactory(port)
	if err != nil {
		t.Fatal(err)
	}
	review, err := Preset(string(dto.TeamKindReview))
	if err != nil {
		t.Fatal(err)
	}
	result, err := factory.Materialize("main-1", review, 3)
	if err != nil {
		t.Fatal(err)
	}
	if result.Spec.TeamKind != string(dto.TeamKindReview) ||
		result.Spec.OrderPolicy != dto.OrderPolicyUserMainDecided {
		t.Fatalf("spec = %+v", result.Spec)
	}
	if len(result.Sessions) != 1 || result.Sessions[0].RoleName != "reviewer" {
		t.Fatalf("sessions = %+v", result.Sessions)
	}
	if strings.Join(result.View.OrderRoles, ",") != "user,main,reviewer" {
		t.Fatalf("order roles = %v", result.View.OrderRoles)
	}
	// 与 goal preset 共用同一条装配路径：不会出现 tl（角色集不同，顺序函数不同）。
	for _, member := range result.View.Members {
		if member.RoleName == "tl" {
			t.Fatalf("review team must not inherit goal roles: %+v", result.View.Members)
		}
	}
}

// TestResearchPresetKeepsScheduledRoleOutOfOrder：定时 agent 只出现在定时分区。
func TestResearchPresetKeepsScheduledRoleOutOfOrder(t *testing.T) {
	port := newFakePort()
	factory, err := NewFactory(port)
	if err != nil {
		t.Fatal(err)
	}
	spec, err := Preset(string(dto.TeamKindResearch))
	if err != nil {
		t.Fatal(err)
	}
	result, err := factory.Materialize("main-1", spec, 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range result.View.OrderRoles {
		if name == "digest" {
			t.Fatalf("scheduled role must not join order_roles: %v", result.View.OrderRoles)
		}
	}
	if len(result.View.Scheduled) != 1 || result.View.Scheduled[0].RoleName != "digest" {
		t.Fatalf("scheduled partition = %+v", result.View.Scheduled)
	}
	if result.View.Scheduled[0].InOrder {
		t.Fatal("scheduled member must be marked out of order")
	}
}

// TestRegistryCRUDAndOrder：角色配置 CRUD 只改注册表；顺序设置只改 lifecycle 字段，
// 且校验角色已注册；删除角色会把它从工作顺序里摘除。
func TestRegistryCRUDAndOrder(t *testing.T) {
	port := newFakePort()
	registry, err := NewRegistry(port)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.PutRole("main-1", dto.RoleSpec{RoleName: "reviewer", RoleKind: dto.RoleKindAgent, OrderPriority: 1}); err != nil {
		t.Fatal(err)
	}
	if len(port.registry.Roles) != 1 || port.registry.Roles[0].RoleName != "reviewer" {
		t.Fatalf("registry = %+v", port.registry)
	}
	if _, err := registry.PutRole("main-1", dto.RoleSpec{RoleName: "main", RoleKind: dto.RoleKindMain}); err == nil {
		t.Fatal("builtin role must not be reconfigured")
	}
	if _, err := registry.SetOrder("main-1", dto.OrderPolicyUserMainDecided,
		[]string{"user", "main", "reviewer"}); err != nil {
		t.Fatal(err)
	}
	if port.policy != dto.OrderPolicyUserMainDecided || len(port.order) != 3 {
		t.Fatalf("order = %q %v", port.policy, port.order)
	}
	if _, err := registry.SetOrder("main-1", dto.OrderPolicyUserMainDecided,
		[]string{"user", "main", "ghost"}); err == nil {
		t.Fatal("unregistered role must not enter order_roles")
	}
	// 注册了但未进顺序的角色要在成员表里被显式提示，而不是静默消失。
	port.registry.Roles = append(port.registry.Roles, dto.RoleSpec{RoleName: "auditor", RoleKind: dto.RoleKindAgent})
	view, err := registry.View("main-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(view.DesignNotice) == 0 {
		t.Fatalf("registered-but-unordered role must be noticed: %+v", view)
	}
	if _, err := registry.DeleteRole("main-1", "reviewer"); err != nil {
		t.Fatal(err)
	}
	if len(port.order) != 2 || port.order[1] != "main" {
		t.Fatalf("order after delete = %v", port.order)
	}
}
