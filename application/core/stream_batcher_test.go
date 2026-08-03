package core

import (
	"context"
	"sync"
	"testing"

	"github.com/RedHuang-0622/seelex/seelexctx/lifecycle"
)

// ── 基建-C：StreamBatcher 流式批次骨架（非 happy path）────────────────

// TestStreamBatcherThrottlesRenders 10k chunks → 渲染调用次数被节流
// （batchSize=32 → renderCalls ≈ 313，远小于 10k）。
func TestStreamBatcherThrottlesRenders(t *testing.T) {
	store := newDiscardStorage()
	var renderCalls int
	batcher := NewStreamBatcher(func([]string) { renderCalls++ }, StreamBatcherOptions{
		BatchSize: 32,
		Store:     store,
	})
	const chunks = 10000
	for i := 0; i < chunks; i++ {
		batcher.OnChunk("chunk")
	}
	if err := batcher.Flush(); err != nil {
		t.Fatal(err)
	}
	stats := batcher.Stats()
	if stats.RenderCalls == 0 || stats.RenderCalls >= chunks/2 {
		t.Fatalf("render calls = %d, want throttled (chunks=%d)", stats.RenderCalls, chunks)
	}
	// 落库零丢失（管道聚合后总量一致）。
	if store.Count() != chunks {
		t.Fatalf("store count = %d, want %d (no chunk lost)", store.Count(), chunks)
	}
	t.Logf("render calls = %d for %d chunks (%.1f%% throttled)", stats.RenderCalls, chunks, 100-100*float64(stats.RenderCalls)/chunks)
}

// TestStreamBatcherConcurrentOnChunk 并发流式：多 goroutine Push，
// 全部落库零丢失（actor 管道串行化）。
func TestStreamBatcherConcurrentOnChunk(t *testing.T) {
	store := newDiscardStorage()
	batcher := NewStreamBatcher(func([]string) {}, StreamBatcherOptions{
		BatchSize: 64,
		Store:     store,
	})
	const workers = 16
	const perWorker = 500
	done := make(chan struct{}, workers)
	for w := 0; w < workers; w++ {
		go func() {
			defer func() { done <- struct{}{} }()
			for i := 0; i < perWorker; i++ {
				batcher.OnChunk("c")
			}
		}()
	}
	for w := 0; w < workers; w++ {
		<-done
	}
	if err := batcher.Flush(); err != nil {
		t.Fatal(err)
	}
	if store.Count() != workers*perWorker {
		t.Fatalf("store count = %d, want %d", store.Count(), workers*perWorker)
	}
}

// TestStreamBatcherBackpressureNoLoss 背压：管道缓冲极小（FlushSize=2）+
// 大批量 → 背压重投路径触发，最终零丢失。
func TestStreamBatcherBackpressureNoLoss(t *testing.T) {
	store := newDiscardStorage()
	batcher := NewStreamBatcher(func([]string) {}, StreamBatcherOptions{
		BatchSize: 16,
		FlushSize: 2, // 极小落库窗口 → 高频 flush → 背压重投路径
		Store:     store,
	})
	const chunks = 5000
	for i := 0; i < chunks; i++ {
		batcher.OnChunk("c")
	}
	if err := batcher.Flush(); err != nil {
		t.Fatal(err)
	}
	if store.Count() != chunks {
		t.Fatalf("store count = %d, want %d (backpressure must not lose data)", store.Count(), chunks)
	}
}

// TestStreamBatcherFlushEmptyIdempotent 空流收尾：无 chunk 时 Flush 幂等。
func TestStreamBatcherFlushEmptyIdempotent(t *testing.T) {
	batcher := NewStreamBatcher(func([]string) {}, StreamBatcherOptions{})
	if err := batcher.Flush(); err != nil {
		t.Fatal(err)
	}
	if err := batcher.Flush(); err != nil { // 幂等（第二次 Close 不再 panic）
		t.Fatal(err)
	}
}

// lifecycleMemoryStorage 是测试辅助：内存计数存储（不驻留内容，
// 模拟磁盘冷存储）。
type lifecycleMemoryStorage struct {
	mu    sync.Mutex
	count int
}

func newDiscardStorage() *lifecycleMemoryStorage { return &lifecycleMemoryStorage{} }

func (s *lifecycleMemoryStorage) Append(_ context.Context, items []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.count += len(items)
	return nil
}
func (s *lifecycleMemoryStorage) ReadRange(_ context.Context, offset, limit int) ([]string, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if offset >= s.count {
		return []string{}, s.count, nil
	}
	return make([]string, 0), s.count, nil
}
func (s *lifecycleMemoryStorage) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.count
}

// 编译期断言：测试存储实现 lifecycle.Storage。
var _ lifecycle.Storage[string] = (*lifecycleMemoryStorage)(nil)
