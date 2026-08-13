package seelebridge

import (
	"context"
	"fmt"

	"github.com/RedHuang-0622/Seele/accountpool"
	"github.com/RedHuang-0622/Seele/agent"
	"github.com/RedHuang-0622/Seele/agent/bridge"
	"github.com/RedHuang-0622/Seele/agent/core/api"
	"github.com/RedHuang-0622/Seele/types"
	"github.com/RedHuang-0622/seelex/seelebridge/internal/model"
)

// clientFor 从账号配置构造一个同步 Completer（agent.Completer）。
// 每个账号一个独立 client，账号选择统一走 accountpool 租约，不做类型断言。
func clientFor(spec model.AccountSpec) agent.Completer {
	client := api.NewChatClient(types.LLMConfig{
		BaseURL: spec.BaseURL, APIKey: spec.APIKey, Model: spec.Model,
		MaxTokens: spec.MaxTokens, Timeout: 300, Temperature: 0.7,
	})
	client.SetProvider(api.ProviderType(spec.Provider))
	return client
}

// registerAccounts 把加载的账号配置注册进 P2C 账号池。
// Metadata 只放非敏感路由属性（provider/model），凭据留在 Value 内部。
func registerAccounts(pool *accountpool.P2CPool[agent.Completer], specs []model.AccountSpec) error {
	for _, spec := range specs {
		if err := pool.Register(accountpool.Account[agent.Completer]{
			ID:             spec.Name,
			Value:          clientFor(spec),
			MaxConcurrency: spec.MaxConcurrency,
			Metadata: map[string]string{
				"provider": spec.Provider,
				"model":    spec.Model,
			},
		}); err != nil {
			return fmt.Errorf("seelebridge: register account %q: %w", spec.Name, err)
		}
	}
	return nil
}

// accountSelector 是主链路共享的账号选择器闭包：读取 Runtime 当前的
// provider 过滤与选中账号（不持有任何 api.ChatClient 引用），把请求条件
// 转换为 accountpool.AcquireRequest。P2C 池负责实际选择与租约。
// Plan 子代理请求（ctx 含 NodeScope）优先走节点账号解析：显式 pin 优先，
// 否则按角色 + branchID 走确定性 hash（ResolveAccountForBranch 逻辑）。
func (r *Runtime) accountSelector(ctx context.Context, messages []types.Message, tools []types.Tool) accountpool.AcquireRequest {
	if scope := nodeScopeFromContextOrEmpty(ctx); scope.NodeID != "" {
		return r.nodeAccountRequest(scope)
	}
	r.branchMu.RLock()
	selected := r.selectedAccountID
	provider := r.providerFilter
	r.branchMu.RUnlock()
	request := accountpool.AcquireRequest{}
	if selected != "" {
		request.AccountID = selected
	}
	if provider != "" {
		if request.Metadata == nil {
			request.Metadata = make(map[string]string)
		}
		request.Metadata["provider"] = provider
	}
	return request
}

// nodeAccountRequest 按节点作用域解析账号租约请求：binding 显式 AccountID
// 直接 pin；否则按 role + planID:branchID 确定性 hash 选择（不占用主链路
// 选中账号，与 ResolveAccountForBranch 语义一致）。
func (r *Runtime) nodeAccountRequest(scope NodeScope) accountpool.AcquireRequest {
	request := accountpool.AcquireRequest{}
	binding := r.currentPlanBranchBinding()
	if binding.AccountID != "" {
		request.AccountID = binding.AccountID
		return request
	}
	seed := scope.BranchID
	if seed == "" {
		seed = scope.NodeID
	}
	accountID, err := ResolveAccountForBranch(r.pool, scope.Role, binding.PlanID+":"+seed)
	if err == nil {
		request.AccountID = accountID
	}
	return request
}

// bridgeAccountCompleter 构造同步 Completer（每次 Complete 恰好一次租约）。
func (r *Runtime) bridgeAccountCompleter() (agent.Completer, error) {
	completer, err := bridge.NewAccountCompleter(r.pool,
		bridge.WithAccountRequestSelector(r.accountSelector),
	)
	if err != nil {
		return nil, fmt.Errorf("seelebridge: assemble account completer: %w", err)
	}
	return completer, nil
}

// ResolveAccountForBranch selects an account without mutating the shared pool
// cursor. The same role and seed always resolve to the same configured account.
func ResolveAccountForBranch(pool *accountpool.P2CPool[agent.Completer], role model.AccountRole, seed string) (string, error) {
	entries := accountsForRole(pool, role)
	if len(entries) == 0 {
		return "", fmt.Errorf("seelebridge: no accounts available")
	}
	return entries[stableIndex(seed, len(entries))].Snapshot.ID, nil
}

func accountsForRole(pool *accountpool.P2CPool[agent.Completer], role model.AccountRole) []accountpool.Entry[agent.Completer] {
	if pool == nil {
		return nil
	}
	all := pool.Entries()
	roles := append([]model.AccountRole{role}, model.FallbackRoles(role)...)
	for _, candidate := range roles {
		matched := make([]accountpool.Entry[agent.Completer], 0)
		for _, entry := range all {
			if !entry.Snapshot.Disabled && model.AccountRoleFromName(entry.Snapshot.ID) == candidate {
				matched = append(matched, entry)
			}
		}
		if len(matched) > 0 {
			return matched
		}
	}
	for _, entry := range all {
		if !entry.Snapshot.Disabled {
			return []accountpool.Entry[agent.Completer]{entry}
		}
	}
	return nil
}

// fallbackRoles 定义见 seelebridge/internal/model（根包 model_aliases.go 重导出）。

func accountByName(specs []model.AccountSpec, name string) *model.AccountSpec {
	for index := range specs {
		if specs[index].Name == name {
			return &specs[index]
		}
	}
	return nil
}
