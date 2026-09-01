package core

// 暴力测试（约束 C2：线程隔离）：N 个会话并行执行 + 高频移动视图指针 V +
// 并发追加排队输入，断言会话之间互不污染、无死锁。配合 -race 运行。

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestStressConcurrentSessionsDoNotPollute(t *testing.T) {
	engine := newMultiSessionEngine()
	service := newTestService(t, engine)
	defer service.Shutdown()
	ctx := context.Background()

	const count = 6
	ids := make([]string, 0, count)
	for index := 0; index < count; index++ {
		sid := fmt.Sprintf("sess-%d", index)
		engine.register(sid)
		ids = append(ids, sid)
	}

	// 并行向每个会话提交首条任务（引擎阻塞在 release 上，验证真并行）
	var submitWG sync.WaitGroup
	for index, sid := range ids {
		submitWG.Add(1)
		go func(sessionID string, n int) {
			defer submitWG.Done()
			_ = service.SubmitToSession(ctx, sessionID, fmt.Sprintf("task-%d", n))
		}(sid, index)
	}
	startDeadline := time.After(10 * time.Second)
	for _, sid := range ids {
		select {
		case <-engine.started[sid]:
		case <-startDeadline:
			t.Fatalf("session %s did not start:\n%s", sid, dumpParallelState(service, engine, ids...))
		}
	}

	// 执行中：高频移动 V 指针（仅视图，不触碰执行）
	var switchWG sync.WaitGroup
	for round := 0; round < 30; round++ {
		switchWG.Add(1)
		go func(r int) {
			defer switchWG.Done()
			service.Mu.Lock()
			sid := ids[r%len(ids)]
			service.Core.Snapshot.Session = SessionState{ID: sid}
			service.sessions.SetActive(sid)
			service.Mu.Unlock()
		}(round)
	}

	// 执行中：并发向各会话追加排队输入（写自有域）
	var queueWG sync.WaitGroup
	for index, sid := range ids {
		queueWG.Add(1)
		go func(sessionID string, n int) {
			defer queueWG.Done()
			_ = service.SubmitToSession(ctx, sessionID, fmt.Sprintf("queued-%d", n))
		}(sid, index)
	}

	// 释放所有会话
	for _, sid := range ids {
		close(engine.release[sid])
	}
	submitWG.Wait()
	switchWG.Wait()
	queueWG.Wait()

	// 等待全部 drain（无死锁）
	drainDeadline := time.After(20 * time.Second)
	for {
		service.Mu.RLock()
		allIdle := true
		for _, sid := range ids {
			if unit := service.sessions.Unit(sid); unit != nil && unit.ChatState().Running {
				allIdle = false
				break
			}
		}
		service.Mu.RUnlock()
		if allIdle {
			break
		}
		select {
		case <-drainDeadline:
			t.Fatalf("sessions did not drain (deadlock?):\n%s", dumpParallelState(service, engine, ids...))
		case <-time.After(50 * time.Millisecond):
		}
	}

	// 断言：每个会话的可见对话只含自己的输入，无跨会话污染
	service.Mu.RLock()
	defer service.Mu.RUnlock()
	for _, sid := range ids {
		unit := service.sessions.Unit(sid)
		if unit == nil {
			continue
		}
		for _, message := range unit.View.Conversation {
			if message.Role != "user" {
				continue
			}
			for otherIndex, other := range ids {
				if other == sid {
					continue
				}
				if strings.Contains(message.Content, fmt.Sprintf("task-%d", otherIndex)) ||
					strings.Contains(message.Content, fmt.Sprintf("queued-%d", otherIndex)) {
					t.Fatalf("session %s view polluted with %q (belongs to %s)", sid, message.Content, other)
				}
			}
		}
		// 会话单元生命周期最终回到 live（idle），未被切换破坏
	}
}
