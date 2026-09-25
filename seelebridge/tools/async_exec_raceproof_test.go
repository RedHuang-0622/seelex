//go:build raceproof

package tools

// 反证用例（opt-in，**永远不进 CI**）：钉住"修之前确实有竞态"这件事，而不是留下
// 一句"看起来像竞态"。
//
// 旧形状：`asyncRegistry.begin` 返回表内那条 `*asyncRun`，派发侧在锁外做整结构体拷贝
// （`renderAccepted(*run, …)`）；执行体收尾时 `finish` 在 g.mu 内写同一条目的
// `state/exit`。两边访问同一块内存、至少一侧在写、且没有共同的锁——教科书形状。
// 之所以日常 `-race` 抓不到，是因为窗口只有"回执正在拷贝"对上"命令恰好这一瞬结束"。
//
// 跑法（预期：报 DATA RACE 并以非零退出——本用例的任务就是报红）：
//
//	go test -tags raceproof ./seelebridge/tools/ -run TestRaceProof -count=20 -race
//
// 修后的真实路径（begin 按值返回）由 `async_exec_test.go` 的
// TestAsyncRegistryBeginHandsOutACopy 与常规 -race 守卫。

import (
	"sync"
	"testing"
)

// beginPointerShape 复刻修 bug 之前 begin 的返回口径：把表内指针交出去。
// begin 现在按值返回，所以这里在锁内取回指针——差的就是派发侧有没有拿到锁外可读的
// 可变字段，这正是要证的那一点。
func (g *asyncRegistry) beginPointerShape(sessionID, command string) *asyncRun {
	run, _, err := g.begin(sessionID, command, "", "")
	if err != nil {
		return nil
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.runs[run.handle]
}

// mutateLockedForRaceProof 复刻执行体收尾时对表内条目的写入（只改 state/exit，不关
// done —— 关两次会 panic，而关通道与本用例要证的读写重叠无关）。
func (g *asyncRegistry) mutateLockedForRaceProof(handle string, state string, exit int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if run, ok := g.runs[handle]; ok {
		run.state = state
		run.exit = exit
	}
}

func TestRaceProofOldPointerShape(t *testing.T) {
	registry := newAsyncRegistry()
	run := registry.beginPointerShape("sess-race", "echo race")
	if run == nil {
		t.Fatal("派发未登记")
	}
	t.Cleanup(func() { registry.close() })

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		// 派发侧：旧代码在锁外做整结构体拷贝。
		for i := 0; i < 2000; i++ {
			_ = *run
		}
	}()
	go func() {
		defer wg.Done()
		// 执行体收尾：在锁内改写同一条目的 state/exit。
		for i := 0; i < 2000; i++ {
			registry.mutateLockedForRaceProof(run.handle, asyncStateDone, i)
		}
	}()
	wg.Wait()
}
