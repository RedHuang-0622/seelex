package teamwork

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RedHuang-0622/Seele/jobs"
	"github.com/RedHuang-0622/seelex/sessionstore"
)

// ── Work Item 口吻的端口替身 ─────────────────────────────────────────
//
// 它们把"隔离（一个 Work Item 一个 Session + worktree）"与"尾插（落点 = teammate 的
// 消息队列）"两件事记下来，于是编排语义可以在没有引擎、没有 git 的用例里逐条验证。

type fakeSpaces struct {
	mu       sync.Mutex
	bound    []WorkspaceBinding
	merged   []string
	released []string
	// mergeErr 让"合并失败"这条残边可复现（插入失败 → 让 leader 亲自执行）。
	mergeErr error
	// mergeGate（可选）在"合并"这一步被调用（参数是这次合并的现场）。并发用例用它把
	// 各家 settle 的"合并"钉成确定的会合点——陈旧快照的窗口因此变成被安排好的时序，
	// 而不是靠调度碰运气（见 items_concurrent_test.go 的 rendezvous）。
	mergeGate func(binding WorkspaceBinding)
}

func (s *fakeSpaces) BindWorkspace(_ context.Context, binding WorkspaceBinding) (WorkspaceBinding, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.bound = append(s.bound, binding)
	return binding, nil
}

func (s *fakeSpaces) MergeWorkspace(_ context.Context, binding WorkspaceBinding) error {
	if s.mergeGate != nil {
		s.mergeGate(binding)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.merged = append(s.merged, binding.WorkItem)
	return s.mergeErr
}

func (s *fakeSpaces) ReleaseWorkspaceItem(_ context.Context, binding WorkspaceBinding) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.released = append(s.released, binding.WorkItem)
	return nil
}

func (s *fakeSpaces) snapshot() (bound, merged, released []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), itemIDs(s.bound)...), append([]string(nil), s.merged...), append([]string(nil), s.released...)
}

func itemIDs(bindings []WorkspaceBinding) []string {
	ids := make([]string, 0, len(bindings))
	for _, binding := range bindings {
		ids = append(ids, binding.WorkItem)
	}
	return ids
}

type fakeQueue struct {
	mu       sync.Mutex
	messages []TeammateMessage
}

func (q *fakeQueue) EnqueueTeammateMessage(_ context.Context, message TeammateMessage) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.messages = append(q.messages, message)
	return nil
}

func (q *fakeQueue) texts() []string {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := make([]string, 0, len(q.messages))
	for _, message := range q.messages {
		out = append(out, message.Text)
	}
	return out
}

// settlerHolder 断开"执行体要先有 settler、settler 又要有 Coordinator"的环。
type settlerHolder struct{ coordinator *Coordinator }

func (h *settlerHolder) SettleWorkItem(ctx context.Context, request WorkerRequest, runErr error) error {
	if h.coordinator == nil {
		return nil
	}
	return h.coordinator.SettleWorkItem(ctx, request, runErr)
}

// ── fixture ─────────────────────────────────────────────────────────

type itemFixture struct {
	coordinator *Coordinator
	store       *memoryPlanStore
	jobs        jobs.Manager
	runner      *fakeRunner
	spaces      *fakeSpaces
	queue       *fakeQueue
	calls       *callLog
}

func newItemFixture(t *testing.T, maxTeammates int) *itemFixture {
	t.Helper()
	store := &memoryPlanStore{}
	runner := &fakeRunner{}
	spaces := &fakeSpaces{}
	queue := &fakeQueue{}
	log := &callLog{}
	holder := &settlerHolder{}
	manager, err := jobs.New(
		jobs.WithExecutor(WorkerExecutor(runner, holder, 4)),
		jobs.WithLimits(jobs.Limits{InFlight: 64}),
	)
	if err != nil {
		t.Fatalf("jobs.New: %v", err)
	}
	t.Cleanup(func() { _ = manager.Close(context.Background()) })
	coordinator, err := New(Options{
		Key:          sessionstore.Key{ProjectID: "p", SessionID: "s"},
		Store:        store,
		Jobs:         manager,
		Workers:      runner,
		Worktrees:    fakeWorktrees{log: log},
		Sessions:     fakeSessions{log: log},
		Boards:       fakeBoards{log: log},
		Spaces:       spaces,
		Teammates:    queue,
		MaxTeammates: maxTeammates,
	})
	if err != nil {
		t.Fatalf("teamwork.New: %v", err)
	}
	holder.coordinator = coordinator
	return &itemFixture{coordinator: coordinator, store: store, jobs: manager, runner: runner, spaces: spaces, queue: queue, calls: log}
}

// itemPlan 是里程碑 + Work Item 口径的 V 模型计划：
//
//	m-build（屏障入口）: wi-req → wi-impl → wi-test（里程碑内 DAG）
//	m-ship （依赖 m-build）
func itemPlan() sessionstore.TeamworkPlan {
	return sessionstore.TeamworkPlan{
		TeamID:  "v-model",
		Version: 1,
		Members: []sessionstore.TeamworkMember{
			{Role: "pm"},
			{Role: "exec", ToolsPolicy: "readwrite", Permission: map[string]int{"rw": 3}},
			{Role: "test_case", ToolsPolicy: "readonly", Permission: map[string]int{"ro": 1}},
		},
		Milestones: []sessionstore.TeamworkMilestone{
			{ID: "m-build", Name: "构建"},
			{ID: "m-ship", Name: "发布", DependsOn: []string{"m-build"}},
		},
	}
}

func buildWork() []WorkItemSpec {
	return []WorkItemSpec{
		{ID: "wi-req", Role: "pm", Name: "需求澄清", Goal: "边界写清"},
		{ID: "wi-impl", Role: "exec", Name: "实现", DependsOn: []string{"wi-req"}},
		{ID: "wi-test", Role: "test_case", Name: "用例跟进", DependsOn: []string{"wi-impl"}},
	}
}

// setupItemPlan 建团队 + 给 m-build 排活。
func setupItemPlan(t *testing.T, fixture *itemFixture) {
	t.Helper()
	ctx := context.Background()
	if err := fixture.coordinator.SetPlan(ctx, itemPlan()); err != nil {
		t.Fatalf("SetPlan: %v", err)
	}
	if err := fixture.coordinator.PlanMilestone(ctx, "m-build", buildWork()); err != nil {
		t.Fatalf("PlanMilestone: %v", err)
	}
}

func itemState(t *testing.T, fixture *itemFixture, id string) sessionstore.TeamworkWorkItem {
	t.Helper()
	views, err := fixture.coordinator.Items(context.Background())
	if err != nil {
		t.Fatalf("Items: %v", err)
	}
	for _, view := range views {
		if view.Item.ID == id {
			return view.Item
		}
	}
	t.Fatalf("工作项 %q 不在计划里", id)
	return sessionstore.TeamworkWorkItem{}
}

// waitRequests 等到执行体收下至少 want 次派发（派发是后台的，载荷到达有先后）。
func waitRequests(t *testing.T, fixture *itemFixture, want int) []WorkerRequest {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		requests := fixture.runner.requestsSnapshot()
		if len(requests) >= want {
			return requests
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("执行体在窗口内只收到 %d 次派发，want ≥ %d", len(fixture.runner.requestsSnapshot()), want)
	return nil
}

func waitItemStatus(t *testing.T, fixture *itemFixture, id, want string) sessionstore.TeamworkWorkItem {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		item := itemState(t, fixture, id)
		if item.StatusOrPending() == want {
			return item
		}
		time.Sleep(2 * time.Millisecond)
	}
	item := itemState(t, fixture, id)
	t.Fatalf("工作项 %s 状态 = %s，want %s（窗口内没收敛）", id, item.StatusOrPending(), want)
	return sessionstore.TeamworkWorkItem{}
}

// ── 用例 4：分里程碑排活（不是一次把全程铺好）─────────────────────────

func TestPlanMilestoneRefusesMilestoneWhoseDependencyIsNotDone(t *testing.T) {
	fixture := newItemFixture(t, 6)
	setupItemPlan(t, fixture)
	err := fixture.coordinator.PlanMilestone(context.Background(), "m-ship", []WorkItemSpec{
		{ID: "wi-ship", Role: "exec", Name: "发布"},
	})
	if err == nil || !strings.Contains(err.Error(), "还没完成") {
		t.Fatalf("上一个里程碑没完成就给下一个排活，必须被拒，得到 %v", err)
	}
}

func TestNextMilestoneOpensOnlyAfterEveryItemIsDone(t *testing.T) {
	fixture := newItemFixture(t, 6)
	setupItemPlan(t, fixture)
	ctx := context.Background()
	// 派发并**验收** m-build 的三件事。
	order := []string{"wi-req", "wi-impl", "wi-test"}
	for _, id := range order {
		if _, err := fixture.coordinator.DispatchItem(ctx, id); err != nil {
			t.Fatalf("DispatchItem(%s): %v", id, err)
		}
		waitItemStatus(t, fixture, id, sessionstore.TeamworkItemReview)
		if err := fixture.coordinator.AcceptItem(ctx, id, "验收通过"); err != nil {
			t.Fatalf("AcceptItem(%s): %v", id, err)
		}
	}
	plan, err := fixture.coordinator.Plan(ctx)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if plan.Milestones[0].Status != sessionstore.TeamworkMilestoneDone {
		t.Fatalf("全部工作项验收通过后，里程碑应 done: %+v", plan.Milestones[0])
	}
	if plan.Milestones[1].Status != sessionstore.TeamworkMilestoneActive {
		t.Fatalf("屏障之后下一个里程碑应转 active: %+v", plan.Milestones[1])
	}
	// 现在才轮得到给 m-ship 排活。
	if err := fixture.coordinator.PlanMilestone(ctx, "m-ship", []WorkItemSpec{{ID: "wi-ship", Role: "exec", Name: "发布"}}); err != nil {
		t.Fatalf("屏障打开后给下一个里程碑排活应当被受理: %v", err)
	}
}

func TestDispatchRefusesItemInMilestoneBehindTheBarrier(t *testing.T) {
	fixture := newItemFixture(t, 6)
	setupItemPlan(t, fixture)
	ctx := context.Background()
	// 直接往 m-ship 里塞一件事（绕过排活闸门，模拟计划被手改），派发仍必须被拒。
	plan, err := fixture.coordinator.Plan(ctx)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	plan.Milestones[1].Items = []sessionstore.TeamworkWorkItem{{ID: "wi-ship", Milestone: "m-ship", Role: "exec", Name: "发布"}}
	if err := fixture.store.WritePlan(ctx, fixture.coordinator.Key(), plan, 6); err != nil {
		t.Fatalf("WritePlan: %v", err)
	}
	if _, err := fixture.coordinator.DispatchItem(ctx, "wi-ship"); err == nil || !strings.Contains(err.Error(), "还没完成") {
		t.Fatalf("屏障没开就派发下一个里程碑的工作必须被拒，得到 %v", err)
	}
}

// ── 用例 5：里程碑内部的依赖（V 模型 exec → test）─────────────────────

func TestDispatchRefusesItemWhoseDependencyIsNotDone(t *testing.T) {
	fixture := newItemFixture(t, 6)
	setupItemPlan(t, fixture)
	ctx := context.Background()
	if _, err := fixture.coordinator.DispatchItem(ctx, "wi-test"); err == nil || !strings.Contains(err.Error(), "还没完成") {
		t.Fatalf("前置工作项没完成就派发必须被拒（V 模型：exec 做完 test 才能跟上），得到 %v", err)
	}
}

func TestDependencyChainReleasesStepByStep(t *testing.T) {
	fixture := newItemFixture(t, 6)
	setupItemPlan(t, fixture)
	ctx := context.Background()
	// wi-req 没有前置：可以直接派。
	if _, err := fixture.coordinator.DispatchItem(ctx, "wi-req"); err != nil {
		t.Fatalf("DispatchItem(wi-req): %v", err)
	}
	waitItemStatus(t, fixture, "wi-req", sessionstore.TeamworkItemReview)
	// 还没验收（仍是 review）：wi-impl 的闸门不该开。
	if _, err := fixture.coordinator.DispatchItem(ctx, "wi-impl"); err == nil || !strings.Contains(err.Error(), "还没完成") {
		t.Fatalf("前置还在待验收时下游必须被闸住，得到 %v", err)
	}
	if err := fixture.coordinator.AcceptItem(ctx, "wi-req", "通过"); err != nil {
		t.Fatalf("AcceptItem: %v", err)
	}
	if _, err := fixture.coordinator.DispatchItem(ctx, "wi-impl"); err != nil {
		t.Fatalf("前置验收通过后下游应当可派: %v", err)
	}
}

// ── 用例 6：一个 teammate 在一个里程碑里承担多件事，各自一套 Session + worktree ──

func TestTeammateRunsMultipleItemsEachWithItsOwnSessionAndWorktree(t *testing.T) {
	fixture := newItemFixture(t, 6)
	ctx := context.Background()
	if err := fixture.coordinator.SetPlan(ctx, itemPlan()); err != nil {
		t.Fatalf("SetPlan: %v", err)
	}
	// exec 在同一个里程碑里承担两件互不依赖的事。
	if err := fixture.coordinator.PlanMilestone(ctx, "m-build", []WorkItemSpec{
		{ID: "wi-a", Role: "exec", Name: "实现 A"},
		{ID: "wi-b", Role: "exec", Name: "实现 B"},
	}); err != nil {
		t.Fatalf("PlanMilestone: %v", err)
	}
	if _, err := fixture.coordinator.DispatchItem(ctx, "wi-a"); err != nil {
		t.Fatalf("DispatchItem(wi-a): %v", err)
	}
	waitItemStatus(t, fixture, "wi-a", sessionstore.TeamworkItemReview)
	if err := fixture.coordinator.AcceptItem(ctx, "wi-a", "通过"); err != nil {
		t.Fatalf("AcceptItem(wi-a): %v", err)
	}
	if _, err := fixture.coordinator.DispatchItem(ctx, "wi-b"); err != nil {
		t.Fatalf("DispatchItem(wi-b): %v", err)
	}
	itemA := itemState(t, fixture, "wi-a")
	itemB := itemState(t, fixture, "wi-b")
	// 验收**只销状态**：A 的现场与会话指针留着（归 team 托管，收口时才拆）。
	if itemA.StatusOrPending() != sessionstore.TeamworkItemDone {
		t.Fatalf("A 应当已验收: %+v", itemA)
	}
	if itemA.SessionID == "" || itemA.Worktree == "" {
		t.Fatalf("验收不该抹掉现场指针（现场活到 team_close）: %+v", itemA)
	}
	if itemB.SessionID == "" || itemB.Worktree == "" {
		t.Fatalf("B 应当有自己的会话与工作区: %+v", itemB)
	}
	if itemB.SessionID == itemA.SessionID || itemB.Worktree == itemA.Worktree {
		t.Fatalf("两件事不能共用一套会话/现场：A=%+v B=%+v", itemA, itemB)
	}
	bound, _, released := fixture.spaces.snapshot()
	if len(bound) != 2 {
		t.Fatalf("两件事各建一次工作区，得到 %v", bound)
	}
	if bound[0] == bound[1] {
		t.Fatalf("两件事的工作区不能同名：%v", bound)
	}
	if len(released) != 0 {
		t.Fatalf("验收不再是回收点（唯一入口是整队收口），却释放了 %v", released)
	}
	// 唯一回收点：收口把两件事的现场一并结束（先把 B 的事落定，收口闸门才放行）。
	waitItemStatus(t, fixture, "wi-b", sessionstore.TeamworkItemReview)
	if _, err := fixture.coordinator.Close(ctx); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, _, released = fixture.spaces.snapshot(); len(released) != 2 {
		t.Fatalf("整队收口应把两件事的现场一并结束，得到 %v", released)
	}
	// 校验会话号按 (主会话, team, 角色, work_item) 派生。
	want := WorkItemSessionID(fixture.coordinator.derive, "s", "v-model", "exec", "wi-b")
	if itemB.SessionID != want {
		t.Fatalf("B 的会话号 = %q, want %q", itemB.SessionID, want)
	}
}

// ── 用例 3：后台执行（受理即返回，不占着 leader）──────────────────────

func TestDispatchItemReturnsBeforeTheRoundFinishes(t *testing.T) {
	fixture := newItemFixture(t, 6)
	setupItemPlan(t, fixture)
	block := make(chan struct{})
	fixture.runner.block = block
	ctx := context.Background()
	start := time.Now()
	handle, err := fixture.coordinator.DispatchItem(ctx, "wi-req")
	if err != nil {
		t.Fatalf("DispatchItem: %v", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("派发必须受理即返回（后台执行），实际阻塞了 %s", elapsed)
	}
	record, ok := fixture.jobs.Observe(handle)
	if !ok || !record.Running() {
		t.Fatalf("派发之后作业应当在跑（后台），得到 %+v ok=%v", record, ok)
	}
	item := itemState(t, fixture, "wi-req")
	if item.StatusOrPending() != sessionstore.TeamworkItemRunning {
		t.Fatalf("派发之后工作项应当是 running，得到 %s", item.StatusOrPending())
	}
	close(block)
	waitItemStatus(t, fixture, "wi-req", sessionstore.TeamworkItemReview)
}

// ── 用例 2：worktree / Session 生命周期（归 team 托管，收口才结束）────────

func TestWorktreeAndSessionSurviveUntilTeamClose(t *testing.T) {
	fixture := newItemFixture(t, 6)
	setupItemPlan(t, fixture)
	block := make(chan struct{})
	fixture.runner.block = block
	ctx := context.Background()
	if _, err := fixture.coordinator.DispatchItem(ctx, "wi-req"); err != nil {
		t.Fatalf("DispatchItem: %v", err)
	}
	// 在跑期间：退场被拒（现场是这件事的证据），绑定还在。
	if err := fixture.coordinator.Retire(ctx, "pm"); err == nil || !strings.Contains(err.Error(), "没落定") {
		t.Fatalf("还有在跑的工作项时退场必须被拒，得到 %v", err)
	}
	_, _, released := fixture.spaces.snapshot()
	if len(released) != 0 {
		t.Fatalf("在跑期间不该有任何工作区被释放: %v", released)
	}
	// 跑完 → 合并（尾插）→ 现场仍在，等 leader 验收。
	close(block)
	waitItemStatus(t, fixture, "wi-req", sessionstore.TeamworkItemReview)
	_, _, released = fixture.spaces.snapshot()
	if len(released) != 0 {
		t.Fatalf("跑完但还没验收，工作区不该提前挂掉: %v", released)
	}
	// 验收通过 → **仍然不动现场**：teammate 的现场与会话归 team 托管。
	if err := fixture.coordinator.AcceptItem(ctx, "wi-req", "通过"); err != nil {
		t.Fatalf("AcceptItem: %v", err)
	}
	_, _, released = fixture.spaces.snapshot()
	if len(released) != 0 {
		t.Fatalf("验收不再是回收点（现场活到整队收口），却释放了 %v", released)
	}
	// 整队收口 = 唯一回收点：现场 + 会话内容一并结束。
	if _, err := fixture.coordinator.Close(ctx); err != nil {
		t.Fatalf("Close: %v", err)
	}
	_, _, released = fixture.spaces.snapshot()
	if len(released) != 1 || released[0] != "wi-req" {
		t.Fatalf("整队收口应当释放这件事的现场: %v", released)
	}
	calls := fixture.calls.snapshot()
	found := false
	for _, call := range calls {
		if strings.HasPrefix(call, "session:") && strings.HasSuffix(call, "-wi-wi-req") {
			found = true
		}
	}
	if !found {
		t.Fatalf("整队收口应当清这件事的会话内容: %v", calls)
	}
}

func TestTeamCloseEndsEveryLiveBinding(t *testing.T) {
	fixture := newItemFixture(t, 6)
	setupItemPlan(t, fixture)
	block := make(chan struct{})
	fixture.runner.block = block
	ctx := context.Background()
	if _, err := fixture.coordinator.DispatchItem(ctx, "wi-req"); err != nil {
		t.Fatalf("DispatchItem(wi-req): %v", err)
	}
	if _, err := fixture.coordinator.DispatchItem(ctx, "wi-impl"); err != nil {
		// wi-impl 依赖 wi-req：此刻还没 done，闸门照旧拦它——本用例只用 wi-req。
		t.Logf("下游被依赖闸门拦下（符合预期）：%v", err)
	}
	// leader 想中途提前 team_done：**在跑时会被收口闸门拦下**（2026-10-04 口径——收口是
	// 唯一会拆 per-item 现场的地方，"还在被尾插使用的现场"不许被它静默拆掉；见
	// TestCloseRefusesWhileItemRunning）。先把这一件落定，再收口：活着的绑定到此为止。
	if _, err := fixture.coordinator.Close(ctx); err == nil {
		t.Fatal("还在跑时收口必须被拦（现场正被尾插使用）")
	}
	close(block)
	waitItemStatus(t, fixture, "wi-req", sessionstore.TeamworkItemReview)
	alreadyClosed, err := fixture.coordinator.Close(ctx)
	if err != nil {
		t.Fatalf("Close: %v", err)
	}
	if alreadyClosed {
		t.Fatal("首次收口不得报 already_closed")
	}
	_, _, released := fixture.spaces.snapshot()
	if len(released) != 1 || released[0] != "wi-req" {
		t.Fatalf("整队收口应当把活着的绑定一并结束: %v", released)
	}
	report, err := fixture.coordinator.Recover(ctx)
	if err != nil {
		t.Fatalf("Recover: %v", err)
	}
	if len(report.Bindings) != 0 {
		t.Fatalf("收口之后不该还有活绑定: %+v", report.Bindings)
	}
}

// ── 用例 9：中断恢复（额度不够 / 重启）────────────────────────────────

func TestRecoverReportsInterruptedItemsAndKeepsTheirMemory(t *testing.T) {
	fixture := newItemFixture(t, 6)
	setupItemPlan(t, fixture)
	block := make(chan struct{})
	fixture.runner.block = block
	ctx := context.Background()
	if _, err := fixture.coordinator.DispatchItem(ctx, "wi-req"); err != nil {
		t.Fatalf("DispatchItem: %v", err)
	}
	before := itemState(t, fixture, "wi-req")

	// 模拟"重启"：换一个 Coordinator 抱着同一份持久面（计划 + 绑定账本），
	// 作业句柄只在内存（jobs I-4），所以新进程的表里一个都查不到。
	restarted := newItemFixtureOver(t, fixture)
	report, err := restarted.coordinator.Recover(ctx)
	if err != nil {
		t.Fatalf("Recover: %v", err)
	}
	if len(report.Interrupted) != 1 || report.Interrupted[0] != "wi-req" {
		t.Fatalf("重启后应报出可重派的工作项，得到 %+v", report)
	}
	if len(report.Bindings) != 1 || report.Bindings[0].SessionID != before.SessionID {
		t.Fatalf("中断恢复必须保住绑定（Session 与 worktree 都还在）: %+v", report.Bindings)
	}

	// 重派：**复用原会话号**——上下文记忆靠它建回来。
	if _, err := restarted.coordinator.DispatchItem(ctx, "wi-req"); err != nil {
		t.Fatalf("恢复之后重派应当被受理: %v", err)
	}
	after := itemState(t, restarted, "wi-req")
	if after.SessionID != before.SessionID {
		t.Fatalf("重派必须复用原会话号（否则上下文记忆就丢了）: %q → %q", before.SessionID, after.SessionID)
	}
	close(block)
}

// newItemFixtureOver 复用另一份 fixture 的**持久面**（计划 + 绑定账本 + 消息队列），
// 换一套内存态（作业表、执行体）——这就是"重启"的最小复现。
func newItemFixtureOver(t *testing.T, source *itemFixture) *itemFixture {
	t.Helper()
	runner := &fakeRunner{}
	spaces := source.spaces
	queue := source.queue
	log := &callLog{}
	holder := &settlerHolder{}
	manager, err := jobs.New(
		jobs.WithExecutor(WorkerExecutor(runner, holder, 4)),
		jobs.WithLimits(jobs.Limits{InFlight: 64}),
	)
	if err != nil {
		t.Fatalf("jobs.New: %v", err)
	}
	t.Cleanup(func() { _ = manager.Close(context.Background()) })
	coordinator, err := New(Options{
		Key:          sessionstore.Key{ProjectID: "p", SessionID: "s"},
		Store:        source.store,
		Jobs:         manager,
		Workers:      runner,
		Worktrees:    fakeWorktrees{log: log},
		Sessions:     fakeSessions{log: log},
		Boards:       fakeBoards{log: log},
		Spaces:       spaces,
		Teammates:    queue,
		MaxTeammates: 6,
	})
	if err != nil {
		t.Fatalf("teamwork.New: %v", err)
	}
	holder.coordinator = coordinator
	return &itemFixture{coordinator: coordinator, store: source.store, jobs: manager, runner: runner, spaces: spaces, queue: queue, calls: log}
}

// ── 用例 8：未开始的工作可调整，已开始/已结束的是既定的 ────────────────

func TestAdjustItemRefusesStartedAndFinishedWork(t *testing.T) {
	fixture := newItemFixture(t, 6)
	setupItemPlan(t, fixture)
	ctx := context.Background()

	// 未开始：可以调整（换人 / 改名 / 改目标 / 改依赖）。
	if err := fixture.coordinator.AdjustItem(ctx, "wi-impl", WorkItemSpec{
		Role: "pm", Name: "实现（改派）", Goal: "换个人做",
	}); err != nil {
		t.Fatalf("未开始的工作项应当可调整: %v", err)
	}
	adjusted := itemState(t, fixture, "wi-impl")
	if adjusted.Role != "pm" || adjusted.Name != "实现（改派）" {
		t.Fatalf("调整没生效: %+v", adjusted)
	}

	// 已开始：既定，不可调整。
	if _, err := fixture.coordinator.DispatchItem(ctx, "wi-req"); err != nil {
		t.Fatalf("DispatchItem: %v", err)
	}
	if err := fixture.coordinator.AdjustItem(ctx, "wi-req", WorkItemSpec{Name: "偷改"}); err == nil || !strings.Contains(err.Error(), "既定的") {
		t.Fatalf("已经开始的工作项必须拒绝调整，得到 %v", err)
	}
	waitItemStatus(t, fixture, "wi-req", sessionstore.TeamworkItemReview)
	if err := fixture.coordinator.AcceptItem(ctx, "wi-req", "通过"); err != nil {
		t.Fatalf("AcceptItem: %v", err)
	}
	// 已结束：同样是既定事实。
	if err := fixture.coordinator.AdjustItem(ctx, "wi-req", WorkItemSpec{Name: "改历史"}); err == nil || !strings.Contains(err.Error(), "既定的") {
		t.Fatalf("已经完成的工作项必须拒绝调整，得到 %v", err)
	}
}

// ── 尾插：先合并、成功才插入；合并失败也要插入，并写明让 leader 亲自执行 ──

func TestSettleMergesTheWorktreeBeforeEnqueuing(t *testing.T) {
	fixture := newItemFixture(t, 6)
	setupItemPlan(t, fixture)
	ctx := context.Background()
	if _, err := fixture.coordinator.DispatchItem(ctx, "wi-req"); err != nil {
		t.Fatalf("DispatchItem: %v", err)
	}
	waitItemStatus(t, fixture, "wi-req", sessionstore.TeamworkItemReview)
	_, merged, _ := fixture.spaces.snapshot()
	if len(merged) != 1 || merged[0] != "wi-req" {
		t.Fatalf("尾插之前必须先合并这件事的 worktree: %v", merged)
	}
	texts := fixture.queue.texts()
	if len(texts) != 1 {
		t.Fatalf("尾插应当恰好插一条（幂等）: %v", texts)
	}
	if !strings.Contains(texts[0], "已合并") {
		t.Fatalf("合并成功时尾插正文应当说明改动已合并: %q", texts[0])
	}
	// 幂等：再 settle 一次不追加第二条。
	if err := fixture.coordinator.SettleWorkItem(ctx, WorkerRequest{WorkItemID: "wi-req"}, nil); err != nil {
		t.Fatalf("SettleWorkItem（重复）: %v", err)
	}
	if texts = fixture.queue.texts(); len(texts) != 1 {
		t.Fatalf("尾插必须恰好一次，得到 %d 条：%v", len(texts), texts)
	}
}

func TestSettleReportsMergeFailureAndAsksTheLeader(t *testing.T) {
	fixture := newItemFixture(t, 6)
	setupItemPlan(t, fixture)
	fixture.spaces.mergeErr = errors.New("merge conflict in main.go")
	ctx := context.Background()
	if _, err := fixture.coordinator.DispatchItem(ctx, "wi-req"); err != nil {
		t.Fatalf("DispatchItem: %v", err)
	}
	item := waitItemStatus(t, fixture, "wi-req", sessionstore.TeamworkItemFailed)
	texts := fixture.queue.texts()
	if len(texts) != 1 {
		t.Fatalf("合并失败时也要插入（说明失败），得到 %v", texts)
	}
	if !strings.Contains(texts[0], "插入失败") || !strings.Contains(texts[0], "leader") {
		t.Fatalf("合并失败时尾插正文必须说明失败并让 leader 亲自执行: %q", texts[0])
	}
	if !strings.Contains(item.Note, "merge conflict in main.go") {
		t.Fatalf("bug 原文必须落到工作项上: %q", item.Note)
	}
	if !itemHasLiveBinding(t, fixture, item.ID) {
		t.Fatal("合并失败时现场必须保留（可重派/人工处置）")
	}
}

// itemHasLiveBinding 读出这件事当前是否还有活绑定（测试助手）。
func itemHasLiveBinding(t *testing.T, fixture *itemFixture, id string) bool {
	t.Helper()
	views, err := fixture.coordinator.Items(context.Background())
	if err != nil {
		t.Fatalf("Items: %v", err)
	}
	for _, view := range views {
		if view.Item.ID == id {
			return view.Live
		}
	}
	return false
}

func TestSettlePrintsTheRunErrorIntoTheTeammateMessage(t *testing.T) {
	fixture := newItemFixture(t, 6)
	setupItemPlan(t, fixture)
	ctx := context.Background()
	if _, err := fixture.coordinator.DispatchItem(ctx, "wi-req"); err != nil {
		t.Fatalf("DispatchItem: %v", err)
	}
	// 直接驱动尾插（模拟执行体带着错误收尾）。
	if err := fixture.coordinator.SettleWorkItem(ctx, WorkerRequest{WorkItemID: "wi-req"}, errors.New("provider 402: quota exhausted")); err != nil {
		t.Fatalf("SettleWorkItem: %v", err)
	}
	texts := fixture.queue.texts()
	if len(texts) != 1 || !strings.Contains(texts[0], "quota exhausted") {
		t.Fatalf("bug 必须直接打印进 teammate 的消息: %v", texts)
	}
	item := itemState(t, fixture, "wi-req")
	if item.StatusOrPending() != sessionstore.TeamworkItemFailed {
		t.Fatalf("带错误收尾应当判失败（可重派），得到 %s", item.StatusOrPending())
	}
}

// ── 派发口径：权责随工作项走（越权由权限面在执行判定上拒）─────────────

func TestDispatchItemCarriesTheOwnersPermissionScope(t *testing.T) {
	fixture := newItemFixture(t, 6)
	setupItemPlan(t, fixture)
	ctx := context.Background()
	if _, err := fixture.coordinator.DispatchItem(ctx, "wi-req"); err != nil {
		t.Fatalf("DispatchItem: %v", err)
	}
	requests := waitRequests(t, fixture, 1)
	request := requests[0]
	if request.Subject != "emp_pm" || request.Role != "pm" {
		t.Fatalf("派发必须以该角色的主体运行（emp_<role>）: %+v", request)
	}
	if request.WorkItemID != "wi-req" || request.Milestone != "m-build" {
		t.Fatalf("载荷必须带上是哪一件工作: %+v", request)
	}

	// 换一个**只读**角色：权责口径原样透传（位由 leader 逐格下发，越权在执行判定上拒）。
	if err := fixture.coordinator.PlanMilestone(ctx, "m-build", []WorkItemSpec{
		{ID: "wi-ro", Role: "test_case", Name: "只读复核"},
	}); err != nil {
		t.Fatalf("PlanMilestone: %v", err)
	}
	if _, err := fixture.coordinator.DispatchItem(ctx, "wi-ro"); err != nil {
		t.Fatalf("DispatchItem(wi-ro): %v", err)
	}
	requests = waitRequests(t, fixture, 2)
	last := requests[len(requests)-1]
	if last.ToolsPolicy != "readonly" {
		t.Fatalf("只读角色的权责档必须随工作项下发: %+v", last)
	}
	if last.PermissionGroups["ro"] != 1 {
		t.Fatalf("逐格权限必须折成位图随工作项下发: %+v", last.PermissionGroups)
	}
	if last.Worktree == "" || !strings.Contains(last.Worktree, "wi-ro") {
		t.Fatalf("每件事一个 worktree（指派名里带工作项 id）: %q", last.Worktree)
	}
}

// ── 收口：中途提前 team_done ────────────────────────────────────────

func TestCloseIsIdempotentAndKeepsThePlanFacts(t *testing.T) {
	fixture := newItemFixture(t, 6)
	setupItemPlan(t, fixture)
	ctx := context.Background()
	if _, err := fixture.coordinator.DispatchItem(ctx, "wi-req"); err != nil {
		t.Fatalf("DispatchItem: %v", err)
	}
	waitItemStatus(t, fixture, "wi-req", sessionstore.TeamworkItemReview)
	if _, err := fixture.coordinator.Close(ctx); err != nil {
		t.Fatalf("Close: %v", err)
	}
	plan, err := fixture.coordinator.Plan(ctx)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if plan.State.State != sessionstore.TeamworkStateClosed {
		t.Fatalf("收口后计划必须标 closed: %+v", plan.State)
	}
	// 收口不改工作项的结论（事实不因收口被改写）。
	if item := itemState(t, fixture, "wi-req"); item.StatusOrPending() != sessionstore.TeamworkItemReview {
		t.Fatalf("收口不该改写工作项的结论: %+v", item)
	}
	if alreadyClosed, err := fixture.coordinator.Close(ctx); err != nil || !alreadyClosed {
		t.Fatalf("重复收口应当幂等: alreadyClosed=%v err=%v", alreadyClosed, err)
	}
}
