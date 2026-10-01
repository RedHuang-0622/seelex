package core

// employee_permission_assembly_test.go — 装配期分配员工权限的验收面。
//
// 口径：员工是在**装配**时进团的（这一刻才知道"谁在编、什么权责"），因此权限分配
// 必须与"写注册表"同一个动作——否则会出现"已经在编、权限还没分配"的窗口，而员工
// 回合在该窗口里按什么判都没有依据。
//
// 本文件钉住三件事：
//  1. 注册表写下去之后，装配期分配被调用，且拿到的是**注册表里的角色**（同一个事实）；
//  2. 分配失败显式上抛，不静默继续（"看起来分配了、其实没分配"是权限面最坏的沉默）；
//  3. 未装配分配面（nil）时装配照常成功（老宿主/桩不因新增写面而炸）。

import (
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/RedHuang-0622/seelex/application/contract"
	"github.com/RedHuang-0622/seelex/application/contract/dto"
)

// recordingEmployeePermissions 记录装配期分配调用（并发安全：装配路径可能在
// 后台 goroutine 上发生，测试侧读取必须过锁）。
type recordingEmployeePermissions struct {
	mu    sync.Mutex
	calls [][]dto.RoleSpec
	err   error
}

func (recorder *recordingEmployeePermissions) AssignEmployeePermissions(roles []dto.RoleSpec) error {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	recorder.calls = append(recorder.calls, append([]dto.RoleSpec(nil), roles...))
	return recorder.err
}

func (recorder *recordingEmployeePermissions) snapshot() [][]dto.RoleSpec {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	out := make([][]dto.RoleSpec, 0, len(recorder.calls))
	for _, call := range recorder.calls {
		out = append(out, append([]dto.RoleSpec(nil), call...))
	}
	return out
}

func withEmployeePermissions(port contract.EmployeePermissionPort) testServiceOption {
	return func(deps *Dependencies) { deps.EmployeePermissions = port }
}

// employeeRolesOf 取记录里出现过的员工角色（按出现顺序；员工 = agent/timer 角色）。
func employeeRolesOf(calls [][]dto.RoleSpec) []string {
	names := make([]string, 0, len(calls))
	for _, call := range calls {
		for _, role := range call {
			if role.RoleKind == dto.RoleKindAgent || role.RoleKind == dto.RoleKindTimer {
				names = append(names, role.RoleName+":"+role.ToolsPolicy)
			}
		}
	}
	return names
}

// assignedRoleNames 取记录里出现过的**全部**角色名（含主代理/评审者等内置角色）：
// 应用层把注册表原样交给分配面，由实现侧按口径过滤——这样"分配面收到的事实"与
// "注册表里的角色"永远是同一个，不会出现两份名单。
func assignedRoleNames(calls [][]dto.RoleSpec) []string {
	names := make([]string, 0, len(calls))
	for _, call := range calls {
		for _, role := range call {
			names = append(names, role.RoleName)
		}
	}
	return names
}

// TestAssemblyAssignsEmployeePermissions：装配（写注册表）之后，员工权限分配被调用，
// 且拿到的是注册表里的员工口径（readonly 员工 → 收到 auditor:readonly）。
func TestAssemblyAssignsEmployeePermissions(t *testing.T) {
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
	recorder := &recordingEmployeePermissions{}
	service := newTestService(t, &fakeEngine{}, withTestSessions(sessions), withEmployeePermissions(recorder))

	// 三个入口都走注册表写面：会话内改角色 / 装配团队 / goal 自动装配。
	if _, err := service.AgentTeamPutRole("main-1", dto.RoleSpec{
		RoleName: "dev", RoleKind: dto.RoleKindAgent, ToolsPolicy: dto.ToolPolicyReadWrite,
	}); err != nil {
		t.Fatalf("AgentTeamPutRole: %v", err)
	}
	calls := recorder.snapshot()
	if len(calls) == 0 {
		t.Fatal("写注册表之后必须调用装配期分配（否则员工权限没有任何分配时机）")
	}
	assigned := strings.Join(employeeRolesOf(calls), ",")
	if !strings.Contains(assigned, "auditor:readonly") {
		t.Fatalf("分配应拿到注册表里的员工口径，得到 %q", assigned)
	}
	if !strings.Contains(assigned, "dev:readwrite") {
		t.Fatalf("新建员工也必须进分配，得到 %q", assigned)
	}
	if strings.Contains(assigned, "user:") || strings.Contains(assigned, "main:") {
		t.Fatalf("分配面收的是注册表全部角色（user/main 由实现侧按口径过滤），这里不该由应用层筛选：%q", assigned)
	}
}

// TestAssemblySurfacesEmployeePermissionFailure：分配失败必须显式上抛。
// 静默继续的后果是"调用方以为分配了、员工按默认口径跑"，这比失败更危险。
func TestAssemblySurfacesEmployeePermissionFailure(t *testing.T) {
	sessions := newLibrarySessions()
	sessions.setRegistry(dto.TeamRegistry{TeamKind: "review-team"})
	recorder := &recordingEmployeePermissions{err: errors.New("权责表写入失败")}
	service := newTestService(t, &fakeEngine{}, withTestSessions(sessions), withEmployeePermissions(recorder))

	_, err := service.AgentTeamPutRole("main-1", dto.RoleSpec{
		RoleName: "dev", RoleKind: dto.RoleKindAgent, ToolsPolicy: dto.ToolPolicyReadWrite,
	})
	if err == nil {
		t.Fatal("分配失败必须上抛（否则调用方以为已经分配）")
	}
	if !strings.Contains(err.Error(), "分配员工权限") {
		t.Fatalf("错误应指明是员工权限分配环节：%v", err)
	}
}

// TestAssemblyWithoutEmployeePermissionsStillWorks：未装配分配面时装配照常成功
// （老宿主 / 纯治理桩不因新增写面而炸）。
func TestAssemblyWithoutEmployeePermissionsStillWorks(t *testing.T) {
	sessions := newLibrarySessions()
	sessions.setRegistry(dto.TeamRegistry{TeamKind: "review-team"})
	service := newTestService(t, &fakeEngine{}, withTestSessions(sessions))

	if _, err := service.AgentTeamPutRole("main-1", dto.RoleSpec{
		RoleName: "dev", RoleKind: dto.RoleKindAgent, ToolsPolicy: dto.ToolPolicyReadWrite,
	}); err != nil {
		t.Fatalf("未装配分配面时装配必须照常成功：%v", err)
	}
	// 降级语义：注册表仍然写下去了（装配是主动作，分配是附加动作）。
	if registry := sessions.registrySnapshot(); len(registry.Roles) == 0 {
		t.Fatal("注册表应照常写入")
	}
}

// TestMaterializeAssignsEmployeePermissions：装配团队（面板「一键装配」/ `@` 召唤 /
// goal 会话里用户自己装配）都必须分配员工权限——否则"装配"会成为绕过权限分配的入口。
//
// 修前这条断言的触发者是"goal 创建自动装配"，那条自动路径已删除（它会整份替换掉会话
// 已有的团队，见 goal_service.GoalBeginFor），断言改为直接打在装配入口上。
func TestMaterializeAssignsEmployeePermissions(t *testing.T) {
	sessions := &teamRecordingSessions{}
	recorder := &recordingEmployeePermissions{}
	service := newTestService(t, &fakeEngine{}, withTestSessions(sessions), withEmployeePermissions(recorder))

	if _, err := service.MaterializeAgentTeam("sess-goal-perm", goalTeamFixture(), 0); err != nil {
		t.Fatalf("MaterializeAgentTeam: %v", err)
	}
	calls := recorder.snapshot()
	if len(calls) == 0 {
		t.Fatal("装配团队必须分配员工权限")
	}
	assigned := strings.Join(assignedRoleNames(calls), ",")
	if !strings.Contains(assigned, "tl") {
		t.Fatalf("装配的角色（含 TL）应原样交给分配面，得到 %q", assigned)
	}
}
