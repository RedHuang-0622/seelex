package seelebridge

// runtime_teamwork_board_closed_test.go — 钉住看板投影的**闭板三字段**（S3）。
//
// 为什么这一片必须单独钉：closed 事实的域内权威在计划（sessionstore.TeamworkPlan.State，
// U3 裁决），而看板存档对 closed 是**整块退场**（readTeamBoardArchive 只恢复 state=active）。
// 所以"收口之后还看得见「已关闭」态"这件事，只能靠活体投影把计划里的三字段搬出来——
// 搬运漏一处，前端就只剩一个说不清状态的空壳。

import (
	"context"
	"testing"

	"github.com/RedHuang-0622/seelex/sessionstore"
)

func TestTeamworkBoardViewCarriesClosedState(t *testing.T) {
	r := newTestRuntime(t)
	defer r.Shutdown()
	store := &memPlanStore{}
	if err := r.SetTeamworkBackend(teamworkTestBackend(store, "s-team")); err != nil {
		t.Fatalf("SetTeamworkBackend: %v", err)
	}
	key := sessionstore.Key{ProjectID: "p-team", SessionID: "s-team"}

	// 未收口：三字段必须是零值（空 = 未收口；这是"还在册"的判据面）。
	if err := store.WritePlan(context.Background(), key, boardPlanFixture(), 6); err != nil {
		t.Fatalf("WritePlan: %v", err)
	}
	open := r.TeamworkBoardSnapshot("s-team")
	if open == nil {
		t.Fatal("未收口的计划必须给得出看板")
	}
	if open.State != "" || open.ClosedAt != 0 || open.ClosedReason != "" {
		t.Fatalf("未收口时闭板三字段应为零值，得到 state=%q closed_at=%d reason=%q",
			open.State, open.ClosedAt, open.ClosedReason)
	}

	// 收口：计划标 closed 之后，看板必须照原样搬出三字段（存档侧此时已整块退场）。
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

	board := r.TeamworkBoardSnapshot("s-team")
	if board == nil {
		t.Fatal("已收口的计划仍要给得出看板——否则「已关闭」态在 UI 上写不出来")
	}
	if board.State != sessionstore.TeamworkStateClosed {
		t.Fatalf("state 必须从计划搬出来（域内权威）：%q", board.State)
	}
	if board.ClosedReason != sessionstore.BoardCloseTeamClose {
		t.Fatalf("closed_reason 必须是收口原因词表里的 %q：%q", sessionstore.BoardCloseTeamClose, board.ClosedReason)
	}
	if board.ClosedAt != 1759392000 {
		t.Fatalf("closed_at 必须原样搬运：%d", board.ClosedAt)
	}
}
