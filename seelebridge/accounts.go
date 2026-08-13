package seelebridge

import (
	"context"
	"fmt"

	"github.com/RedHuang-0622/Seele/accountpool"
	"github.com/RedHuang-0622/Seele/agent"
	"github.com/RedHuang-0622/Seele/agent/bridge"
	"github.com/RedHuang-0622/Seele/types"
	"github.com/RedHuang-0622/seelex/seelebridge/account"
	"github.com/RedHuang-0622/seelex/seelebridge/internal/model"
	seenode "github.com/RedHuang-0622/seelex/seelebridge/node"
)

// 账号装配与选择域已迁入 seelebridge/account（ClientFor/RegisterAccounts/
// ResolveForBranch/ForRole/ByName/StableIndex）。本文件只保留 Runtime 侧的
// 选择器接线（读 Runtime 当前 provider 过滤与选中账号）。

// accountSelector 是主链路共享的账号选择器闭包：读取 Runtime 当前的
// provider 过滤与选中账号（不持有任何 api.ChatClient 引用），把请求条件
// 转换为 accountpool.AcquireRequest。P2C 池负责实际选择与租赁。
// Plan 子代理请求（ctx 含 NodeScope）优先走节点账号解析：显式 pin 优先，
// 否则按角色 + branchID 走确定性 hash（ResolveForBranch 逻辑）。
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

// nodeAccountRequest 按节点作用域解析账号租赁请求：binding 显式 AccountID
// 直接 pin；否则按 role + planID:branchID 确定性 hash 选择（不占用主链路
// 选中账号，与 ResolveForBranch 语义一致）。
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
