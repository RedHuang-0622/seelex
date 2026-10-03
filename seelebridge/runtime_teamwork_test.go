package seelebridge

// runtime_teamwork_test.go — 钉住 teamwork 编排面在 Runtime 上的接线：
//   - 未注入 backend = 不注册这族工具（有就有、没有就是没装配）；
//   - 注入后六件套 + jobs_manage 出现在工具面；
//   - leader 工具经 Coordinator 落到持久面（计划写回可读）；
//   - 派发一个不在编的角色被显式拒绝（不静默排队、不静默放行）。

import (
	"context"
	"sync"
	"testing"

	seeletelemetry "github.com/RedHuang-0622/seelex/seelebridge/internal/telemetry"
	"github.com/RedHuang-0622/seelex/sessionstore"
)

type memPlanStore struct {
	mu       sync.Mutex
	plans    map[sessionstore.Key]sessionstore.TeamworkPlan
	events   map[sessionstore.Key][]sessionstore.TeamworkEvent
	bindings map[sessionstore.Key][]sessionstore.TeamworkBinding
}

func (m *memPlanStore) WritePlan(_ context.Context, key sessionstore.Key, plan sessionstore.TeamworkPlan, _ int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.plans == nil {
		m.plans = map[sessionstore.Key]sessionstore.TeamworkPlan{}
	}
	m.plans[key] = plan
	return nil
}

func (m *memPlanStore) ReadPlan(_ context.Context, key sessionstore.Key) (sessionstore.TeamworkPlan, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	plan, ok := m.plans[key]
	if !ok {
		return sessionstore.TeamworkPlan{}, context.Canceled // 任意错误：缺失即未写入
	}
	return plan, nil
}

func (m *memPlanStore) AppendEvent(_ context.Context, key sessionstore.Key, event sessionstore.TeamworkEvent) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.events == nil {
		m.events = map[sessionstore.Key][]sessionstore.TeamworkEvent{}
	}
	m.events[key] = append(m.events[key], event)
	return nil
}

func (m *memPlanStore) ReadEvents(_ context.Context, key sessionstore.Key) ([]sessionstore.TeamworkEvent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]sessionstore.TeamworkEvent(nil), m.events[key]...), nil
}

func (m *memPlanStore) AppendBinding(_ context.Context, key sessionstore.Key, binding sessionstore.TeamworkBinding) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.bindings == nil {
		m.bindings = map[sessionstore.Key][]sessionstore.TeamworkBinding{}
	}
	m.bindings[key] = append(m.bindings[key], binding)
	return nil
}

func (m *memPlanStore) ReadBindings(_ context.Context, key sessionstore.Key) ([]sessionstore.TeamworkBinding, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]sessionstore.TeamworkBinding(nil), m.bindings[key]...), nil
}

func teamworkTestBackend(store *memPlanStore, sessionID string) TeamworkBackend {
	return TeamworkBackend{
		Store: store,
		KeyFor: func(id string) (sessionstore.Key, bool) {
			if id != sessionID {
				return sessionstore.Key{}, false
			}
			return sessionstore.Key{ProjectID: "p-team", SessionID: sessionID}, true
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
	for _, name := range []string{"team_plan", "team_dispatch", "team_join", "team_milestone", "team_retire", "jobs_manage"} {
		if _, ok := r.registry.FindTool(name); !ok {
			t.Fatalf("注入 backend 后 %s 应在工具面里", name)
		}
	}
}

func TestTeamworkPlanHandlerPersistsPlan(t *testing.T) {
	r := newTestRuntime(t)
	defer r.Shutdown()
	store := &memPlanStore{}
	if err := r.SetTeamworkBackend(teamworkTestBackend(store, "s-team")); err != nil {
		t.Fatalf("SetTeamworkBackend: %v", err)
	}
	ctx := seeletelemetry.WithSessionID(context.Background(), "s-team")
	receipt, err := r.teamPlanHandler(ctx, `{
		"team_id": "v-model",
		"stages": [{"id":"req","roles":["pm"]},{"id":"impl","roles":["exec"],"depends_on":["req"]}],
		"members": [{"role":"pm"},{"role":"exec"}]
	}`)
	if err != nil {
		t.Fatalf("team_plan: %v", err)
	}
	if receipt == "" {
		t.Fatal("team_plan 应返回受理回执")
	}
	plan, err := store.ReadPlan(context.Background(), sessionstore.Key{ProjectID: "p-team", SessionID: "s-team"})
	if err != nil {
		t.Fatalf("计划未落持久面: %v", err)
	}
	if plan.TeamID != "v-model" || len(plan.Stages) != 2 || len(plan.Members) != 2 {
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
	store := &memPlanStore{}
	if err := r.SetTeamworkBackend(teamworkTestBackend(store, "s-team")); err != nil {
		t.Fatalf("SetTeamworkBackend: %v", err)
	}
	ctx := seeletelemetry.WithSessionID(context.Background(), "s-team")
	if _, err := r.teamPlanHandler(ctx, `{"team_id":"t","stages":[{"id":"req","roles":["pm"]}],"members":[{"role":"pm"}]}`); err != nil {
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
	if _, err := r.teamPlanHandler(context.Background(), `{"team_id":"t","stages":[{"id":"a","roles":["pm"]}],"members":[{"role":"pm"}]}`); err == nil {
		t.Fatal("没有会话归属的调用应被拒绝（工具必须在会话回合内调用）")
	}
}
