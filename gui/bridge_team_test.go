package gui

import (
	"context"
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
	"github.com/RedHuang-0622/seelex/application/model"
)

// fakeAgentTeamApplication 复刻 Bridge 消费的 A2A 角色管理面（S27 之后的纯
// DTO 形态），记录每次转发的会话号与参数，便于断言 Bridge 只做归一与转发。
type fakeAgentTeamApplication struct {
	*fakeApplication
	viewSession        string
	materializeCalls   []string
	orderSession       string
	orderPolicy        string
	orderRoles         []string
	putSession         string
	putRole            dto.RoleSpec
	deleteSession      string
	deleteRole         string
	roleSnapshotMain   string
	roleSnapshotRole   string
	roleSnapshotID     string
	instantiateSession string
	instantiateRole    dto.RoleSpec
	instantiateJoinSeq uint64
	librarySession     string
	savedTeam          dto.TeamLibraryEntry
	savedTeamName      string
	savedTeamID        string
	deletedTeamID      string
	materializeTeam    string
	promptSession      string
	promptRole         string
	optimizeRequest    dto.RolePromptOptimizeRequest
	savedEmployee      dto.RoleSpec
	deletedEmployee    string
	defaultOrderPolicy string
	defaultOrderRoles  []string
	publishedName      string
	publishedTeamID    string
}

func newFakeAgentTeamApplication(sessionID string) *fakeAgentTeamApplication {
	app := &fakeAgentTeamApplication{fakeApplication: newFakeApplication()}
	app.snapshotMu.Lock()
	app.snapshot.Session = model.SessionState{ID: sessionID}
	app.snapshotMu.Unlock()
	return app
}

func (app *fakeAgentTeamApplication) AgentTeamPresets() []dto.TeamSpec {
	return []dto.TeamSpec{{TeamKind: "goal-a2a"}, {TeamKind: "review-team"}}
}

func (app *fakeAgentTeamApplication) AgentTeamView(mainSessionID string) (dto.TeamView, error) {
	app.viewSession = mainSessionID
	return dto.TeamView{
		SessionID: mainSessionID, TeamKind: "goal-a2a", OrderRoles: []string{"user", "main", "tl"},
		Members: []dto.TeamMember{
			{RoleName: "user", RoleKind: dto.RoleKindUser, InOrder: true},
			{RoleName: "main", RoleKind: dto.RoleKindMain, InOrder: true},
			{RoleName: "tl", RoleKind: dto.RoleKindTechlead, RoleSessionID: "goal-a2a-tl", InOrder: true},
		},
	}, nil
}

func (app *fakeAgentTeamApplication) MaterializeAgentTeamPreset(mainSessionID, teamKind string, joinSeq uint64) (dto.TeamMaterializeResult, error) {
	app.materializeCalls = append(app.materializeCalls, mainSessionID+"|"+teamKind)
	return dto.TeamMaterializeResult{Spec: dto.TeamSpec{TeamKind: teamKind}, View: dto.TeamView{SessionID: mainSessionID}}, nil
}

func (app *fakeAgentTeamApplication) AgentTeamPutRole(mainSessionID string, role dto.RoleSpec) (dto.TeamRegistry, error) {
	app.putSession, app.putRole = mainSessionID, role
	return dto.TeamRegistry{Configured: true}, nil
}

func (app *fakeAgentTeamApplication) AgentTeamDeleteRole(mainSessionID, roleName string) (dto.TeamRegistry, error) {
	app.deleteSession, app.deleteRole = mainSessionID, roleName
	return dto.TeamRegistry{Configured: true}, nil
}

func (app *fakeAgentTeamApplication) AgentTeamInstantiateRole(mainSessionID string, role dto.RoleSpec, joinSeq uint64) (dto.RoleInstantiation, error) {
	app.instantiateSession, app.instantiateRole, app.instantiateJoinSeq = mainSessionID, role, joinSeq
	return dto.RoleInstantiation{
		Role:        role,
		Session:     dto.TeamRoleSession{RoleName: role.RoleName, RoleSessionID: "goal-a2a-" + role.RoleName, Exists: true, Created: true},
		OrderPolicy: dto.OrderPolicyGoalLoop,
		OrderRoles:  []string{"user", "main", "tl", role.RoleName},
		InOrder:     true,
		OrderIndex:  3,
		Executor:    "scheduler",
	}, nil
}

func (app *fakeAgentTeamApplication) AgentTeamSetOrder(mainSessionID, policy string, orderRoles []string) (dto.TeamView, error) {
	app.orderSession, app.orderPolicy, app.orderRoles = mainSessionID, policy, orderRoles
	return dto.TeamView{SessionID: mainSessionID, OrderPolicy: policy, OrderRoles: orderRoles}, nil
}

func (app *fakeAgentTeamApplication) RoleSnapshot(mainSessionID, roleName, roleSessionID string) (dto.RoleSnapshot, error) {
	app.roleSnapshotMain, app.roleSnapshotRole, app.roleSnapshotID = mainSessionID, roleName, roleSessionID
	return dto.RoleSnapshot{
		MainSessionID: mainSessionID, RoleName: roleName, RoleSessionID: roleSessionID,
		RoleRows: []dto.RoleRow{{Role: "assistant", Content: "role row", RoleName: roleName}},
	}, nil
}

func (app *fakeAgentTeamApplication) AgentTeamLibrary(mainSessionID string) (dto.TeamLibrary, error) {
	app.librarySession = mainSessionID
	return dto.TeamLibrary{Configured: true, Teams: []dto.TeamLibraryEntry{{TeamID: "review-team", Name: "评审"}}}, nil
}

func (app *fakeAgentTeamApplication) AgentTeamSaveTeam(mainSessionID string, entry dto.TeamLibraryEntry) (dto.TeamLibrary, error) {
	app.librarySession, app.savedTeam = mainSessionID, entry
	return dto.TeamLibrary{Configured: true}, nil
}

func (app *fakeAgentTeamApplication) AgentTeamSaveCurrentTeam(mainSessionID, name, teamID string) (dto.TeamLibrary, error) {
	app.librarySession, app.savedTeamName, app.savedTeamID = mainSessionID, name, teamID
	return dto.TeamLibrary{Configured: true}, nil
}

func (app *fakeAgentTeamApplication) AgentTeamDeleteTeam(mainSessionID, teamID string) (dto.TeamLibrary, error) {
	app.librarySession, app.deletedTeamID = mainSessionID, teamID
	return dto.TeamLibrary{Configured: true}, nil
}

func (app *fakeAgentTeamApplication) AgentTeamMaterializeTeam(mainSessionID, teamID string, joinSeq uint64) (dto.TeamMaterializeResult, error) {
	app.materializeTeam = mainSessionID + "|" + teamID
	return dto.TeamMaterializeResult{Spec: dto.TeamSpec{TeamID: teamID}, View: dto.TeamView{SessionID: mainSessionID}}, nil
}

func (app *fakeAgentTeamApplication) AgentTeamGlobalConfig(mainSessionID string) (dto.TeamGlobalConfig, error) {
	app.librarySession = mainSessionID
	return dto.TeamGlobalConfig{
		Library:     dto.TeamLibrary{Configured: true, Teams: []dto.TeamLibraryEntry{{TeamID: "review-team"}}},
		Employees:   dto.EmployeeLibrary{Configured: true, Employees: []dto.RoleSpec{{RoleName: "reviewer", RoleKind: dto.RoleKindAgent}}},
		Order:       dto.DefaultOrder{Configured: true, OrderRoles: []string{"user", "main", "reviewer"}},
		Composition: dto.TeamComposition{SessionID: mainSessionID, Employees: []dto.RoleSpec{{RoleName: "reviewer"}}},
	}, nil
}

func (app *fakeAgentTeamApplication) AgentTeamSaveEmployee(mainSessionID string, role dto.RoleSpec) (dto.EmployeeLibrary, error) {
	app.librarySession, app.savedEmployee = mainSessionID, role
	return dto.EmployeeLibrary{Configured: true}, nil
}

func (app *fakeAgentTeamApplication) AgentTeamDeleteEmployee(mainSessionID, roleName string) (dto.EmployeeLibrary, error) {
	app.librarySession, app.deletedEmployee = mainSessionID, roleName
	return dto.EmployeeLibrary{Configured: true}, nil
}

func (app *fakeAgentTeamApplication) AgentTeamSetDefaultOrder(mainSessionID, policy string, orderRoles []string) (dto.DefaultOrder, error) {
	app.librarySession, app.defaultOrderPolicy, app.defaultOrderRoles = mainSessionID, policy, orderRoles
	return dto.DefaultOrder{Configured: true, OrderPolicy: policy, OrderRoles: orderRoles}, nil
}

func (app *fakeAgentTeamApplication) AgentTeamPublishToGlobal(mainSessionID, name, teamID string) (dto.TeamGlobalConfig, error) {
	app.librarySession, app.publishedName, app.publishedTeamID = mainSessionID, name, teamID
	return dto.TeamGlobalConfig{Library: dto.TeamLibrary{Configured: true}}, nil
}

func (app *fakeAgentTeamApplication) AgentTeamRolePrompt(mainSessionID, roleName string) (string, error) {
	app.promptSession, app.promptRole = mainSessionID, roleName
	return "你是评审员", nil
}

func (app *fakeAgentTeamApplication) AgentTeamOptimizeRolePrompt(_ context.Context, mainSessionID string, request dto.RolePromptOptimizeRequest) (dto.RolePromptOptimizeResult, error) {
	app.promptSession, app.optimizeRequest = mainSessionID, request
	return dto.RolePromptOptimizeResult{RoleName: request.RoleName, Original: request.SystemPrompt, Optimized: "优化后"}, nil
}

func TestBridgeAgentTeamResolvesCurrentSession(t *testing.T) {
	app := newFakeAgentTeamApplication("main-1")
	bridge, err := NewBridge(app, Options{})
	if err != nil {
		t.Fatalf("NewBridge: %v", err)
	}

	view, err := bridge.AgentTeamView("")
	if err != nil {
		t.Fatalf("AgentTeamView(\"\"): %v", err)
	}
	if app.viewSession != "main-1" || view.SessionID != "main-1" {
		t.Fatalf("空会话号必须解析为当前视图会话：forwarded=%q view=%q", app.viewSession, view.SessionID)
	}
	if _, err := bridge.AgentTeamView("explicit-2"); err != nil {
		t.Fatalf("AgentTeamView(explicit): %v", err)
	}
	if app.viewSession != "explicit-2" {
		t.Fatalf("显式会话号必须原样转发，got %q", app.viewSession)
	}
}

func TestBridgeAgentTeamForwardsTrimmedArguments(t *testing.T) {
	app := newFakeAgentTeamApplication("main-1")
	bridge, err := NewBridge(app, Options{})
	if err != nil {
		t.Fatalf("NewBridge: %v", err)
	}

	if _, err := bridge.AgentTeamMaterialize("", "  review-team  ", 0); err != nil {
		t.Fatalf("AgentTeamMaterialize: %v", err)
	}
	if len(app.materializeCalls) != 1 || app.materializeCalls[0] != "main-1|review-team" {
		t.Fatalf("装配转发 = %v, want [main-1|review-team]", app.materializeCalls)
	}

	if _, err := bridge.AgentTeamSetOrder("", " user_main_decided ", []string{"user", "main", "reviewer"}); err != nil {
		t.Fatalf("AgentTeamSetOrder: %v", err)
	}
	if app.orderSession != "main-1" || app.orderPolicy != "user_main_decided" {
		t.Fatalf("顺序转发 = %q/%q", app.orderSession, app.orderPolicy)
	}
	if strings.Join(app.orderRoles, ",") != "user,main,reviewer" {
		t.Fatalf("顺序表转发 = %v", app.orderRoles)
	}

	if _, err := bridge.AgentTeamPutRole("", dto.RoleSpec{RoleName: "auditor", RoleKind: dto.RoleKindAgent}); err != nil {
		t.Fatalf("AgentTeamPutRole: %v", err)
	}
	if app.putSession != "main-1" || app.putRole.RoleName != "auditor" {
		t.Fatalf("角色写入转发 = %q/%+v", app.putSession, app.putRole)
	}

	if _, err := bridge.AgentTeamInstantiateRole("", dto.RoleSpec{RoleName: " reviewer ", RoleKind: dto.RoleKindAgent}, 7); err != nil {
		t.Fatalf("AgentTeamInstantiateRole: %v", err)
	}
	if app.instantiateSession != "main-1" || app.instantiateRole.RoleName != "reviewer" || app.instantiateJoinSeq != 7 {
		t.Fatalf("role instantiation forwarded = %q/%+v/%d", app.instantiateSession, app.instantiateRole, app.instantiateJoinSeq)
	}

	if _, err := bridge.AgentTeamDeleteRole("", " auditor "); err != nil {
		t.Fatalf("AgentTeamDeleteRole: %v", err)
	}
	if app.deleteSession != "main-1" || app.deleteRole != "auditor" {
		t.Fatalf("角色删除转发 = %q/%q", app.deleteSession, app.deleteRole)
	}

	presets, err := bridge.AgentTeamPresets()
	if err != nil || len(presets) != 2 {
		t.Fatalf("AgentTeamPresets = %v err=%v", presets, err)
	}

	snapshot, err := bridge.AgentTeamRoleSnapshot("", " tl ", " role-1 ")
	if err != nil {
		t.Fatalf("AgentTeamRoleSnapshot: %v", err)
	}
	if app.roleSnapshotMain != "main-1" || app.roleSnapshotRole != "tl" || app.roleSnapshotID != "role-1" {
		t.Fatalf("角色会话查看转发 = %q/%q/%q", app.roleSnapshotMain, app.roleSnapshotRole, app.roleSnapshotID)
	}
	if snapshot.RoleName != "tl" || len(snapshot.RoleRows) != 1 || snapshot.RoleRows[0].RoleName != "tl" {
		t.Fatalf("角色会话查看返回 = %+v", snapshot)
	}

	// 前端只带 role_name（注册表不落盘角色会话号）时，Bridge 从成员表解析派生号。
	if _, err := bridge.AgentTeamRoleSnapshot("", "tl", ""); err != nil {
		t.Fatalf("AgentTeamRoleSnapshot(空会话号)必须从成员表解析: %v", err)
	}
	if app.roleSnapshotID != "goal-a2a-tl" {
		t.Fatalf("派生的角色会话号 = %q, want goal-a2a-tl", app.roleSnapshotID)
	}
}

func TestBridgeAgentTeamRequiresAssembly(t *testing.T) {
	bridge, err := NewBridge(newFakeApplication(), Options{})
	if err != nil {
		t.Fatalf("NewBridge: %v", err)
	}
	if _, err := bridge.AgentTeamView("main-1"); err == nil {
		t.Fatal("未装配 A2A 角色管理面时必须返回可展示错误，而不是空视图")
	}
	if _, err := bridge.AgentTeamPresets(); err == nil {
		t.Fatal("未装配时 preset 清单也必须报错")
	}
	if _, err := bridge.AgentTeamMaterialize("", "goal-a2a", 0); err == nil {
		t.Fatal("未装配时装配调用必须报错")
	}
	if _, err := bridge.AgentTeamRoleSnapshot("", "tl", "role-1"); err == nil {
		t.Fatal("未装配时角色会话查看必须报错")
	}
	if _, err := bridge.AgentTeamLibrary(""); err == nil {
		t.Fatal("未装配时团队库读取必须报错")
	}
	if _, err := bridge.AgentTeamSaveTeam("", dto.TeamLibraryEntry{TeamID: "t"}); err == nil {
		t.Fatal("未装配时团队库写入必须报错")
	}
	if _, err := bridge.AgentTeamMaterializeTeam("", "t", 0); err == nil {
		t.Fatal("未装配时按库装配必须报错")
	}
	if _, err := bridge.AgentTeamRolePrompt("", "tl"); err == nil {
		t.Fatal("未装配时读员工提示词必须报错")
	}
	if _, err := bridge.AgentTeamOptimizePrompt("", dto.RolePromptOptimizeRequest{RoleName: "tl", SystemPrompt: "x"}); err == nil {
		t.Fatal("未装配时提示词优化必须报错")
	}
	if _, err := bridge.AgentTeamGlobalConfig(""); err == nil {
		t.Fatal("未装配时读全局母本必须报错")
	}
	if _, err := bridge.AgentTeamSaveEmployee("", dto.RoleSpec{RoleName: "reviewer"}); err == nil {
		t.Fatal("未装配时写员工库必须报错")
	}
	if _, err := bridge.AgentTeamSetDefaultOrder("", "", []string{"user", "main"}); err == nil {
		t.Fatal("未装配时写默认顺序必须报错")
	}
	if _, err := bridge.AgentTeamPublishToGlobal("", "", ""); err == nil {
		t.Fatal("未装配时普及搭配必须报错")
	}
}

// TestBridgeAgentTeamLibraryAndPromptForwarding：团队库 CRUD / 按库装配 / 员工
// 提示词读写都只做参数归一（trim）与窄转发，会话号空值解析为当前视图会话。
func TestBridgeAgentTeamLibraryAndPromptForwarding(t *testing.T) {
	app := newFakeAgentTeamApplication("main-1")
	bridge, err := NewBridge(app, Options{})
	if err != nil {
		t.Fatalf("NewBridge: %v", err)
	}

	if _, err := bridge.AgentTeamLibrary(""); err != nil {
		t.Fatalf("AgentTeamLibrary: %v", err)
	}
	if app.librarySession != "main-1" {
		t.Fatalf("团队库会话解析 = %q, want main-1", app.librarySession)
	}

	if _, err := bridge.AgentTeamSaveTeam("", dto.TeamLibraryEntry{
		TeamID: "  review-team  ", TeamKind: " review-team ", Name: " 评审 ",
	}); err != nil {
		t.Fatalf("AgentTeamSaveTeam: %v", err)
	}
	if app.savedTeam.TeamID != "review-team" || app.savedTeam.TeamKind != "review-team" || app.savedTeam.Name != "评审" {
		t.Fatalf("团队库写入必须 trim 身份字段：%+v", app.savedTeam)
	}

	if _, err := bridge.AgentTeamSaveCurrentTeam("", " 我的队 ", " my-team "); err != nil {
		t.Fatalf("AgentTeamSaveCurrentTeam: %v", err)
	}
	if app.savedTeamName != "我的队" || app.savedTeamID != "my-team" {
		t.Fatalf("保存当前团队转发 = %q/%q", app.savedTeamName, app.savedTeamID)
	}

	if _, err := bridge.AgentTeamDeleteTeam("", " review-team "); err != nil {
		t.Fatalf("AgentTeamDeleteTeam: %v", err)
	}
	if app.deletedTeamID != "review-team" {
		t.Fatalf("团队删除转发 = %q", app.deletedTeamID)
	}

	if _, err := bridge.AgentTeamMaterializeTeam("", " review-team ", 3); err != nil {
		t.Fatalf("AgentTeamMaterializeTeam: %v", err)
	}
	if app.materializeTeam != "main-1|review-team" {
		t.Fatalf("按库装配转发 = %q", app.materializeTeam)
	}

	if _, err := bridge.AgentTeamRolePrompt("", " tl "); err != nil {
		t.Fatalf("AgentTeamRolePrompt: %v", err)
	}
	if app.promptSession != "main-1" || app.promptRole != "tl" {
		t.Fatalf("提示词读取转发 = %q/%q", app.promptSession, app.promptRole)
	}

	result, err := bridge.AgentTeamOptimizePrompt("", dto.RolePromptOptimizeRequest{
		RoleName: " tl ", SystemPrompt: "  你是评审员  ",
	})
	if err != nil {
		t.Fatalf("AgentTeamOptimizePrompt: %v", err)
	}
	if app.optimizeRequest.RoleName != "tl" || app.optimizeRequest.SystemPrompt != "你是评审员" {
		t.Fatalf("提示词优化入参必须 trim：%+v", app.optimizeRequest)
	}
	if result.Optimized != "优化后" || result.Original != "你是评审员" {
		t.Fatalf("提示词优化回执 = %+v", result)
	}
}

// TestBridgeAgentTeamGlobalForwarding：全局母本读 / 员工库写 / 默认顺序写 /
// 「确认普及搭配到全局」都只做参数归一（trim）与窄转发，会话号空值解析为当前视图会话。
func TestBridgeAgentTeamGlobalForwarding(t *testing.T) {
	app := newFakeAgentTeamApplication("main-1")
	bridge, err := NewBridge(app, Options{})
	if err != nil {
		t.Fatalf("NewBridge: %v", err)
	}

	config, err := bridge.AgentTeamGlobalConfig("")
	if err != nil {
		t.Fatalf("AgentTeamGlobalConfig: %v", err)
	}
	if app.librarySession != "main-1" || config.Composition.SessionID != "main-1" {
		t.Fatalf("全局母本会话解析 = %q / %q", app.librarySession, config.Composition.SessionID)
	}
	if len(config.Employees.Employees) != 1 || len(config.Order.OrderRoles) != 3 {
		t.Fatalf("全局母本读回 = %+v", config)
	}

	if _, err := bridge.AgentTeamSaveEmployee("", dto.RoleSpec{RoleName: " reviewer ", RoleKind: dto.RoleKindAgent}); err != nil {
		t.Fatalf("AgentTeamSaveEmployee: %v", err)
	}
	if app.librarySession != "main-1" || app.savedEmployee.RoleName != "reviewer" {
		t.Fatalf("员工库写入转发 = %q/%+v", app.librarySession, app.savedEmployee)
	}

	if _, err := bridge.AgentTeamDeleteEmployee("", " reviewer "); err != nil {
		t.Fatalf("AgentTeamDeleteEmployee: %v", err)
	}
	if app.deletedEmployee != "reviewer" {
		t.Fatalf("员工库删除转发 = %q", app.deletedEmployee)
	}
	if _, err := bridge.AgentTeamDeleteEmployee("", "   "); err == nil {
		t.Fatal("空角色名必须拒绝，不把空名转发给应用层")
	}

	if _, err := bridge.AgentTeamSetDefaultOrder("", " user_main_decided ", []string{"user", "main", "reviewer"}); err != nil {
		t.Fatalf("AgentTeamSetDefaultOrder: %v", err)
	}
	if app.defaultOrderPolicy != "user_main_decided" || strings.Join(app.defaultOrderRoles, ",") != "user,main,reviewer" {
		t.Fatalf("默认顺序转发 = %q/%v", app.defaultOrderPolicy, app.defaultOrderRoles)
	}

	if _, err := bridge.AgentTeamPublishToGlobal("", " 我的队 ", " my-team "); err != nil {
		t.Fatalf("AgentTeamPublishToGlobal: %v", err)
	}
	if app.librarySession != "main-1" || app.publishedName != "我的队" || app.publishedTeamID != "my-team" {
		t.Fatalf("普及搭配转发 = %q/%q/%q", app.librarySession, app.publishedName, app.publishedTeamID)
	}
}
