package seelebridge

import (
	"context"
	"fmt"
	"sort"

	"github.com/RedHuang-0622/Seele/accountpool"
	"github.com/RedHuang-0622/Seele/agent"
	"github.com/RedHuang-0622/Seele/agent/bridge"
	"github.com/RedHuang-0622/Seele/types"

	"github.com/RedHuang-0622/seelex/seelebridge/account"
	"github.com/RedHuang-0622/seelex/seelebridge/internal/model"
	"github.com/RedHuang-0622/seelex/seelebridge/internal/stream"
	seenode "github.com/RedHuang-0622/seelex/seelebridge/node"
)

// accountSelector 是主链路共享的账号选择器闭包：读取 Runtime 当前的
// provider 过滤与选中账号（不持有任何 api.ChatClient 引用），把请求条件
// 转换为 accountpool.AcquireRequest。P2C 池负责实际选择与租赁。
// Plan 子代理请求（ctx 含 NodeScope）优先走节点账号解析：显式 pin 优先，
// 否则按角色 + branchID 走确定性 hash（account.ResolveForBranch 逻辑）。
func (r *Runtime) accountSelector(ctx context.Context, messages []types.Message, tools []types.Tool) accountpool.AcquireRequest {
	if scope := model.NodeScopeFromContextOrEmpty(ctx); scope.NodeID != "" {
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

// assembleCompleters 构造同步与流式 completer，两者共享同一个账号选择器闭包。
func (r *Runtime) assembleCompleters() error {
	completer, err := r.bridgeAccountCompleter()
	if err != nil {
		return err
	}
	r.completer = completer
	r.streamer = stream.NewStreamingCompleter(r.pool, r.accountSelector)
	return nil
}

// bridgeAccountCompleter 构造同步 Completer（每次 Complete 恰好一次租赁）。
func (r *Runtime) bridgeAccountCompleter() (agent.Completer, error) {
	completer, err := bridge.NewAccountCompleter(r.pool,
		bridge.WithAccountRequestSelector(r.accountSelector),
	)
	if err != nil {
		return nil, fmt.Errorf("seelebridge: assemble account completer: %w", err)
	}
	return completer, nil
}

// nodeAccountRequest 按节点作用域解析账号租赁请求：binding 显式 AccountID
// 直接 pin；否则按 role + planID:branchID 确定性 hash 选择。
func (r *Runtime) nodeAccountRequest(scope seenode.NodeScope) accountpool.AcquireRequest {
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
	accountID, err := account.ResolveForBranch(r.pool, scope.Role, binding.PlanID+":"+seed)
	if err == nil {
		request.AccountID = accountID
	}
	return request
}

// setSelectedAccount 切换主链路选中账号（provider 过滤跟随账号规格）。
func (r *Runtime) setSelectedAccount(name string) {
	r.branchMu.Lock()
	r.selectedAccountID = name
	r.branchMu.Unlock()
}
func (r *Runtime) Accounts() []Account {
	entries := r.pool.Entries()
	result := make([]Account, 0, len(entries))
	for _, entry := range entries {
		result = append(result, Account{
			Name:     entry.Snapshot.ID,
			Provider: entry.Snapshot.Metadata["provider"],
			Model:    entry.Snapshot.Metadata["model"],
			Disabled: entry.Snapshot.Disabled,
		})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}
func (r *Runtime) Provider() string {
	r.branchMu.RLock()
	provider := r.providerFilter
	r.branchMu.RUnlock()
	if provider == "" {
		if spec := account.ByName(r.accountSpecList(), r.defaultAccountID); spec != nil {
			provider = spec.Provider
		}
	}
	return provider
}
func (r *Runtime) SelectAccount(name string) bool {
	spec := account.ByName(r.accountSpecList(), name)
	if spec == nil {
		return false
	}
	r.branchMu.Lock()
	r.selectedAccountID = spec.Name
	r.providerFilter = spec.Provider
	r.branchMu.Unlock()
	return true
}
func (r *Runtime) SetProvider(provider string) {
	r.branchMu.Lock()
	r.providerFilter = provider
	if provider != "" {
		// 切换 provider 时清除固定账号，让 P2C 在过滤集内选择。
		r.selectedAccountID = ""
	}
	r.branchMu.Unlock()
}
func (r *Runtime) accountSpecList() []model.AccountSpec {
	specs := make([]model.AccountSpec, 0, len(r.accountSpecs))
	for _, spec := range r.accountSpecs {
		specs = append(specs, spec)
	}
	return specs
}
