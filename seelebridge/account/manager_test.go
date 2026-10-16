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

func TestManagerSelectorNodePin(t *testing.T) {
	pool := accountpool.New[agent.Completer]()
	manager := NewManager(
		[]model.AccountSpec{{Name: "main", Provider: "openai", Model: "gpt-4o"}},
		nil, "main", pool,
	)
	selector := manager.Selector(SelectorDeps{
		BranchBinding: func() (binding dto.PlanBranchBinding) {
			binding.AccountID = "pinned"
			return binding
		},
	})
	ctx := model.WithNodeScope(context.Background(), model.NodeScope{NodeID: "n1", Role: model.RoleSubAgent})
	req := selector(ctx, nil, nil)
	if req.AccountID != "pinned" {
		t.Fatalf("node request must pin binding account, got %q", req.AccountID)
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
// NodeScope、未固定账号）的候选必须是 agent 角色的账号——负载只在角色内部做，
// 不允许 P2C 落到 subagent/goalplan 账号上。
func TestManagerSelectorMainChainAgentRole(t *testing.T) {
	pool := accountpool.New[agent.Completer]()
	registerCompleterAccount(t, pool, "agent-1", "openai", "gpt-4o")
	registerCompleterAccount(t, pool, "agent-2", "deepseek", "v3")
	registerCompleterAccount(t, pool, "subagent-1", "openai", "r1")
	registerCompleterAccount(t, pool, "goalplan-1", "openai", "o3")
	manager := NewManager(nil, nil, "agent-1", pool)
	request := manager.Selector(SelectorDeps{})(context.Background(), nil, nil)
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
	request := manager.Selector(SelectorDeps{})(context.Background(), nil, nil)
	if request.Predicate != nil && !request.Predicate(accountpool.AccountSnapshot{ID: "subagent-1"}) {
		t.Fatal("fallback: no agent-role accounts means the predicate must not exclude the only account")
	}
}
