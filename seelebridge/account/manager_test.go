package account

import (
	"context"
	"testing"

	"github.com/RedHuang-0622/Seele/accountpool"
	"github.com/RedHuang-0622/Seele/agent"
	"github.com/RedHuang-0622/seelex/application/contract/dto"
	"github.com/RedHuang-0622/seelex/seelebridge/internal/config"
	"github.com/RedHuang-0622/seelex/seelebridge/internal/model"
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
