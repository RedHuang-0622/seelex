package table

import (
	"sync/atomic"
	"testing"
	"time"
)

// 续跑信号源：一条信号 = 扫一次某会话的门；欠账（忙/退避）走同一条
// RetryDelay 定时器，**不新增定时器**（docs/arch/goal-auto-continue.md §2.5）。
func TestAsyncConsumerDispatchesContinuationSignals(t *testing.T) {
	continuationEvents := make(chan string, 2)
	stop := make(chan struct{})
	var calls atomic.Int32
	sessions := make(chan string, 4)
	consumer := NewAsyncConsumer(AsyncConsumerDeps{
		ContinuationEvents: continuationEvents,
		Stop:               stop,
		TriggerContinuation: func(triggered map[string]struct{}, sessionID string) int {
			calls.Add(1)
			sessions <- sessionID
			return 0
		},
		RetryDelay: 5 * time.Millisecond,
	})
	done := make(chan struct{})
	go func() {
		consumer.Run()
		close(done)
	}()
	continuationEvents <- "session-a"
	select {
	case sessionID := <-sessions:
		if sessionID != "session-a" {
			t.Fatalf("session = %q", sessionID)
		}
	case <-time.After(time.Second):
		t.Fatal("续跑信号没有触发扫描")
	}
	// 空 sessionID 不扫（信号源里的脏值不能被当成一个会话）。
	continuationEvents <- ""
	time.Sleep(20 * time.Millisecond)
	if got := calls.Load(); got != 1 {
		t.Fatalf("空 sessionID 也扫了：calls = %d", got)
	}
	close(stop)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("consumer did not stop")
	}
}

// 欠账必须靠定时器重试（忙会话/退避期都是这个形状）。
func TestAsyncConsumerRetriesPendingContinuation(t *testing.T) {
	continuationEvents := make(chan string, 1)
	stop := make(chan struct{})
	var calls atomic.Int32
	consumer := NewAsyncConsumer(AsyncConsumerDeps{
		ContinuationEvents: continuationEvents,
		Stop:               stop,
		TriggerContinuation: func(map[string]struct{}, string) int {
			if calls.Add(1) == 1 {
				return 1 // 第一次：会话忙，欠一次
			}
			return 0
		},
		RetryDelay: 5 * time.Millisecond,
	})
	done := make(chan struct{})
	go func() {
		consumer.Run()
		close(done)
	}()
	continuationEvents <- "session-a"
	deadline := time.After(time.Second)
	for calls.Load() < 2 {
		select {
		case <-deadline:
			t.Fatalf("退避/忙之后没有重试：calls = %d", calls.Load())
		default:
			time.Sleep(time.Millisecond)
		}
	}
	close(stop)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("consumer did not stop after continuation retry")
	}
}

// 信号源关闭不能空转（否则 CPU 打满）。
func TestAsyncConsumerStopsSpinningAfterContinuationChannelClosed(t *testing.T) {
	continuationEvents := make(chan string)
	stop := make(chan struct{})
	var calls atomic.Int32
	consumer := NewAsyncConsumer(AsyncConsumerDeps{
		ContinuationEvents: continuationEvents,
		Stop:               stop,
		TriggerContinuation: func(map[string]struct{}, string) int {
			calls.Add(1)
			return 0
		},
		RetryDelay: 5 * time.Millisecond,
	})
	done := make(chan struct{})
	go func() {
		consumer.Run()
		close(done)
	}()
	close(continuationEvents)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("信号源关闭后 consumer 没退出（空转）")
	}
	if got := calls.Load(); got != 0 {
		t.Fatalf("关闭的信号源还触发了扫描：%d", got)
	}
	close(stop)
}
