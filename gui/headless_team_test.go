package gui

import (
	"testing"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
)

// fakeTeamApplication 在 headless RPC 单测里复刻 Application 的 AgentTeam 扩展面；
// 装配语义由 application/core/agentteam 与 sessionstore 的测试覆盖。
type fakeTeamApplication struct {
	*fakeApplication
	materialized dto.TeamMaterializeResult
	registry     dto.TeamRegistry
	view         dto.TeamView
	orderPolicy  string
	orderRoles   []string
}

func newFakeTeamApplication() *fakeTeamApplication {
	return &fakeTeamApplication{fakeApplication: newFakeApplication()}
}

func (app *fakeTeamApplication) AgentTeamPresets() []dto.TeamSpec {
	return []dto.TeamSpec{{TeamID: "goal-a2a", TeamKind: "goal-a2a", OrderPolicy: dto.OrderPolicyGoalLoop}}
}

func (app *fakeTeamApplication) MaterializeAgentTeam(mainSessionID string, spec dto.TeamSpec, joinSeq uint64) (dto.TeamMaterializeResult, error) {
	app.materialized = dto.TeamMaterializeResult{
		Spec: spec,
		View: dto.TeamView{SessionID: mainSessionID, TeamKind: spec.TeamKind, OrderRoles: spec.OrderRoles},
		Sessions: []dto.TeamRoleSession{{
			RoleName: "reviewer", RoleSessionID: spec.TeamID + "-reviewer", Exists: true, Created: true,
		}},
	}
	return app.materialized, nil
}

func (app *fakeTeamApplication) MaterializeAgentTeamPreset(mainSessionID, teamKind string, joinSeq uint64) (dto.TeamMaterializeResult, error) {
	spec := dto.TeamSpec{TeamID: teamKind, TeamKind: teamKind, OrderPolicy: dto.OrderPolicyGoalLoop,
		OrderRoles: []string{"user", "main", "tl"}}
	return app.MaterializeAgentTeam(mainSessionID, spec, joinSeq)
}

func (app *fakeTeamApplication) AgentTeamView(mainSessionID string) (dto.TeamView, error) {
	app.view = dto.TeamView{
		SessionID:   mainSessionID,
		OrderPolicy: firstNonEmptyString(app.orderPolicy, dto.OrderPolicyGoalLoop),
		OrderRoles:  app.orderRoles,
	}
	return app.view, nil
}

func (app *fakeTeamApplication) AgentTeamPutRole(mainSessionID string, role dto.RoleSpec) (dto.TeamRegistry, error) {
	app.registry = dto.TeamRegistry{TeamKind: "review-team", Configured: true, Roles: []dto.RoleSpec{role}}
	return app.registry, nil
}

func (app *fakeTeamApplication) AgentTeamDeleteRole(mainSessionID, roleName string) (dto.TeamRegistry, error) {
	app.registry = dto.TeamRegistry{TeamKind: "review-team", Configured: true}
	return app.registry, nil
}

func (app *fakeTeamApplication) AgentTeamSetOrder(mainSessionID, policy string, orderRoles []string) (dto.TeamView, error) {
	app.orderPolicy = policy
	app.orderRoles = orderRoles
	return app.AgentTeamView(mainSessionID)
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

// TestHeadlessTeamRPC 覆盖 `team.*` 的装配/成员表/角色配置/顺序设置契约。
func TestHeadlessTeamRPC(t *testing.T) {
	app := newFakeTeamApplication()
	base := newHeadlessTestServer(t, app)

	result := headlessRPC(t, base, "team.presets", map[string]any{})
	if !result.OK {
		t.Fatalf("team.presets = ok=%v err=%q", result.OK, result.Error)
	}

	result = headlessRPC(t, base, "team.materialize", map[string]any{
		"main_session_id": "main-1", "team_kind": "goal-a2a", "join_seq_id": 4,
	})
	if !result.OK || app.materialized.View.TeamKind != "goal-a2a" {
		t.Fatalf("team.materialize = ok=%v err=%q result=%+v", result.OK, result.Error, app.materialized)
	}
	if len(app.materialized.Sessions) != 1 || app.materialized.Sessions[0].RoleName != "reviewer" {
		t.Fatalf("materialize sessions = %+v", app.materialized.Sessions)
	}

	result = headlessRPC(t, base, "team.materialize", map[string]any{
		"main_session_id": "main-1",
		"spec": map[string]any{
			"team_id": "review-team", "team_kind": "review-team",
			"order_policy": "user_main_decided", "order_roles": []string{"user", "main", "reviewer"},
		},
	})
	if !result.OK || app.materialized.Spec.TeamID != "review-team" {
		t.Fatalf("team.materialize(spec) = ok=%v err=%q spec=%+v", result.OK, result.Error, app.materialized.Spec)
	}

	result = headlessRPC(t, base, "team.view", map[string]any{"main_session_id": "main-1"})
	if !result.OK || app.view.SessionID != "main-1" {
		t.Fatalf("team.view = ok=%v err=%q view=%+v", result.OK, result.Error, app.view)
	}

	result = headlessRPC(t, base, "team.put_role", map[string]any{
		"main_session_id": "main-1",
		"role":            map[string]any{"role_name": "reviewer", "role_kind": "agent", "tools_policy": "readonly"},
	})
	if !result.OK || len(app.registry.Roles) != 1 || app.registry.Roles[0].ToolsPolicy != "readonly" {
		t.Fatalf("team.put_role = ok=%v err=%q registry=%+v", result.OK, result.Error, app.registry)
	}

	result = headlessRPC(t, base, "team.set_order", map[string]any{
		"main_session_id": "main-1",
		"order_policy":    "user_main_decided",
		"order_roles":     []string{"user", "main", "reviewer"},
	})
	if !result.OK || app.orderPolicy != "user_main_decided" || len(app.orderRoles) != 3 {
		t.Fatalf("team.set_order = ok=%v err=%q policy=%q roles=%v", result.OK, result.Error, app.orderPolicy, app.orderRoles)
	}

	result = headlessRPC(t, base, "team.delete_role", map[string]any{
		"main_session_id": "main-1", "role_name": "reviewer",
	})
	if !result.OK || len(app.registry.Roles) != 0 {
		t.Fatalf("team.delete_role = ok=%v err=%q registry=%+v", result.OK, result.Error, app.registry)
	}

	result = headlessRPC(t, base, "team.unknown", map[string]any{})
	if result.OK {
		t.Fatal("unknown team method must fail")
	}
}
