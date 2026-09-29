package seelebridge

import (
	"testing"
	"time"
)

// TestSessionReadPathDoesNotTakeWriteLock 钉住 §2.14：Session()/CurrentSession()
// 是**纯读**，只允许取读锁。
//
// 判据不是"能不能返回"（写锁也能返回），而是"另一个纯读在持读锁时它能不能
// 进"：sessionBundle.mu 若是 sync.Mutex，读读之间也互斥——装配/切换期间任何
// 读面都会被无谓串行；是 sync.RWMutex 时两个纯读并存。
//
// 修前的形态是 `bundle.mu.Lock(); defer Unlock; return bundle.session`：读面
// 持写锁，本用例取 bundle.mu.RLock() 时即与之互斥。
func TestSessionReadPathDoesNotTakeWriteLock(t *testing.T) {
	rt := &Runtime{bundles: map[string]*sessionBundle{}}
	rt.bundleFor("sess-read")

	bundle := rt.activeBundle()
	if bundle == nil {
		t.Fatal("bundle 未登记，用例失去意义")
	}
	// 模拟"另一个纯读"正持读锁。
	bundle.mu.RLock()
	defer bundle.mu.RUnlock()

	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = rt.Session()
		_ = rt.CurrentSession()
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Runtime.Session() 被另一个纯读阻塞：读面仍取写锁（应为 RLock）")
	}
}
