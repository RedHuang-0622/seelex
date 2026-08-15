package core

import (
	"context"
	"runtime"
	"sync/atomic"
	"time"

	"github.com/RedHuang-0622/seelex/seelexctx/lifecycle"
)

// StreamBatcher serializes chunk delivery through BatchPipeline. The storage
// adapter renders each flushed batch and optionally mirrors it to a test or
// audit store; production does not retain raw chunks.
type StreamBatcher struct {
	pipe  *lifecycle.BatchPipeline[string]
	store *streamBatchStorage
}

type StreamBatcherOptions struct {
	FlushSize  int
	Interval   time.Duration
	BufferSize int
	// BatchSize 是 FlushSize 的兼容别名（当前仅测试使用；移除窗口：
	// FlushSize 全量接管后删除，建议随 v0.2 清理批次，2026-12-31）。
	BatchSize int
	Store     lifecycle.Storage[string]
}

func NewStreamBatcher(render func([]string), options StreamBatcherOptions) *StreamBatcher {
	flushSize := options.FlushSize
	if flushSize <= 0 {
		flushSize = options.BatchSize
	}
	store := &streamBatchStorage{render: render, mirror: options.Store}
	return &StreamBatcher{
		store: store,
		pipe: lifecycle.NewBatchPipeline[string](store, lifecycle.PipelineOptions{
			FlushSize:  flushSize,
			Interval:   options.Interval,
			BufferSize: options.BufferSize,
		}),
	}
}

func (b *StreamBatcher) OnChunk(chunk string) {
	if b == nil || chunk == "" {
		return
	}
	for {
		err := b.pipe.Push(chunk)
		switch err {
		case nil:
			return
		case lifecycle.ErrPipelineFull:
			runtime.Gosched()
		default:
			return
		}
	}
}

func (b *StreamBatcher) Flush() error {
	if b == nil || b.pipe == nil {
		return nil
	}
	return b.pipe.Close()
}

func (b *StreamBatcher) FlushPending() error {
	if b == nil || b.pipe == nil {
		return nil
	}
	return b.pipe.Flush()
}

type StreamBatcherStats struct {
	RenderCalls int
	Batches     int
	Pipe        lifecycle.PipelineStats
}

func (b *StreamBatcher) Stats() StreamBatcherStats {
	if b == nil {
		return StreamBatcherStats{}
	}
	renders := int(b.store.renderCalls.Load())
	return StreamBatcherStats{RenderCalls: renders, Batches: renders, Pipe: b.pipe.Stats()}
}

type streamBatchStorage struct {
	render      func([]string)
	mirror      lifecycle.Storage[string]
	count       atomic.Int64
	renderCalls atomic.Int64
}

func (s *streamBatchStorage) Append(ctx context.Context, items []string) error {
	batch := append([]string(nil), items...)
	if s.mirror != nil {
		if err := s.mirror.Append(ctx, batch); err != nil {
			return err
		}
	}
	s.count.Add(int64(len(batch)))
	if s.render != nil {
		s.renderCalls.Add(1)
		s.render(batch)
	}
	return nil
}

func (s *streamBatchStorage) ReadRange(context.Context, int, int) ([]string, int, error) {
	return []string{}, int(s.count.Load()), nil
}

func (s *streamBatchStorage) Count() int { return int(s.count.Load()) }

var _ lifecycle.Storage[string] = (*streamBatchStorage)(nil)
