package seelebridge

import (
	"context"
	"fmt"

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
		return "", "", nil, fmt.Errorf("seelebridge: acquire streaming client: %w", err)
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
		return "", "", nil, fmt.Errorf("seelebridge: stream with account %q: %w", lease.AccountID(), err)
	}
	return content, reasoningContent, toolCalls, nil
}

var _ agent.StreamCompleter = (*streamingAccountCompleter)(nil)
