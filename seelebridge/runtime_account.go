package seelebridge

import (
	"context"

	"github.com/RedHuang-0622/Seele/accountpool"
	"github.com/RedHuang-0622/Seele/agent"
	"github.com/RedHuang-0622/Seele/agent/bridge"
	"github.com/RedHuang-0622/Seele/types"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
	"github.com/RedHuang-0622/seelex/seelebridge/account"
	"github.com/RedHuang-0622/seelex/seelebridge/internal/config"
	seetelemetry "github.com/RedHuang-0622/seelex/seelebridge/internal/telemetry"
	"github.com/RedHuang-0622/seelex/seelebridge/internal/model"
	"github.com/RedHuang-0622/seelex/seelebridge/internal/stream"
)

// 账号路由状态已收敛为 account.Manager（选中账号/provider 过滤/限额/选择器）。
// 本文件只保留 Runtime 侧委托与 completer 装配接线。

// routingNoticeChannelBuffer 是路由事实 channel 的缓冲：投递是非阻塞的
// （满则丢——提示是 best-effort，绝不反压流式请求），容量只需要吸收
// application 消费者的调度延迟。
const routingNoticeChannelBuffer = 16

// accountSelector 是主链路共享的账号选择器闭包（委托 account.Manager.Selector；
// plan 分支绑定经闭包注入）。
func (r *Runtime) accountSelector(ctx context.Context, messages []types.Message, tools []types.Tool) accountpool.AcquireRequest {
	return r.accounts.Selector(account.SelectorDeps{
		BranchBinding: r.currentPlanBranchBinding,
	})(ctx, messages, tools)
}

// assembleCompleters 构造同步与流式 completer，两者共享同一个账号选择器闭包。
// 流式 completer 另接路由事实观察者：换号回退与「未固定账号的池内选定」
// 会被投影成 B 类摘要事件（durable）+ CSP channel（application 消息提示）。
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
// 调用，必须非阻塞）：request 钩子补全思考强度后直接投 CSP channel（不落
// B 类摘要——它是每请求一条的瞬态元信息，落库就是噪音）；换号回退与「未固
// 定账号的池内选定」照旧写 B 类摘要（durable）+ CSP channel。
func (r *Runtime) observeRoutingNotice(notice stream.RoutingNotice) {
	if r == nil {
		return
	}
	if notice.Reason == stream.ReasonRequest {
		notice.ReasoningEffort = r.reasoningEffortForAccount(notice.To)
		r.publishRoutingNotice(r.routingNoticeDTO(notice))
		return
	}
	// unpinned_selection 每个会话+账号 只提示一次：主链路未固定账号时它每轮
	// 都会发生，重复提示是噪音；换号（failover）不去重——每一次都必须可见。
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
	message := r.routingNoticeMessage(notice)
	if r.summaryLog != nil {
		r.summaryLog.RecordSummary(seetelemetry.SummaryEvent{
			Kind:   "route",
			Name:   "reroute",
			Status: "completed",
			At:     notice.At,
			NodeID: notice.NodeID,
			Detail: message,
		})
	}
	r.publishRoutingNotice(dto.RoutingNotice{
		SessionID:   notice.SessionID,
		NodeID:      notice.NodeID,
		At:          notice.At,
		Reason:      notice.Reason,
		From:        notice.From,
		To:          notice.To,
		Model:       notice.Model,
		Provider:    notice.Provider,
		FailureKind: notice.FailureKind,
	})
}

// routingNoticeMessage 把内部路由事实投影成 dto 并取共用文案
// （dto.RoutingNotice.Message——会话消息与摘要 Detail 同源）。
func (r *Runtime) routingNoticeMessage(notice stream.RoutingNotice) string {
	return r.routingNoticeDTO(notice).Message()
}

// reasoningEffortForAccount 查账号配置的思考强度（config 解析后的账号级值，
// 可能是 "session"——由 application 消费端解析成当前 wire 值）。未知账号返回空。
func (r *Runtime) reasoningEffortForAccount(name string) string {
	if r == nil || name == "" {
		return ""
	}
	for _, spec := range r.accountSpecList() {
		if spec.Name == name {
			return spec.ReasoningEffort
		}
	}
	return ""
}

// routingNoticeDTO 把内部路由事实投影成跨层 DTO。
func (r *Runtime) routingNoticeDTO(notice stream.RoutingNotice) dto.RoutingNotice {
	return dto.RoutingNotice{
		SessionID:       notice.SessionID,
		NodeID:          notice.NodeID,
		At:              notice.At,
		Reason:          notice.Reason,
		Role:            notice.Role,
		From:            notice.From,
		To:              notice.To,
		Model:           notice.Model,
		Provider:        notice.Provider,
		ReasoningEffort: notice.ReasoningEffort,
		FailureKind:     notice.FailureKind,
	}
}

// publishRoutingNotice 非阻塞投递路由事实（CSP；channel 未装配或满则丢，
// best-effort 不反压请求路径）。
func (r *Runtime) publishRoutingNotice(notice dto.RoutingNotice) {
	if r == nil || r.routingNotices == nil {
		return
	}
	select {
	case r.routingNotices <- notice:
	default:
	}
}

// RoutingNoticeChannel 返回账号路由事实 channel（CSP：application 生命周期
// 消费者串行处理；runtime 侧非阻塞投递，满则丢）。未装配时返回 nil。
func (r *Runtime) RoutingNoticeChannel() <-chan dto.RoutingNotice {
	if r == nil || r.routingNotices == nil {
		return nil
	}
	return r.routingNotices
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

// SetSessionReasoningEffort 实现 application/contract.ReasoningEffortPort（窄可选
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
