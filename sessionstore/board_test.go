package sessionstore

// board_test.go — 两块看板存档（moduleBoardGoal / moduleBoardTeam）的验收。
//
// 钉住的是设计稿 docs/arch/session-board-metadata-lifecycle.md 的不变式：
//   - §3.1  active seq 的 goal 与 history goals 分离（同一 id 不许同时在里面）；
//   - §4    两个模块 = 两个文件、两把锁（不与领域模块共用）；
//   - §6    重启（换一个 repository 实例冷读）后快照仍在；
//   - §8-I6 未知/损坏存档视为"没有存档"，不半读。

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func boardFixture(t *testing.T) (*jsonRepository, Key) {
	t.Helper()
	repository, err := newJSONRepository(t.TempDir(), storageSettings{})
	if err != nil {
		t.Fatalf("newJSONRepository: %v", err)
	}
	key := Key{ProjectID: "project-board", SessionID: "session-board"}
	return repository, key
}

func sampleGoalBoard() GoalBoardMeta {
	return GoalBoardMeta{
		BoardLifecycle: BoardLifecycle{
			Kind: BoardKindGoal, State: BoardStateActive, Seq: 3,
			OpenedAt: 1000, UpdatedAt: 1200, Fingerprint: "sha256:active",
		},
		Active: &GoalBoardActive{
			GoalID: "g-7", Title: "团队看板接线", Status: "active",
			Statement:  "把看板接进活体 GUI",
			Acceptance: []string{"面板出现", "TUI 同源"},
			Progress:   []GoalProgress{{At: 1100, Kind: "result", Content: "接线完成"}},
			CreatedAt:  1000, UpdatedAt: 1200,
		},
		History: []GoalBoardHistory{
			{GoalID: "g-6", Title: "评审过程退场", Status: "completed", ClosedAt: 900, ClosedReason: BoardCloseGoalFinish, ProgressCount: 5},
			{GoalID: "g-5", Title: "旧目标", Status: "aborted", ClosedAt: 800, ClosedReason: BoardCloseGoalAbort, ProgressCount: 2},
		},
	}
}

func sampleTeamBoard(t *testing.T) TeamBoardMeta {
	t.Helper()
	snapshot, err := json.Marshal(map[string]any{
		"team_id": "v-model",
		"version": 2,
		"milestones": []map[string]any{{"id": "m-impl", "name": "实现"}},
		"jobs":    []map[string]any{{"handle": "a7", "state": "running", "bytes": 128}},
	})
	if err != nil {
		t.Fatalf("marshal snapshot: %v", err)
	}
	return TeamBoardMeta{
		BoardLifecycle: BoardLifecycle{
			Kind: BoardKindTeam, State: BoardStateActive, Seq: 2,
			OpenedAt: 2000, UpdatedAt: 2100, Fingerprint: "sha256:team",
		},
		TeamID: "v-model", Version: 2, Snapshot: snapshot,
	}
}

// TestGoalBoardRoundTrip：active seq 的 goal 与 history goals 各自完整往返。
func TestGoalBoardRoundTrip(t *testing.T) {
	repository, key := boardFixture(t)
	meta := sampleGoalBoard()
	if err := repository.WriteGoalBoard(context.Background(), key, meta); err != nil {
		t.Fatalf("WriteGoalBoard: %v", err)
	}
	loaded, err := repository.ReadGoalBoard(context.Background(), key)
	if err != nil {
		t.Fatalf("ReadGoalBoard: %v", err)
	}
	if loaded.Active == nil || loaded.Active.GoalID != "g-7" {
		t.Fatalf("active seq 的 goal 丢了：%+v", loaded.Active)
	}
	if len(loaded.History) != 2 || loaded.History[0].GoalID != "g-6" || loaded.History[1].ClosedReason != BoardCloseGoalAbort {
		t.Fatalf("history goals 丢了或次序变了：%+v", loaded.History)
	}
	if loaded.Seq != 3 || loaded.State != BoardStateActive {
		t.Fatalf("生命周期头丢了：%+v", loaded.BoardLifecycle)
	}
}

// TestTeamBoardRoundTrip：团队看板的载荷是不透明视图快照，原样往返（存储层不解析它）。
func TestTeamBoardRoundTrip(t *testing.T) {
	repository, key := boardFixture(t)
	meta := sampleTeamBoard(t)
	if err := repository.WriteTeamBoard(context.Background(), key, meta); err != nil {
		t.Fatalf("WriteTeamBoard: %v", err)
	}
	loaded, err := repository.ReadTeamBoard(context.Background(), key)
	if err != nil {
		t.Fatalf("ReadTeamBoard: %v", err)
	}
	if loaded.TeamID != "v-model" || loaded.Version != 2 {
		t.Fatalf("团队看板头丢了：%+v", loaded)
	}
	var snapshot struct {
		Milestones []struct {
			ID string `json:"id"`
		} `json:"milestones"`
		Jobs []struct {
			Handle string `json:"handle"`
		} `json:"jobs"`
	}
	if err := json.Unmarshal(loaded.Snapshot, &snapshot); err != nil {
		t.Fatalf("快照不是原样的 JSON：%v", err)
	}
	if len(snapshot.Milestones) != 1 || snapshot.Milestones[0].ID != "m-impl" || snapshot.Jobs[0].Handle != "a7" {
		t.Fatalf("快照内容变了：%+v", snapshot)
	}
}

// TestBoardReadMissingIsNotExist：没写过 → fs.ErrNotExist（读侧据此当成"没有存档"）。
func TestBoardReadMissingIsNotExist(t *testing.T) {
	repository, key := boardFixture(t)
	if _, err := repository.ReadGoalBoard(context.Background(), key); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("空 goal 看板 err = %v, want fs.ErrNotExist", err)
	}
	if _, err := repository.ReadTeamBoard(context.Background(), key); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("空团队看板 err = %v, want fs.ErrNotExist", err)
	}
}

// TestBoardArchiveSurvivesReopen：**重启恢复**的存储面证据——换一个 repository 实例
// 冷读（进程重起后就是这个姿势），history 与快照都必须还在。
func TestBoardArchiveSurvivesReopen(t *testing.T) {
	root := t.TempDir()
	key := Key{ProjectID: "project-reopen", SessionID: "session-reopen"}
	first, err := newJSONRepository(root, storageSettings{})
	if err != nil {
		t.Fatalf("newJSONRepository: %v", err)
	}
	if err := first.WriteGoalBoard(context.Background(), key, sampleGoalBoard()); err != nil {
		t.Fatalf("WriteGoalBoard: %v", err)
	}
	if err := first.WriteTeamBoard(context.Background(), key, sampleTeamBoard(t)); err != nil {
		t.Fatalf("WriteTeamBoard: %v", err)
	}

	second, err := newJSONRepository(root, storageSettings{})
	if err != nil {
		t.Fatalf("newJSONRepository(second): %v", err)
	}
	goalBoard, err := second.ReadGoalBoard(context.Background(), key)
	if err != nil {
		t.Fatalf("冷读 goal 看板: %v", err)
	}
	if goalBoard.Active == nil || goalBoard.Active.GoalID != "g-7" || len(goalBoard.History) != 2 {
		t.Fatalf("重启后 goal 看板不完整：%+v", goalBoard)
	}
	teamBoard, err := second.ReadTeamBoard(context.Background(), key)
	if err != nil {
		t.Fatalf("冷读团队看板: %v", err)
	}
	if len(teamBoard.Snapshot) == 0 {
		t.Fatal("重启后团队看板快照空了")
	}
}

// TestBoardModulesAreSeparateFiles：两块看板是**两个模块文件**（各自 head、各自锁），
// 不与彼此的领域模块共用文件。
func TestBoardModulesAreSeparateFiles(t *testing.T) {
	repository, key := boardFixture(t)
	if err := repository.WriteGoalBoard(context.Background(), key, sampleGoalBoard()); err != nil {
		t.Fatalf("WriteGoalBoard: %v", err)
	}
	if err := repository.WriteTeamBoard(context.Background(), key, sampleTeamBoard(t)); err != nil {
		t.Fatalf("WriteTeamBoard: %v", err)
	}
	layout := repository.layout
	for _, mod := range []storageModule{moduleBoardGoal, moduleBoardTeam} {
		if _, err := os.Stat(layout.modulePath(key, mod)); err != nil {
			t.Fatalf("模块 %q 的文件没落盘：%v", mod, err)
		}
	}
	if layout.modulePath(key, moduleBoardGoal) == layout.modulePath(key, moduleBoardTeam) {
		t.Fatal("两块看板共用一个文件")
	}
	if layout.modulePath(key, moduleBoardGoal) == layout.modulePath(key, moduleStackGoal) {
		t.Fatal("goal 看板存档与 goal 栈共用一个文件——存档必须是下游副本，不是栈本身")
	}
	// 两块看板的锁必须与彼此、与各自领域模块的锁都不同。
	if store := layout; store.mu(key, moduleBoardGoal) == store.mu(key, moduleBoardTeam) ||
		store.mu(key, moduleBoardGoal) == store.mu(key, moduleStackGoal) ||
		store.mu(key, moduleBoardTeam) == store.mu(key, moduleTeamwork) {
		t.Fatal("看板存档的模块锁与别的模块共用了一把")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(layout.modulePath(key, moduleBoardGoal)), "board_goal.json")); err != nil {
		t.Fatalf("文件名必须是 board_goal.json：%v", err)
	}
}

// TestGoalBoardRejectsSameGoalInActiveAndHistory：§3.1 的硬不变式。
func TestGoalBoardRejectsSameGoalInActiveAndHistory(t *testing.T) {
	repository, key := boardFixture(t)
	meta := sampleGoalBoard()
	meta.History[0].GoalID = "g-7" // 与 Active 撞 id
	err := repository.WriteGoalBoard(context.Background(), key, meta)
	if err == nil {
		t.Fatal("同一个 goal 同时出现在 active 与 history，写入竟然通过了")
	}
	if _, readErr := repository.ReadGoalBoard(context.Background(), key); !errors.Is(readErr, os.ErrNotExist) {
		t.Fatalf("校验失败却落了盘：%v", readErr)
	}
}

// TestGoalBoardRejectsMalformedLifecycle：生命周期头是硬约束（close 必须说明原因与时间）。
func TestGoalBoardRejectsMalformedLifecycle(t *testing.T) {
	repository, key := boardFixture(t)
	cases := []struct {
		name   string
		mutate func(*GoalBoardMeta)
	}{
		{"kind 不匹配", func(m *GoalBoardMeta) { m.Kind = BoardKindTeam }},
		{"seq 为 0", func(m *GoalBoardMeta) { m.Seq = 0 }},
		{"state 非法", func(m *GoalBoardMeta) { m.State = "paused" }},
		{"closed 无原因", func(m *GoalBoardMeta) {
			m.State, m.Active, m.ClosedAt, m.ClosedReason = BoardStateClosed, nil, 5000, ""
		}},
		{"closed 无时间", func(m *GoalBoardMeta) {
			m.State, m.Active, m.ClosedReason = BoardStateClosed, nil, BoardCloseGoalFinish
		}},
		{"未关闭却带 closed_at", func(m *GoalBoardMeta) { m.ClosedAt = 999 }},
		{"active 缺 id", func(m *GoalBoardMeta) { m.Active.GoalID = "" }},
		{"history 缺 id", func(m *GoalBoardMeta) { m.History[0].GoalID = "" }},
		{"history 重复 id", func(m *GoalBoardMeta) { m.History[1].GoalID = "g-6" }},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			meta := sampleGoalBoard()
			testCase.mutate(&meta)
			if err := repository.WriteGoalBoard(context.Background(), key, meta); err == nil {
				t.Fatalf("%s：非法存档竟然通过了", testCase.name)
			}
		})
	}
}

// TestTeamBoardRejectsActiveWithoutSnapshot：在册的团队看板没有快照 = 重启后什么都没有。
func TestTeamBoardRejectsActiveWithoutSnapshot(t *testing.T) {
	repository, key := boardFixture(t)
	meta := sampleTeamBoard(t)
	meta.Snapshot = nil
	if err := repository.WriteTeamBoard(context.Background(), key, meta); err == nil {
		t.Fatal("没有快照的在册团队看板竟然通过了")
	}
}

// TestBoardClosedKeepsClosedFacts：close 之后存档保留（§9 第 3 条），
// 且"为什么关、什么时候关"读得回来。
func TestBoardClosedKeepsClosedFacts(t *testing.T) {
	repository, key := boardFixture(t)
	meta := sampleGoalBoard()
	meta.State = BoardStateClosed
	meta.Active = nil
	meta.Seq = 4
	meta.ClosedAt = 5000
	meta.ClosedReason = BoardCloseGoalFinish
	if err := repository.WriteGoalBoard(context.Background(), key, meta); err != nil {
		t.Fatalf("WriteGoalBoard(closed): %v", err)
	}
	loaded, err := repository.ReadGoalBoard(context.Background(), key)
	if err != nil {
		t.Fatalf("ReadGoalBoard: %v", err)
	}
	if !loaded.Closed() {
		t.Fatalf("闭态没读回来：%+v", loaded.BoardLifecycle)
	}
	if loaded.ClosedReason != BoardCloseGoalFinish || loaded.ClosedAt != 5000 {
		t.Fatalf("关闭事实丢了：%+v", loaded.BoardLifecycle)
	}
	if len(loaded.History) != 2 {
		t.Fatalf("关闭不该清掉 history：%+v", loaded.History)
	}
}

// TestBoardsNotInRepositoryMainInterface：看板存档是**可选能力面**（与 teamwork 同一形态），
// 不进 Repository 主接口——主接口是"每个后端都必须给出"的能力面。
func TestBoardsNotInRepositoryMainInterface(t *testing.T) {
	repository, _ := boardFixture(t)
	boards, ok := Boards(repository)
	if !ok || boards == nil {
		t.Fatal("JSON 后端必须实现 BoardRepository")
	}
	if _, ok := any(repository).(interface {
		WriteGoalBoard(context.Context, Key, GoalBoardMeta) error
	}); !ok {
		t.Fatal("类型断言面必须成立")
	}
	var router *Router
	if _, ok := router.BoardsFor(); ok {
		t.Fatal("nil router 不该给出看板面")
	}
}
