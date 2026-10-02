package seelebridge

// runtime_teamwork_board_closed_test.go — 钉住「整队收口 ⇒ 看板退场」（2026-10-03 口径修正）。
//
// 口径：`team_close` 之后看板**整块退场**，与目标看板「结束就是没有了」同一口径。
//
// 为什么改（修前事实，Confirmed）：改前活体投影对已收口计划**照常出图**——计划里
// stages 还在，只有 plan.State.State=closed，于是 `TeamworkBoardSnapshot` 照旧返回一块
// 看板，GUI 的「团队看板」section 与 TUI 的看板节都不退场，连同在编名册（成员行）一起
// 残留在收口之后。读侧因此分不清"这支队还在跑"与"早就收口了"。
//
// 三份事实的分工不变：closed 的**域内权威仍是计划**（sessionstore.TeamworkPlan.State），
// 它管的是"还在不在册"；本文件管的是投影侧的口径——**不在册就没有看板**。存档侧本来就是
// 同一口径（readTeamBoardArchive 只恢复 state=active），活体与存档因此不再自相矛盾。
//
// 计划留在原处（不删、不改落盘形状）：收口原因、收口时间仍是可核对的事实，只是不再上板。

import (
	"context"
	"testing"

	"github.com/RedHuang-0622/seelex/sessionstore"
)

func TestTeamworkBoardSnapshotRetiresClosedPlan(t *testing.T) {
	r := newTestRuntime(t)
	defer r.Shutdown()
	store := &memPlanStore{}
	if err := r.SetTeamworkBackend(teamworkTestBackend(store, "s-team")); err != nil {
		t.Fatalf("SetTeamworkBackend: %v", err)
	}
	key := sessionstore.Key{ProjectID: "p-team", SessionID: "s-team"}

	// 未收口：计划在册 → 看板在（这是"还在册"的正例，别把退场读成"看板从来不出现"）。
	if err := store.WritePlan(context.Background(), key, boardPlanFixture(), 6); err != nil {
		t.Fatalf("WritePlan: %v", err)
	}
	open := r.TeamworkBoardSnapshot("s-team")
	if open == nil {
		t.Fatal("未收口的计划必须给得出看板")
	}
	if open.State != "" {
		t.Fatalf("未收口时 state 应为零值，得到 %q", open.State)
	}

	// 收口：计划标 closed（域内权威），看板必须退场——不留空壳、不留残留的在编名册。
	closed := boardPlanFixture()
	closed.State = sessionstore.TeamworkState{
		State:        sessionstore.TeamworkStateClosed,
		ClosedAt:     1759392000,
		ClosedReason: sessionstore.BoardCloseTeamClose,
	}
	if err := store.WritePlan(context.Background(), key, closed, 6); err != nil {
		t.Fatalf("WritePlan(closed): %v", err)
	}
	r.invalidateTeamworkBoard()

	if board := r.TeamworkBoardSnapshot("s-team"); board != nil {
		t.Fatalf("已收口的计划必须让看板退场（结束就是没有了），得到 %+v（在编 %d 人仍残留）",
			board, len(board.Members))
	}
	// 退场是稳定的：再采集一次也不能"活过来"（负缓存/存档兜底都不许把它复活）。
	if board := r.TeamworkBoardSnapshot("s-team"); board != nil {
		t.Fatalf("收口后的看板退场必须稳定，第二次采集又拿到了 %+v", board)
	}
}
