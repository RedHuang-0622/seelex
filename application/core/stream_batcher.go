package core

import (
	"runtime"
	"sync"
	"sync/atomic"

	"github.com/RedHuang-0622/seelex/seelexctx/lifecycle"
)

// ── 基建-C：流式批次渲染骨架（docs/2026-08-04-context-memory-lifecycle/plan.md §2.3）──
// StreamBatcher 把流式 onChunk 接入 BatchPipeline 的接缝：
//   - 批量落库：chunk 经管道聚合（累计 N 条/间隔 X ms）落库（冷存储面）；
//   - 节流渲染：chunk 聚合为渲染批次，每批次只调用一次 render（替代
//     逐 chunk 事件——渲染频率 O(chunks/batchSize)）；
//   - 背压：管道满返回 ErrPipelineFull → 聚合到当前渲染批次重投（不丢）。
//
// 生产接线（后续切片）：chat.go appendDelta 的 onChunk 改走 StreamBatcher，
// render 回调 = 前端增量事件（批量 Publish），落库 = RouterStorage。

// StreamBatcher 是流式输出管道的批次骨架（并发安全：Push 可多 goroutine）。
type StreamBatcher struct {
	pipe      *lifecycle.BatchPipeline[string]
	render    func(batch []string) // 每渲染批次调用一次（节流）
	batchSize int                  // 渲染批次目标大小（重置时保持）

	mu          sync.Mutex
	pending     []string // 当前渲染批次（背压重投聚合）
	renderCalls atomic.Int64 // 渲染调用次数（emit 跨 goroutine 原子）
	batches     int
}

// StreamBatcherOptions 是构造参数（0 字段 = 默认）。
type StreamBatcherOptions struct {
	// FlushSize 落库聚合条数（透传 BatchPipeline；默认 64）。
	FlushSize int
	// BatchSize 渲染批次大小（默认 32；render 调用频率 = chunks/BatchSize）。
	BatchSize int
	// Store 冷存储（nil = 内存 mock；生产注入 RouterStorage）。
	Store lifecycle.Storage[string]
}

// NewStreamBatcher 构造流式批次骨架（启动管道）。
func NewStreamBatcher(render func([]string), options StreamBatcherOptions) *StreamBatcher {
	batchSize := options.BatchSize
	if batchSize <= 0 {
		batchSize = 32
	}
	pipe := lifecycle.NewBatchPipeline[string](options.Store, lifecycle.PipelineOptions{
		FlushSize: options.FlushSize,
	})
	return &StreamBatcher{
		pipe:      pipe,
		render:    render,
		batchSize: batchSize,
		pending:   make([]string, 0, batchSize),
	}
}

// OnChunk 流式入口（chat.go onChunk 接缝）：chunk 聚合为渲染批次；
// 背压（管道满）时合并到当前批次重投（不丢数据）。
func (b *StreamBatcher) OnChunk(chunk string) {
	b.mu.Lock()
	b.pending = append(b.pending, chunk)
	if len(b.pending) < b.batchSize {
		b.mu.Unlock()
		return
	}
	batch := b.pending
	b.pending = make([]string, 0, b.batchSize)
	b.batches++
	b.mu.Unlock()
	b.emit(batch)
}

// emit 渲染批次 + 逐条落库（管道聚合）。
func (b *StreamBatcher) emit(batch []string) {
	if b.render != nil {
		b.renderCalls.Add(1)
		b.render(batch)
	}
	for _, chunk := range batch {
		for {
			err := b.pipe.Push(chunk)
			if err == nil {
				break
			}
			if err == lifecycle.ErrPipelineFull {
				// 背压：聚合重投（不丢；退避避免忙等）。
				runtime.Gosched()
				continue
			}
			return // 管道已关闭：放弃（流式收尾后不应再投递）
		}
	}
}

// Flush 强制落库剩余缓冲（流式结束收尾）。
func (b *StreamBatcher) Flush() error {
	b.mu.Lock()
	if len(b.pending) > 0 {
		batch := b.pending
		b.pending = nil
		b.batches++
		b.mu.Unlock()
		b.emit(batch)
	} else {
		b.mu.Unlock()
	}
	return b.pipe.Close()
}

// Stats 返回批次统计（审计：渲染调用次数/渲染批次/落库次数）。
type StreamBatcherStats struct {
	RenderCalls int
	Batches     int
	Pipe        lifecycle.PipelineStats
}

func (b *StreamBatcher) Stats() StreamBatcherStats {
	b.mu.Lock()
	defer b.mu.Unlock()
	return StreamBatcherStats{
		RenderCalls: int(b.renderCalls.Load()),
		Batches:     b.batches,
		Pipe:        b.pipe.Stats(),
	}
}
