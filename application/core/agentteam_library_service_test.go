package core

import (
	"context"
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
)

// librarySessions 在 teamRecordingSessions 之上补出**全局母本**读写面
// （团队库 + 员工库 + 默认顺序，均内存态），用于验证 application 层的母本 CRUD /
// 按库装配 / 「确认普及搭配到全局」/ 提示词读面。
type librarySessions struct {
	*teamRecordingSessions
	library         dto.TeamLibrary
	writes          int
	employeeLibrary dto.EmployeeLibrary
	defaultOrder    dto.DefaultOrder
	globalWrites    int
}

func newLibrarySessions() *librarySessions {
	return &librarySessions{teamRecordingSessions: &teamRecordingSessions{}}
}

func (s *librarySessions) ReadTeamLibrary(string) (dto.TeamLibrary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.library, nil
}

func (s *librarySessions) WriteTeamLibrary(_ string, library dto.TeamLibrary) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.writes++
	library.Configured = true
	s.library = library
	return nil
}

func (s *librarySessions) ReadEmployeeLibrary(string) (dto.EmployeeLibrary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	library := s.employeeLibrary
	library.Employees = append([]dto.RoleSpec(nil), s.employeeLibrary.Employees...)
	return library, nil
}

func (s *librarySessions) WriteEmployeeLibrary(_ string, library dto.EmployeeLibrary) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.globalWrites++
	library.Configured = true
	s.employeeLibrary = library
	return nil
}

func (s *librarySessions) ReadDefaultOrder(string) (dto.DefaultOrder, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	order := s.defaultOrder
	order.OrderRoles = append([]string(nil), s.defaultOrder.OrderRoles...)
	return order, nil
}

func (s *librarySessions) WriteDefaultOrder(_ string, order dto.DefaultOrder) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.globalWrites++
	order.Configured = true
	s.defaultOrder = order
	return nil
}

// globalWriteCount 读当前全局母本写次数（断言"读不写盘"用）。
func (s *librarySessions) globalWriteCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.globalWrites
}

// fakeRolePrompt 是"员工提示词一次有界优化"的端口桩。
type fakeRolePrompt struct {
	requests []dto.RolePromptOptimizeRequest
}

func (p *fakeRolePrompt) OptimizeRolePrompt(_ context.Context, request dto.RolePromptOptimizeRequest) (dto.RolePromptOptimizeResult, error) {
	p.requests = append(p.requests, request)
	return dto.RolePromptOptimizeResult{Optimized: "优化后的提示词", Model: "fake-model"}, nil
}

func withRolePrompt(port RolePromptPort) testServiceOption {
	return func(deps *Dependencies) { deps.RolePrompt = port }
}

// TestAgentTeamSaveCurrentTeamAndMaterialize 是"新建团队要计入存储、并能装配回
// 会话"的端到端口径：当前会话在编员工 → 团队库条目（保提示词/权限/顺序）→
// 装配到会话（写 registry + 写 lifecycle 顺序）。
func TestAgentTeamSaveCurrentTeamAndMaterialize(t *testing.T) {
	sessions := newLibrarySessions()
	sessions.registry = dto.TeamRegistry{
		TeamID: "review-team", TeamKind: "review-team", OrderPolicy: dto.OrderPolicyUserMainDecided,
		Roles: []dto.RoleSpec{
			{RoleName: "user", RoleKind: dto.RoleKindUser},
			{RoleName: "main", RoleKind: dto.RoleKindMain},
			{RoleName: "auditor", RoleKind: dto.RoleKindAgent, SystemPrompt: "审计员提示词", ToolsPolicy: dto.ToolPolicyReadonly},
		},
	}
	sessions.policy, sessions.order = dto.OrderPolicyUserMainDecided, []string{"user", "main", "auditor"}
	service := newTestService(t, &fakeEngine{}, withTestSessions(sessions))

	library, err := service.AgentTeamSaveCurrentTeam("main-1", "我的审计队", "")
	if err != nil {
		t.Fatalf("AgentTeamSaveCurrentTeam: %v", err)
	}
	if !library.Configured || len(library.Teams) != 1 || sessions.writes != 1 {
		t.Fatalf("团队库应写入一条：%+v writes=%d", library, sessions.writes)
	}
	entry := library.Teams[0]
	if entry.Name != "我的审计队" || entry.Origin != "current-session" {
		t.Fatalf("库条目 = %+v", entry)
	}
	if len(entry.Roles) != 1 || entry.Roles[0].RoleName != "auditor" ||
		entry.Roles[0].SystemPrompt != "审计员提示词" || entry.Roles[0].ToolsPolicy != "readonly" {
		t.Fatalf("库条目必须保提示词与权限（且不含 user/main）：%+v", entry.Roles)
	}
	if strings.Join(entry.OrderRoles, ",") != "user,main,auditor" {
		t.Fatalf("库条目顺序 = %v", entry.OrderRoles)
	}

	// 装配回会话：重置会话现场，验证装配把库条目写成 registry + lifecycle 顺序。
	sessions.registry = dto.TeamRegistry{}
	sessions.order = nil
	result, err := service.AgentTeamMaterializeTeam("main-1", entry.TeamID, 7)
	if err != nil {
		t.Fatalf("AgentTeamMaterializeTeam: %v", err)
	}
	if result.View.TeamKind != "review-team" {
		t.Fatalf("装配视图 = %+v", result.View)
	}
	stored := sessions.registry
	// 库条目里的角色清单就是"员工"（user/main 由会话本身提供）：装配写入 registry
	// 的也正好是这些员工，提示词/权限原样保留。
	if len(stored.Roles) != 1 || stored.Roles[0].RoleName != "auditor" {
		t.Fatalf("装配必须把库条目的员工写进会话 registry：%+v", stored.Roles)
	}
	if stored.Roles[0].SystemPrompt != "审计员提示词" || stored.Roles[0].ToolsPolicy != "readonly" {
		t.Fatalf("装配后的 auditor 角色应保留提示词/权限：%+v", stored.Roles[0])
	}
	if strings.Join(sessions.order, ",") != "user,main,auditor" {
		t.Fatalf("装配后顺序 = %v", sessions.order)
	}

	// 未知团队显式报错（不是静默空装配）。
	if _, err := service.AgentTeamMaterializeTeam("main-1", "ghost-team", 0); err == nil {
		t.Fatal("未知团队必须报错")
	}

	after, err := service.AgentTeamDeleteTeam("main-1", entry.TeamID)
	if err != nil {
		t.Fatalf("AgentTeamDeleteTeam: %v", err)
	}
	if len(after.Teams) != 0 {
		t.Fatalf("删除后库 = %+v", after.Teams)
	}
}

// TestAgentTeamRolePromptAndOptimize：ADVISOR 提示词读面走注册表只读路径；
// 优化走 RolePromptPort，未装配时显式报错。
func TestAgentTeamRolePromptAndOptimize(t *testing.T) {
	sessions := newLibrarySessions()
	sessions.registry = dto.TeamRegistry{
		TeamKind: "goal-a2a",
		Roles: []dto.RoleSpec{
			{RoleName: "tl", RoleKind: dto.RoleKindTechlead, SystemPrompt: "你是我司的评审官"},
		},
	}
	prompt := &fakeRolePrompt{}
	service := newTestService(t, &fakeEngine{}, withTestSessions(sessions), withRolePrompt(prompt))

	got, err := service.AgentTeamRolePrompt("main-1", "tl")
	if err != nil || got != "你是我司的评审官" {
		t.Fatalf("提示词读面 = %q err=%v", got, err)
	}
	if missing, err := service.AgentTeamRolePrompt("main-1", "ghost"); err != nil || missing != "" {
		t.Fatalf("未登记角色应返回空串：%q err=%v", missing, err)
	}

	result, err := service.AgentTeamOptimizeRolePrompt(context.Background(), "main-1", dto.RolePromptOptimizeRequest{
		RoleName: "tl", RoleKind: "techlead", SystemPrompt: "你评审一下",
	})
	if err != nil {
		t.Fatalf("AgentTeamOptimizeRolePrompt: %v", err)
	}
	if result.Optimized != "优化后的提示词" || result.Original != "你评审一下" || result.RoleName != "tl" {
		t.Fatalf("优化回执 = %+v", result)
	}
	if len(prompt.requests) != 1 || prompt.requests[0].TeamKind != "goal-a2a" {
		t.Fatalf("优化请求应补齐团队上下文：%+v", prompt.requests)
	}

	// 未装配优化端口：显式报错（不静默返回原文）。
	plain := newTestService(t, &fakeEngine{}, withTestSessions(sessions))
	if _, err := plain.AgentTeamOptimizeRolePrompt(context.Background(), "main-1", dto.RolePromptOptimizeRequest{
		RoleName: "tl", SystemPrompt: "x",
	}); err == nil {
		t.Fatal("未装配提示词优化端口必须报错")
	}
	if _, err := plain.AgentTeamOptimizeRolePrompt(context.Background(), "main-1", dto.RolePromptOptimizeRequest{
		RoleName: "tl",
	}); err == nil {
		t.Fatal("缺提示词原文必须报错")
	}
}

// TestAgentTeamGlobalPublish：全局母本的读是只读；「确认普及搭配到全局」才把会话
// 副本的 {员工, 顺序} 写回母本（员工库 + 默认顺序 + 一条团队库条目）。
func TestAgentTeamGlobalPublish(t *testing.T) {
	sessions := newLibrarySessions()
	sessions.registry = dto.TeamRegistry{
		TeamID: "review-team", TeamKind: "review-team", OrderPolicy: dto.OrderPolicyUserMainDecided,
		Roles: []dto.RoleSpec{
			{RoleName: "user", RoleKind: dto.RoleKindUser},
			{RoleName: "main", RoleKind: dto.RoleKindMain},
			{RoleName: "auditor", RoleKind: dto.RoleKindAgent, SystemPrompt: "审计员提示词", ToolsPolicy: dto.ToolPolicyReadonly},
		},
	}
	sessions.policy, sessions.order = dto.OrderPolicyUserMainDecided, []string{"user", "main", "auditor"}
	service := newTestService(t, &fakeEngine{}, withTestSessions(sessions))

	config, err := service.AgentTeamGlobalConfig("main-1")
	if err != nil {
		t.Fatalf("AgentTeamGlobalConfig: %v", err)
	}
	if config.Employees.Configured || len(config.Employees.Employees) != 0 {
		t.Fatalf("未建员工库应为空：%+v", config.Employees)
	}
	if len(config.Composition.Employees) != 1 || config.Composition.Employees[0].RoleName != "auditor" {
		t.Fatalf("会话副本投影应剔除 user/main：%+v", config.Composition.Employees)
	}
	if config.Composition.Employees[0].SystemPrompt != "审计员提示词" {
		t.Fatalf("会话副本必须带提示词/权限：%+v", config.Composition.Employees[0])
	}
	if strings.Join(config.Composition.OrderRoles, ",") != "user,main,auditor" {
		t.Fatalf("会话副本顺序 = %v", config.Composition.OrderRoles)
	}
	if sessions.globalWriteCount() != 0 {
		t.Fatal("读全局母本不应产生任何写盘")
	}

	config, err = service.AgentTeamPublishToGlobal("main-1", "我的审计队", "audit-team")
	if err != nil {
		t.Fatalf("AgentTeamPublishToGlobal: %v", err)
	}
	if len(config.Employees.Employees) != 1 || config.Employees.Employees[0].RoleName != "auditor" {
		t.Fatalf("普及后员工库 = %+v", config.Employees.Employees)
	}
	if config.Employees.Employees[0].ToolsPolicy != dto.ToolPolicyReadonly || config.Employees.Employees[0].SystemPrompt != "审计员提示词" {
		t.Fatalf("普及必须保提示词/权限：%+v", config.Employees.Employees[0])
	}
	if strings.Join(config.Order.OrderRoles, ",") != "user,main,auditor" || config.Order.OrderPolicy != dto.OrderPolicyUserMainDecided {
		t.Fatalf("普及后默认顺序 = %+v", config.Order)
	}
	if len(config.Library.Teams) != 1 || config.Library.Teams[0].TeamID != "audit-team" || config.Library.Teams[0].Name != "我的审计队" {
		t.Fatalf("普及应写下全局团队库条目：%+v", config.Library.Teams)
	}
	if sessions.globalWriteCount() == 0 {
		t.Fatal("普及必须写全局母本")
	}
}

// TestAgentTeamGlobalEmployeeCRUD：员工库 / 默认顺序是"库管理"动作，直接写全局
// 母本；内置角色不入库，引用不存在的角色不进默认顺序，删除幂等。
func TestAgentTeamGlobalEmployeeCRUD(t *testing.T) {
	sessions := newLibrarySessions()
	service := newTestService(t, &fakeEngine{}, withTestSessions(sessions))

	library, err := service.AgentTeamSaveEmployee("main-1", dto.RoleSpec{RoleName: " reviewer ", RoleKind: dto.RoleKindAgent})
	if err != nil {
		t.Fatalf("AgentTeamSaveEmployee: %v", err)
	}
	if len(library.Employees) != 1 || library.Employees[0].RoleName != "reviewer" {
		t.Fatalf("员工库 = %+v", library.Employees)
	}

	if _, err := service.AgentTeamSaveEmployee("main-1", dto.RoleSpec{RoleName: "main"}); err == nil {
		t.Fatal("内置角色不得进入员工库")
	}
	if _, err := service.AgentTeamSaveEmployee("main-1", dto.RoleSpec{}); err == nil {
		t.Fatal("缺 role_name 必须报错")
	}

	order, err := service.AgentTeamSetDefaultOrder("main-1", "", []string{"user", "main", "ghost", "reviewer"})
	if err != nil {
		t.Fatalf("AgentTeamSetDefaultOrder: %v", err)
	}
	if strings.Join(order.OrderRoles, ",") != "user,main,reviewer" {
		t.Fatalf("默认顺序应剔除未知角色并补齐 user/main：%v", order.OrderRoles)
	}

	after, err := service.AgentTeamDeleteEmployee("main-1", " reviewer ")
	if err != nil {
		t.Fatalf("AgentTeamDeleteEmployee: %v", err)
	}
	if len(after.Employees) != 0 {
		t.Fatalf("删除后员工库 = %+v", after.Employees)
	}
	if _, err := service.AgentTeamDeleteEmployee("main-1", "reviewer"); err != nil {
		t.Fatalf("重复删除必须幂等：%v", err)
	}
}

// TestAgentTeamGlobalRequiresPort：宿主没实现全局母本端口时显式报错（不静默返回
// 空母本，否则前端会以为"库是空的"）。
func TestAgentTeamGlobalRequiresPort(t *testing.T) {
	service := newTestService(t, &fakeEngine{}, withTestSessions(&teamRecordingSessions{}))
	if _, err := service.AgentTeamGlobalConfig("main-1"); err == nil {
		t.Fatal("未装配全局母本端口必须报错")
	}
	if _, err := service.AgentTeamPublishToGlobal("main-1", "", ""); err == nil {
		t.Fatal("未装配全局母本端口时普及必须报错")
	}
}
