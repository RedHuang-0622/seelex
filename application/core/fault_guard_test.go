package core

import (
	"testing"
	"time"
)

// TestDegradeAndRequestExitNotifiesHost：降级退出必须幂等、标记 closed/
// draining，并向订阅者发布进程级退出事件（host 据此关窗/退出）。
func TestDegradeAndRequestExitNotifiesHost(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	defer service.Shutdown()
	sub := service.Subscribe(16)
	defer sub.Close()

	service.degradeAndRequestExit("probe")
	service.degradeAndRequestExit("probe again") // 幂等

	service.ViewMu.RLock()
	closed := service.closed
	draining := service.draining
	service.ViewMu.RUnlock()
	if !closed || !draining {
		t.Fatalf("degraded state = closed:%v draining:%v, want both true", closed, draining)
	}
	deadline := time.After(2 * time.Second)
	for {
		select {
		case event := <-sub.Events:
			if event.Kind == EventExitRequested {
				return
			}
		case <-deadline:
			t.Fatal("host never received EventExitRequested after degrade")
		}
	}
}

// TestLifecycleFaultPanicsAfterDegrade：生命周期消费者中的故障不再被静默
// 吞掉——先降级退出，再重新抛出 panic（fail-fast），宿主进程及时退出而
// 不是带着半坏状态继续运行。
func TestLifecycleFaultPanicsAfterDegrade(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	defer service.Shutdown()

	var panicked any
	func() {
		defer func() { panicked = recover() }()
		service.safeLifecycleCall(func() { panic("concurrency-fault-probe") })
	}()
	if panicked == nil || panicked != "concurrency-fault-probe" {
		t.Fatalf("panic not rethrown after degrade: %v", panicked)
	}
	service.ViewMu.RLock()
	closed := service.closed
	service.ViewMu.RUnlock()
	if !closed {
		t.Fatal("service did not enter degraded state before panic")
	}
}
