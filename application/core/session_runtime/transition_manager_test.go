package session_runtime

import (
	"sync"
	"testing"
	"time"
)

// TestSessionTransitionManagerSerializesSameKey G5：同一 key 的命令串行
// （同会话生命周期命令互斥）。
func TestSessionTransitionManagerSerializesSameKey(t *testing.T) {
	manager := NewSessionTransitionManager()
	defer manager.Close()

	lockerA := manager.Lock("session-a")
	lockerB := manager.Lock("session-a")
	lockerA.Lock()
	granted := make(chan struct{})
	go func() {
		lockerB.Lock()
		close(granted)
	}()
	select {
	case <-granted:
		t.Fatal("second same-key holder granted while first active")
	case <-time.After(30 * time.Millisecond):
	}
	lockerA.Unlock()
	select {
	case <-granted:
	case <-time.After(time.Second):
		t.Fatal("same-key waiter not granted after release")
	}
	lockerB.Unlock()
}

// TestSessionTransitionManagerParallelAcrossKeys G5：不同 key 的命令并行
// （A/B 两个会话各持一把过渡锁时互不阻塞）。
func TestSessionTransitionManagerParallelAcrossKeys(t *testing.T) {
	manager := NewSessionTransitionManager()
	defer manager.Close()

	lockerA := manager.Lock("session-a")
	lockerB := manager.Lock("session-b")
	lockerA.Lock()
	defer lockerA.Unlock()

	done := make(chan struct{})
	go func() {
		lockerB.Lock()
		lockerB.Unlock()
		close(done)
	}()
	select {
	case <-done:
		// B 在 A 持有期间完成：跨会话并行成立。
	case <-time.After(time.Second):
		t.Fatal("different-key transition blocked behind another session's lock")
	}
}

// TestSessionTransitionManagerViewKeyAliasesEmpty G5：空 key 与保留视图 key
// 视为同一把锁（视图命令无论以哪种写法都应互斥）。
func TestSessionTransitionManagerViewKeyAliasesEmpty(t *testing.T) {
	manager := NewSessionTransitionManager()
	defer manager.Close()

	viewLocker := manager.Lock("")
	explicitLocker := manager.Lock(viewTransitionKey)
	viewLocker.Lock()
	granted := make(chan struct{})
	go func() {
		explicitLocker.Lock()
		close(granted)
	}()
	select {
	case <-granted:
		t.Fatal("view-key waiter granted while empty-key holder active")
	case <-time.After(30 * time.Millisecond):
	}
	viewLocker.Unlock()
	select {
	case <-granted:
	case <-time.After(time.Second):
		t.Fatal("view-key waiter not granted after release")
	}
	explicitLocker.Unlock()
}

// TestSessionTransitionManagerCloseReleasesWaiters G5：关闭后释放全部等待者，
// 后续 Lock/Unlock 为空操作（退出路径不卡死、不泄漏）。
func TestSessionTransitionManagerCloseReleasesWaiters(t *testing.T) {
	manager := NewSessionTransitionManager()
	locker := manager.Lock("session-a")
	locker.Lock()

	waiters := make(chan struct{}, 4)
	var group sync.WaitGroup
	for index := 0; index < 4; index++ {
		group.Add(1)
		go func() {
			defer group.Done()
			waiter := manager.Lock("session-a")
			waiter.Lock()
			waiter.Unlock()
			waiters <- struct{}{}
		}()
	}
	time.Sleep(10 * time.Millisecond)
	manager.Close()
	locker.Unlock()
	group.Wait()

	// 关闭后的 Lock/Unlock 为空操作：不 panic、不永久阻塞。
	after := manager.Lock("session-a")
	after.Lock()
	after.Unlock()
}
