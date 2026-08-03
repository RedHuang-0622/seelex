package lifecycle

import (
	"context"
	"sync"
)

// memoryStorage 是 Storage 的内存实现（mock/基准场景的冷存储面）。
// actor 外直连（管道/测试）时自锁；actor 内访问与 actor 串行无冲突。
type memoryStorage[T any] struct {
	mu    sync.Mutex
	items []T
}

func newMemoryStorage[T any]() *memoryStorage[T] {
	return &memoryStorage[T]{items: make([]T, 0)}
}

func (s *memoryStorage[T]) Append(_ context.Context, items []T) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items = append(s.items, items...)
	return nil
}

func (s *memoryStorage[T]) ReadRange(_ context.Context, offset, limit int) ([]T, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if offset < 0 || offset >= len(s.items) {
		return []T{}, len(s.items), nil
	}
	end := min(offset+limit, len(s.items))
	out := make([]T, 0, end-offset)
	out = append(out, s.items[offset:end]...)
	return out, len(s.items), nil
}

func (s *memoryStorage[T]) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.items)
}

// discardStorage 是"磁盘冷存储"的 mock：Append 只计数不保留内容
// （模拟落库后内容不在内存——真实实现为 sessionstore 磁盘后端）。
// 内存对比基准用它验证冷加载策略的驻留收益（对比 memoryStorage 的
// 假冷：内容仍驻留）。
type discardStorage[T any] struct {
	mu    sync.Mutex
	count int
}

func newDiscardStorage[T any]() *discardStorage[T] { return &discardStorage[T]{} }

func (s *discardStorage[T]) Append(_ context.Context, items []T) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.count += len(items)
	return nil
}

func (s *discardStorage[T]) ReadRange(_ context.Context, offset, limit int) ([]T, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if offset < 0 || offset >= s.count {
		return []T{}, s.count, nil
	}
	end := min(offset+limit, s.count)
	// 内容不可读（磁盘语义）；返回占位（基准/边界测试用长度）。
	out := make([]T, 0, end-offset)
	for i := 0; i < end-offset; i++ {
		var zero T
		out = append(out, zero)
	}
	return out, s.count, nil
}

func (s *discardStorage[T]) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.count
}
