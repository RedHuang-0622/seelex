package session_runtime

import (
	"runtime"
	"sync"
	"testing"
	"time"
)

// TestTransitionActorSerializesConcurrentAcquire（无锁化语义）：并发
// Acquire 只放行一个持有者，FIFO 依序授予；全部释放后无死锁。
func TestTransitionActorSerializesConcurrentAcquire(t *testing.T) {
	actor := NewSessionTransitionActor()
	defer actor.Close()

	const holders = 8
	var (
		mu         sync.Mutex
		concurrent int
		maxSeen    int
		wg         sync.WaitGroup
	)
	for index := 0; index < holders; index++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			actor.Acquire()
			mu.Lock()
			concurrent++
			if concurrent > maxSeen {
				maxSeen = concurrent
			}
			mu.Unlock()
			time.Sleep(time.Millisecond)
			mu.Lock()
			concurrent--
			mu.Unlock()
			actor.Release()
		}()
	}
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("transition actor deadlock（并发 Acquire 未收敛）")
	}
	if maxSeen != 1 {
		t.Fatalf("max concurrent holders = %d, want 1（互斥语义破坏）", maxSeen)
	}
}

// TestTransitionActorFIFOOrder：等待者按请求顺序被授予（队列即等待队列）。
func TestTransitionActorFIFOOrder(t *testing.T) {
	actor := NewSessionTransitionActor()
	defer actor.Close()

	actor.Acquire()
	// 持有期间按确定顺序注入 3 个 acquire（同包直接驱动命令通道）。
	replies := make([]chan struct{}, 3)
	for index := range replies {
		replies[index] = make(chan struct{}, 1)
		actor.cmds <- transitionCmd{acquire: true, reply: replies[index]}
	}
	for index := range replies {
		actor.Release()
		select {
		case <-replies[index]:
			// 依序授予
		case <-time.After(time.Second):
			t.Fatalf("waiter %d not granted in FIFO order", index)
		}
		// 后续等待者必须仍被阻塞。
		if index+1 < len(replies) {
			select {
			case <-replies[index+1]:
				t.Fatalf("waiter %d granted before waiter %d（FIFO 破坏）", index+1, index)
			case <-time.After(10 * time.Millisecond):
			}
		}
	}
}

// TestTransitionActorCloseReleasesGoroutine：Close 停止 actor goroutine，
// 之后 Acquire/Release 为无操作（不 panic、不永久阻塞）。
func TestTransitionActorCloseReleasesGoroutine(t *testing.T) {
	before := runtime.NumGoroutine()
	actor := NewSessionTransitionActor()
	actor.Acquire()
	actor.Release()
	actor.Close()
	// 关闭后调用安全。
	actor.Acquire()
	actor.Release()
	actor.Close() // 幂等
	deadline := time.Now().Add(2 * time.Second)
	for runtime.NumGoroutine() > before+2 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if runtime.NumGoroutine() > before+2 {
		t.Fatalf("goroutine count after Close = %d, before = %d（actor 泄漏）",
			runtime.NumGoroutine(), before)
	}
}

// TestTransitionLockerAdapter：sync.Locker 适配层与现有调用方契约一致。
func TestTransitionLockerAdapter(t *testing.T) {
	actor := NewSessionTransitionActor()
	defer actor.Close()
	locker := transitionLocker{actor: actor}
	locker.Lock()
	// 互斥语义：持有期间第二个 Acquire 不得放行。
	granted := make(chan struct{})
	go func() {
		actor.Acquire()
		close(granted)
	}()
	select {
	case <-granted:
		t.Fatal("second acquire granted while first holder active")
	case <-time.After(20 * time.Millisecond):
	}
	locker.Unlock()
	select {
	case <-granted:
	case <-time.After(time.Second):
		t.Fatal("second acquire not granted after release")
	}
}
