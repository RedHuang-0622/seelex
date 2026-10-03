package teamwork

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RedHuang-0622/Seele/jobs"
	"github.com/RedHuang-0622/seelex/sessionstore"
)

// ── 端口替身（本包因此可在没有引擎、没有 git 的测试里跑完编排语义）──

type memoryPlanStore struct {
	mu       sync.Mutex
	plan     sessionstore.TeamworkPlan
	events   []sessionstore.TeamworkEvent
	bindings []sessionstore.TeamworkBinding
	limit    int
}

func (s *memoryPlanStore) WritePlan(_ context.Context, _ sessionstore.Key, plan sessionstore.TeamworkPlan, maxTeammates int) error {
	if err := sessionstore.ValidateTeamworkPlan(plan, maxTeammates); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.plan = plan
	return nil
}

func (s *memoryPlanStore) ReadPlan(context.Context, sessionstore.Key) (sessionstore.TeamworkPlan, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.plan.TeamID == "" {
		return sessionstore.TeamworkPlan{}, errors.New("teamwork: 计划不存在")
	}
	return s.plan, nil
}

func (s *memoryPlanStore) AppendEvent(_ context.Context, _ sessionstore.Key, event sessionstore.TeamworkEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, event)
	return nil
}

func (s *memoryPlanStore) ReadEvents(context.Context, sessionstore.Key) ([]sessionstore.TeamworkEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]sessionstore.TeamworkEvent(nil), s.events...), nil
}

func (s *memoryPlanStore) AppendBinding(_ context.Context, _ sessionstore.Key, binding sessionstore.TeamworkBinding) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.bindings = append(s.bindings, binding)
	return nil
}

func (s *memoryPlanStore) ReadBindings(context.Context, sessionstore.Key) ([]sessionstore.TeamworkBinding, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]sessionstore.TeamworkBinding(nil), s.bindings...), nil
}

func (s *memoryPlanStore) kinds() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	kinds := make([]string, 0, len(s.events))
	for _, event := range s.events {
		kinds = append(kinds, event.Kind)
	}
	return kinds
}

type fakeRunner struct {
	mu       sync.Mutex
	requests []WorkerRequest
	block    chan struct{}
}

func (r *fakeRunner) RunWorker(ctx context.Context, request WorkerRequest, sink jobs.Sink) error {
	r.mu.Lock()
	r.requests = append(r.requests, request)
	block := r.block
	r.mu.Unlock()
	sink.Note("worker ran: " + request.Role + "\n")
	if block != nil {
		select {
		case <-block:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	sink.Complete(jobs.StateDone, "ok")
	return nil
}

func (r *fakeRunner) requestsSnapshot() []WorkerRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]WorkerRequest(nil), r.requests...)
}

type callLog struct {
	mu    sync.Mutex
	calls []string
}

func (l *callLog) record(entry string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.calls = append(l.calls, entry)
}

func (l *callLog) snapshot() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.calls...)
}

type fakeWorktrees struct{ log *callLog }

func (w fakeWorktrees) ReleaseWorkspace(_ context.Context, role string) error {
	w.log.record("worktree:" + role)
	return nil
}

// fakeBoards 只记"封板被调用过"：本包不认识看板存档的形状，封板是端口的事。
type fakeBoards struct{ log *callLog }

func (b fakeBoards) CloseTeamBoard(context.Context) error {
	b.log.record("seal")
	return nil
}

type fakeSessions struct{ log *callLog }

func (s fakeSessions) ResetSession(_ context.Context, roleSessionID string) error {
	s.log.record("session:" + roleSessionID)
	return nil
}

// ── fixture ─────────────────────────────────────────────────────────

type fixture struct {
	coordinator *Coordinator
	store       *memoryPlanStore
	jobs        jobs.Manager
	runner      *fakeRunner
	calls       *callLog
}

func newFixture(t *testing.T, maxTeammates int) *fixture {
	t.Helper()
	store := &memoryPlanStore{}
	runner := &fakeRunner{}
	log := &callLog{}
	manager, err := jobs.New(
		jobs.WithExecutor(WorkerExecutor(runner, nil, 4)),
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
		MaxTeammates: maxTeammates,
	})
	if err != nil {
		t.Fatalf("teamwork.New: %v", err)
	}
	return &fixture{coordinator: coordinator, store: store, jobs: manager, runner: runner, calls: log}
}

// vmodelPlan 是 V 模型口径的计划：**里程碑屏障**（m-req → m-impl）+ 里程碑内的工作项。
// **没有阶段**（2026-10-04 阶段口径整条退场）：顺序的唯一事实是 milestones[].depends_on
// 与 items[].depends_on。
func vmodelPlan() sessionstore.TeamworkPlan {
	return sessionstore.TeamworkPlan{
		TeamID:  "v-model",
		Version: 1,
		Members: []sessionstore.TeamworkMember{
			{Role: "pm"},
			{Role: "exec", Worktree: "seelex/exec"},
			{Role: "test_case"},
		},
		Milestones: []sessionstore.TeamworkMilestone{
			{
				ID: "m-req", Name: "需求", Required: []string{"pm"},
				Items: []sessionstore.TeamworkWorkItem{{ID: "wi-req", Milestone: "m-req", Role: "pm", Name: "需求"}},
			},
			{
				ID: "m-impl", Name: "实现", Required: []string{"exec"}, DependsOn: []string{"m-req"},
				Items: []sessionstore.TeamworkWorkItem{{ID: "wi-impl", Milestone: "m-impl", Role: "exec", Name: "实现"}},
			},
		},
	}
}

// waitTerminal 轮询到作业终态（测试里用真实时钟，只等一个短窗口）。
func waitTerminal(t *testing.T, manager jobs.Manager, handle jobs.Handle) jobs.Record {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if record, ok := manager.Observe(handle); ok && record.State.Terminal() {
			return record
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("作业 %s 没有在窗口内终态", handle)
	return jobs.Record{}
}

// ── 用例 ───────────────────────────────────────────────────────────

func TestSetPlanDerivesRoleSessionIDsAndAudits(t *testing.T) {
	fixture := newFixture(t, 6)
	ctx := context.Background()
	if err := fixture.coordinator.SetPlan(ctx, vmodelPlan()); err != nil {
		t.Fatalf("SetPlan: %v", err)
	}
	plan, err := fixture.coordinator.Plan(ctx)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if plan.Members[0].RoleSessionID != "s-v-model-pm" {
		t.Fatalf("role_session_id 未按 (主会话, team_id, role) 派生: %q", plan.Members[0].RoleSessionID)
	}
	if kinds := fixture.store.kinds(); len(kinds) != 1 || kinds[0] != sessionstore.TeamworkEventPlan {
		t.Fatalf("计划改写必须留下审计行: %v", kinds)
	}
}

func TestDispatchJoinMilestoneLifecycle(t *testing.T) {
	fixture := newFixture(t, 6)
	ctx := context.Background()
	if err := fixture.coordinator.SetPlan(ctx, vmodelPlan()); err != nil {
		t.Fatalf("SetPlan: %v", err)
	}
	handle, err := fixture.coordinator.Dispatch(ctx, "exec", "实现 v-model 的 impl 里程碑")
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	record := waitTerminal(t, fixture.jobs, handle)
	if record.Scope.Subject != "emp_exec" {
		t.Fatalf("作业作用域错了: %+v", record)
	}
	if record.Description == "" {
		t.Fatal("作业行标题必须来自派发时那句话")
	}

	// 里程碑：屏障（m-req）已 done ⇒ 允许声明内容并收口。
	if err := fixture.coordinator.Milestone(ctx, "m-req", "需求定稿"); err != nil {
		t.Fatalf("前置里程碑收口: %v", err)
	}
	if err := fixture.coordinator.Milestone(ctx, "m-impl", "impl 完成，进入 test"); err != nil {
		t.Fatalf("Milestone: %v", err)
	}
	plan, _ := fixture.coordinator.Plan(ctx)
	if plan.State.Milestones["m-impl"] == "" || plan.Milestones[0].Status != sessionstore.TeamworkMilestoneDone {
		t.Fatalf("里程碑没有落到计划: %+v", plan.Milestones[0])
	}

	joined, err := fixture.coordinator.Join(ctx, []jobs.Handle{handle}, 2*time.Second)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}
	if len(joined) != 1 || joined[0].Running() {
		t.Fatalf("Join 没有收敛: %+v", joined)
	}

	// 退场：四步顺序固定。
	if err := fixture.coordinator.Retire(ctx, "exec"); err != nil {
		t.Fatalf("Retire: %v", err)
	}
	calls := fixture.calls.snapshot()
	if len(calls) != 2 || calls[0] != "worktree:exec" || calls[1] != "session:s-v-model-exec" {
		t.Fatalf("退场步骤顺序错了: %v", calls)
	}
	if _, ok := fixture.jobs.Observe(handle); !ok {
		t.Fatal("Retire 不得回收作业：回收统一收口到整队 Close（作业正文在整队关闭前一直在册）")
	}
	plan, _ = fixture.coordinator.Plan(ctx)
	if plan.Members[1].Worktree != "" {
		t.Fatalf("退场后 worktree 指派名应清空待重派: %+v", plan.Members[1])
	}
	if len(plan.Members) != 3 {
		t.Fatal("退场删的是会话内容与检出，不是注册/在编")
	}
}

// TestMilestoneRefusesMilestoneBehindTheBarrier：屏障（依赖的里程碑）还没 done 就声明
// 里程碑内容必须被拒——"里程碑"不能是一句没有事实支撑的口号。阶段制时代这条判据看的是
// `after` 里的阶段派发过没有；阶段口径退场后，判据回到唯一那份顺序事实：depends_on。
func TestMilestoneRefusesMilestoneBehindTheBarrier(t *testing.T) {
	fixture := newFixture(t, 6)
	ctx := context.Background()
	if err := fixture.coordinator.SetPlan(ctx, vmodelPlan()); err != nil {
		t.Fatalf("SetPlan: %v", err)
	}
	if err := fixture.coordinator.Milestone(ctx, "m-impl", "x"); err == nil {
		t.Fatal("依赖的里程碑（m-req）还没 done 就声明里程碑必须被拒")
	}
}

func TestDispatchRefusesUnknownRole(t *testing.T) {
	fixture := newFixture(t, 6)
	ctx := context.Background()
	if err := fixture.coordinator.SetPlan(ctx, vmodelPlan()); err != nil {
		t.Fatalf("SetPlan: %v", err)
	}
	if _, err := fixture.coordinator.Dispatch(ctx, "ghost", "x"); err == nil {
		t.Fatal("不在编的角色必须被拒")
	}
}

func TestDispatchRefusesWhenTeamIsFull(t *testing.T) {
	fixture := newFixture(t, 2)
	eventually := make(chan struct{})
	fixture.runner.block = eventually
	ctx := context.Background()
	plan := vmodelPlan()
	plan.Members = plan.Members[:2]
	if err := fixture.coordinator.SetPlan(ctx, plan); err != nil {
		t.Fatalf("SetPlan: %v", err)
	}
	// 直接把"在跑人数"顶到上限（模拟同会话里已经跑着的两个 teammate 作业）。
	// 载荷必须**可解码**：worker 执行体在载荷解码失败时会把作业立刻判失败终态，
	// 占位作业就顶不住人数——机器负载高时"派发 → 终态"的窗口被压缩，本用例会偶发
	// 假绿（2026-10-01 全量串行跑时复现）。
	for index := 0; index < 2; index++ {
		if _, err := fixture.jobs.Dispatch(ctx, jobs.Spec{
			Kind:        KindWorker,
			Scope:       jobs.Scope{Session: "s", Subject: fmt.Sprintf("emp_other%d", index)},
			Description: "占位",
			Payload:     []byte(fmt.Sprintf(`{"main_session_id":"s","role":"other%d"}`, index)),
		}); err != nil {
			t.Fatalf("Dispatch: %v", err)
		}
	}
	_, err := fixture.coordinator.Dispatch(ctx, "exec", "第三个")
	if err == nil || !strings.Contains(err.Error(), "max_teammates") {
		t.Fatalf("超员必须显式拒绝并点明上限，得到 %v", err)
	}
	close(eventually)
}

func TestDispatchDedupsSameRole(t *testing.T) {
	fixture := newFixture(t, 6)
	eventually := make(chan struct{})
	fixture.runner.block = eventually
	ctx := context.Background()
	if err := fixture.coordinator.SetPlan(ctx, vmodelPlan()); err != nil {
		t.Fatalf("SetPlan: %v", err)
	}
	first, err := fixture.coordinator.Dispatch(ctx, "exec", "第一轮")
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	second, err := fixture.coordinator.Dispatch(ctx, "exec", "第一轮")
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if first != second {
		t.Fatalf("同一角色的重复派发必须折叠到一个在跑的作业: %s vs %s", first, second)
	}
	close(eventually)
}

// TestRetireKeepsJobOnRosterAndTouchesOnlyThatTeammate：Retire 只做"释放 worktree → 清
// 会话内容 → 保在线"，**不回收作业**（回收统一收口到整队 Close）——作业正文在整队
// 关闭前一直在册；且退场只动这一个 teammate，邻居的作业不受牵连。
func TestRetireKeepsJobOnRosterAndTouchesOnlyThatTeammate(t *testing.T) {
	fixture := newFixture(t, 6)
	eventually := make(chan struct{})
	fixture.runner.block = eventually
	ctx := context.Background()
	if err := fixture.coordinator.SetPlan(ctx, vmodelPlan()); err != nil {
		t.Fatalf("SetPlan: %v", err)
	}
	execHandle, err := fixture.coordinator.Dispatch(ctx, "exec", "impl")
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	testHandle, err := fixture.coordinator.Dispatch(ctx, "test_case", "test")
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if err := fixture.coordinator.Retire(ctx, "exec"); err != nil {
		t.Fatalf("Retire: %v", err)
	}
	if _, ok := fixture.jobs.Observe(execHandle); !ok {
		t.Fatal("Retire 不得回收作业：作业正文在整队 Close 之前一直在册（回收统一收口）")
	}
	if _, ok := fixture.jobs.Observe(testHandle); !ok {
		t.Fatal("退场只动这一个 teammate，邻居的作业不该被牵连")
	}
	calls := fixture.calls.snapshot()
	if len(calls) != 2 || calls[0] != "worktree:exec" || calls[1] != "session:s-v-model-exec" {
		t.Fatalf("退场的「释放工作区 → 清会话内容」两步照旧要发生: %v", calls)
	}
	close(eventually)
}

func TestRetireRequiresWorkspaceAndSessionPorts(t *testing.T) {
	store := &memoryPlanStore{}
	manager, err := jobs.New(jobs.WithExecutor(WorkerExecutor(&fakeRunner{}, nil, 4)))
	if err != nil {
		t.Fatalf("jobs.New: %v", err)
	}
	t.Cleanup(func() { _ = manager.Close(context.Background()) })
	coordinator, err := New(Options{
		Key:   sessionstore.Key{ProjectID: "p", SessionID: "s"},
		Store: store,
		Jobs:  manager,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx := context.Background()
	if err := coordinator.SetPlan(ctx, vmodelPlan()); err != nil {
		t.Fatalf("SetPlan: %v", err)
	}
	err = coordinator.Retire(ctx, "exec")
	if err == nil || !strings.Contains(err.Error(), "步 2") {
		t.Fatalf("缺工作区端口时必须在步 2 显式报错，得到 %v", err)
	}
}

func TestSetPlanDelegatesStructuralValidation(t *testing.T) {
	fixture := newFixture(t, 6)
	plan := vmodelPlan()
	plan.Milestones[0].DependsOn = []string{"m-impl"} // 环（m-impl 依赖 m-req）
	if err := fixture.coordinator.SetPlan(context.Background(), plan); err == nil {
		t.Fatal("带环的计划必须在落盘那一步就被拒")
	}
}

func TestNewRefusesIncompleteWiring(t *testing.T) {
	if _, err := New(Options{}); err == nil {
		t.Fatal("缺会话作用域必须被拒")
	}
	if _, err := New(Options{Key: sessionstore.Key{ProjectID: "p", SessionID: "s"}}); err == nil {
		t.Fatal("缺 PlanStore 必须被拒")
	}
	manager, err := jobs.New()
	if err != nil {
		t.Fatalf("jobs.New: %v", err)
	}
	t.Cleanup(func() { _ = manager.Close(context.Background()) })
	if _, err := New(Options{Key: sessionstore.Key{ProjectID: "p", SessionID: "s"}, Store: &memoryPlanStore{}}); err == nil {
		t.Fatal("缺 jobs.Manager 必须被拒")
	}
}

func TestSubjectIsEmployeeSubject(t *testing.T) {
	if SubjectForRole("exec") != "emp_exec" {
		t.Fatalf("主体名必须是 emp_<role>（权限面与作用域共用同一套命名）")
	}
}

func TestWorkerExecutorWithoutRunnerFails(t *testing.T) {
	executor := WorkerExecutor(nil, nil, 1)
	manager, err := jobs.New(jobs.WithExecutor(executor))
	if err != nil {
		t.Fatalf("jobs.New: %v", err)
	}
	t.Cleanup(func() { _ = manager.Close(context.Background()) })
	handle, err := manager.Dispatch(context.Background(), jobs.Spec{
		Kind: KindWorker, Scope: jobs.Scope{Session: "s"}, Description: "x",
	})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	record := waitTerminal(t, manager, handle)
	if record.State != jobs.StateFailed {
		t.Fatalf("缺 Runner 必须以失败收场（不静默成功）: %+v", record)
	}
}

// TestCoordinatorCloseSealsBoard：整队收口 —— 逐在编成员走同一套四步（reclaim=true）
// → 封板 → 计划标 closed → 落一条 close 审计。
func TestCoordinatorCloseSealsBoard(t *testing.T) {
	fixture := newFixture(t, 6)
	eventually := make(chan struct{})
	fixture.runner.block = eventually
	ctx := context.Background()
	if err := fixture.coordinator.SetPlan(ctx, vmodelPlan()); err != nil {
		t.Fatalf("SetPlan: %v", err)
	}
	execHandle, err := fixture.coordinator.Dispatch(ctx, "exec", "impl")
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	alreadyClosed, err := fixture.coordinator.Close(ctx)
	if err != nil {
		t.Fatalf("Close: %v", err)
	}
	if alreadyClosed {
		t.Fatal("首次收口不得报 already_closed")
	}
	// 1. 回收：Retire 不做的那一步收口到这里（作业不再在册）。
	if _, ok := fixture.jobs.Observe(execHandle); ok {
		t.Fatal("整队收口必须回收在编成员的作业")
	}
	// 2. 封板：端口被调用恰好一次，且发生在成员退场之后（先归位再封板）。
	calls := fixture.calls.snapshot()
	if len(calls) == 0 || calls[len(calls)-1] != "seal" {
		t.Fatalf("封板必须发生在成员退场之后: %v", calls)
	}
	seals := 0
	for _, call := range calls {
		if call == "seal" {
			seals++
		}
	}
	if seals != 1 {
		t.Fatalf("封板恰好一次，实际 %d 次: %v", seals, calls)
	}
	// 3. 计划标 closed（U3 双写里的域内权威那一份）。
	plan, err := fixture.coordinator.Plan(ctx)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if plan.State.State != sessionstore.TeamworkStateClosed {
		t.Fatalf("收口后计划必须标 closed: %+v", plan.State)
	}
	if plan.State.ClosedAt == 0 || plan.State.ClosedReason != sessionstore.BoardCloseTeamClose {
		t.Fatalf("收口事实必须带 closed_at / closed_reason(team.close): %+v", plan.State)
	}
	if len(plan.State.Jobs) != 0 {
		t.Fatalf("收口后不该留下句柄投影: %+v", plan.State.Jobs)
	}
	for _, member := range plan.Members {
		if member.Worktree != "" {
			t.Fatalf("成员 %q 的 worktree 指派名应清空待重派: %+v", member.Role, member)
		}
	}
	// 4. 审计：恰好一条 close，且没有逐人 retire（收口是团队级动作）。
	closes, retires := 0, 0
	for _, kind := range fixture.store.kinds() {
		switch kind {
		case sessionstore.TeamworkEventClose:
			closes++
		case sessionstore.TeamworkEventRetire:
			retires++
		}
	}
	if closes != 1 || retires != 0 {
		t.Fatalf("整队收口的审计应恰好一条 close、零条 retire: closes=%d retires=%d %v", closes, retires, fixture.store.kinds())
	}
	close(eventually)
}

// TestCloseIdempotentAlreadyClosed：重复收口是幂等的读事实——第二次报 already_closed，
// 且不再退场、不再封板、不再落审计。
func TestCloseIdempotentAlreadyClosed(t *testing.T) {
	fixture := newFixture(t, 6)
	ctx := context.Background()
	if err := fixture.coordinator.SetPlan(ctx, vmodelPlan()); err != nil {
		t.Fatalf("SetPlan: %v", err)
	}
	if alreadyClosed, err := fixture.coordinator.Close(ctx); err != nil || alreadyClosed {
		t.Fatalf("首次收口应成功且不报 already_closed: alreadyClosed=%v err=%v", alreadyClosed, err)
	}
	callsBefore := len(fixture.calls.snapshot())
	auditBefore := len(fixture.store.kinds())
	alreadyClosed, err := fixture.coordinator.Close(ctx)
	if err != nil {
		t.Fatalf("重复收口必须幂等（不是错误路径）: %v", err)
	}
	if !alreadyClosed {
		t.Fatal("第二次收口必须报 already_closed=true")
	}
	if got := len(fixture.calls.snapshot()); got != callsBefore {
		t.Fatalf("重复收口不得再退场/封板: 端口调用 %d → %d (%v)", callsBefore, got, fixture.calls.snapshot())
	}
	if got := len(fixture.store.kinds()); got != auditBefore {
		t.Fatalf("重复收口不得重复落审计: 审计行 %d → %d (%v)", auditBefore, got, fixture.store.kinds())
	}
	closes := 0
	for _, kind := range fixture.store.kinds() {
		if kind == sessionstore.TeamworkEventClose {
			closes++
		}
	}
	if closes != 1 {
		t.Fatalf("close 审计恰好一条，实际 %d: %v", closes, fixture.store.kinds())
	}
}

// TestRedispatchAfterRetireIsNotRefusedByCapacity：Retire 不再回收作业的**连锁**处置——
// "在跑作业数"不再等于"未被退场的在编人数"（已退场但作业仍在跑的成员会一直占名额）。
// 派发**同一个角色**时不计它自己那一条，于是退场后重派不会自己把自己顶在上限外；
// 真超员（另一个角色顶到上限）照旧显式拒绝。
//
// 已知残差（回执里记为未决项，本用例只钉"受理"这一条，不替它盖章）：框架的 dedup 判据是
// 载荷**逐字节相同**（Seele jobs manager.dedupLocked），因此重派带新内容时同一角色会有两条
// 在跑作业（下面 t.Logf 把事实记下来）。
func TestRedispatchAfterRetireIsNotRefusedByCapacity(t *testing.T) {
	fixture := newFixture(t, 2)
	eventually := make(chan struct{})
	fixture.runner.block = eventually
	ctx := context.Background()
	plan := vmodelPlan()
	plan.Members = plan.Members[:2] // pm / exec，上限 = 2
	if err := fixture.coordinator.SetPlan(ctx, plan); err != nil {
		t.Fatalf("SetPlan: %v", err)
	}
	// 一个占位作业先占掉一个名额（同会话、不同主体）。
	if _, err := fixture.jobs.Dispatch(ctx, jobs.Spec{
		Kind:        KindWorker,
		Scope:       jobs.Scope{Session: "s", Subject: "emp_other"},
		Description: "占位",
		Payload:     []byte(`{"main_session_id":"s","role":"other"}`),
	}); err != nil {
		t.Fatalf("Dispatch(占位): %v", err)
	}
	first, err := fixture.coordinator.Dispatch(ctx, "pm", "需求")
	if err != nil {
		t.Fatalf("Dispatch(pm): %v", err)
	}
	// 上限已满：真超员（另一个角色）照旧显式拒绝并点明上限。
	if _, err := fixture.coordinator.Dispatch(ctx, "exec", "第三个"); err == nil || !strings.Contains(err.Error(), "max_teammates") {
		t.Fatalf("真超员必须显式拒绝并点明上限，得到 %v", err)
	}
	// 退场 pm：作业仍在跑（本轮语义），它仍占着名额。
	if err := fixture.coordinator.Retire(ctx, "pm"); err != nil {
		t.Fatalf("Retire: %v", err)
	}
	if _, ok := fixture.jobs.Observe(first); !ok {
		t.Fatal("Retire 不得回收作业（回收统一收口到整队 Close）")
	}
	// 重派（同样内容）：必须被受理，且折叠到在跑的那一条。
	again, err := fixture.coordinator.Dispatch(ctx, "pm", "需求")
	if err != nil {
		t.Fatalf("退场后重派同一角色不得撞 max_teammates: %v", err)
	}
	if again != first {
		t.Fatalf("同样内容的重派应折叠到在跑的那一条: %s vs %s", again, first)
	}
	// 重派（新内容）：同样必须被受理——这是本轮口径要求的"不得误计上限"。
	fresh, err := fixture.coordinator.Dispatch(ctx, "pm", "下一批工作正文")
	if err != nil {
		t.Fatalf("退场后重派同一角色（新内容）不得撞 max_teammates: %v", err)
	}
	if fresh == first {
		t.Logf("意外折叠：新内容与在跑载荷不同，框架 dedup 不该合并（若真合并，本用例的残差说明已过时）")
	} else {
		t.Logf("已知残差复现：同一角色出现第二条在跑作业 %s（在跑 %s）；"+
			"框架 dedup 判据是载荷逐字节相同，新内容不合并", fresh, first)
	}
	close(eventually)
}
