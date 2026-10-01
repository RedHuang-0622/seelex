package seelebridge

// runtime_teamwork_board_test.go — 钉住团队看板只读投影（契约
// docs/arch/team-board-gui-tui-contract.md §3/§4）：
//   - 未装配 teamwork → nil（面板整块退场，不留空壳）；
//   - 装配了但该会话没有计划 → nil；
//   - 有计划 → 阶段/在编/里程碑/审计逐字段搬出来，且**顺序唯一事实**是 depends_on；
//   - 阶段/角色是桥给出的**权威归属**（不是靠前端回落链猜的）；
//   - 句柄投影过期（jobs I-4）显形为 Stale；
//   - 采集是高频路径：「计划 + 审计」两份文件读按会话缓存，team_* 之后失效。

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	seeletelemetry "github.com/RedHuang-0622/seelex/seelebridge/internal/telemetry"
	"github.com/RedHuang-0622/seelex/sessionstore"
)

// countingPlanStore 记读写次数：性能口径（§4）要靠计数验证，不能靠"看起来很快"。
type countingPlanStore struct {
	inner *memPlanStore

	mu        sync.Mutex
	planReads int
	evReads   int
}

func newCountingPlanStore() *countingPlanStore {
	return &countingPlanStore{inner: &memPlanStore{}}
}

func (s *countingPlanStore) WritePlan(ctx context.Context, key sessionstore.Key, plan sessionstore.TeamworkPlan, max int) error {
	return s.inner.WritePlan(ctx, key, plan, max)
}

func (s *countingPlanStore) ReadPlan(ctx context.Context, key sessionstore.Key) (sessionstore.TeamworkPlan, error) {
	s.mu.Lock()
	s.planReads++
	s.mu.Unlock()
	return s.inner.ReadPlan(ctx, key)
}

func (s *countingPlanStore) AppendEvent(ctx context.Context, key sessionstore.Key, event sessionstore.TeamworkEvent) error {
	return s.inner.AppendEvent(ctx, key, event)
}

func (s *countingPlanStore) ReadEvents(ctx context.Context, key sessionstore.Key) ([]sessionstore.TeamworkEvent, error) {
	s.mu.Lock()
	s.evReads++
	s.mu.Unlock()
	return s.inner.ReadEvents(ctx, key)
}

func (s *countingPlanStore) reads() (int, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.planReads, s.evReads
}

func countingBackend(store *countingPlanStore, sessionID string) TeamworkBackend {
	return TeamworkBackend{
		Store: store,
		KeyFor: func(id string) (sessionstore.Key, bool) {
			if id != sessionID {
				return sessionstore.Key{}, false
			}
			return sessionstore.Key{ProjectID: "p-team", SessionID: sessionID}, true
		},
		MaxTeammates: 6,
	}
}

func boardPlanFixture() sessionstore.TeamworkPlan {
	return sessionstore.TeamworkPlan{
		TeamID:  "team-board-gui-tui",
		Version: 2,
		Stages: []sessionstore.TeamworkStage{
			{ID: "design", Roles: []string{"arch"}},
			{ID: "impl", Roles: []string{"impl_core", "impl_ui"}, DependsOn: []string{"design"}},
		},
		Members: []sessionstore.TeamworkMember{
			{Role: "arch", RoleSessionID: "s-team-arch", ToolsPolicy: "readwrite"},
			{Role: "impl_ui", RoleSessionID: "s-team-impl_ui", ToolsPolicy: "readwrite", Worktree: "wt-ui"},
		},
		Milestones: []sessionstore.TeamworkMilestone{
			{ID: "m-design", After: []string{"design"}, Content: "契约定稿", Status: "done"},
		},
	}
}

func TestTeamworkBoardSnapshotNilWithoutBackend(t *testing.T) {
	r := newTestRuntime(t)
	defer r.Shutdown()
	if board := r.TeamworkBoardSnapshot("s-team"); board != nil {
		t.Fatalf("未装配 teamwork 时应返回 nil，得到 %+v", board)
	}
}

func TestTeamworkBoardSnapshotNilWithoutPlan(t *testing.T) {
	r := newTestRuntime(t)
	defer r.Shutdown()
	if err := r.SetTeamworkBackend(teamworkTestBackend(&memPlanStore{}, "s-team")); err != nil {
		t.Fatalf("SetTeamworkBackend: %v", err)
	}
	if board := r.TeamworkBoardSnapshot("s-team"); board != nil {
		t.Fatalf("没有计划时应返回 nil（不留空壳），得到 %+v", board)
	}
	if board := r.TeamworkBoardSnapshot("s-other"); board != nil {
		t.Fatalf("解析不出作用域的会话应返回 nil，得到 %+v", board)
	}
}

func TestTeamworkBoardSnapshotProjectsPlan(t *testing.T) {
	r := newTestRuntime(t)
	defer r.Shutdown()
	store := &memPlanStore{}
	if err := r.SetTeamworkBackend(teamworkTestBackend(store, "s-team")); err != nil {
		t.Fatalf("SetTeamworkBackend: %v", err)
	}
	ctx := seeletelemetry.WithSessionID(context.Background(), "s-team")
	if _, err := r.teamPlanHandler(ctx, `{
		"team_id": "team-board-gui-tui",
		"version": 2,
		"stages": [{"id":"design","roles":["arch"]},{"id":"impl","roles":["impl_ui"],"depends_on":["design"]}],
		"members": [{"role":"arch","role_session_id":"s-team-arch","tools_policy":"readwrite"}],
		"milestones": [{"id":"m-design","after":["design"],"content":"契约定稿"}]
	}`); err != nil {
		t.Fatalf("team_plan: %v", err)
	}

	board := r.TeamworkBoardSnapshot("s-team")
	if board == nil {
		t.Fatal("有计划的会话必须给出看板投影")
	}
	if board.TeamID != "team-board-gui-tui" || board.Version != 2 {
		t.Fatalf("计划头搬运不一致：%+v", board)
	}
	if board.MaxMembers != 6 {
		t.Fatalf("在编上限必须来自 TeamworkBackend.MaxTeammates（看板要能区分正常与顶到上限）：%d", board.MaxMembers)
	}
	if len(board.Stages) != 2 || board.Stages[1].ID != "impl" ||
		len(board.Stages[1].DependsOn) != 1 || board.Stages[1].DependsOn[0] != "design" {
		t.Fatalf("阶段与依赖边必须原样搬运（顺序的唯一事实）：%+v", board.Stages)
	}
	if len(board.Members) != 1 || board.Members[0].RoleSessionID != "s-team-arch" ||
		board.Members[0].ToolsPolicy != "readwrite" {
		t.Fatalf("在编成员搬运不一致：%+v", board.Members)
	}
	if len(board.Milestones) != 1 || board.Milestones[0].After[0] != "design" {
		t.Fatalf("里程碑搬运不一致：%+v", board.Milestones)
	}
	// team_plan 落一条审计：看板必须能看到它（口径：审计是派发/里程碑/retire 的事实流水）。
	if len(board.Events) == 0 || board.Events[0].Kind != "plan" {
		t.Fatalf("审计流水缺 plan 事件：%+v", board.Events)
	}
	if board.Events[0].At == 0 {
		t.Fatal("审计事件必须带 unix 秒时间戳（面板据此显示 HH:MM）")
	}
	if board.Stale {
		t.Fatal("计划里没有句柄投影时不得标过期")
	}
}

func TestTeamworkBoardSnapshotMarksStaleHandleProjection(t *testing.T) {
	r := newTestRuntime(t)
	defer r.Shutdown()
	store := &memPlanStore{}
	if err := r.SetTeamworkBackend(teamworkTestBackend(store, "s-team")); err != nil {
		t.Fatalf("SetTeamworkBackend: %v", err)
	}
	plan := boardPlanFixture()
	// 计划里残留着上一个进程的句柄（jobs I-4：句柄只在内存）。
	plan.State = sessionstore.TeamworkState{Jobs: map[string]string{"arch": "a9"}}
	key := sessionstore.Key{ProjectID: "p-team", SessionID: "s-team"}
	if err := store.WritePlan(context.Background(), key, plan, 6); err != nil {
		t.Fatalf("WritePlan: %v", err)
	}
	board := r.TeamworkBoardSnapshot("s-team")
	if board == nil {
		t.Fatal("有计划的会话必须给出看板投影")
	}
	if !board.Stale {
		t.Fatal("计划里有句柄、句柄表里查不到 → 必须显形为过期（不假装是最新事实）")
	}
	if len(board.Jobs) != 0 {
		t.Fatalf("句柄表为空时不应凭空造作业行：%+v", board.Jobs)
	}
}

func TestTeamworkBoardSnapshotCachesFileReads(t *testing.T) {
	r := newTestRuntime(t)
	defer r.Shutdown()
	store := newCountingPlanStore()
	if err := r.SetTeamworkBackend(countingBackend(store, "s-team")); err != nil {
		t.Fatalf("SetTeamworkBackend: %v", err)
	}
	ctx := seeletelemetry.WithSessionID(context.Background(), "s-team")
	if _, err := r.teamPlanHandler(ctx, `{
		"team_id": "t", "stages": [{"id":"a","roles":["x"]}],
		"members": [{"role":"x","role_session_id":"s-team-x"}]
	}`); err != nil {
		t.Fatalf("team_plan: %v", err)
	}

	for index := 0; index < 100; index++ {
		if board := r.TeamworkBoardSnapshot("s-team"); board == nil {
			t.Fatalf("第 %d 次采集丢掉了看板", index)
		}
	}
	planReads, eventReads := store.reads()
	if planReads != 1 || eventReads != 1 {
		t.Fatalf("100 次采集应只读一次计划 + 一次审计（会话快照是高频路径），实际 %d 计划 / %d 审计",
			planReads, eventReads)
	}

	// team_* 成功返回后必须失效：下一次采集恰好再读一次。
	if _, err := r.teamPlanHandler(ctx, `{
		"team_id": "t", "version": 2, "stages": [{"id":"a","roles":["x"]}],
		"members": [{"role":"x","role_session_id":"s-team-x"}]
	}`); err != nil {
		t.Fatalf("team_plan(2): %v", err)
	}
	board := r.TeamworkBoardSnapshot("s-team")
	if board == nil || board.Version != 2 {
		t.Fatalf("缓存失效后必须读到新计划：%+v", board)
	}
	planReads, _ = store.reads()
	if planReads != 2 {
		t.Fatalf("team_plan 之后应恰好再读一次计划，实际 %d", planReads)
	}
}

func TestTeamworkBoardSnapshotCapsAuditWindow(t *testing.T) {
	r := newTestRuntime(t)
	defer r.Shutdown()
	store := &memPlanStore{}
	if err := r.SetTeamworkBackend(teamworkTestBackend(store, "s-team")); err != nil {
		t.Fatalf("SetTeamworkBackend: %v", err)
	}
	key := sessionstore.Key{ProjectID: "p-team", SessionID: "s-team"}
	if err := store.WritePlan(context.Background(), key, boardPlanFixture(), 6); err != nil {
		t.Fatalf("WritePlan: %v", err)
	}
	for index := 0; index < teamworkBoardEventLimit+8; index++ {
		if err := store.AppendEvent(context.Background(), key, sessionstore.TeamworkEvent{
			At: time.Unix(int64(index+1), 0), Kind: "dispatch", Detail: "x",
		}); err != nil {
			t.Fatalf("AppendEvent: %v", err)
		}
	}
	board := r.TeamworkBoardSnapshot("s-team")
	if board == nil {
		t.Fatal("有计划的会话必须给出看板投影")
	}
	if len(board.Events) != teamworkBoardEventLimit {
		t.Fatalf("审计必须限窗到 %d 条，实际 %d", teamworkBoardEventLimit, len(board.Events))
	}
	// 取的是**最近**的：末条 = 最后追加的那条。
	if board.Events[len(board.Events)-1].At != int64(teamworkBoardEventLimit+8) {
		t.Fatalf("审计应取最近若干条（保持发生顺序），末条 = %+v", board.Events[len(board.Events)-1])
	}
}

func TestTeamworkBoardSnapshotNegativeCache(t *testing.T) {
	// 绝大多数会话**没有**团队计划，而快照是高频路径：踩空也必须进缓存，
	// 否则每次采集都会去 stat 一个不存在的计划文件。
	r := newTestRuntime(t)
	defer r.Shutdown()
	store := newCountingPlanStore()
	if err := r.SetTeamworkBackend(countingBackend(store, "s-team")); err != nil {
		t.Fatalf("SetTeamworkBackend: %v", err)
	}
	for index := 0; index < 50; index++ {
		if board := r.TeamworkBoardSnapshot("s-team"); board != nil {
			t.Fatalf("没有计划时必须返回 nil，得到 %+v", board)
		}
	}
	if planReads, _ := store.reads(); planReads != 1 {
		t.Fatalf("没有计划的会话只该读一次（负缓存），实际读 %d 次", planReads)
	}

	// team_plan 之后失效：下一次采集必须看到新计划（负缓存不得把面板永久钉在"没有"）。
	ctx := seeletelemetry.WithSessionID(context.Background(), "s-team")
	if _, err := r.teamPlanHandler(ctx, `{
		"team_id": "t", "stages": [{"id":"a","roles":["x"]}],
		"members": [{"role":"x","role_session_id":"s-team-x"}]
	}`); err != nil {
		t.Fatalf("team_plan: %v", err)
	}
	if board := r.TeamworkBoardSnapshot("s-team"); board == nil {
		t.Fatal("负缓存必须在 team_plan 之后失效（否则新建的团队永远不显示）")
	}
}

func TestTeamworkBoardSnapshotPortsRoleFromSubject(t *testing.T) {
	// 归属口径：作业行的角色/阶段由桥给权威值——阶段 = record.Node，
	// 角色 = 作用域主体 emp_<role> 反解。渲染件里的回落链不该被走到。
	if got := roleFromSubject("emp_impl_ui"); got != "impl_ui" {
		t.Fatalf("emp_ 前缀应反解成角色名，得到 %q", got)
	}
	if got := roleFromSubject("main"); got != "main" {
		t.Fatalf("不是 emp_ 前缀时原样返回（不猜），得到 %q", got)
	}
	if !strings.HasPrefix(strings.Join([]string{teamworkSubjectPrefix(), "arch"}, ""), "emp_") {
		t.Fatal("作用域前缀必须与 teamwork.SubjectForRole 同源")
	}
}

// BenchmarkTeamworkBoardSnapshot 是采集路径的**量级证据**（契约 §4）：
// 稳态下每次采集不该碰文件——b.N 次里计划/审计读次数恒为 1。
func BenchmarkTeamworkBoardSnapshot(b *testing.B) {
	r := newTestRuntime(b)
	defer r.Shutdown()
	store := newCountingPlanStore()
	if err := r.SetTeamworkBackend(countingBackend(store, "s-team")); err != nil {
		b.Fatalf("SetTeamworkBackend: %v", err)
	}
	key := sessionstore.Key{ProjectID: "p-team", SessionID: "s-team"}
	if err := store.WritePlan(context.Background(), key, boardPlanFixture(), 6); err != nil {
		b.Fatalf("WritePlan: %v", err)
	}
	for index := 0; index < teamworkBoardEventLimit; index++ {
		if err := store.AppendEvent(context.Background(), key, sessionstore.TeamworkEvent{
			At: time.Unix(int64(index+1), 0), Kind: "dispatch", Detail: "x",
		}); err != nil {
			b.Fatalf("AppendEvent: %v", err)
		}
	}
	// 预热一次（冷启动那一次读文件），之后的采集全部走缓存。
	if board := r.TeamworkBoardSnapshot("s-team"); board == nil {
		b.Fatal("预热采集失败")
	}
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		if board := r.TeamworkBoardSnapshot("s-team"); board == nil {
			b.Fatal("采集丢掉了看板")
		}
	}
	b.StopTimer()
	planReads, eventReads := store.reads()
	if planReads != 1 || eventReads != 1 {
		b.Fatalf("稳态采集不应碰文件：计划读 %d 次、审计读 %d 次", planReads, eventReads)
	}
}
