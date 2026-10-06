package core

// goal_board_archive_read_test.go — 钉住 goal 看板存档的**读侧兜底**这条端到端
// 投影：活体栈为空时按存档重建看板（Recovered），关闭的存档不让看板诈尸，而
// 收口账本（History）在活体可用时也照常下发、且不混进活动栈。
//
// 为什么单独立文件：goal_coordinator_test.go 钉的是治理循环的推进语义；这条钉的是
// "重启/兜底时前端看到什么"——看板只有一份事实（活体栈），存档是它旁边的派生快照。

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	goaldomain "github.com/RedHuang-0622/seelex/application/core/goal"
	"github.com/RedHuang-0622/seelex/sessionstore"
)

// boardArchiveReadCtx 是本文件的测试上下文（不借用其它测试文件的包级变量）。
var boardArchiveReadCtx = context.Background()

// boardRouterForArchive 建一个临时会话存储（看板存档落在其 metadata 目录）。
func boardRouterForArchive(t *testing.T) *sessionstore.Router {
	t.Helper()
	root := t.TempDir()
	router, err := sessionstore.NewRouter(filepath.Join(root, "session-storage.json"), root)
	if err != nil {
		t.Fatalf("new session router: %v", err)
	}
	t.Cleanup(func() { _ = router.Close() })
	return router
}

// boardStoreFor 返回协调器的按会话存储入口（每次给一个新的会话存储实例：与生产
// 装配同形，协调器只从中取 Router 与活栈）。
func boardStoreFor(router *sessionstore.Router) func(string) *sessionstore.SessionContextStore {
	return func(sessionID string) *sessionstore.SessionContextStore {
		return sessionstore.NewSessionContextStore(router, sessionID)
	}
}

// writeBoardArchiveFor 直接发布一份看板存档（构造"只有存档、没有活体栈"的现场）。
func writeBoardArchiveFor(t *testing.T, router *sessionstore.Router, sessionID string, meta sessionstore.GoalBoardMeta) {
	t.Helper()
	boards, ok := router.BoardsForSession(sessionID)
	if !ok {
		t.Fatal("看板存档取用面未装配（BoardsForSession 返回 false）")
	}
	// board_kind 是存档的必填头（ValidateGoalBoardMeta）：用例只关心各自那条
	// 语义，统一在这里补齐，免得每处字面量都抄一遍。
	meta.Kind = sessionstore.BoardKindGoal
	if err := boards.WriteGoalBoard(boardArchiveReadCtx, meta); err != nil {
		t.Fatalf("写看板存档: %v", err)
	}
}

// TestGoalGovernanceViewRecoversBoardFromArchive 验证兜底读：本次进程还没碰过该
// 会话的 goal（没有 bundle）、活体栈为空，而存档里 state=active —— 此时必须按
// 存档的当前帧把看板重建出来，并标 Recovered=true（"这一帧来自快照"）。
func TestGoalGovernanceViewRecoversBoardFromArchive(t *testing.T) {
	router := boardRouterForArchive(t)
	sessionID := "session-board-recover"
	now := time.Now().UTC()
	writeBoardArchiveFor(t, router, sessionID, sessionstore.GoalBoardMeta{
		BoardLifecycle: sessionstore.BoardLifecycle{
			State: sessionstore.BoardStateActive, Seq: 2,
			OpenedAt: now.Add(-time.Hour).Unix(), UpdatedAt: now.Unix(),
		},
		Active: &sessionstore.GoalBoardActive{
			GoalID: "g-1", Title: "上一轮还在跑的目标", Status: "active",
			Acceptance: []string{"go test 全绿"},
			Progress: []sessionstore.GoalProgress{
				{At: now.Unix(), Kind: "milestone", Content: "已接线"},
			},
		},
	})

	coordinator := newGoalCoordinator(goalCoordinatorDeps{StoreFor: boardStoreFor(router)})
	view := coordinator.GoalGovernanceViewFor(sessionID)
	if view == nil {
		t.Fatal("存档里 state=active 时不应退回 nil：看板要能被重建出来")
	}
	if !view.Active || !view.Recovered {
		t.Fatalf("兜底读应 Active 且标 Recovered: %+v", view)
	}
	if view.GoalID != "g-1" || view.Title != "上一轮还在跑的目标" || view.Status != goaldomain.StatusActive {
		t.Fatalf("看板应取存档的当前帧: %+v", view)
	}
	if len(view.Stack) != 1 || view.Stack[0].ID != "g-1" || !view.Stack[0].Active {
		t.Fatalf("恢复出来的当前帧应进活动栈投影: %+v", view.Stack)
	}
	if len(view.Stack[0].ProgressAll) != 1 || view.Stack[0].ProgressAll[0].Content != "已接线" {
		t.Fatalf("恢复出来的帧应带打点流水: %+v", view.Stack[0].ProgressAll)
	}
	if len(view.History) != 0 {
		t.Fatalf("没有收口条目不产生空壳账本: %+v", view.History)
	}
}

// TestGoalGovernanceViewRetiresBoardOnClosedArchive 验证"看板退场"：存档 state=closed
// 时不得把上一轮的帧带回看板（Active 保持 false，活动栈不得有帧），但收口账本
// （History）该下发。
func TestGoalGovernanceViewRetiresBoardOnClosedArchive(t *testing.T) {
	router := boardRouterForArchive(t)
	sessionID := "session-board-closed"
	closedAt := time.Now().UTC()
	writeBoardArchiveFor(t, router, sessionID, sessionstore.GoalBoardMeta{
		BoardLifecycle: sessionstore.BoardLifecycle{
			State: sessionstore.BoardStateClosed, Seq: 3,
			ClosedAt: closedAt.Unix(), ClosedReason: "goal.finish",
		},
		History: []sessionstore.GoalBoardHistory{{
			GoalID: "g-1", Title: "已收口目标", Status: "completed",
			ClosedAt: closedAt.Unix(), ClosedReason: "goal.finish",
		}},
	})

	coordinator := newGoalCoordinator(goalCoordinatorDeps{StoreFor: boardStoreFor(router)})
	// 让该会话有活体 bundle（活体栈为空），走"活体栈为空 → 读存档"这条分支。
	coordinator.bundleFor(sessionID)
	view := coordinator.GoalGovernanceViewFor(sessionID)
	if view == nil {
		t.Fatal("有 bundle 的会话应给出治理视图（Active:false → 面板隐藏）")
	}
	if view.Active || view.Recovered {
		t.Fatalf("closed 存档不得让看板出现: %+v", view)
	}
	if view.GoalID != "" || len(view.Stack) != 0 {
		t.Fatalf("closed 存档不得带回当前帧（不留空壳）: %+v", view)
	}
	if len(view.History) != 1 || view.History[0].GoalID != "g-1" ||
		view.History[0].ClosedReason != "goal.finish" {
		t.Fatalf("收口账本应照常下发: %+v", view.History)
	}
}

// TestGoalGovernanceViewCarriesHistoryLedgerWhileLive 验证账本与活动栈**正交**：
// 活体可用时 history 照样下发（它不是兜底专属），但已收口目标不得混进 Stack，
// 也不得冒充 active 那一帧。
func TestGoalGovernanceViewCarriesHistoryLedgerWhileLive(t *testing.T) {
	router := boardRouterForArchive(t)
	sessionID := "session-board-ledger"
	// 先用 goal 域的写侧把"收口一轮 + 再开一轮"这条真实路径跑出来（存档与账本
	// 都走生产代码，不在测试里手搓现场）。
	session := sessionstore.NewSessionContextStore(router, sessionID)
	if err := session.Load(boardArchiveReadCtx); err != nil {
		t.Fatalf("load session context: %v", err)
	}
	boards, ok := router.BoardsForSession(sessionID)
	if !ok {
		t.Fatal("看板存档取用面未装配")
	}
	adapter := goaldomain.NewContextStateStore(session, boards)
	controller := goaldomain.NewController(goaldomain.Options{
		Depth: 2, Store: adapter, Audit: adapter,
	})
	if _, err := controller.Begin(boardArchiveReadCtx, goaldomain.BeginRequest{Title: "第一轮"}); err != nil {
		t.Fatalf("begin first: %v", err)
	}
	if _, err := controller.Finish(boardArchiveReadCtx, goaldomain.FinishRequest{Reason: "已交付"}); err != nil {
		t.Fatalf("finish first: %v", err)
	}
	if _, err := controller.Begin(boardArchiveReadCtx, goaldomain.BeginRequest{Title: "第二轮"}); err != nil {
		t.Fatalf("begin second: %v", err)
	}

	coordinator := newGoalCoordinator(goalCoordinatorDeps{
		StoreFor: boardStoreFor(router),
	})
	// bundle 的 Reload 从活栈通道恢复第二轮目标（活体栈可用）。
	coordinator.bundleFor(sessionID)
	view := coordinator.GoalGovernanceViewFor(sessionID)
	if view == nil || !view.Active {
		t.Fatalf("活体目标在时看板应 Active: %+v", view)
	}
	if view.Recovered {
		t.Fatalf("活体可用时不应标 Recovered: %+v", view)
	}
	if view.GoalID != "g-2" {
		t.Fatalf("当前帧应是第二轮目标: %+v", view)
	}
	if len(view.History) != 1 || view.History[0].GoalID != "g-1" ||
		view.History[0].Title != "第一轮" || view.History[0].ClosedReason != "已交付" {
		t.Fatalf("活体可用时账本也要下发（它是对账本、不是兜底专属）: %+v", view.History)
	}
	for _, frame := range view.Stack {
		if frame.ID == "g-1" {
			t.Fatalf("已收口目标不得混进活动栈: %+v", view.Stack)
		}
	}
}

// TestGoalGovernanceViewWithoutArchiveStaysHidden 验证未装配/无存档时的降级：
// 没有任何存档面的会话仍然返回 nil（前端隐藏），不报错、不阻断。
func TestGoalGovernanceViewWithoutArchiveStaysHidden(t *testing.T) {
	coordinator := newGoalCoordinator(goalCoordinatorDeps{})
	if view := coordinator.GoalGovernanceViewFor("session-board-none"); view != nil {
		t.Fatalf("未装配存档面时不应产生治理视图: %+v", view)
	}
	router := boardRouterForArchive(t)
	withRouter := newGoalCoordinator(goalCoordinatorDeps{StoreFor: boardStoreFor(router)})
	if view := withRouter.GoalGovernanceViewFor("session-board-no-archive"); view != nil {
		t.Fatalf("有存档面但还没有存档时不应产生治理视图: %+v", view)
	}
}
