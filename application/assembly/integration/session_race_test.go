package integration

import (
	"context"
	"sync"
	"testing"
	"time"
)

// 竞态用例（test-cases.md 第 9 节，-race 运行）：TC-R-02 快照 bump 与
// runChat 尾部并发。

// TestSnapshotBumpConcurrentWithRunChatTail（TC-R-02）：并发 Submit（触发
// runChat 尾部 bump/落盘）与 Snapshot 读取，-race 下不得有数据竞争。
func TestSnapshotBumpConcurrentWithRunChatTail(t *testing.T) {
	service := newLegacyTestService(t, &fakeEngine{chunks: []string{"ok"}})
	defer service.components.lifecycleApplication.Shutdown()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := service.components.submission.Submit(ctx, "first"); err != nil {
		t.Fatal(err)
	}
	if err := service.components.lifecycleApplication.WaitForIdle(ctx); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_ = service.components.frontendSnapshot.Snapshot()
		}()
		go func() {
			defer wg.Done()
			// 运行中提交进入队列；错误（ErrChatRunning）可忽略，重点是并发面。
			_ = service.components.submission.Submit(ctx, "queued")
		}()
	}
	wg.Wait()
}
