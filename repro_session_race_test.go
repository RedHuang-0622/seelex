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
