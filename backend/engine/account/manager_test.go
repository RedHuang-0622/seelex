package account

import (
	"context"
	"testing"

	"github.com/RedHuang-0622/Seele/accountpool"
	"github.com/RedHuang-0622/Seele/agent"
	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/internal/config"
	"github.com/RedHuang-0622/seelex/internal/model"
)

func TestManagerSelectProviderAndLimits(t *testing.T) {
	pool := accountpool.New[agent.Completer]()
	manager := NewManager(
		[]model.AccountSpec{{Name: "agent-1", Provider: "openai", Model: "gpt-4o"}, {Name: "sub-1", Provider: "deepseek", Model: "r1"}},
		map[string]config.AccountLimits{"agent-1": {ContextWindow: 16000, MaxOutputTokens: 2000}},
		"agent-1", pool,
	)
	if manager.Provider() != "openai" {
		t.Fatalf("default provider = %q, want openai", manager.Provider())
	}
	if manager.Limits().MaxOutputTokens != 2000 {
		t.Fatalf("default limits = %+v", manager.Limits())
	}
	if !manager.Select("sub-1") {
		t.Fatal("select known account must succeed")
	}
	if manager.Provider() != "deepseek" {
		t.Fatalf("provider after select = %q", manager.Provider())
	}
	if manager.Select("missing") {
		t.Fatal("select unknown account must fail")
	}
	manager.SetProvider("openai")
	if manager.Selected() != "" {
		t.Fatalf("SetProvider must clear fixed account, selected=%q", manager.Selected())
	}
}

// registerCompleterAccount 把一个账号注册进测试池（Value 无需真实 client：
// 谓词路由只读快照）。
func registerCompleterAccount(t *testing.T, pool *accountpool.P2CPool[agent.Completer], id, provider, model2 string) {
	t.Helper()
	if err := pool.Register(accountpool.Account[agent.Completer]{
		ID: id, Value: nil, MaxConcurrency: 1,
		Metadata: map[string]string{"provider": provider, "model": model2},
	}); err != nil {
		t.Fatalf("register %s: %v", id, err)
	}
}

// TestManagerSelectorMainChainAgentRole 钉住主链路角色约束：主会话请求（无
// SceneScope、未固定账号）的候选必须是 agent 角色的账号——负载只在角色内部做，
// 不允许 P2C 落到 subagent/goalplan 账号上。
func TestManagerSelectorMainChainAgentRole(t *testing.T) {
	pool := accountpool.New[agent.Completer]()
	registerCompleterAccount(t, pool, "agent-1", "openai", "gpt-4o")
	registerCompleterAccount(t, pool, "agent-2", "deepseek", "v3")
	registerCompleterAccount(t, pool, "subagent-1", "openai", "r1")
	registerCompleterAccount(t, pool, "goalplan-1", "openai", "o3")
	manager := NewManager(nil, nil, "agent-1", pool)
	request := manager.Selector()(context.Background(), nil, nil)
	if request.AccountID != "" {
		t.Fatalf("unpinned main-chain request must not pin an account, got %q", request.AccountID)
	}
	if request.Predicate == nil {
		t.Fatal("main-chain request must carry the agent-role predicate")
	}
	for _, id := range []string{"agent-1", "agent-2"} {
		if !request.Predicate(accountpool.AccountSnapshot{ID: id}) {
			t.Errorf("agent account %s must be eligible", id)
		}
	}
	for _, id := range []string{"subagent-1", "goalplan-1"} {
		if request.Predicate(accountpool.AccountSnapshot{ID: id}) {
			t.Errorf("non-agent account %s must be excluded from main-chain load balancing", id)
		}
	}
}

// TestManagerSelectorMainChainRoleFallback：agent 角色没有任何可用账号时，
// 谓词不叠加（回退"池内任选"），单角色安装不被判死。
func TestManagerSelectorMainChainRoleFallback(t *testing.T) {
	pool := accountpool.New[agent.Completer]()
	registerCompleterAccount(t, pool, "subagent-1", "openai", "r1")
	manager := NewManager(nil, nil, "subagent-1", pool)
	request := manager.Selector()(context.Background(), nil, nil)
	if request.Predicate != nil && !request.Predicate(accountpool.AccountSnapshot{ID: "subagent-1"}) {
		t.Fatal("fallback: no agent-role accounts means the predicate must not exclude the only account")
	}
}

// TestManagerAddRegistersSpecAndLimits 覆盖热注册在管理器这一侧的判据：空名拒绝、
// 重名拒绝（不静默覆盖正在承流的账号）、新账号的限额按名字可读。
func TestManagerAddRegistersSpecAndLimits(t *testing.T) {
	manager := NewManager(
		[]model.AccountSpec{{Name: "agent-1", Provider: "openai", Model: "m", Role: model.RoleAgent}},
		map[string]config.AccountLimits{"agent-1": {ContextWindow: 100_000, MaxOutputTokens: 8_192}},
		"agent-1",
		nil,
	)

	if err := manager.Add(model.AccountSpec{Name: "  "}, config.AccountLimits{}); err == nil {
		t.Fatalf("空名应被拒绝")
	}
	if err := manager.Add(
		model.AccountSpec{Name: "agent-2", Provider: "openai", Model: "m2", Role: model.RoleAgent},
		config.AccountLimits{ContextWindow: 300_000, MaxOutputTokens: 8_192},
	); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if got := manager.LimitsFor("agent-2").ContextWindow; got != 300_000 {
		t.Fatalf("agent-2 的窗口 = %d，期望 300000", got)
	}
	// 重名：报错，且既有条目（agent-1）的限额一个字节都没被改。
	if err := manager.Add(
		model.AccountSpec{Name: "agent-1", Provider: "openai", Model: "hijack", Role: model.RoleAgent},
		config.AccountLimits{ContextWindow: 1, MaxOutputTokens: 1},
	); err == nil {
		t.Fatalf("重名应被拒绝")
	}
	if got := manager.LimitsFor("agent-1").ContextWindow; got != 100_000 {
		t.Fatalf("重名注册污染了既有账号: %d", got)
	}
	// 加账号不等于切账号：选中项与默认账号都不动。
	if got := manager.Selected(); got != "" {
		t.Fatalf("Add 不该改选中账号，实际 %q", got)
	}
	var nilManager *Manager
	if err := nilManager.Add(model.AccountSpec{Name: "x"}, config.AccountLimits{}); err == nil {
		t.Fatalf("nil manager 应报错")
	}
}

func TestManagerAccountsCarryExplicitRoleAndCredentialState(t *testing.T) {
	pool := accountpool.New[agent.Completer]()
	registerCompleterAccount(t, pool, "teammate-7", "deepseek", "v3")
	manager := NewManager(
		[]model.AccountSpec{{
			Name: "teammate-7", Provider: "deepseek", Model: "v3",
			Role: model.RoleTeammate, APIKey: "secret-not-exposed",
		}},
		nil, "teammate-7", pool,
	)
	accounts := manager.Accounts()
	if len(accounts) != 1 {
		t.Fatalf("Accounts = %+v", accounts)
	}
	if accounts[0].Role != string(model.RoleTeammate) {
		t.Fatalf("Role = %q, want teammate（不能由名字或默认 agent 猜）", accounts[0].Role)
	}
	if !accounts[0].APIKeySet {
		t.Fatal("APIKeySet=false，want true")
	}
}

func TestManagerRoleAccountPinsScopedRequest(t *testing.T) {
	manager := NewManager([]model.AccountSpec{
		{Name: "teammate-1", Role: model.RoleTeammate},
		{Name: "teammate-2", Role: model.RoleTeammate},
	}, nil, "teammate-1", nil)
	if err := manager.SetRoleAccount(model.RoleTeammate, "teammate-2"); err != nil {
		t.Fatal(err)
	}
	ctx := dto.WithSceneScope(context.Background(), dto.SceneScope{
		SceneID: "role-1", Role: dto.RoleTeammate, BranchID: "branch-1",
	})
	request := manager.Selector()(ctx, nil, nil)
	if request.AccountID != "teammate-2" {
		t.Fatalf("AccountID = %q, want teammate-2", request.AccountID)
	}
	if err := manager.SetRoleAccount(model.RoleTeammate, "teammate-1b"); err == nil {
		t.Fatal("未知账号必须报错")
	}
}
