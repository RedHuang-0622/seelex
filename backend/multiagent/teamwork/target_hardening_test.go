package teamwork

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/RedHuang-0622/Seele/jobs"
	"github.com/RedHuang-0622/seelex/store"
)

// TestClosedMilestoneRejectsWorkAdjustmentAndDispatch 钉住已收口里程碑的唯一闸门：
// team_work / team_item / team_dispatch 三条写路都必须拒绝，只读看板不受影响。
func TestClosedMilestoneRejectsWorkAdjustmentAndDispatch(t *testing.T) {
	fixture := newItemFixture(t, 6)
	ctx := context.Background()
	plan := itemPlan()
	if err := fixture.coordinator.SetPlan(ctx, plan); err != nil {
		t.Fatalf("SetPlan: %v", err)
	}
	plan, err := fixture.coordinator.Plan(ctx)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	plan.Milestones[0].Status = store.TeamworkMilestoneDone
	plan.Milestones[0].Items = []store.TeamworkWorkItem{{
		ID: "wi-closed", Milestone: "m-build", Role: "exec", Name: "已收口里的遗留项",
		Status: store.TeamworkItemPending,
	}}
	if err := fixture.st.WritePlan(ctx, fixture.coordinator.Key(), plan, 6); err != nil {
		t.Fatalf("WritePlan: %v", err)
	}

	if err := fixture.coordinator.PlanMilestone(ctx, "m-build", []WorkItemSpec{{
		ID: "wi-new", Role: "exec", Name: "追加工作",
	}}); err == nil || !strings.Contains(err.Error(), "已收口") {
		t.Fatalf("team_work 必须在已收口里程碑上被拒，得到 %v", err)
	}
	if err := fixture.coordinator.AdjustItem(ctx, "wi-closed", WorkItemSpec{Name: "偷改"}); err == nil || !strings.Contains(err.Error(), "已收口") {
		t.Fatalf("team_item 必须在已收口里程碑上被拒，得到 %v", err)
	}
	if _, err := fixture.coordinator.DispatchItem(ctx, "wi-closed"); err == nil || !strings.Contains(err.Error(), "已收口") {
		t.Fatalf("team_dispatch 必须在已收口里程碑上被拒，得到 %v", err)
	}
	if _, err := fixture.coordinator.Items(ctx); err != nil {
		t.Fatalf("已收口里程碑仍必须可读: %v", err)
	}
}

// TestAcceptReleasesOnlyTheWorkItemWorktree 钉住 accept / close 的生命周期边界：
// accept 释放该 WorkItem 的 worktree，但不重置 Session；close 才重置角色与 WorkItem Session。
func TestAcceptReleasesOnlyTheWorkItemWorktree(t *testing.T) {
	fixture := newItemFixture(t, 6)
	setupItemPlan(t, fixture)
	ctx := context.Background()
	if _, err := fixture.coordinator.DispatchItem(ctx, "wi-req"); err != nil {
		t.Fatalf("DispatchItem: %v", err)
	}
	waitItemStatus(t, fixture, "wi-req", store.TeamworkItemReview)
	if err := fixture.coordinator.AcceptItem(ctx, "wi-req", "通过"); err != nil {
		t.Fatalf("AcceptItem: %v", err)
	}
	_, _, released := fixture.spaces.snapshot()
	if len(released) != 1 || released[0] != "wi-req" {
		t.Fatalf("accept 只应释放该 WorkItem 的 worktree，得到 %v", released)
	}
	for _, call := range fixture.calls.snapshot() {
		if strings.Contains(call, "-wi-wi-req") {
			t.Fatalf("accept 不得清 WorkItem Session，实际调用 %v", fixture.calls.snapshot())
		}
	}

	if _, err := fixture.coordinator.Close(ctx); err != nil {
		t.Fatalf("Close: %v", err)
	}
	calls := fixture.calls.snapshot()
	var roleReset, itemReset bool
	for _, call := range calls {
		if call == "session:s-v-model-pm" {
			roleReset = true
		}
		if strings.Contains(call, "-wi-wi-req") {
			itemReset = true
		}
	}
	if !roleReset || !itemReset {
		t.Fatalf("close 必须清角色级与 WorkItem 级 Session，calls=%v", calls)
	}
}

// TestAcceptCleanupFailureIsIdempotentlyRetryable 钉住验收先落 done、再清理的
// 中间态：清理失败时 WorkItem 已是 done 且 Session 仍保留；再次 accept 只重试清理。
func TestAcceptCleanupFailureIsIdempotentlyRetryable(t *testing.T) {
	fixture := newItemFixture(t, 6)
	setupItemPlan(t, fixture)
	ctx := context.Background()
	if _, err := fixture.coordinator.DispatchItem(ctx, "wi-req"); err != nil {
		t.Fatalf("DispatchItem: %v", err)
	}
	waitItemStatus(t, fixture, "wi-req", store.TeamworkItemReview)
	sessionID := itemState(t, fixture, "wi-req").SessionID
	fixture.spaces.setReleaseError(errors.New("worktree busy"))

	err := fixture.coordinator.AcceptItem(ctx, "wi-req", "通过")
	if !IsAcceptedWithCleanupPending(err) {
		t.Fatalf("first accept must expose accepted-with-cleanup-pending, got %v", err)
	}
	if item := itemState(t, fixture, "wi-req"); item.StatusOrPending() != store.TeamworkItemDone {
		t.Fatalf("done must be persisted before cleanup, got %+v", item)
	}
	for _, call := range fixture.calls.snapshot() {
		if call == "session:"+sessionID {
			t.Fatalf("accept cleanup must never reset the WorkItem Session, calls=%v", fixture.calls.snapshot())
		}
	}

	fixture.spaces.setReleaseError(nil)
	if err := fixture.coordinator.AcceptItem(ctx, "wi-req", "通过"); err != nil {
		t.Fatalf("cleanup retry must succeed: %v", err)
	}
	_, _, released := fixture.spaces.snapshot()
	if len(released) < 2 || released[len(released)-1] != "wi-req" {
		t.Fatalf("retry must target the same WorkItem worktree, released=%v", released)
	}
}

// recordingJobs 让派发事务的顺序可观察，同时保留真实 jobs.Manager 的执行语义。
type recordingJobs struct {
	jobs.Manager
	mu            sync.Mutex
	calls         *callLog
	store         *memoryPlanStore
	key           store.Key
	startSnapshot func()
}

func (m *recordingJobs) record(call string) {
	if m.calls != nil {
		m.calls.record(call)
	}
}

func (m *recordingJobs) Dispatch(ctx context.Context, spec jobs.Spec) (jobs.Handle, error) {
	m.record("dispatch")
	return m.Manager.Dispatch(ctx, spec)
}

func (m *recordingJobs) Declare(ctx context.Context, spec jobs.Spec) (jobs.Handle, error) {
	m.record("declare")
	return m.Manager.Declare(ctx, spec)
}

func (m *recordingJobs) Start(ctx context.Context, handle jobs.Handle) error {
	m.record("start")
	if m.startSnapshot != nil {
		m.startSnapshot()
	}
	return m.Manager.Start(ctx, handle)
}

// orderedPlanStore 把持久化写点记进同一份顺序日志。
type orderedPlanStore struct {
	*memoryPlanStore
	calls *callLog
}

func (s orderedPlanStore) WritePlan(ctx context.Context, key store.Key, plan store.TeamworkPlan, maxTeammates int) error {
	s.calls.record("plan")
	return s.memoryPlanStore.WritePlan(ctx, key, plan, maxTeammates)
}

func (s orderedPlanStore) AppendBinding(ctx context.Context, key store.Key, binding store.TeamworkBinding) error {
	s.calls.record("binding")
	return s.memoryPlanStore.AppendBinding(ctx, key, binding)
}

func (s orderedPlanStore) AppendEvent(ctx context.Context, key store.Key, event store.TeamworkEvent) error {
	s.calls.record("audit:" + event.Kind)
	return s.memoryPlanStore.AppendEvent(ctx, key, event)
}

// TestDispatchUsesDeclarePersistThenStart 钉住派发事务屏障：执行体启动时，
// plan / binding / audit 必须已经可见；Start 之前 executor 不得运行。
func TestDispatchUsesDeclarePersistThenStart(t *testing.T) {
	ctx := context.Background()
	memory := &memoryPlanStore{}
	calls := &callLog{}
	runner := &fakeRunner{}
	holder := &settlerHolder{}
	manager, err := jobs.New(
		jobs.WithNamer(testNamer{}),
		jobs.WithExecutor(WorkerExecutor(runner, holder, 4)),
		jobs.WithLimits(jobs.Limits{InFlight: 64}),
	)
	if err != nil {
		t.Fatalf("jobs.New: %v", err)
	}
	t.Cleanup(func() { _ = manager.Close(context.Background()) })
	key := store.Key{ProjectID: "p", SessionID: "s"}
	ordered := orderedPlanStore{memoryPlanStore: memory, calls: calls}
	recorder := &recordingJobs{Manager: manager, calls: calls, store: memory, key: key}
	spaces := &fakeSpaces{}
	coordinator, err := New(Options{
		Key: key, Store: ordered, Jobs: recorder, Workers: runner,
		Worktrees: fakeWorktrees{log: calls}, Sessions: fakeSessions{log: calls},
		Spaces: spaces, Teammates: &fakeQueue{}, MaxTeammates: 6,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	holder.coordinator = coordinator
	if err := coordinator.SetPlan(ctx, itemPlan()); err != nil {
		t.Fatalf("SetPlan: %v", err)
	}
	if err := coordinator.PlanMilestone(ctx, "m-build", []WorkItemSpec{{
		ID: "wi-order", Role: "exec", Name: "顺序",
	}}); err != nil {
		t.Fatalf("PlanMilestone: %v", err)
	}
	calls.mu.Lock()
	calls.calls = nil
	calls.mu.Unlock()
	recorder.startSnapshot = func() {
		plan, readErr := memory.ReadPlan(ctx, key)
		if readErr != nil {
			t.Errorf("Start 时读计划失败: %v", readErr)
			return
		}
		_, item, ok := findItem(&plan, "wi-order")
		if !ok || item.StatusOrPending() != store.TeamworkItemRunning {
			t.Errorf("Start 时计划尚未写 running: %+v", item)
		}
		bindings, readErr := memory.ReadBindings(ctx, key)
		if readErr != nil || len(bindings) == 0 {
			t.Errorf("Start 时绑定尚未写: bindings=%+v err=%v", bindings, readErr)
		}
		events, readErr := memory.ReadEvents(ctx, key)
		if readErr != nil {
			t.Errorf("Start 时读审计失败: %v", readErr)
			return
		}
		dispatched := false
		for _, event := range events {
			if event.Kind == store.TeamworkEventDispatch && event.WorkItem == "wi-order" {
				dispatched = true
			}
		}
		if !dispatched {
			t.Errorf("Start 时派发审计尚未写: %+v", events)
		}
	}
	if _, err := coordinator.DispatchItem(ctx, "wi-order"); err != nil {
		t.Fatalf("DispatchItem: %v", err)
	}
	order := calls.snapshot()
	assertCallBefore(t, order, "declare", "binding")
	assertCallBefore(t, order, "binding", "plan")
	assertCallBefore(t, order, "plan", "start")
	if containsCall(order, "dispatch") {
		t.Fatalf("生产派发不得走 jobs.Dispatch 抢先启动，实际顺序 %v", order)
	}
}

// TestDispatchWorktreeFailureStopsBeforeDeclare 钉住 strict worktree 失败的边界：
// 没有现场就不得登记作业、写 running 计划或启动 executor。审批回退只能由调用方
// 在用户批准后以 workspace=main 显式重试。
func TestDispatchWorktreeFailureStopsBeforeDeclare(t *testing.T) {
	fixture := newItemFixture(t, 6)
	setupItemPlan(t, fixture)
	ctx := context.Background()
	fixture.spaces.bindErr = errors.New("worktree unavailable")

	if _, err := fixture.coordinator.DispatchItem(ctx, "wi-req"); err == nil ||
		!strings.Contains(err.Error(), "worktree unavailable") {
		t.Fatalf("worktree failure must be returned, got %v", err)
	}
	if item := itemState(t, fixture, "wi-req"); item.StatusOrPending() != store.TeamworkItemPending {
		t.Fatalf("worktree failure must leave the WorkItem pending, got %+v", item)
	}
	if records := fixture.jobs.Snapshot(jobs.Scope{Session: fixture.coordinator.Key().SessionID}); len(records) != 0 {
		t.Fatalf("worktree failure must not Declare a job, got %+v", records)
	}
	if requests := fixture.runner.requestsSnapshot(); len(requests) != 0 {
		t.Fatalf("worktree failure must not start the executor, got %+v", requests)
	}
	rows, err := fixture.st.ReadBindings(ctx, fixture.coordinator.Key())
	if err != nil {
		t.Fatalf("ReadBindings: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("worktree failure must not write an execution binding, got %+v", rows)
	}
}

// failingPlanStore fails only the next running-plan write, after Declare/binding/audit
// have succeeded. It makes the "persist before Start" rule independently testable.
type failingPlanStore struct {
	*memoryPlanStore
	failRunning bool
}

func (s *failingPlanStore) WritePlan(ctx context.Context, key store.Key, plan store.TeamworkPlan, maxTeammates int) error {
	if s.failRunning {
		for _, milestone := range plan.Milestones {
			for _, item := range milestone.Items {
				if item.StatusOrPending() == store.TeamworkItemRunning {
					return errors.New("injected running-plan write failure")
				}
			}
		}
	}
	return s.memoryPlanStore.WritePlan(ctx, key, plan, maxTeammates)
}

// TestDispatchPersistFailureDoesNotStartExecutor 钉住 Start 提交屏障：running 计划
// 写失败时作业只能撤销声明，执行体绝不能先跑。
func TestDispatchPersistFailureDoesNotStartExecutor(t *testing.T) {
	ctx := context.Background()
	memory := &memoryPlanStore{}
	storeWithFailure := &failingPlanStore{memoryPlanStore: memory}
	runner := &fakeRunner{}
	holder := &settlerHolder{}
	manager, err := jobs.New(
		jobs.WithNamer(testNamer{}),
		jobs.WithExecutor(WorkerExecutor(runner, holder, 4)),
		jobs.WithLimits(jobs.Limits{InFlight: 64}),
	)
	if err != nil {
		t.Fatalf("jobs.New: %v", err)
	}
	t.Cleanup(func() { _ = manager.Close(context.Background()) })
	key := store.Key{ProjectID: "p", SessionID: "s"}
	coordinator, err := New(Options{
		Key: key, Store: storeWithFailure, Jobs: manager, Workers: runner,
		Worktrees: fakeWorktrees{log: &callLog{}}, Sessions: fakeSessions{log: &callLog{}},
		Spaces: &fakeSpaces{}, Teammates: &fakeQueue{}, MaxTeammates: 6,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	holder.coordinator = coordinator
	if err := coordinator.SetPlan(ctx, itemPlan()); err != nil {
		t.Fatalf("SetPlan: %v", err)
	}
	if err := coordinator.PlanMilestone(ctx, "m-build", []WorkItemSpec{{
		ID: "wi-persist", Role: "exec", Name: "持久化失败",
	}}); err != nil {
		t.Fatalf("PlanMilestone: %v", err)
	}
	storeWithFailure.failRunning = true
	if _, err := coordinator.DispatchItem(ctx, "wi-persist"); err == nil ||
		!strings.Contains(err.Error(), "injected running-plan write failure") {
		t.Fatalf("running-plan persistence failure must be returned, got %v", err)
	}
	if requests := runner.requestsSnapshot(); len(requests) != 0 {
		t.Fatalf("Start must not run after persistence failed, got %+v", requests)
	}
}

// TestDispatchMainWorkspaceRequiresExplicitOption 钉住降级边界：默认仍建 worktree；
// 只有显式 WorkspaceMain 才跳过绑定，并把 main 模式写进计划、账本与 worker 载荷。
func TestDispatchMainWorkspaceRequiresExplicitOption(t *testing.T) {
	fixture := newItemFixture(t, 6)
	setupItemPlan(t, fixture)
	ctx := context.Background()
	if _, err := fixture.coordinator.DispatchItemWith(ctx, "wi-req", DispatchOptions{
		Workspace: WorkspaceMain,
	}); err != nil {
		t.Fatalf("DispatchItemWith(main): %v", err)
	}
	waitRequests(t, fixture, 1)
	bound, _, _ := fixture.spaces.snapshot()
	if len(bound) != 0 {
		t.Fatalf("显式 main 模式不得再创建 worktree，bound=%v", bound)
	}
	item := itemState(t, fixture, "wi-req")
	if item.WorkspaceMode != string(WorkspaceMain) {
		t.Fatalf("计划必须记录 main 模式，得到 %+v", item)
	}
	rows, err := fixture.st.ReadBindings(ctx, fixture.coordinator.Key())
	if err != nil {
		t.Fatalf("ReadBindings: %v", err)
	}
	if len(rows) != 1 || rows[0].WorkspaceMode != string(WorkspaceMain) {
		t.Fatalf("绑定账本必须记录 main 模式，得到 %+v", rows)
	}
	requests := fixture.runner.requestsSnapshot()
	if requests[0].Worktree != "" || requests[0].WorkspaceMode != string(WorkspaceMain) {
		t.Fatalf("worker 载荷必须显式走 main（不能带 worktree 指派）: %+v", requests[0])
	}
}

// TestMaxJobsLimitsActiveJobsNotRoles 钉住 max_teammates 与 max_jobs 的分离：
// 同一 role 的两个 WorkItem 各占一个 job 名额；max_jobs 超限时拒绝，但不影响
// 相同 WorkItem 的幂等折叠。
func TestMaxJobsLimitsActiveJobsNotRoles(t *testing.T) {
	fixture := newItemFixtureWithJobs(t, 6, 1)
	ctx := context.Background()
	if err := fixture.coordinator.SetPlan(ctx, itemPlan()); err != nil {
		t.Fatalf("SetPlan: %v", err)
	}
	if err := fixture.coordinator.PlanMilestone(ctx, "m-build", []WorkItemSpec{
		{ID: "wi-a", Role: "exec", Name: "A"},
		{ID: "wi-b", Role: "exec", Name: "B"},
	}); err != nil {
		t.Fatalf("PlanMilestone: %v", err)
	}
	block := make(chan struct{})
	fixture.runner.block = block
	first, err := fixture.coordinator.DispatchItem(ctx, "wi-a")
	if err != nil {
		t.Fatalf("DispatchItem(wi-a): %v", err)
	}
	if again, err := fixture.coordinator.DispatchItem(ctx, "wi-a"); err != nil || again != first {
		t.Fatalf("同一 WorkItem 重派必须幂等且不额外占名额: handle=%s err=%v", again, err)
	}
	if _, err := fixture.coordinator.DispatchItem(ctx, "wi-b"); err == nil || !strings.Contains(err.Error(), "max_jobs") {
		t.Fatalf("同一 role 的第二件 WorkItem 必须受 max_jobs 限制，得到 %v", err)
	}
	close(block)
}

// TestTeammateRouteConcurrencyIsNarrowerThanMaxJobs 钉住可选的第二层限制：
// enabled=false 时只有 max_jobs；开启后同一 role 的 active jobs 达到配置值即拒绝，
// 不改变 max_jobs 对整支团队的硬上限。
func TestTeammateRouteConcurrencyIsNarrowerThanMaxJobs(t *testing.T) {
	fixture := newItemFixtureWithJobs(t, 6, 6)
	ctx := context.Background()
	if err := fixture.coordinator.SetPlan(ctx, itemPlan()); err != nil {
		t.Fatalf("SetPlan: %v", err)
	}
	if err := fixture.coordinator.PlanMilestone(ctx, "m-build", []WorkItemSpec{
		{ID: "wi-a", Role: "exec", Name: "A"},
		{ID: "wi-b", Role: "exec", Name: "B"},
	}); err != nil {
		t.Fatalf("PlanMilestone: %v", err)
	}
	fixture.coordinator.teammateConcurrencyEnabled = true
	fixture.coordinator.teammateConcurrencyMax = 1
	block := make(chan struct{})
	fixture.runner.block = block
	if _, err := fixture.coordinator.DispatchItem(ctx, "wi-a"); err != nil {
		t.Fatalf("DispatchItem(wi-a): %v", err)
	}
	if _, err := fixture.coordinator.DispatchItem(ctx, "wi-b"); err == nil ||
		!strings.Contains(err.Error(), "limits.concurrency.teammate") {
		t.Fatalf("同 role 的第二件必须被路由并发限制拒绝，得到 %v", err)
	}
	close(block)
}

func containsCall(calls []string, want string) bool {
	for _, call := range calls {
		if call == want {
			return true
		}
	}
	return false
}

func assertCallBefore(t *testing.T, calls []string, first, second string) {
	t.Helper()
	firstAt, secondAt := -1, -1
	for index, call := range calls {
		if call == first && firstAt < 0 {
			firstAt = index
		}
		if call == second && secondAt < 0 {
			secondAt = index
		}
	}
	if firstAt < 0 || secondAt < 0 || firstAt >= secondAt {
		t.Fatalf("派发顺序必须 %s 在 %s 之前，实际 %v", first, second, calls)
	}
}
