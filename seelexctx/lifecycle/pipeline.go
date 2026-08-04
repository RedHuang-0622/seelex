package lifecycle

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"
)

func fmtError(message string) error { return errors.New(message) }

// BatchPipeline batches a bounded stream before persisting it. The run
// goroutine is the sole owner of the mutable buffer. Push and Flush never
// hold a caller lock while Storage.Append is running.
type BatchPipeline[T any] struct {
	store  Storage[T]
	items  chan T
	flush  chan pipelineFlushRequest
	done   chan struct{}
	ctx    context.Context
	cancel context.CancelFunc

	flushSize int
	interval  time.Duration
	opTimeout time.Duration
	closeOnce sync.Once
	gate      sync.RWMutex
	closed    bool
	closeErr  error

	flushes   atomic.Int64
	chunks    atomic.Int64
	committed atomic.Int64
	syncFlush atomic.Int64
}

type pipelineFlushRequest struct {
	target int64
	reply  chan error
}

// PipelineOptions configures the bounded producer mailbox and every Storage
// call. A zero OperationTimeout defaults to five seconds.
type PipelineOptions struct {
	FlushSize        int
	Interval         time.Duration
	BufferSize       int
	OperationTimeout time.Duration
}

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
	opTimeout := options.OperationTimeout
	if opTimeout <= 0 {
		opTimeout = 5 * time.Second
	}
	if store == nil {
		store = newMemoryStorage[T]()
	}
	ctx, cancel := context.WithCancel(context.Background())
	pipeline := &BatchPipeline[T]{
		store: store, items: make(chan T, bufferSize), flush: make(chan pipelineFlushRequest),
		done: make(chan struct{}), ctx: ctx, cancel: cancel,
		flushSize: flushSize, interval: interval, opTimeout: opTimeout,
	}
	go pipeline.run()
	return pipeline
}

func (p *BatchPipeline[T]) run() {
	defer close(p.done)
	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()
	buffer := make([]T, 0, p.flushSize)
	consumed := int64(0)
	appendItem := func(item T) error {
		buffer = append(buffer, item)
		consumed++
		if len(buffer) >= p.flushSize {
			return p.flushBuffer(&buffer)
		}
		return nil
	}
	flushThrough := func(target int64) error {
		for consumed < target {
			item, ok := <-p.items
			if !ok {
				return p.flushBuffer(&buffer)
			}
			if err := appendItem(item); err != nil {
				return err
			}
		}
		return p.flushBuffer(&buffer)
	}
	for {
		select {
		case <-ticker.C:
			_ = p.flushBuffer(&buffer)
		case request := <-p.flush:
			request.reply <- flushThrough(request.target)
		case item, ok := <-p.items:
			if !ok {
				p.closeErr = p.flushBuffer(&buffer)
				return
			}
			_ = appendItem(item)
		}
	}
}

func (p *BatchPipeline[T]) operationContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(p.ctx, p.opTimeout)
}

func (p *BatchPipeline[T]) flushBuffer(buffer *[]T) error {
	if len(*buffer) == 0 {
		return nil
	}
	batch := *buffer
	*buffer = make([]T, 0, p.flushSize)
	ctx, cancel := p.operationContext()
	err := p.store.Append(ctx, batch)
	cancel()
	if err != nil {
		*buffer = append(batch, *buffer...)
		return err
	}
	p.flushes.Add(1)
	p.committed.Add(int64(len(batch)))
	return nil
}

// Push transfers ownership without blocking. A full producer mailbox is an
// explicit backpressure signal; a caller can retry or coalesce its chunk.
func (p *BatchPipeline[T]) Push(item T) error {
	p.gate.RLock()
	defer p.gate.RUnlock()
	if p.closed {
		return ErrPipelineClosed
	}
	select {
	case p.items <- item:
		p.chunks.Add(1)
		return nil
	default:
		p.syncFlush.Add(1)
		return ErrPipelineFull
	}
}

// FlushContext waits only for chunks accepted before it captured target. The
// run goroutine drains that prefix before replying, so it cannot spin when
// the items and flush channels are selected in a different order.
func (p *BatchPipeline[T]) FlushContext(ctx context.Context) error {
	p.gate.RLock()
	defer p.gate.RUnlock()
	if p.closed {
		return ErrPipelineClosed
	}
	request := pipelineFlushRequest{target: p.chunks.Load(), reply: make(chan error, 1)}
	select {
	case p.flush <- request:
	case <-ctx.Done():
		return ctx.Err()
	case <-p.ctx.Done():
		return ErrPipelineClosed
	}
	select {
	case err := <-request.reply:
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-p.ctx.Done():
		return ErrPipelineClosed
	}
}

func (p *BatchPipeline[T]) Flush() error {
	ctx, cancel := context.WithTimeout(context.Background(), p.opTimeout)
	defer cancel()
	return p.FlushContext(ctx)
}

// CloseContext first cancels in-flight Storage work, then closes admission and
// drains accepted items. Drain calls receive the canceled context and finish
// with an error rather than starting new unbounded persistence work. A timed-
// out caller may return while the drain continues; a later Close observes the
// final result.
func (p *BatchPipeline[T]) CloseContext(ctx context.Context) error {
	p.closeOnce.Do(func() {
		p.gate.Lock()
		p.closed = true
		p.cancel()
		close(p.items)
		p.gate.Unlock()
	})
	select {
	case <-p.done:
		return p.closeErr
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (p *BatchPipeline[T]) Close() error {
	return p.CloseContext(context.Background())
}

var ErrPipelineFull = fmtError("lifecycle: pipeline buffer full")
var ErrPipelineClosed = fmtError("lifecycle: pipeline closed")

type PipelineStats struct {
	Flushes   int64
	Chunks    int64
	SyncFlush int64
}

func (p *BatchPipeline[T]) Stats() PipelineStats {
	return PipelineStats{Flushes: p.flushes.Load(), Chunks: p.chunks.Load(), SyncFlush: p.syncFlush.Load()}
}
