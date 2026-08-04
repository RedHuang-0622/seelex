package lifecycle

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

func fmtError(message string) error { return errors.New(message) }

// BatchPipeline 是流式输出管道的批量落库面（docs/2026-08-04-
// context-memory-lifecycle/plan.md §2.3）：onChunk 经 Push 进入有界缓冲，
// 按「累计 N 条 或 间隔 X ms」聚合 flush 到 Storage——流式接收的内存
// 有界（缓冲上限 = 聚合窗口），落库次数 O(chunks/batchSize)。
//
// Actor 语义（2026-08-04 重构，修复数据竞争）：Push 经有界 channel 投递，
// **单一 flush goroutine 持有全部缓冲状态**（无锁闭包状态）——业务代码零
// mutex，多 goroutine 并发 Push 安全（channel 串行化）。
//
// 背压语义：缓冲满时 Push 不阻塞（返回 ErrPipelineFull 计数），数据由
// 调用方决定重试/聚合——流式上下文不可丢失，调用方应聚合重试。
type BatchPipeline[T any] struct {
	store Storage[T]
	items chan T // 有界投递通道（缓冲上限 = 聚合窗口）
	done  chan struct{}
	flush chan chan error

	flushSize int
	interval  time.Duration
	closeOnce sync.Once
	gate      sync.RWMutex // Push 与 close(items) 的生命周期门
	closed    bool
	closeErr  error

	// 统计（原子，跨 goroutine 审计）。
	flushes   atomic.Int64 // 落库次数
	chunks    atomic.Int64 // 接收 chunk 总数
	committed atomic.Int64 // 本管道成功落库的 chunk 总数
	syncFlush atomic.Int64 // 背压同步 flush 次数
}

// PipelineOptions 是管道构造参数（0 字段 = 默认）。
type PipelineOptions struct {
	// FlushSize 累计条数触发 flush（默认 64）。
	FlushSize int
	// Interval flush 时间间隔（默认 100ms）。
	Interval time.Duration
	// BufferSize 投递通道容量（默认 FlushSize×2）。
	BufferSize int
}

// NewBatchPipeline 构造批量落库管道：启动 flush goroutine（唯一状态持有者）。
// store 为 nil 时使用内存存储（mock）。
func NewBatchPipeline[T any](store Storage[T], options PipelineOptions) *BatchPipeline[T] {
	flushSize := options.FlushSize
	if flushSize <= 0 {
		flushSize = 64
	}
	interval := options.Interval
	if interval <= 0 {
		interval = 100 * time.Millisecond
	}
	bufferSize := options.BufferSize
	if bufferSize <= 0 {
		bufferSize = flushSize * 2
	}
	if store == nil {
		store = newMemoryStorage[T]()
	}
	pipe := &BatchPipeline[T]{
		store:     store,
		items:     make(chan T, bufferSize),
		done:      make(chan struct{}),
		flush:     make(chan chan error),
		flushSize: flushSize,
		interval:  interval,
	}
	go pipe.run()
	return pipe
}

// run 是唯一状态持有者：消费 items 通道累计缓冲，间隔/阈值触发批量落库。
func (p *BatchPipeline[T]) run() {
	defer close(p.done)
	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()
	buffer := make([]T, 0, p.flushSize)
	for {
		select {
		case <-ticker.C:
			_ = p.flushBuffer(&buffer)
		case reply := <-p.flush:
			reply <- p.flushBuffer(&buffer)
		case item, ok := <-p.items:
			if !ok {
				p.closeErr = p.flushBuffer(&buffer)
				return
			}
			buffer = append(buffer, item)
			if len(buffer) >= p.flushSize {
				_ = p.flushBuffer(&buffer)
			}
		}
	}
}

// flushBuffer 批量落库（run goroutine 内；buffer 状态私有，无锁）。
// Flush persists every item accepted before the call returns while keeping
// the pipeline open for subsequent chunks.
func (p *BatchPipeline[T]) Flush() error {
	p.gate.RLock()
	defer p.gate.RUnlock()
	if p.closed {
		return ErrPipelineClosed
	}
	target := p.chunks.Load()
	for {
		reply := make(chan error, 1)
		p.flush <- reply
		if err := <-reply; err != nil {
			return err
		}
		if p.committed.Load() >= target {
			return nil
		}
		runtime.Gosched()
	}
}

func (p *BatchPipeline[T]) flushBuffer(buffer *[]T) error {
	if len(*buffer) == 0 {
		return nil
	}
	batch := *buffer
	*buffer = make([]T, 0, p.flushSize)
	if err := p.store.Append(context.Background(), batch); err != nil {
		// 落库失败：数据回灌缓冲（不丢）——下次 flush 重试。
		*buffer = append(batch, *buffer...)
		return err
	}
	p.flushes.Add(1)
	p.committed.Add(int64(len(batch)))
	return nil
}

// Push 写入一个 chunk（流式 onChunk 调用面；并发安全——channel 投递）。
// 缓冲满时返回 ErrPipelineFull（调用方聚合重试，不阻塞流）。
func (p *BatchPipeline[T]) Push(item T) error {
	p.gate.RLock()
	defer p.gate.RUnlock()
	if p.closed {
		return ErrPipelineClosed
	}
	select {
	case p.items <- item:
		p.chunks.Add(1) // 投递成功才计数（背压重试的失败调用不计）
		return nil
	default:
		p.syncFlush.Add(1)
		return ErrPipelineFull
	}
}

// Close 停止管道并 flush 剩余缓冲（流式结束收尾；阻塞等待确认；幂等）。
// 关闭协议（修复零丢失）：① 先关 closeCh（禁止新 Push）② flush 请求
// （run 排空已投递 items + flush，确认后返回）③ 等 run 退出。
func (p *BatchPipeline[T]) Close() error {
	p.closeOnce.Do(func() {
		p.gate.Lock()
		p.closed = true
		close(p.items)
		p.gate.Unlock()
		<-p.done
	})
	return p.closeErr
}

// ErrPipelineFull 是背压信号：缓冲已满，chunk 未被接收（调用方聚合重试）。
var ErrPipelineFull = fmtError("lifecycle: pipeline buffer full")

// ErrPipelineClosed 是管道关闭后的投递错误。
var ErrPipelineClosed = fmtError("lifecycle: pipeline closed")

// Stats 返回管道统计（审计：落库次数/接收 chunk/背压次数）。
type PipelineStats struct {
	Flushes   int64
	Chunks    int64
	SyncFlush int64
}

func (p *BatchPipeline[T]) Stats() PipelineStats {
	return PipelineStats{
		Flushes:   p.flushes.Load(),
		Chunks:    p.chunks.Load(),
		SyncFlush: p.syncFlush.Load(),
	}
}
