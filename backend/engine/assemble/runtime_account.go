package assemble

import (
	"context"
	"fmt"
	"strings"

	"github.com/RedHuang-0622/Seele/accountpool"
	"github.com/RedHuang-0622/Seele/agent"
	"github.com/RedHuang-0622/Seele/agent/bridge"
	"github.com/RedHuang-0622/Seele/types"

	"github.com/RedHuang-0622/seelex/backend/engine/account"
	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/internal/config"
	"github.com/RedHuang-0622/seelex/internal/model"
	"github.com/RedHuang-0622/seelex/internal/stream"
)

// 账号路由状态已收敛为 account.Manager（选中账号/provider 过滤/限额/选择器）。
// 本文件只保留 Runtime 侧委托与 completer 装配接线。

// accountSelector 是主链路共享的账号选择器闭包（委托 account.Manager.Selector）。
func (r *Runtime) accountSelector(ctx context.Context, messages []types.Message, tools []types.Tool) accountpool.AcquireRequest {
	return r.accounts.Selector()(ctx, messages, tools)
}

// assembleCompleters 构造同步与流式 completer，两者共享同一个账号选择器闭包。
// 流式 completer 另接路由事实观察者：request/failover 事实覆盖 per-session
// ServedRoute 注册表（application 回合收尾取走、回填真相源）；换号回退与
// 「未固定账号的池内选定」另落 B 类摘要事件（durable，审计用）。
func (r *Runtime) assembleCompleters() error {
	completer, err := r.bridgeAccountCompleter()
	if err != nil {
		return err
	}
	r.completer = completer
	r.streamer = stream.NewStreamingCompleterWithObserver(r.pool, r.accountSelector, r.observeRoutingNotice)
	return nil
}

// observeRoutingNotice 是流式 completer 的路由事实观察者（租约路径上同步调用，必须非阻塞）。
//
// 落点沿革（P21-3「域方法随类型走」）：注册表状态、三条判据（哪些原因覆盖、只有主链路才记、
// unpinned 摘要去重）与摘要文案都搬进账号域
// [`backend/engine/account/served_route.go`](../account/served_route.go) 的 `ServedRouteRegistry`；
// 这里只留一行转发（`Runtime.servedRoute` 是装配点注入的句柄）。
func (r *Runtime) observeRoutingNotice(notice stream.RoutingNotice) {
	if r == nil || r.servedRoute == nil {
		return
	}
	r.servedRoute.Observe(notice)
}

// TakeServedRoute 取出（并清掉）该会话最近一次请求的实际承接路由（读即清；见域包判据）。
func (r *Runtime) TakeServedRoute(sessionID string) (dto.ServedRoute, bool) {
	if r == nil || r.servedRoute == nil {
		return dto.ServedRoute{}, false
	}
	return r.servedRoute.Take(sessionID)
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

// SetRoleAccount 把角色默认账号推入运行时账号路由；刷新后由角色回合按
// SceneScope.Role 选取。账号存在性与角色归属由账号管理器校验。
func (r *Runtime) SetRoleAccount(role, account string) error {
	if r == nil || r.accounts == nil {
		return fmt.Errorf("seelex: runtime accounts are not assembled")
	}
	role = strings.TrimSpace(strings.ToLower(role))
	valid := false
	for _, candidate := range dto.AccountRoleVocabulary() {
		if role == candidate {
			valid = true
			break
		}
	}
	if !valid {
		return fmt.Errorf("seelex: role %q is not an account role", role)
	}
	return r.accounts.SetRoleAccount(model.AccountRole(role), account)
}

// RegisterAccount 把一个新账号注册进运行时（界面「手动添加账号」的落点）：
// 先注册进 P2C 池（新账号真正能承流），再登记进账号管理器（路由、限额与窗口读数）。
//
// 顺序不能反：池注册失败时管理器不该先认下这条账号——否则账号列表里会出现一条
// 永远承不了流的假条目。反向的残留（池里有、管理器没有）不会出现，因为管理器这步
// 是纯内存登记，只会在重名时失败（重名在池那步就已经报错了）。
func (r *Runtime) RegisterAccount(spec model.AccountSpec, limits config.AccountLimits) error {
	if r == nil {
		return fmt.Errorf("seelex: runtime is unavailable")
	}
	if err := account.RegisterAccounts(r.pool, []model.AccountSpec{spec}); err != nil {
		return err
	}
	return r.accounts.Add(spec, limits)
}

// AccountBalance 查询指定账号此刻的余额。
//
// 认账号名不认索引：名字是界面唯一持有的身份（`agent-1`），未知名字报错而不是
// 回退到"当前选中账号"——那会让用户看到另一个账号的钱。
func (r *Runtime) AccountBalance(ctx context.Context, name string) (account.Balance, error) {
	if r == nil {
		return account.Balance{}, fmt.Errorf("seelex: runtime is unavailable")
	}
	for _, spec := range r.accounts.Specs() {
		if spec.Name == name {
			return account.QueryBalance(ctx, spec)
		}
	}
	return account.Balance{}, fmt.Errorf("seelex: unknown account %q", name)
}

// Provider 返回当前 provider 过滤；未过滤时回退默认账号。
func (r *Runtime) Provider() string {
	return r.accounts.Provider()
}

// SelectAccount 切换主链路选中账号；未知账号返回 false。
func (r *Runtime) SelectAccount(name string) bool {
	return r.accounts.Select(name)
}

// SetSessionReasoningEffort 实现 contract.ReasoningEffortPort（窄可选
// 能力面，结构满足、不引 application 依赖）：把档位对应的 wire 思考强度下发给
// **配置为跟随会话**的账号，返回被改动的账号数；写死强度的账号不动。
//
// 为什么委托而不是自己遍历：判定（哪些账号算"跟随会话"）与唯一的类型断言点
// （池里存的是 agent.Completer，只有 Seele 的 *api.ChatClient 支持改强度）都收在
// account.SetSessionReasoningEffort 一处，本方法只是 Runtime 侧的接线。
func (r *Runtime) SetSessionReasoningEffort(effort string) int {
	if r == nil {
		return 0
	}
	return account.SetSessionReasoningEffort(r.pool, r.accountSpecList(), effort)
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

// ── 每账号上下文窗口（界面设置面）────────────────────────────────────
//
// 落点沿革（P21-8「域方法随类型走」）：覆盖层的活体状态（互斥量 + 表）与三条判据
// （入口过滤、改一格/清一格、以及"覆盖 > limits.context_window > 账号档自报"的优先级）
// 都搬进账号域 [`backend/engine/account/window_overlay.go`](../account/window_overlay.go)
// 的 `WindowOverlay`；这里只剩四个薄转发（`r.accountWindows` 是装配点注入的句柄）。
// 落盘（数据根 `runtime-limits.json`）在宿主侧 [`backend/limit/overlay`](../../limit/overlay/README.md)，
// 界面侧的校验在 [`backend/limit/accountwindow`](../../limit/accountwindow/README.md)，三者不各判一次。

// SetAccountWindow 改一个账号的窗口覆盖（window <= 0 = 清掉这一格，回落到全局值）。
func (r *Runtime) SetAccountWindow(name string, window int) {
	if r == nil {
		return
	}
	r.accountWindows.Set(name, window)
}

// AccountWindows 返回一份拷贝（诊断与落盘对账用；调用方改不动运行时的表）。
func (r *Runtime) AccountWindows() map[string]int {
	if r == nil {
		return nil
	}
	return r.accountWindows.Snapshot()
}

// ContextWindowFor 返回指定账号**生效**的上下文窗口（优先级判据在域包）。
func (r *Runtime) ContextWindowFor(name string) int {
	if r == nil {
		return 0
	}
	return r.accountWindows.Effective(name, account.WindowDeps{
		Global:          r.limits.ContextWindow,
		AccountFallback: func(accountName string) int { return r.accounts.LimitsFor(accountName).ContextWindow },
	})
}
