// Package stream 承载流式账号 Completer 适配器：把账号池的同步 Completer
// 适配为流式 agent.StreamCompleter。属于根 facade 的装配细节（仅
// runtime.go 装配使用），置于 internal/。
package stream

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/RedHuang-0622/Seele/accountpool"
	"github.com/RedHuang-0622/Seele/agent"
	"github.com/RedHuang-0622/Seele/agent/bridge"
	"github.com/RedHuang-0622/Seele/types"

	"github.com/RedHuang-0622/seelex/seelebridge/account"
)

// streamFailoverAttempts 是一次流式请求最多换几个账号。
//
// 上限不是性能考量，而是「真的都没额度时别拿同一个错误把每个账号逐个撞一遍」：
// 每次尝试都要真发一次请求（额度耗尽通常在上游立即被拒，但仍然是一次往返），
// 而耗尽场景下第 3 次与第 30 次的结果没有区别。
const streamFailoverAttempts = 3

// streamingAccountCompleter 把账号池的同步 Completer 适配为流式
// agent.StreamCompleter。与 bridge.AccountCompleter 的每次调用一次租约不同，
// 流式路径的租约必须覆盖整条流直到 EOF/错误/Close 才释放（plan.md §3.6），
// 否则并发账号会在流中途被其他请求抢占，造成并发超售。
//
// 换号回退（2026-10-01）：「第一志愿没额度 → 第二志愿」在**这里**落地，不在会话循环。
// 账号池不做重试（Seele accountpool/README.md），而额度/鉴权失败是**账号资格**问题：
// 同一个账号重试多少次都是同一个 402。因此本适配器在租约已释放之后按
// `account.ClassifyFailure` 的语义决定是否换号重试——网络抖动/上游故障原样上抛
// （换号不会改变结果），额度/凭据失败则排除该账号再取一个。
type streamingAccountCompleter struct {
	pool     *accountpool.P2CPool[agent.Completer]
	selector bridge.AccountRequestSelector
}

// NewStreamingCompleter 构造流式 Completer（pool 与可选账号选择器）。
func NewStreamingCompleter(pool *accountpool.P2CPool[agent.Completer], selector bridge.AccountRequestSelector) agent.StreamCompleter {
	return &streamingAccountCompleter{pool: pool, selector: selector}
}

// CompleteStream 取租约 → 流式调用 → 释放租约；账号资格失败时换号重试（有界）。
func (c *streamingAccountCompleter) CompleteStream(
	ctx context.Context,
	messages []types.Message,
	tools []types.Tool,
	onChunk func(string),
) (content string, reasoningContent string, toolCalls []types.ToolCall, err error) {
	if c == nil || c.pool == nil {
		return "", "", nil, fmt.Errorf("seelebridge: streaming account pool is unavailable")
	}
	base := accountpool.AcquireRequest{}
	if c.selector != nil {
		base = c.selector(ctx, messages, tools)
	}

	// excluded 是「这一轮已经试过且被账号资格判据拒掉」的账号：用请求级 predicate
	// 排除，而不是改池状态（Disable 会把换号这件事变成一次全局副作用，且没人负责
	// 重新启用）。
	excluded := map[string]bool{}
	var lastFailure error
	for attempt := 0; attempt < streamFailoverAttempts; attempt++ {
		request := base
		if len(excluded) > 0 {
			request = withExcludedAccounts(base, excluded)
		}
		lease, resolveErr := c.pool.Resolve(ctx, request)
		if resolveErr != nil {
			if lastFailure != nil {
				// 已经换过号：把最后一次的真实失败交回（它自带账号与上游措辞），
				// 而不是用「取不到账号」覆盖掉最初的原因。
				return "", "", nil, lastFailure
			}
			return "", "", nil, fmt.Errorf("seelebridge: acquire streaming client: %w", withCancelCause(ctx, resolveErr))
		}
		accountID := lease.AccountID()
		content, reasoningContent, toolCalls, err = c.streamWithLease(ctx, lease, messages, tools, onChunk)
		// 租约覆盖整条流：EOF / 错误 / 提前 Close 任一退出路径都恰好释放一次
		// （accountpool.Lease.Release 幂等）。
		if releaseErr := lease.Release(); releaseErr != nil && err == nil {
			err = fmt.Errorf("seelebridge: release streaming client %q: %w", accountID, releaseErr)
		}
		if err == nil {
			return content, reasoningContent, toolCalls, nil
		}
		kind := account.ClassifyFailure(err)
		if kind == account.FailureNone {
			// 不是账号资格问题：原样上抛，换号不会改变结果。
			return "", "", nil, err
		}
		excluded[accountID] = true
		lastFailure = err
		log.Printf("seelebridge: 账号 %q %s，改用同族下一个账号重试（第 %d 次）",
			accountID, account.FailureAdvice(kind), len(excluded))
	}
	return "", "", nil, fmt.Errorf("%w（已排除 %d 个账号后仍无可用账号）", lastFailure, len(excluded))
}

// streamWithLease 在一次租约内完成流式调用（含「账号只实现同步 Complete」的退化路径）。
func (c *streamingAccountCompleter) streamWithLease(
	ctx context.Context,
	lease accountpool.ClientLease[agent.Completer],
	messages []types.Message,
	tools []types.Tool,
	onChunk func(string),
) (string, string, []types.ToolCall, error) {
	client := lease.Client()
	streamer, ok := client.(agent.StreamCompleter)
	if !ok {
		// 账号只实现了同步 Completer：退化为单次非流式返回（agent 侧同等回退）。
		message, completeErr := client.Complete(ctx, messages, tools)
		if completeErr != nil {
			return "", "", nil, fmt.Errorf("seelebridge: complete with account %q: %w", lease.AccountID(), completeErr)
		}
		text := ""
		if message.Content != nil {
			text = *message.Content
			if onChunk != nil && text != "" {
				onChunk(text)
			}
		}
		return text, message.ReasoningContent, message.ToolCalls, nil
	}
	content, reasoningContent, toolCalls, err := streamer.CompleteStream(ctx, messages, tools, onChunk)
	if err != nil {
		return "", "", nil, fmt.Errorf("seelebridge: stream with account %q: %w", lease.AccountID(), withCancelCause(ctx, err))
	}
	return content, reasoningContent, toolCalls, nil
}

// withExcludedAccounts 在请求上叠加「排除这些账号」的判据。
//
// 两条口径：
//   - 显式 pin（AccountID）被拒时**取消 pin**：pin 的含义是「优先用它」而不是
//     「只准用它」——第一志愿没额度时，正确的行为是走第二志愿，而不是把整轮判死。
//   - 其余账号用 predicate 排除，保留调用方原有的 predicate 与 Metadata 过滤。
func withExcludedAccounts(base accountpool.AcquireRequest, excluded map[string]bool) accountpool.AcquireRequest {
	request := base
	if excluded[request.AccountID] {
		request.AccountID = ""
	}
	previous := base.Predicate
	request.Predicate = func(snapshot accountpool.AccountSnapshot) bool {
		if excluded[snapshot.ID] {
			return false
		}
		return previous == nil || previous(snapshot)
	}
	return request
}

// withCancelCause 在 ctx 因上游 cancel(cause) 中止时，把真实原因附加到错误文本。
//
// 为什么必须显式取 context.Cause：accountpool.Acquire 只用 ctx.Err() 包装
// （pool.go 的 ctx.Err() 短路与 select <-ctx.Done() 两处），而 WithCancelCause
// 派生出的子 ctx 的 Err() 恒为 context.Canceled、真实原因只存在于 Cause——
// 兄弟节点因此报出没有来由的 "accountpool: acquire: context canceled"
// （2026-09-29 事故：fail-fast 连坐把「谁失败了、为什么失败」整个丢掉）。
//
// 只在确有独立 cause 时附加：plain cancel / deadline 的 Cause 就是 ctx.Err()
// （无额外信息），错误文本里已经含原因时也不重复附加。
func withCancelCause(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	cause := context.Cause(ctx)
	if cause == nil || errors.Is(cause, ctx.Err()) {
		return err
	}
	if cause.Error() == err.Error() || strings.Contains(err.Error(), cause.Error()) {
		return err
	}
	return fmt.Errorf("%w (canceled by: %v)", err, cause)
}

var _ agent.StreamCompleter = (*streamingAccountCompleter)(nil)
