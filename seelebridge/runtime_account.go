package seelebridge

import (
	"context"
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
	seetelemetry "github.com/RedHuang-0622/seelex/internal/telemetry"
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

// observeRoutingNotice 是流式 completer 的路由事实观察者（租约路径上同步
// 调用，必须非阻塞）：request 钩子与 failover 都覆盖该会话的 ServedRoute
// 注册表（最终承接账号覆盖前面的尝试）；failover / unpinned_selection 另落
// B 类摘要（durable）。不再投会话 system 消息——会话内通知已退役
// （2026-10-10），路由元信息随 response 长在 assistant 消息上。
func (r *Runtime) observeRoutingNotice(notice stream.RoutingNotice) {
	if r == nil {
		return
	}
	if notice.Reason == stream.ReasonRequest || notice.Reason == stream.ReasonFailover {
		r.recordServedRoute(notice)
	}
	// unpinned_selection 每个会话+账号 只记一次摘要：主链路未固定账号时它每轮
	// 都会发生，重复记录是噪音；换号（failover）不去重——每一次都必须可审计。
	if notice.Reason == stream.ReasonUnpinnedSelection {
		key := notice.SessionID + "|" + notice.To
		r.routingNoticeMu.Lock()
		seen := r.routingNoticeSeen[key]
		r.routingNoticeSeen[key] = true
		r.routingNoticeMu.Unlock()
		if seen {
			return
		}
	}
	if notice.Reason != stream.ReasonFailover && notice.Reason != stream.ReasonUnpinnedSelection {
		return
	}
	if r.summaryLog != nil {
		r.summaryLog.RecordSummary(seetelemetry.SummaryEvent{
			Kind:   "route",
			Name:   "reroute",
			Status: "completed",
			At:     notice.At,
			NodeID: notice.NodeID,
			Detail: r.routingNoticeMessage(notice),
		})
	}
}

// recordServedRoute 把一次路由事实覆盖进该会话的 ServedRoute 注册表。只有
// 主链路请求（NodeID 空、会话可解析）才记录：子代理/plan 节点的路由归各自
// 的节点视图，进程级请求（无会话归属）无处回填。
func (r *Runtime) recordServedRoute(notice stream.RoutingNotice) {
	if notice.SessionID == "" || notice.NodeID != "" {
		return
	}
	r.routingNoticeMu.Lock()
	r.servedRoutes[notice.SessionID] = dto.ServedRoute{
		Role:     notice.Role,
		Account:  notice.To,
		Provider: notice.Provider,
		Model:    notice.Model,
		At:       notice.At,
	}
	r.routingNoticeMu.Unlock()
}

// TakeServedRoute 取出（并清掉）该会话最近一次请求的实际承接路由。读即清：
// 事实只服务「本轮 response 回填真相源」一次，残留会让下一轮在没有请求真正
// 派发时误用旧路由。无事实返回 ok=false。
func (r *Runtime) TakeServedRoute(sessionID string) (dto.ServedRoute, bool) {
	if r == nil || sessionID == "" {
		return dto.ServedRoute{}, false
	}
	r.routingNoticeMu.Lock()
	route, ok := r.servedRoutes[sessionID]
	delete(r.servedRoutes, sessionID)
	r.routingNoticeMu.Unlock()
	return route, ok
}

// routingNoticeMessage 把内部路由事实翻成 B 类摘要的一句可读说明
// （dto.RoutingNotice.Message 同源文案；只带非敏感路由属性）。
func (r *Runtime) routingNoticeMessage(notice stream.RoutingNotice) string {
	return dto.RoutingNotice{
		SessionID:   notice.SessionID,
		NodeID:      notice.NodeID,
		At:          notice.At,
		Reason:      notice.Reason,
		Role:        notice.Role,
		From:        notice.From,
		To:          notice.To,
		Model:       notice.Model,
		Provider:    notice.Provider,
		FailureKind: notice.FailureKind,
	}.Message()
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
// 三个来源各有自己的家，这里只做取值的那一跳：
//
//	账号级覆盖（界面设过；数据根 runtime-limits.json）
//	  > limits.context_window（config/seelex.yaml 的全局值）
//	    > 账号档自报的 context_window（遗留兼容键）
//	      > 代码兜底 DefaultContextWindow
//
// 为什么要账号级：窗口是压缩预算的基数，而"主账号 1M、便宜的子代理账号 200k"
// 是常态——一刀切会让子代理账号被按大窗口算预算。覆盖值只影响判据用的窗口，
// provider 侧仍有自己的硬限制（填大了请求会被拒）。

// copyAccountWindows 复制一份账号窗口表（装配期一次性调用；空表返回 nil）。
func copyAccountWindows(source map[string]int) map[string]int {
	if len(source) == 0 {
		return nil
	}
	copied := make(map[string]int, len(source))
	for name, window := range source {
		if strings.TrimSpace(name) == "" || window <= 0 {
			continue
		}
		copied[name] = window
	}
	if len(copied) == 0 {
		return nil
	}
	return copied
}

// SetAccountWindow 改一个账号的窗口覆盖（window <= 0 = 清掉这一格，回落到全局值）。
// 改后**立刻生效**：下一次装配/判据就按新值算，不需要重启——界面上的挡位因此是
// "按下去就作数"，与 SessionReleaseWindow 同一条口径。
func (r *Runtime) SetAccountWindow(name string, window int) {
	if r == nil {
		return
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}
	r.accountWindowsMu.Lock()
	defer r.accountWindowsMu.Unlock()
	if window > 0 {
		if r.accountWindows == nil {
			r.accountWindows = map[string]int{}
		}
		r.accountWindows[name] = window
		return
	}
	delete(r.accountWindows, name)
}

// AccountWindows 返回一份拷贝（诊断与落盘对账用；调用方改不动运行时的表）。
func (r *Runtime) AccountWindows() map[string]int {
	if r == nil {
		return nil
	}
	r.accountWindowsMu.RLock()
	defer r.accountWindowsMu.RUnlock()
	return copyAccountWindows(r.accountWindows)
}

// accountWindow 读该账号的界面覆盖（没有 → 0）。
func (r *Runtime) accountWindow(name string) int {
	if r == nil || strings.TrimSpace(name) == "" {
		return 0
	}
	r.accountWindowsMu.RLock()
	defer r.accountWindowsMu.RUnlock()
	return r.accountWindows[name]
}

// ContextWindowFor 返回指定账号**生效**的上下文窗口（优先级见本文件顶部注释）。
func (r *Runtime) ContextWindowFor(name string) int {
	if r == nil {
		return 0
	}
	if window := r.accountWindow(name); window > 0 {
		return window
	}
	if r.limits.ContextWindow > 0 {
		return r.limits.ContextWindow
	}
	return r.accounts.LimitsFor(name).ContextWindow
}
