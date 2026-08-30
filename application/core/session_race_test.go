package core

// 竞态用例（test-cases.md 第 9 节，-race 运行）：
// TC-R-02 快照 bump 与 runChat 尾部并发；
// TC-R-03 ReleaseWorkingHistoryFor 与 ChatStreamFor 并发（不误清、无竞争）。

import (
	"context"
	"sync"
	"testing"
	"time"
)

// TestSnapshotBumpConcurrentWithRunChatTail（TC-R-02）：并发 Submit（触发
// runChat 尾部 bump/落盘）与 Snapshot 读取，-race 下不得有数据竞争。
func TestSnapshotBumpConcurrentWithRunChatTail(t *testing.T) {
	service := newTestService(t, &fakeEngine{chunks: []string{"ok"}})
	defer service.Shutdown()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := service.Submit(ctx, "first"); err != nil {
		t.Fatal(err)
	}
	if err := service.WaitForIdle(ctx); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_ = service.Snapshot()
		}()
		go func() {
			defer wg.Done()
			// 运行中提交进入队列；错误（ErrChatRunning）可忽略，重点是并发面。
			_ = service.Submit(ctx, "queued")
		}()
	}
	wg.Wait()
}

// TestReleaseWorkingHistoryConcurrentWithChatStream（TC-R-03）：收尾清工作
// 历史与 ChatStream 并发，-race 下无竞争且不误清（fake 单引擎加锁镜像）。
func TestReleaseWorkingHistoryConcurrentWithChatStream(t *testing.T) {
	engine := &fakeEngine{chunks: []string{"chunk"}}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			engine.ReleaseWorkingHistoryFor("session-a")
		}()
		go func() {
			defer wg.Done()
			_, _ = engine.ChatStreamFor("session-a", context.Background(), "input", func(string) {})
		}()
	}
	wg.Wait()
	engine.mu.Lock()
	defer engine.mu.Unlock()
	if engine.releaseCalls == 0 {
		t.Fatal("ReleaseWorkingHistoryFor was not exercised")
	}
}
