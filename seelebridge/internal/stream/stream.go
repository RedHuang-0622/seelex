// Package stream 承载流式账号 Completer 适配器：把账号池的同步 Completer
// 适配为流式 agent.StreamCompleter。属于根 facade 的装配细节（仅
// runtime.go 装配使用），置于 internal/。
package stream

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/RedHuang-0622/Seele/accountpool"
	"github.com/RedHuang-0622/Seele/agent"
	"github.com/RedHuang-0622/Seele/agent/bridge"
	"github.com/RedHuang-0622/Seele/types"
)

// streamingAccountCompleter 把账号池的同步 Completer 适配为流式
// agent.StreamCompleter。与 bridge.AccountCompleter 的每次调用一次租约不同，
// 流式路径的租约必须覆盖整条流直到 EOF/错误/Close 才释放（plan.md §3.6），
// 否则并发账号会在流中途被其他请求抢占，造成并发超售。
type streamingAccountCompleter struct {
	pool     *accountpool.P2CPool[agent.Completer]
	selector bridge.AccountRequestSelector
}

// NewStreamingCompleter 构造流式 Completer（pool 与可选账号选择器）。
func NewStreamingCompleter(pool *accountpool.P2CPool[agent.Completer], selector bridge.AccountRequestSelector) agent.StreamCompleter {
	return &streamingAccountCompleter{pool: pool, selector: selector}
}

// CompleteStream 获取租约 → 委托流式调用 → defer Release（幂等，覆盖整个流生命周期）。
func (c *streamingAccountCompleter) CompleteStream(
	ctx context.Context,
	messages []types.Message,
	tools []types.Tool,
	onChunk func(string),
) (content string, reasoningContent string, toolCalls []types.ToolCall, err error) {
	if c == nil || c.pool == nil {
		return "", "", nil, fmt.Errorf("seelebridge: streaming account pool is unavailable")
	}
	request := accountpool.AcquireRequest{}
	if c.selector != nil {
		request = c.selector(ctx, messages, tools)
	}
	lease, err := c.pool.Resolve(ctx, request)
	if err != nil {
		return "", "", nil, fmt.Errorf("seelebridge: acquire streaming client: %w", withCancelCause(ctx, err))
	}
	// defer 保证 EOF / 错误 / ctx 取消 / 提前 Close 任一退出路径都恰好释放一次。
	// accountpool.Lease.Release 本身幂等（sync.Once），重复调用无害。
	defer func() {
		if releaseErr := lease.Release(); releaseErr != nil && err == nil {
			err = fmt.Errorf("seelebridge: release streaming client %q: %w", lease.AccountID(), releaseErr)
		}
	}()

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
	content, reasoningContent, toolCalls, err = streamer.CompleteStream(ctx, messages, tools, onChunk)
	if err != nil {
		return "", "", nil, fmt.Errorf("seelebridge: stream with account %q: %w", lease.AccountID(), withCancelCause(ctx, err))
	}
	return content, reasoningContent, toolCalls, nil
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
