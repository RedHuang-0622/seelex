// seelexCompressor 是 seelectx.Compressor 实现（plan.md §3.5）：
//
//  1. 短历史快速路径：低于阈值的历史直接返回（免 LLM）；
//  2. QuickChat 结构化 checkpoint：无工具、独立 history 的隔离调用
//     （RecursiveCompressor 语义），产物作为压缩摘要；
//  3. 跨会话承袭：有 ContextSnapshot 时经 compactor.Compactor 按 token
//     预算压缩快照（三级预算），供 Controller 的显式压缩路径使用。
package seelexctx

import (
	"context"
	"fmt"

	"github.com/RedHuang-0622/Seele/seelectx"
	"github.com/RedHuang-0622/Seele/types"

	"github.com/RedHuang-0622/seelex/seelexctx/compactor"
	"github.com/RedHuang-0622/seelex/seelexctx/snapshot"
)

// CompressorOptions 压缩器依赖。
type CompressorOptions struct {
	// QuickChat 无工具隔离压缩（短历史之外的结构化 checkpoint 路径）。
	QuickChat seelectx.QuickChat

	// Compactor 跨会话快照压缩（三级预算）；SnapshotFor 一起提供时启用。
	Compactor *compactor.Compactor

	// SnapshotFor 返回跨会话承袭快照（nil 表示非跨会话场景）。
	// 与 Compactor 组合使用；快照压缩失败时回退 QuickChat 路径。
	SnapshotFor func(ctx context.Context, request seelectx.CompressionRequest) *snapshot.ContextSnapshot

	// ShortThreshold 短对话免 LLM 阈值（条数；≤0 用 RecursiveCompressor
	// 默认 6）。
	ShortThreshold int

	// MinMessages / MinTokens / MaxDepth 透传 RecursiveCompressor（≤0 用默认）。
	MinMessages int
	MinTokens   int
	MaxDepth    int
}

// seelexCompressor 实现 seelectx.Compressor。
type seelexCompressor struct {
	options CompressorOptions
}

// NewCompressor 构造 seelex 压缩器。QuickChat 为 nil 时仅支持短历史快速
// 路径与快照路径；其余请求显式报错。
func NewCompressor(options CompressorOptions) seelectx.Compressor {
	return seelexCompressor{options: options}
}

// Compress 实现 seelectx.Compressor。
func (c seelexCompressor) Compress(ctx context.Context, request seelectx.CompressionRequest) (seelectx.CompressionResult, error) {
	// 1. 短历史快速路径：不送 LLM，原样返回（RecursiveCompressor 语义）。
	threshold := c.options.ShortThreshold
	if threshold <= 0 {
		threshold = defaultShortThreshold
	}
	if len(request.History) < threshold {
		return seelectx.CompressionResult{Messages: request.History}, nil
	}

	// 2. 跨会话承袭：快照按 token 预算压缩（compactor 三级预算）。
	if c.options.Compactor != nil && c.options.SnapshotFor != nil {
		if snap := c.options.SnapshotFor(ctx, request); snap != nil {
			budget := request.MaxTokens
			if budget <= 0 {
				budget = DefaultMaxTokens
			}
			compacted, err := c.options.Compactor.Compact(snap, budget)
			if err == nil {
				return seelectx.CompressionResult{Messages: snapshotMessages(compacted)}, nil
			}
			// 预算不足以保留最小安全快照 → 回退 QuickChat 结构化 checkpoint。
		}
	}

	// 3. QuickChat 结构化 checkpoint（无工具隔离调用）。
	if c.options.QuickChat == nil {
		return seelectx.CompressionResult{}, fmt.Errorf("seelexctx: compressor requires QuickChat for history length %d", len(request.History))
	}
	recursive := seelectx.RecursiveCompressor{
		Chat:        c.options.QuickChat,
		MinMessages: c.options.MinMessages,
		MinTokens:   c.options.MinTokens,
		MaxDepth:    c.options.MaxDepth,
	}
	return recursive.Compress(ctx, request)
}

// defaultShortThreshold 短对话免 LLM 的默认阈值（与 RecursiveCompressor
// 默认 MinMessages 一致）。
const defaultShortThreshold = 6

// snapshotMessages 把压缩后的快照渲染为单条 user 消息（压缩摘要块）。
func snapshotMessages(snap *snapshot.ContextSnapshot) []types.Message {
	content := "## 压缩摘要 (Compacted Context)\n" + snap.Format()
	return []types.Message{{Role: "user", Content: &content}}
}
