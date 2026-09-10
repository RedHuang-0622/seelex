package gui

import (
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
	"github.com/RedHuang-0622/seelex/application/model"
)

// fakeAgentTeamApplication 复刻 Bridge 消费的 A2A 角色管理面（S27 之后的纯
// DTO 形态），记录每次转发的会话号与参数，便于断言 Bridge 只做归一与转发。
type fakeAgentTeamApplication struct {
	*fakeApplication
	viewSession      string
	materializeCalls []string
	orderSession     string
	orderPolicy      string
	orderRoles       []string
	putSession       string
	putRole          dto.RoleSpec
	deleteSession    string
	deleteRole       string
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
	return dto.TeamView{SessionID: mainSessionID, TeamKind: "goal-a2a", OrderRoles: []string{"user", "main", "tl"}}, nil
}

func (app *fakeAgentTeamApplication) AgentTeamMaterializePreset(mainSessionID, teamKind string, joinSeq uint64) (dto.TeamMaterializeResult, error) {
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

func (app *fakeAgentTeamApplication) AgentTeamSetOrder(mainSessionID, policy string, orderRoles []string) (dto.TeamView, error) {
	app.orderSession, app.orderPolicy, app.orderRoles = mainSessionID, policy, orderRoles
	return dto.TeamView{SessionID: mainSessionID, OrderPolicy: policy, OrderRoles: orderRoles}, nil
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
}
