package actor

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type replyCommand struct {
	value string
	reply chan string
}

// TestActorSerializesCommands 验证命令在单消费者 goroutine 内串行处理且
// 顺序稳定（不会并发交错）。
func TestActorSerializesCommands(t *testing.T) {
	var processed atomic.Int64
	var busy atomic.Bool
	actor := New(func(string) {
		if !busy.CompareAndSwap(false, true) {
			t.Error("handler entered concurrently")
		}
		defer busy.Store(false)
		processed.Add(1)
		time.Sleep(time.Millisecond)
	})
	defer actor.Close()
	for i := 0; i < 50; i++ {
		if !actor.Send("cmd") {
			t.Fatal("Send failed before close")
		}
	}
	deadline := time.Now().Add(2 * time.Second)
	for processed.Load() != 50 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := processed.Load(); got != 50 {
		t.Fatalf("processed = %d, want 50", got)
	}
}

// TestActorReplyRoundTrip 验证命令自带回复通道的完整往返（Send + 回复等待
// 调用方样板）。
func TestActorReplyRoundTrip(t *testing.T) {
	actor := New(func(command replyCommand) {
		command.reply <- "echo:" + command.value
	})
	defer actor.Close()
	reply := make(chan string, 1)
	if !actor.Send(replyCommand{value: "hello", reply: reply}) {
		t.Fatal("Send failed")
	}
	select {
	case got := <-reply:
		if got != "echo:hello" {
			t.Fatalf("reply = %q, want echo:hello", got)
		}
	case <-time.After(time.Second):
		t.Fatal("reply timed out")
	}
}

// TestActorSendAfterClose 验证关闭后 Send/SendTimeout/TrySend 全部快返 false。
func TestActorSendAfterClose(t *testing.T) {
	actor := New(func(string) {})
	actor.Close()
	if actor.Send("x") {
		t.Fatal("Send after Close must return false")
	}
	if actor.SendTimeout("x", time.Second) {
		t.Fatal("SendTimeout after Close must return false")
	}
	if actor.TrySend("x") {
		t.Fatal("TrySend after Close must return false")
	}
}

// TestActorCloseIsIdempotent 验证 Close 幂等且 Wait 在 Close 后返回。
func TestActorCloseIsIdempotent(t *testing.T) {
	var handled atomic.Int64
	actor := New(func(string) { handled.Add(1) })
	actor.Close()
	actor.Close()
	done := make(chan struct{})
	go func() {
		actor.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Wait did not return after Close")
	}
}

// TestActorTrySendFullNonBlocking 验证通道满时 TrySend 立即返回 false。
func TestActorTrySendFullNonBlocking(t *testing.T) {
	blocked := make(chan struct{})
	started := make(chan struct{})
	var startOnce sync.Once
	actor := New(func(string) {
		startOnce.Do(func() { close(started) })
		<-blocked
	}, WithCap(2))
	defer func() {
		close(blocked)
		actor.Close()
	}()
	// 先投递一条唤醒 handler（阻塞在 <-blocked），此后邮箱清空可再填满。
	if !actor.TrySend("kick") {
		t.Fatal("kick failed while capacity remains")
	}
	<-started
	for i := 0; i < 2; i++ {
		if !actor.TrySend("fill") {
			t.Fatalf("TrySend #%d failed while capacity remains", i)
		}
	}
	if actor.TrySend("overflow") {
		t.Fatal("TrySend must fail when mailbox is full")
	}
}

// TestActorConcurrentSenders 验证并发投递安全（多生产者单消费者）。
func TestActorConcurrentSenders(t *testing.T) {
	var processed atomic.Int64
	actor := New(func(string) { processed.Add(1) })
	defer actor.Close()
	var wg sync.WaitGroup
	for producer := 0; producer < 8; producer++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				actor.Send("cmd")
			}
		}()
	}
	wg.Wait()
	deadline := time.Now().Add(2 * time.Second)
	for processed.Load() != 400 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := processed.Load(); got != 400 {
		t.Fatalf("processed = %d, want 400", got)
	}
}
