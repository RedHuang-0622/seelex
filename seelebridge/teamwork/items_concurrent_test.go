package teamwork

// items_concurrent_test.go — 「计划头读-改-写原子化」的两条并发回归用例。
//
// 钉死的是 2026-10-05 自体勘察实测的那条丢失更新（docs/devlog/2026-10-05-teamwork-self-survey.md
// §2）：5 个工作项并发 settle，2 个被后写覆盖回 running。旧实现里 SettleWorkItem 是
// `ReadPlan →(MergeWorkspace，慢 git) → WritePlan` 的读-改-写，跨这三步没有锁，各持
// 陈旧快照写回、后写覆盖先写。
//
// 两条用例都把"合并"这一步卡成**确定可见**的会合点（fixture 的 mergeGate），让"陈旧快照"
// 不再是"碰运气才出现"的时序，而是**被安排好的**时序：
//
//	用例 1（全 settle）：所有 settle 都先读过了计划、都到达合并，再一起放行。
//	用例 2（settle 混 accept）：两家 settle 读过后被钉在半路，窗口里另一件事
//	                       settle → accept 走完整条链，才放行那两家。
//
// 于是"修复前必失败"是可复现的（见 §证据：tmp/prefix-baseline 跑同一份用例，
// 3/3 次都报 `wi-0 状态 = running`），而不是依赖调度骰子。

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/sessionstore"
)

// clonePlan 用 JSON 往返深拷贝一份计划。**这是生产持久面的语义**（sessionstore 把计划
// 序列化成 JSON 落盘、读回时解析），所以内存替身（teamwork_test.go 的 memoryPlanStore）
// 也照抄：否则 ReadPlan 返回的是与 store 内部**共享底层数组**的计划，调用方对
// `item.Status` 的原地修改会绕过 WritePlan 直接落到 store 上、并让 `Items()` 的只读扫描
// 与写入方并发读写同一片数组——既把并发写-写冲突悄悄吸收掉（用例假绿），又是一处
// 数据竞争（2026-10-05 复现时踩到过）。
func clonePlan(plan sessionstore.TeamworkPlan) sessionstore.TeamworkPlan {
	raw, err := json.Marshal(plan)
	if err != nil {
		panic("memoryPlanStore: 计划无法序列化: " + err.Error())
	}
	var out sessionstore.TeamworkPlan
	if err := json.Unmarshal(raw, &out); err != nil {
		panic("memoryPlanStore: 计划无法反序列化: " + err.Error())
	}
	return out
}

// rendezvous 是一个 N 路会合点（到达者都阻塞，直到放行）。
//
//	auto = true：第 want 个到达者放行全部（用例 1：把"所有 settle 都已读过计划"钉成事实）。
//	auto = false：只由测试显式 open() 放行（用例 2：把"早读的两家"停在半路，让别处的
//	             并发动作插进它们的窗口，之后再放行）。
//
// arrivedCount() 让测试能**等到**"都停在会合点上"这件事发生，而不是 sleep 猜。
type rendezvous struct {
	mu      sync.Mutex
	want    int
	auto    bool
	arrived int
	opened  bool
	release chan struct{}
}

func newRendezvous(want int, auto bool) *rendezvous {
	return &rendezvous{want: want, auto: auto, release: make(chan struct{})}
}

// arrive 到达会合点并阻塞，直到放行。
func (r *rendezvous) arrive() {
	r.mu.Lock()
	r.arrived++
	if r.auto && r.arrived >= r.want {
		r.openLocked()
	}
	r.mu.Unlock()
	<-r.release
}

// open 显式放行全部到达者（幂等）。
func (r *rendezvous) open() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.openLocked()
}

func (r *rendezvous) openLocked() {
	if !r.opened {
		r.opened = true
		close(r.release)
	}
}

func (r *rendezvous) arrivedCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.arrived
}

// waitUntil 在窗口内等一个条件成立（等的是**事实**，不是时间）。
func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("窗口内没等到：%s", what)
}

// dispatchBlockedItems 把 n 个工作项（同一里程碑、彼此无依赖）逐一派发，并让执行体
// 停在半路——于是它们都停在 `running`，settle 由用例手动并发驱动。
func dispatchBlockedItems(t *testing.T, fixture *itemFixture, n int) chan struct{} {
	t.Helper()
	ctx := context.Background()
	if err := fixture.coordinator.SetPlan(ctx, itemPlan()); err != nil {
		t.Fatalf("SetPlan: %v", err)
	}
	specs := make([]WorkItemSpec, 0, n)
	for i := 0; i < n; i++ {
		specs = append(specs, WorkItemSpec{ID: itemID(i), Role: "exec", Name: fmt.Sprintf("实现 %d", i)})
	}
	if err := fixture.coordinator.PlanMilestone(ctx, "m-build", specs); err != nil {
		t.Fatalf("PlanMilestone: %v", err)
	}
	block := make(chan struct{})
	fixture.runner.block = block
	for i := 0; i < n; i++ {
		if _, err := fixture.coordinator.DispatchItem(ctx, itemID(i)); err != nil {
			t.Fatalf("DispatchItem(%s): %v", itemID(i), err)
		}
	}
	return block
}

func itemID(i int) string { return fmt.Sprintf("wi-%d", i) }

func assertItemStatus(t *testing.T, fixture *itemFixture, id, want string) {
	t.Helper()
	if got := itemState(t, fixture, id).StatusOrPending(); got != want {
		t.Fatalf("工作项 %s 状态 = %s，want %s", id, got, want)
	}
}

// TestConcurrentSettleDoesNotLoseUpdates 并发 settle N 个仍在跑的工作项，断言**全部**
// 落到 review（没有一件被覆盖回 running），且每件恰好尾插一次。
func TestConcurrentSettleDoesNotLoseUpdates(t *testing.T) {
	const n = 5
	fixture := newItemFixture(t, n+3)
	ctx := context.Background()
	block := dispatchBlockedItems(t, fixture, n)
	// 所有 settle 都先把计划读成"全 running"，再一起放行。
	parked := newRendezvous(n, true)
	fixture.spaces.mergeGate = func(WorkspaceBinding) { parked.arrive() }

	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			<-start
			if err := fixture.coordinator.SettleWorkItem(ctx, WorkerRequest{WorkItemID: id}, nil); err != nil {
				t.Errorf("SettleWorkItem(%s): %v", id, err)
			}
		}(itemID(i))
	}
	close(start)
	wg.Wait()
	close(block)

	for i := 0; i < n; i++ {
		if status := itemState(t, fixture, itemID(i)).StatusOrPending(); status != sessionstore.TeamworkItemReview {
			t.Fatalf("并发 settle 丢失更新：%s 状态 = %s，want review", itemID(i), status)
		}
	}
	if texts := fixture.queue.texts(); len(texts) != n {
		t.Fatalf("每件工作项应恰好尾插一次，得到 %d 条：%v", len(texts), texts)
	}
}

// TestConcurrentSettleAndAcceptDoNotResurrectDone 把 settle 与 accept 混跑，钉死
// **终态单调**：一件已经验收入账（done）的工作项，不许被任何并发的陈旧快照写回 running。
//
// 时序（用会合点排定，不靠调度骰子）：
//
//	① wi-0 / wi-1 的 settle 读过计划（快照：4 件全 running）→ 停在被钉住的"合并"上；
//	② 窗口里 wi-2 / wi-3 走完整条链：先 settle（→ review），再并发 accept（→ done）；
//	③ 放行 wi-0 / wi-1：它们的快照里 wi-2 / wi-3 **还是 running**。
//
// 修复后：③ 进入临界区时会**重读**计划（wi-2 / wi-3 已是 done），只写回自己那一件的
// review，已 done 的保持 done。修复前：③ 整份陈旧快照写回，done 被覆盖回 running。
func TestConcurrentSettleAndAcceptDoNotResurrectDone(t *testing.T) {
	const n = 4
	fixture := newItemFixture(t, n+4)
	ctx := context.Background()
	block := dispatchBlockedItems(t, fixture, n)

	// 只钉住 wi-0 / wi-1 的合并：它们是"早读"的两家。
	parked := newRendezvous(2, false)
	fixture.spaces.mergeGate = func(binding WorkspaceBinding) {
		if binding.WorkItem == itemID(0) || binding.WorkItem == itemID(1) {
			parked.arrive()
		}
	}
	var early sync.WaitGroup
	for i := 0; i < 2; i++ {
		early.Add(1)
		go func(id string) {
			defer early.Done()
			if err := fixture.coordinator.SettleWorkItem(ctx, WorkerRequest{WorkItemID: id}, nil); err != nil {
				t.Errorf("SettleWorkItem(%s): %v", id, err)
			}
		}(itemID(i))
	}
	// 等到"两家都已读过计划、都停在合并上"——此刻它们的快照确定是"全 running"。
	waitUntil(t, "两家早读的 settle 停在合并上", func() bool { return parked.arrivedCount() == 2 })

	// ② 窗口里另一件事跑完整条链：settle → review，accept → done（两条 accept 并发）。
	for i := 2; i < n; i++ {
		if err := fixture.coordinator.SettleWorkItem(ctx, WorkerRequest{WorkItemID: itemID(i)}, nil); err != nil {
			t.Fatalf("SettleWorkItem(%s): %v", itemID(i), err)
		}
	}
	var accepts sync.WaitGroup
	for i := 2; i < n; i++ {
		accepts.Add(1)
		go func(id string) {
			defer accepts.Done()
			if err := fixture.coordinator.AcceptItem(ctx, id, "验收通过"); err != nil {
				t.Errorf("AcceptItem(%s): %v", id, err)
			}
		}(itemID(i))
	}
	accepts.Wait()

	// ③ 放行那两家：它们的快照里 wi-2 / wi-3 还是 running。
	parked.open()
	early.Wait()
	close(block)

	for i := 2; i < n; i++ {
		if status := itemState(t, fixture, itemID(i)).StatusOrPending(); status != sessionstore.TeamworkItemDone {
			t.Fatalf("已 done 被并发 settle 的陈旧快照覆盖：%s 状态 = %s，want done", itemID(i), status)
		}
	}
	for i := 0; i < 2; i++ {
		assertItemStatus(t, fixture, itemID(i), sessionstore.TeamworkItemReview)
	}
	for i := 0; i < n; i++ {
		if status := itemState(t, fixture, itemID(i)).StatusOrPending(); status == sessionstore.TeamworkItemRunning {
			t.Fatalf("收口风暴之后仍有工作项停在 running：%s（settle 与 accept 混跑丢了更新）", itemID(i))
		}
	}
	if texts := fixture.queue.texts(); len(texts) != n {
		t.Fatalf("每件工作项应恰好尾插一次，得到 %d 条：%v", len(texts), texts)
	}
}
