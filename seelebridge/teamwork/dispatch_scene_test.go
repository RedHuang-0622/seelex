package teamwork

// dispatch_scene_test.go — 老口径（teammate 级）派发的**现场**回归守卫
// （2026-10-05，F2/F3）。
//
// 缺陷形状（修复前的实测读数）：
//   - `Dispatch(role, goal)` 从头到尾没有一次 `BindWorkspace`：`WorkerRequest.Worktree`
//     取的是 `member.Worktree`，而那个字段的唯一写点把它清成空 ⇒ 老口径派发的
//     teammate 一定落在主工作区（bindWorkerProjectRoot 查空后回退主会话根）；
//   - 退场步 2 只拿**角色名**去查一次现场，而注册键是裸 nodeID（Work Item 级 =
//     `<role>-<itemID>`）⇒ 那些现场永远留在盘上，"脏工作区显式报错"这条保证也永不
//     触发（查不到现场 = 幂等返回）。
//
// 本文件钉住三条事实：① 派发建现场（`seelex/<role>`）+ 账本留行；④ 退场释放该角色
// **全部**现场（角色级 + 每一件已派发的 Work Item）；⑤ 某一份现场释放失败时必须点明
// 是哪一个、且不牵连同批其余（Runtime 侧的两条——真释放与脏现场报错——见
// seelebridge/runtime_teamwork_items_test.go）。

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/RedHuang-0622/Seele/jobs"
	"github.com/RedHuang-0622/seelex/sessionstore"
)

// boundWorktrees 返回建现场时收到的**指派名**——现场在册与否看的就是这个键。放在这
// 里而不是 items_test.go：既有替身不动。
func (s *fakeSpaces) boundWorktrees() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	names := make([]string, 0, len(s.bound))
	for _, binding := range s.bound {
		names = append(names, binding.Worktree)
	}
	return names
}

func containsName(names []string, want string) bool {
	for _, name := range names {
		if name == want {
			return true
		}
	}
	return false
}

// TestDispatchBindsTeammateSceneAndRecordsLedger —— ① 老口径派发必须建出
// `seelex/<role>` 现场，并把指派名同时写进 WorkerRequest 与成员条目（绑根侧与看板
// 都要看得到），再记一行 teammate 级账本。
//
// 计划**刻意不带成员 worktree**：带的话，"成员条目看到了指派名"会被计划里原样留着
// 的字段蒙混过关（那正是缺陷里"字段没有消费者"的形状）。
func TestDispatchBindsTeammateSceneAndRecordsLedger(t *testing.T) {
	fixture := newItemFixture(t, 6)
	ctx := context.Background()
	if err := fixture.coordinator.SetPlan(ctx, itemPlan()); err != nil {
		t.Fatalf("SetPlan: %v", err)
	}
	if _, err := fixture.coordinator.Dispatch(ctx, "exec", "老口径的一轮"); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	requests := waitRequests(t, fixture, 1)
	if got := requests[0].Worktree; got != "seelex/exec" {
		t.Fatalf("WorkerRequest.Worktree = %q，want %q（空 = 绑根查空后回退主工作区）", got, "seelex/exec")
	}
	if bound := fixture.spaces.boundWorktrees(); !containsName(bound, "seelex/exec") {
		t.Fatalf("派发必须经 Workspaces.BindWorkspace 建现场，实际建过 %v", bound)
	}
	plan, err := fixture.coordinator.Plan(ctx)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	member, ok := memberFor(plan, "exec")
	if !ok {
		t.Fatal("exec 应在编")
	}
	if member.Worktree != "seelex/exec" {
		t.Fatalf("成员条目应看到现场指派名，得 %q", member.Worktree)
	}
	rows, err := fixture.store.ReadBindings(ctx, fixture.coordinator.Key())
	if err != nil {
		t.Fatalf("ReadBindings: %v", err)
	}
	live := sessionstore.TeamworkBindings(rows)
	found := false
	for _, binding := range live {
		if binding.Role == "exec" && binding.Worktree == "seelex/exec" {
			found = true
		}
	}
	if !found {
		t.Fatalf("teammate 级派发必须留一行账本，实际活绑定：%+v", live)
	}
}

// TestRetireReleasesEverySceneOfRole —— ④ 一角色两 Work Item + 角色级现场：退场必须
// **全部**释放（角色级走 WorkspaceReleaser，两件 Work Item 逐个走 Workspaces），
// 不得只释放一个。
func TestRetireReleasesEverySceneOfRole(t *testing.T) {
	fixture := newItemFixture(t, 6)
	ctx := context.Background()
	if err := fixture.coordinator.SetPlan(ctx, itemPlan()); err != nil {
		t.Fatalf("SetPlan: %v", err)
	}
	if err := fixture.coordinator.PlanMilestone(ctx, "m-build", []WorkItemSpec{
		{ID: "wi-a", Role: "exec", Name: "第一件事"},
		{ID: "wi-b", Role: "exec", Name: "第二件事"},
	}); err != nil {
		t.Fatalf("PlanMilestone: %v", err)
	}
	// 角色级现场（老口径派发）。
	if _, err := fixture.coordinator.Dispatch(ctx, "exec", "角色级的一轮"); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	// 两件 Work Item 各自的现场：跑完置 failed（现场与记忆都留着，且不拦退场）。
	for _, id := range []string{"wi-a", "wi-b"} {
		if _, err := fixture.coordinator.DispatchItem(ctx, id); err != nil {
			t.Fatalf("DispatchItem(%s): %v", id, err)
		}
		waitItemStatus(t, fixture, id, sessionstore.TeamworkItemReview)
		if err := fixture.coordinator.FailItem(ctx, id, "留现场待处置"); err != nil {
			t.Fatalf("FailItem(%s): %v", id, err)
		}
	}
	if err := fixture.coordinator.Retire(ctx, "exec"); err != nil {
		t.Fatalf("Retire: %v", err)
	}
	if calls := fixture.calls.snapshot(); !containsName(calls, "worktree:exec") {
		t.Fatalf("退场必须释放角色级现场（脏检查在这条路上），端口调用见 %v", calls)
	}
	_, _, released := fixture.spaces.snapshot()
	for _, id := range []string{"wi-a", "wi-b"} {
		if !containsName(released, id) {
			t.Fatalf("工作项 %q 的现场没有释放（已释放 %v）——不得只释放一个", id, released)
		}
	}
	rows, err := fixture.store.ReadBindings(ctx, fixture.coordinator.Key())
	if err != nil {
		t.Fatalf("ReadBindings: %v", err)
	}
	for _, binding := range sessionstore.TeamworkBindings(rows) {
		if binding.Role == "exec" {
			t.Fatalf("退场后该角色名下不该还有活绑定: %+v", binding)
		}
	}
	plan, err := fixture.coordinator.Plan(ctx)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if member, ok := memberFor(plan, "exec"); ok && member.Worktree != "" {
		t.Fatalf("退场后成员的现场指派名应清空待重派: %+v", member)
	}
}

// failingSpaces 让指定工作项的现场释放失败（"脏现场显式报错"这条保证的替身：真实
// 实现那一侧由 Runtime 的 ReleaseWorkspaceItem → CleanupWorktree 报 git 原文）。
type failingSpaces struct {
	*fakeSpaces
	mu   sync.Mutex
	fail map[string]error
}

func (s *failingSpaces) ReleaseWorkspaceItem(ctx context.Context, binding WorkspaceBinding) error {
	s.mu.Lock()
	err := s.fail[binding.WorkItem]
	s.mu.Unlock()
	if err != nil {
		return err
	}
	return s.fakeSpaces.ReleaseWorkspaceItem(ctx, binding)
}

// newSceneFixture 建一个与 newItemFixture 同构的 fixture，只是 Workspaces 换成可注入
// 失败的替身（既有 fixture 不为"释放失败"留口子，这里不改动它）。
func newSceneFixture(t *testing.T, spaces *failingSpaces) *itemFixture {
	t.Helper()
	store := &memoryPlanStore{}
	runner := &fakeRunner{}
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
		MaxTeammates: 6,
	})
	if err != nil {
		t.Fatalf("teamwork.New: %v", err)
	}
	holder.coordinator = coordinator
	return &itemFixture{
		coordinator: coordinator, store: store, jobs: manager,
		runner: runner, spaces: spaces.fakeSpaces, queue: queue, calls: log,
	}
}

// TestRetireNamesTheItemWhoseSceneFailedToRelease —— 退场释放 Work Item 级现场时，
// 失败必须**说清是哪一个**；同一批里其余现场照旧释放（不得只释放一个），失败的那一
// 份不记释放行（现场还在，账不能先销）。
func TestRetireNamesTheItemWhoseSceneFailedToRelease(t *testing.T) {
	spaces := &failingSpaces{
		fakeSpaces: &fakeSpaces{},
		fail:       map[string]error{"wi-b": errors.New("worktree: 未提交改动")},
	}
	fixture := newSceneFixture(t, spaces)
	ctx := context.Background()
	if err := fixture.coordinator.SetPlan(ctx, itemPlan()); err != nil {
		t.Fatalf("SetPlan: %v", err)
	}
	if err := fixture.coordinator.PlanMilestone(ctx, "m-build", []WorkItemSpec{
		{ID: "wi-a", Role: "exec", Name: "第一件事"},
		{ID: "wi-b", Role: "exec", Name: "第二件事"},
	}); err != nil {
		t.Fatalf("PlanMilestone: %v", err)
	}
	for _, id := range []string{"wi-a", "wi-b"} {
		if _, err := fixture.coordinator.DispatchItem(ctx, id); err != nil {
			t.Fatalf("DispatchItem(%s): %v", id, err)
		}
		waitItemStatus(t, fixture, id, sessionstore.TeamworkItemReview)
		if err := fixture.coordinator.FailItem(ctx, id, "留现场待处置"); err != nil {
			t.Fatalf("FailItem(%s): %v", id, err)
		}
	}
	err := fixture.coordinator.Retire(ctx, "exec")
	if err == nil {
		t.Fatal("有现场释放不了时退场必须显式报错，不得静默跳过")
	}
	if !strings.Contains(err.Error(), "wi-b") {
		t.Fatalf("错误必须点明失败的是哪一个工作项，得到 %v", err)
	}
	if _, _, released := fixture.spaces.snapshot(); !containsName(released, "wi-a") {
		t.Fatalf("同一批里其余的现场照旧要释放（不得只释放一个），已释放 %v", released)
	}
	rows, err := fixture.store.ReadBindings(ctx, fixture.coordinator.Key())
	if err != nil {
		t.Fatalf("ReadBindings: %v", err)
	}
	if _, alive := sessionstore.TeamworkBindings(rows)["wi-b"]; !alive {
		t.Fatal("释放失败的现场不该记成已释放（账不先销，现场还在）")
	}
}
