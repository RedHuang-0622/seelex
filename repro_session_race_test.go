package main

// 竞态用例（test-cases.md 第 9 节 TC-R-01，-race 运行）：
// 会话切换/提交/快照读取与后台完成并发；A 的 record 键与内容仍正确。

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestWorkspaceSwitchConcurrentWithBackgroundPersist（TC-R-01）：A 后台完成
// 后，并发执行 ResumeSession(B)/Submit/Snapshot 与落盘读；断言 A record
// 无 B 内容（-race 下无数据竞争）。
func TestWorkspaceSwitchConcurrentWithBackgroundPersist(t *testing.T) {
	scenario := startBackgroundCompletionScenario(t, true)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(3)
		go func() {
			defer wg.Done()
			_ = scenario.harness.app.Snapshot()
		}()
		go func() {
			defer wg.Done()
			_ = scenario.harness.app.ResumeSession(scenario.sessionB)
		}()
		go func() {
			defer wg.Done()
			_ = scenario.harness.app.Submit(ctx, "concurrent input")
		}()
	}
	wg.Wait()

	// 收尾收敛（这一步是 2026-09-26 复现后补的，不是装饰）：
	//
	// `Submit` 返回 **不等于**这一轮结束——上面 8 次并发提交会继续在后台跑模型、
	// 写会话存储。不等整进程空闲就退出，`t.TempDir()` 的清理会与这些残留写者抢同
	// 一个目录，Windows 上的表现是"断言全过、红在清理"：
	//
	//	TempDir RemoveAll cleanup: unlinkat …\sessions-json\…\metadata: The directory is not empty.
	//
	// WaitForIdle 等的是**全部会话**的已接受工作（AnyChatRunning 语义），且从不取消
	// 活跃 chat；用有界 ctx 兜住"真的收敛不了"的情形，避免用例挂死。
	settleCtx, settleCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer settleCancel()
	if err := scenario.harness.app.WaitForIdle(settleCtx); err != nil {
		t.Fatalf("concurrent submits did not settle before assertions: %v", err)
	}

	record, ok := loadStoredRecord(t, scenario.store, "", scenario.sessionA)
	if !ok {
		t.Fatalf("A record missing after concurrent activity")
	}
	joined := strings.Join(recordConversationTexts(record), "\n")
	if !strings.Contains(joined, "long task A") {
		t.Fatalf("A record lost own message after concurrent activity: %v", recordConversationTexts(record))
	}
	if strings.Contains(joined, "hello B") {
		t.Fatalf("A record polluted with B content after concurrent activity: %v", recordConversationTexts(record))
	}
}
