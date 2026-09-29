package sessionstore

import (
	"testing"
	"time"
)

// testInFlightOps 读取在途 repository 操作计数（Configure/Close 的等待依据）。
func testInFlightOps(router *Router) int {
	router.opsMu.Lock()
	defer router.opsMu.Unlock()
	return router.activeOps
}

// TestJSONRuntimeEntryRegistersInFlightOp 钉住 §2.13：运行期「仅 JSON 布局」
// 入口必须在执行期间登记在途操作，Configure/Close 才会等到它结束再关闭旧后端。
//
// 修前的形态是 `router.mu.RLock(); repository, ok := jsonRepositoryLocked();
// router.mu.RUnlock()` 后**放锁执行**：取到指针就与后端解绑，存储切换/Close
// 可以在同一后端上并发关闭，而调用方还不知道自己踩在正在拆除的目录上。
func TestJSONRuntimeEntryRegistersInFlightOp(t *testing.T) {
	router := newTestRouter(t)

	entered := make(chan struct{})
	release := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		_, _ = router.withJSONRepositoryAt("", func(*jsonRepository, string) error {
			close(entered)
			<-release
			return nil
		})
	}()
	<-entered

	if got := testInFlightOps(router); got == 0 {
		t.Fatal("运行期入口未登记在途操作：Configure/Close 会与它并发关闭旧后端")
	}

	// 在途期间 Close 必须等：此刻它不该返回。
	closed := make(chan error, 1)
	go func() { closed <- router.Close() }()
	select {
	case err := <-closed:
		t.Fatalf("Close 未等待在途运行期入口即返回（err=%v）", err)
	case <-time.After(150 * time.Millisecond):
	}

	close(release)
	<-finished
	if got := testInFlightOps(router); got != 0 {
		t.Fatalf("操作结束后在途计数 = %d, want 0", got)
	}
	select {
	case err := <-closed:
		if err != nil {
			t.Fatalf("Close: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("在途操作已结束，Close 仍未返回")
	}
}

// TestRuntimeEntriesKeepOKFalseOnClosedRouter 是 §2.13 的**口径锁**：
// Router 已关闭时，这批入口必须仍然返回 (false, nil)——调用方把 ok=false 当
// "该布局/能力不可用 → 回退旧链路"，把 error 当**硬失败**（例：
// application/core/session_history.go 的 wire 装配遇 err 直接中断会话恢复）。
// 所以它们不能改走 withRepositoryAt（那条路的关闭分支返回 error）。
//
// 若有人把这些入口改成 withRepositoryAt，本用例立刻转红。
func TestRuntimeEntriesKeepOKFalseOnClosedRouter(t *testing.T) {
	router := newTestRouter(t)
	if err := router.Close(); err != nil {
		t.Fatal(err)
	}
	const projectID, sessionID = "project-closed", "session-closed"

	// RetentionAdvisoryWorkspace 无 ok 位：关闭后给 legacy 零值 + nil。
	advisory, err := router.RetentionAdvisoryWorkspace(projectID, sessionID)
	if err != nil || advisory.Layout != "legacy" {
		t.Fatalf("RetentionAdvisoryWorkspace 关闭后 = %+v err=%v, want legacy 零值 + nil", advisory, err)
	}

	cases := []struct {
		name string
		call func() (bool, error)
	}{
		{"AssembleWireWorkspace", func() (bool, error) {
			_, ok, err := router.AssembleWireWorkspace(projectID, sessionID, 100, 3)
			return ok, err
		}},
		{"CommitCompactFrameWorkspace", func() (bool, error) {
			return router.CommitCompactFrameWorkspace(projectID, sessionID, CompactFrame{SegmentID: "s"})
		}},
		{"LRUDeleteWorkspace", func() (bool, error) {
			return router.LRUDeleteWorkspace(projectID, sessionID, 0, true)
		}},
		{"LifecycleRecoverWorkspace", func() (bool, error) {
			_, ok, err := router.LifecycleRecoverWorkspace(projectID, sessionID)
			return ok, err
		}},
		{"PendingMessageTailWorkspace", func() (bool, error) {
			_, ok, err := router.PendingMessageTailWorkspace(projectID, sessionID)
			return ok, err
		}},
		{"RecoverPendingMessageTailWorkspace", func() (bool, error) {
			_, ok, err := router.RecoverPendingMessageTailWorkspace(projectID, sessionID)
			return ok, err
		}},
		{"DiscardPendingMessageTailWorkspace", func() (bool, error) {
			_, ok, err := router.DiscardPendingMessageTailWorkspace(projectID, sessionID)
			return ok, err
		}},
		{"QueueEnqueueWorkspace", func() (bool, error) {
			return false, router.QueueEnqueueWorkspace(projectID, sessionID, "req", "content")
		}},
		{"QueueMarkConsumedWorkspace", func() (bool, error) {
			return false, router.QueueMarkConsumedWorkspace(projectID, sessionID, "turn")
		}},
		{"QueueConfirmConsumedWorkspace", func() (bool, error) {
			return false, router.QueueConfirmConsumedWorkspace(projectID, sessionID, "turn")
		}},
		{"QueueFailConsumedWorkspace", func() (bool, error) {
			return false, router.QueueFailConsumedWorkspace(projectID, sessionID, "turn")
		}},
		{"QueueItemsWorkspace", func() (bool, error) {
			_, ok, err := router.QueueItemsWorkspace(projectID, sessionID)
			return ok, err
		}},
		{"QueueRecoverItemsWorkspace", func() (bool, error) {
			_, ok, err := router.QueueRecoverItemsWorkspace(projectID, sessionID)
			return ok, err
		}},
	}
	for _, testCase := range cases {
		ok, err := testCase.call()
		if err != nil {
			t.Fatalf("%s：关闭后必须回退 ok=false（调用方据此降级），实际返回 error=%v", testCase.name, err)
		}
		if ok {
			t.Fatalf("%s：关闭后 ok=true", testCase.name)
		}
	}
}
