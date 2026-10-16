package plan

// slot_race_test.go — 槽读的锁边界验收（2026-09-29 锁面审计 §2.11）。
//
// 判据是 `-race`：槽读若把**槽指针**带到读锁外再解引用，就与写锁内的
// SetPolicyFor / SetBindingFor / beginRunFor 并发成数据竞争。旧实现的三处读
// （PolicyFor / BindingFor / CurrentRunIDFor）都是 `readSlot` → 解锁 → 读字段。

import (
	"sync"
	"testing"
	"time"
)

// TestSlotReadsCopyValueInsideLock 并发「写槽」与「读槽」：读必须返回**值**，
// 而不是解锁后再解引用槽指针。加 `-race` 跑即有牙（本用例不需要 sleep 判绿，
// 它判的是竞争检测器有没有报错）。
func TestSlotReadsCopyValueInsideLock(t *testing.T) {
	executor := NewExecutor(ExecutorDeps{}, 1, 1, 1, time.Minute)
	const sessionID = "session-slot-race"

	stop := make(chan struct{})
	var wg sync.WaitGroup
	for index := 0; index < 4; index++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				switch index % 4 {
				case 0:
					executor.SetPolicyFor(sessionID, PlanPolicy{Effort: "high", MaxNodes: index + 1})
				case 1:
					_ = executor.PolicyFor(sessionID)
				case 2:
					executor.SetBindingFor(sessionID, PlanBranchBinding{SessionID: sessionID, PlanID: "plan-race"})
					_ = executor.BindingFor(sessionID)
				default:
					runID := executor.beginRunFor(sessionID)
					_ = executor.CurrentRunIDFor(sessionID)
					executor.endRunFor(sessionID, runID)
				}
			}
		}(index)
	}
	time.Sleep(100 * time.Millisecond)
	close(stop)
	wg.Wait()

	// 语义面：写进去的值读得回来（并发压测不能把语义改掉）。
	executor.SetPolicyFor(sessionID, PlanPolicy{Effort: "high", MaxNodes: 7})
	if got := executor.PolicyFor(sessionID); got.MaxNodes != 7 || got.Effort != "high" {
		t.Fatalf("PolicyFor = %+v, want MaxNodes 7 / Effort high", got)
	}
	executor.SetBindingFor(sessionID, PlanBranchBinding{SessionID: sessionID, PlanID: "plan-final"})
	if got := executor.BindingFor(sessionID); got.PlanID != "plan-final" {
		t.Fatalf("BindingFor = %+v, want PlanID plan-final", got)
	}
	executor.endRunFor(sessionID, executor.beginRunFor(sessionID))
	if got := executor.CurrentRunIDFor(sessionID); got != "" {
		t.Fatalf("endRunFor 后 CurrentRunIDFor = %q, want 空", got)
	}
}
