package residency

import (
	"context"
	"errors"
	"testing"
	"time"
)

// fakeRefresher 是 CatalogRefresher 的最小结构满足实现。
type fakeRefresher struct {
	done    chan struct{}
	request int
}

func (refresher *fakeRefresher) RequestCatalogRefresh() <-chan struct{} {
	refresher.request++
	if refresher.done == nil {
		refresher.done = make(chan struct{})
	}
	return refresher.done
}

// TestWaitCatalogRefreshReturnsWhenRefreshCompletes：刷新回执关闭即返回 nil（恰好请求一次）。
func TestWaitCatalogRefreshReturnsWhenRefreshCompletes(t *testing.T) {
	refresher := &fakeRefresher{done: make(chan struct{})}
	go func() {
		time.Sleep(5 * time.Millisecond)
		close(refresher.done)
	}()
	if err := WaitCatalogRefresh(context.Background(), refresher); err != nil {
		t.Fatalf("刷新完成应返回 nil，got %v", err)
	}
	if refresher.request != 1 {
		t.Fatalf("应恰好请求一次刷新，got %d", refresher.request)
	}
}

// TestWaitCatalogRefreshTimeoutIsNotFailure：ctx 超时返回 ctx.Err()（不是"失败"），
// 调用方按最佳努力收敛即可。
func TestWaitCatalogRefreshTimeoutIsNotFailure(t *testing.T) {
	refresher := &fakeRefresher{done: make(chan struct{})}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	err := WaitCatalogRefresh(ctx, refresher)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("超时应返回 ctx.Err()，got %v", err)
	}
	// 之后刷新仍会完成：等待方不因此拿到"永久失败"。
	close(refresher.done)
	if err := WaitCatalogRefresh(context.Background(), refresher); err != nil {
		t.Fatalf("回执到达后应返回 nil，got %v", err)
	}
}
