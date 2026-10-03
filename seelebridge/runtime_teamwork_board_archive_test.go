package seelebridge

// runtime_teamwork_board_archive_test.go — 钉住团队看板**存档 + 生命周期**（契约
// docs/arch/session-board-metadata-lifecycle.md §5/§6/§7）：
//   - team_plan 写入新版本时，旧版先显式收口（closed/team.replan），新版再开
//     （opened_at、seq=1）——写序即事实；
//   - §7 指纹节流：载荷不变不写盘，也不推进 seq；
//   - §6 重启恢复：活体给不出看板时用存档快照，标 Recovered=true 且 Stale=true；
//   - state=closed 的存档仍然退场（返回 nil），不是一次"恢复"；
//   - 存档真的落进存储后端（metadata/board_team.json），换一个 Runtime 实例冷读仍在。

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	seetelemetry "github.com/RedHuang-0622/seelex/seelebridge/internal/telemetry"
	seeteamwork "github.com/RedHuang-0622/seelex/seelebridge/teamwork"
	"github.com/RedHuang-0622/seelex/sessionstore"
)

// memBoardStore 是看板存档的进程内实现（并记录写序——"先关旧、后开新"这条
// 只能靠写序验证，盘上最终态看不出来）。
//
// 它实现整面 BoardRepository（含 goal 面）：本文件只验团队那一半，goal 的两个方法
// 留作"这个桩不支持 goal 看板"的显式失败，而不是让类型断言在装配处就崩掉。
type memBoardStore struct {
	mu     sync.Mutex
	metas  map[sessionstore.Key]sessionstore.TeamBoardMeta
	writes []sessionstore.TeamBoardMeta
}

func (m *memBoardStore) WriteGoalBoard(context.Context, sessionstore.Key, sessionstore.GoalBoardMeta) error {
	return errors.New("测试桩：不支持 goal 看板存档")
}

func (m *memBoardStore) ReadGoalBoard(context.Context, sessionstore.Key) (sessionstore.GoalBoardMeta, error) {
	return sessionstore.GoalBoardMeta{}, fs.ErrNotExist
}

func (m *memBoardStore) WriteTeamBoard(_ context.Context, key sessionstore.Key, meta sessionstore.TeamBoardMeta) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.metas == nil {
		m.metas = map[sessionstore.Key]sessionstore.TeamBoardMeta{}
	}
	m.metas[key] = meta
	m.writes = append(m.writes, meta)
	return nil
}

func (m *memBoardStore) ReadTeamBoard(_ context.Context, key sessionstore.Key) (sessionstore.TeamBoardMeta, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	meta, ok := m.metas[key]
	if !ok {
		return sessionstore.TeamBoardMeta{}, fs.ErrNotExist
	}
	return meta, nil
}

func (m *memBoardStore) snapshot() []sessionstore.TeamBoardMeta {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]sessionstore.TeamBoardMeta(nil), m.writes...)
}

func (m *memBoardStore) latest(key sessionstore.Key) (sessionstore.TeamBoardMeta, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	meta, ok := m.metas[key]
	return meta, ok
}

// archiveBackend 装配一份带存档面的 backend（计划面用内存桩）。
func archiveBackend(plans *memPlanStore, boards *memBoardStore, sessionID string) TeamworkBackend {
	return TeamworkBackend{
		Store:  plans,
		Boards: boards,
		KeyFor: func(id string) (sessionstore.Key, bool) {
			if id != sessionID {
				return sessionstore.Key{}, false
			}
			return sessionstore.Key{ProjectID: "p-team", SessionID: sessionID}, true
		},
		MaxTeammates: 6,
	}
}

// boardPlanArgs 造一份最小合法计划（一个里程碑 + 一个在编角色；阶段口径已退场）。
func boardPlanArgs(teamID string, version int) string {
	payload := map[string]any{
		"team_id": teamID,
		"version": version,
		"milestones": []map[string]any{{"id": "m-design", "name": "设计"}},
		"members": []map[string]any{{"role": "arch"}},
	}
	encoded, _ := json.Marshal(payload)
	return string(encoded)
}

func TestTeamBoardArchiveVersionProgressionClosesOld(t *testing.T) {
	r := newTestRuntime(t)
	defer r.Shutdown()
	boards := &memBoardStore{}
	if err := r.SetTeamworkBackend(archiveBackend(&memPlanStore{}, boards, "s-team")); err != nil {
		t.Fatalf("SetTeamworkBackend: %v", err)
	}
	ctx := seetelemetry.WithSessionID(context.Background(), "s-team")
	key := sessionstore.Key{ProjectID: "p-team", SessionID: "s-team"}

	if _, err := r.teamPlanHandler(ctx, boardPlanArgs("t-archive", 1)); err != nil {
		t.Fatalf("team_plan(v1): %v", err)
	}
	writes := boards.snapshot()
	if len(writes) != 1 {
		t.Fatalf("首次 team_plan 应恰好写一版存档，实际 %d：%+v", len(writes), writes)
	}
	first := writes[0]
	if first.State != sessionstore.BoardStateActive || first.Version != 1 || first.Seq != 1 {
		t.Fatalf("首版应为 active v1 seq=1：%+v", first)
	}
	if first.OpenedAt <= 0 || first.UpdatedAt <= 0 {
		t.Fatalf("active 存档必须带 opened_at/updated_at：%+v", first)
	}
	if len(first.Snapshot) == 0 {
		t.Fatal("active 存档必须带快照载荷（否则重启无从恢复）")
	}

	// 版本递进：旧版先显式收口（team.replan），再写新的 active（opened_at、seq=1）。
	if _, err := r.teamPlanHandler(ctx, boardPlanArgs("t-archive", 2)); err != nil {
		t.Fatalf("team_plan(v2): %v", err)
	}
	writes = boards.snapshot()
	if len(writes) != 3 {
		t.Fatalf("版本递进应写两次（关旧 + 开新），累计 3，实际 %d：%+v", len(writes), writes)
	}
	closed := writes[1]
	if closed.State != sessionstore.BoardStateClosed || closed.ClosedReason != sessionstore.BoardCloseTeamReplan {
		t.Fatalf("旧版必须先记为 closed/team.replan：%+v", closed)
	}
	if closed.Version != 1 {
		t.Fatalf("被收口的是旧版本 v1：%+v", closed)
	}
	if closed.ClosedAt <= 0 {
		t.Fatal("closed 存档必须带 closed_at")
	}
	opened := writes[2]
	if opened.State != sessionstore.BoardStateActive || opened.Version != 2 || opened.Seq != 1 {
		t.Fatalf("新版必须重新开一版 active v2 seq=1（不是续 seq）：%+v", opened)
	}
	latest, ok := boards.latest(key)
	if !ok || latest.State != sessionstore.BoardStateActive || latest.Version != 2 {
		t.Fatalf("盘上最终态应是 active v2：%+v", latest)
	}
}

func TestTeamBoardArchiveThrottlesIdenticalPayload(t *testing.T) {
	r := newTestRuntime(t)
	defer r.Shutdown()
	boards := &memBoardStore{}
	if err := r.SetTeamworkBackend(archiveBackend(&memPlanStore{}, boards, "s-team")); err != nil {
		t.Fatalf("SetTeamworkBackend: %v", err)
	}
	ctx := seetelemetry.WithSessionID(context.Background(), "s-team")
	key := sessionstore.Key{ProjectID: "p-team", SessionID: "s-team"}

	if _, err := r.teamPlanHandler(ctx, boardPlanArgs("t-archive", 1)); err != nil {
		t.Fatalf("team_plan: %v", err)
	}
	if got := len(boards.snapshot()); got != 1 {
		t.Fatalf("team_plan 成功应写一次存档，实际 %d", got)
	}

	// 计划/审计/作业都没变 → 载荷逐字节不变 → §7 指纹节流：不写盘、不推进 seq。
	r.archiveTeamBoard(ctx)
	if got := len(boards.snapshot()); got != 1 {
		t.Fatalf("载荷不变时不得写盘（指纹节流），实际写 %d 次", got)
	}
	latest, ok := boards.latest(key)
	if !ok || latest.Seq != 1 {
		t.Fatalf("节流不得推进 seq：%+v", latest)
	}
}

func TestTeamworkBoardSnapshotRecoversFromArchive(t *testing.T) {
	// 兜底读：活体没有计划，但存档里躺着一份 active 快照。
	r := newTestRuntime(t)
	defer r.Shutdown()
	boards := &memBoardStore{}
	if err := r.SetTeamworkBackend(archiveBackend(&memPlanStore{}, boards, "s-team")); err != nil {
		t.Fatalf("SetTeamworkBackend: %v", err)
	}
	key := sessionstore.Key{ProjectID: "p-team", SessionID: "s-team"}
	snapshot := json.RawMessage(`{"team_id":"t-archive","version":3,"milestones":[{"id":"m-design","name":"设计"}]}`)
	if err := boards.WriteTeamBoard(context.Background(), key, sessionstore.TeamBoardMeta{
		BoardLifecycle: sessionstore.BoardLifecycle{
			Kind: sessionstore.BoardKindTeam, State: sessionstore.BoardStateActive, Seq: 2,
		},
		TeamID: "t-archive", Version: 3, Snapshot: snapshot,
	}); err != nil {
		t.Fatalf("WriteTeamBoard: %v", err)
	}

	board := r.TeamworkBoardSnapshot("s-team")
	if board == nil {
		t.Fatal("活体无计划时应回落到存档快照（否则重启后面板一片空白）")
	}
	if !board.Recovered {
		t.Fatal("从存档恢复的看板必须标 Recovered=true（前端要能区分活体与存档）")
	}
	if !board.Stale {
		t.Fatal("恢复的作业行来自上一个进程（jobs I-4 句柄只在内存）：必须标 Stale=true")
	}
	if board.TeamID != "t-archive" || board.Version != 3 {
		t.Fatalf("恢复出的计划身份必须来自存档：%+v", board)
	}
	if len(board.Milestones) != 1 || board.Milestones[0].ID != "m-design" {
		t.Fatalf("恢复出的里程碑必须来自存档快照：%+v", board.Milestones)
	}
}

func TestTeamworkBoardSnapshotNilWhenArchiveClosed(t *testing.T) {
	// 显式收口过（state=closed）的存档不是资源：看板退场就是退场。
	r := newTestRuntime(t)
	defer r.Shutdown()
	boards := &memBoardStore{}
	if err := r.SetTeamworkBackend(archiveBackend(&memPlanStore{}, boards, "s-team")); err != nil {
		t.Fatalf("SetTeamworkBackend: %v", err)
	}
	key := sessionstore.Key{ProjectID: "p-team", SessionID: "s-team"}
	if err := boards.WriteTeamBoard(context.Background(), key, sessionstore.TeamBoardMeta{
		BoardLifecycle: sessionstore.BoardLifecycle{
			Kind: sessionstore.BoardKindTeam, State: sessionstore.BoardStateClosed,
			ClosedAt: 1760000000, ClosedReason: sessionstore.BoardCloseTeamClose,
		},
		TeamID: "t-archive", Version: 1,
		Snapshot: json.RawMessage(`{"team_id":"t-archive","version":1,"milestones":[{"id":"m-design"}]}`),
	}); err != nil {
		t.Fatalf("WriteTeamBoard: %v", err)
	}
	if board := r.TeamworkBoardSnapshot("s-team"); board != nil {
		t.Fatalf("state=closed 的存档不得恢复出看板，得到 %+v", board)
	}
}

func TestTeamBoardArchiveSurvivesColdRuntime(t *testing.T) {
	// "重启"验收：存档真的落进存储后端（metadata/board_team.json），换一个
	// Runtime 实例（活体计划面为空）冷读仍在——存档不在 Runtime 的内存里。
	root := t.TempDir()
	router, err := sessionstore.NewRouter(filepath.Join(root, "session-storage.json"), root)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	t.Cleanup(func() { _ = router.Close() })
	plans, ok := router.TeamworkFor()
	if !ok {
		t.Fatal("JSON 后端应提供 teamwork 读/写面")
	}
	boards, ok := router.BoardsFor()
	if !ok {
		t.Fatal("JSON 后端应提供团队看板存档面（BoardsFor）")
	}

	first := newTestRuntime(t)
	planStore := seeteamwork.NewPlanStore(plans)
	if err := first.SetTeamworkBackend(TeamworkBackend{
		Store: planStore, Boards: boards,
		KeyFor: func(id string) (sessionstore.Key, bool) {
			return sessionstore.Key{ProjectID: "p-archive", SessionID: id}, true
		},
		MaxTeammates: 6,
	}); err != nil {
		t.Fatalf("SetTeamworkBackend: %v", err)
	}
	ctx := seetelemetry.WithSessionID(context.Background(), "s-cold")
	if _, err := first.teamPlanHandler(ctx, boardPlanArgs("t-cold", 1)); err != nil {
		t.Fatalf("team_plan: %v", err)
	}
	first.Shutdown()

	// 存档落在 metadata/board_team.json（模块 head 的固定路径）。
	found := false
	_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr == nil && !entry.IsDir() && entry.Name() == "board_team.json" {
			found = true
		}
		return nil
	})
	if !found {
		t.Fatalf("存档必须落 metadata/board_team.json（根 %s 下没找到）", root)
	}

	// 换一个 Runtime 实例冷读：活体计划面空缺，只有存档可用。
	second := newTestRuntime(t)
	defer second.Shutdown()
	if err := second.SetTeamworkBackend(TeamworkBackend{
		Store: &memPlanStore{}, Boards: boards,
		KeyFor: func(id string) (sessionstore.Key, bool) {
			return sessionstore.Key{ProjectID: "p-archive", SessionID: id}, true
		},
		MaxTeammates: 6,
	}); err != nil {
		t.Fatalf("SetTeamworkBackend(cold): %v", err)
	}
	board := second.TeamworkBoardSnapshot("s-cold")
	if board == nil {
		t.Fatal("重启后快照必须仍在（从存档冷读）")
	}
	if !board.Recovered || board.TeamID != "t-cold" || board.Version != 1 {
		t.Fatalf("冷读应恢复出存档里的看板：%+v", board)
	}
}

// TestEveryTeamworkMutationHandlerRefreshesBoardArchive 是接线守卫（设计契约 §10 第 5 条）：
// **每一个改变编排现状的 team_* 动作都必须同时做两件事**——
//   - 失效看板缓存（否则面板要等到下一个 team_* 才看到这次变化，就是"静默陈旧"）；
//   - 刷新看板存档（否则重启后的快照停在上一次动作之前）。
//
// 为什么是静态断言而不是行为用例：行为用例只能覆盖已经接上的那条路径，而"漏焊"的表现
// 恰恰是某个 handler 少了这两行——文件级断言在这里最直接（也是本仓既有的守卫风格）。
// 动作清单对齐契约 §5 的 update 时机表：plan / dispatch / join / milestone / retire。
func TestEveryTeamworkMutationHandlerRefreshesBoardArchive(t *testing.T) {
	source, err := os.ReadFile("runtime_teamwork.go")
	if err != nil {
		t.Fatalf("读 runtime_teamwork.go: %v", err)
	}
	text := string(source)
	for _, handler := range []string{
		"teamPlanHandler", "teamDispatchHandler", "teamJoinHandler",
		"teamMilestoneHandler", "teamRetireHandler",
	} {
		body, ok := teamworkHandlerBody(text, handler)
		if !ok {
			t.Fatalf("%s 未找到（守卫失效：handler 被改名或搬走了？）", handler)
		}
		if !strings.Contains(body, "invalidateTeamworkBoard()") {
			t.Fatalf("%s 成功后没有失效看板缓存：面板会静默陈旧（要等到下一个 team_* 才刷新）", handler)
		}
		if !strings.Contains(body, "archiveTeamBoard(ctx)") {
			t.Fatalf("%s 成功后没有刷新看板存档：重启后的快照会停在上一次动作之前", handler)
		}
	}
}

// teamworkHandlerBody 截出一个 handler 的函数体（到下一个顶层 func 为止）。
func teamworkHandlerBody(source, name string) (string, bool) {
	start := strings.Index(source, "func (r *Runtime) "+name+"(")
	if start < 0 {
		return "", false
	}
	rest := source[start+1:]
	if end := strings.Index(rest, "\nfunc "); end >= 0 {
		return rest[:end], true
	}
	return rest, true
}
