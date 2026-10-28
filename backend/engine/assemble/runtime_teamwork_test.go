package assemble

// runtime_teamwork_test.go — 钉住 teamwork 编排面在 Runtime 上的接线：
//   - 未注入 backend = 不注册这族工具（有就有、没有就是没装配）；
//   - 注入后六件套 + jobs_manage 出现在工具面；
//   - leader 工具经 Coordinator 落到持久面（计划写回可读）；
//   - 派发一个不在编的角色被显式拒绝（不静默排队、不静默放行）。

import (
	"context"
	"sync"
	"testing"

	"github.com/RedHuang-0622/seelex/contract/dto"
	seeletelemetry "github.com/RedHuang-0622/seelex/internal/telemetry"
	storeteamwork "github.com/RedHuang-0622/seelex/store/teamwork"
)

type memPlanStore struct {
	mu       sync.Mutex
	plans    map[dto.SessionKey]storeteamwork.Plan
	events   map[dto.SessionKey][]storeteamwork.Event
	bindings map[dto.SessionKey][]storeteamwork.Binding
}

func (m *memPlanStore) WritePlan(_ context.Context, key dto.SessionKey, plan storeteamwork.Plan, _ int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.plans == nil {
		m.plans = map[dto.SessionKey]storeteamwork.Plan{}
	}
	m.plans[key] = plan
	return nil
}

func (m *memPlanStore) ReadPlan(_ context.Context, key dto.SessionKey) (storeteamwork.Plan, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	plan, ok := m.plans[key]
	if !ok {
		return storeteamwork.Plan{}, context.Canceled // 任意错误：缺失即未写入
	}
	return plan, nil
}

func (m *memPlanStore) AppendEvent(_ context.Context, key dto.SessionKey, event storeteamwork.Event) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.events == nil {
		m.events = map[dto.SessionKey][]storeteamwork.Event{}
	}
	m.events[key] = append(m.events[key], event)
	return nil
}

func (m *memPlanStore) ReadEvents(_ context.Context, key dto.SessionKey) ([]storeteamwork.Event, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]storeteamwork.Event(nil), m.events[key]...), nil
}

func (m *memPlanStore) AppendBinding(_ context.Context, key dto.SessionKey, binding storeteamwork.Binding) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.bindings == nil {
		m.bindings = map[dto.SessionKey][]storeteamwork.Binding{}
	}
	m.bindings[key] = append(m.bindings[key], binding)
	return nil
}

func (m *memPlanStore) ReadBindings(_ context.Context, key dto.SessionKey) ([]storeteamwork.Binding, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]storeteamwork.Binding(nil), m.bindings[key]...), nil
}

func teamworkTestBackend(st *memPlanStore, sessionID string) TeamworkBackend {
	return TeamworkBackend{
		Store: st,
		KeyFor: func(id string) (dto.SessionKey, bool) {
			if id != sessionID {
				return dto.SessionKey{}, false
			}
			return dto.SessionKey{ProjectID: "p-team", SessionID: sessionID}, true
		},
		MaxTeammates: 6,
	}
}

func TestTeamworkToolsRegisteredOnlyWithBackend(t *testing.T) {
	r := newTestRuntime(t)
	defer r.Shutdown()
	if _, ok := r.registry.FindTool("team_plan"); ok {
		t.Fatal("未注入 backend 时不应注册 team_plan（不是注册一堆永远报错的空壳）")
	}
	if err := r.SetTeamworkBackend(teamworkTestBackend(&memPlanStore{}, "s-team")); err != nil {
		t.Fatalf("SetTeamworkBackend: %v", err)
	}
	r.registerTeamworkTools()
	for _, name := range []string{"team_plan", "team_dispatch", "team_join", "team_milestone", "team_close", "jobs_manage"} {
		if _, ok := r.registry.FindTool(name); !ok {
			t.Fatalf("注入 backend 后 %s 应在工具面里", name)
		}
	}
}

func TestTeamworkPlanHandlerPersistsPlan(t *testing.T) {
	r := newTestRuntime(t)
	defer r.Shutdown()
	st := &memPlanStore{}
	if err := r.SetTeamworkBackend(teamworkTestBackend(st, "s-team")); err != nil {
		t.Fatalf("SetTeamworkBackend: %v", err)
	}
	ctx := seeletelemetry.WithSessionID(context.Background(), "s-team")
	receipt, err := r.teamPlanHandler(ctx, `{
		"team_id": "v-model",
		"milestones": [{"id":"m-req","name":"需求"},{"id":"m-impl","name":"实现","depends_on":["m-req"]}],
		"members": [{"role":"pm"},{"role":"exec"}]
	}`)
	if err != nil {
		t.Fatalf("team_plan: %v", err)
	}
	if receipt == "" {
		t.Fatal("team_plan 应返回受理回执")
	}
	plan, err := st.ReadPlan(context.Background(), dto.SessionKey{ProjectID: "p-team", SessionID: "s-team"})
	if err != nil {
		t.Fatalf("计划未落持久面: %v", err)
	}
	if plan.TeamID != "v-model" || len(plan.Milestones) != 2 || len(plan.Members) != 2 {
		t.Fatalf("计划内容不符: %+v", plan)
	}
	if plan.Version != 1 {
		t.Fatalf("缺省版本应为 1，得 %d", plan.Version)
	}
	// 缺 role_session_id 的成员由 (team_id, role) 派生补齐。
	for _, member := range plan.Members {
		if member.RoleSessionID == "" {
			t.Fatalf("成员 %q 的 role_session_id 应被派生补齐", member.Role)
		}
	}
}

func TestTeamworkDispatchRefusesUnenrolledRole(t *testing.T) {
	r := newTestRuntime(t)
	defer r.Shutdown()
	st := &memPlanStore{}
	if err := r.SetTeamworkBackend(teamworkTestBackend(st, "s-team")); err != nil {
		t.Fatalf("SetTeamworkBackend: %v", err)
	}
	ctx := seeletelemetry.WithSessionID(context.Background(), "s-team")
	if _, err := r.teamPlanHandler(ctx, `{"team_id":"t","milestones":[{"id":"m-req"}],"members":[{"role":"pm"}]}`); err != nil {
		t.Fatalf("team_plan: %v", err)
	}
	if _, err := r.teamDispatchHandler(ctx, `{"role":"ghost","goal":"x"}`); err == nil {
		t.Fatal("派发不在编的角色应被显式拒绝")
	}
}

func TestTeamworkToolsNeedSessionScope(t *testing.T) {
	r := newTestRuntime(t)
	defer r.Shutdown()
	if err := r.SetTeamworkBackend(teamworkTestBackend(&memPlanStore{}, "s-team")); err != nil {
		t.Fatalf("SetTeamworkBackend: %v", err)
	}
	if _, err := r.teamPlanHandler(context.Background(), `{"team_id":"t","milestones":[{"id":"m-a"}],"members":[{"role":"pm"}]}`); err == nil {
		t.Fatal("没有会话归属的调用应被拒绝（工具必须在会话回合内调用）")
	}
}
