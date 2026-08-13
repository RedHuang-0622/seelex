package seelebridge

import (
	"context"

	"github.com/RedHuang-0622/Seele/accountpool"
	"github.com/RedHuang-0622/Seele/agent"
	"github.com/RedHuang-0622/Seele/agent/bridge"
	"github.com/RedHuang-0622/Seele/types"

	"github.com/RedHuang-0622/seelex/seelebridge/account"
	"github.com/RedHuang-0622/seelex/seelebridge/internal/config"
	"github.com/RedHuang-0622/seelex/seelebridge/internal/model"
	"github.com/RedHuang-0622/seelex/seelebridge/internal/stream"
)

// 账号路由状态已收敛为 account.Manager（选中账号/provider 过滤/限额/选择器）。
// 本文件只保留 Runtime 侧委托与 completer 装配接线。

// accountSelector 是主链路共享的账号选择器闭包（委托 account.Manager.Selector；
// plan 分支绑定经闭包注入）。
func (r *Runtime) accountSelector(ctx context.Context, messages []types.Message, tools []types.Tool) accountpool.AcquireRequest {
	return r.accounts.Selector(account.SelectorDeps{
		BranchBinding: r.currentPlanBranchBinding,
	})(ctx, messages, tools)
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
		return nil, err
	}
	return completer, nil
}

// Accounts 返回账号池条目的路由摘要（按名称排序）。
func (r *Runtime) Accounts() []Account {
	return r.accounts.Accounts()
}

// Provider 返回当前 provider 过滤；未过滤时回退默认账号。
func (r *Runtime) Provider() string {
	return r.accounts.Provider()
}

// SelectAccount 切换主链路选中账号；未知账号返回 false。
func (r *Runtime) SelectAccount(name string) bool {
	return r.accounts.Select(name)
}

// SetProvider 切换 provider 过滤（非空时清除固定账号）。
func (r *Runtime) SetProvider(provider string) {
	r.accounts.SetProvider(provider)
}

// setSelectedAccount 设置选中账号（plan 分支冻结路径用）。
func (r *Runtime) setSelectedAccount(name string) {
	r.accounts.SetSelected(name)
}

// accountSpecList 返回账号规格列表（委托 account.Manager）。
func (r *Runtime) accountSpecList() []model.AccountSpec {
	return r.accounts.Specs()
}

// currentAccountLimits 返回当前账号限额（委托 account.Manager）。
func (r *Runtime) currentAccountLimits() config.AccountLimits {
	return r.accounts.Limits()
}
