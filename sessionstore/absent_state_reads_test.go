package sessionstore

// absent_state_reads_test.go — **缺失即状态**的门禁（2026-10-15 审计 D3）。
//
// 口径与理由见 docs/arch/absent-state-reads.md。这条用例把那份文档里的三档**逐条登记**：
//
//   - ① 空值档：head 还没写过 ⇒ 零值 + nil（"还没有这份数据"是状态）；
//   - ② 哨兵档：按显式 id / 压缩链内部状态 ⇒ 明确的哨兵错误，且调用方在边界处翻成状态；
//   - ③ 退役档：这个面已经不存在 ⇒ 恒返回 fs.ErrNotExist（不是"缺失"）。
//
// **新增 head 读面必须进这张表**：漏登记的面没有门禁，它就会是下一个把"还没有团队/还没有存档"
// 报成 IO 错误、被模型读成"工具故障"的面（真实现场：`open …\metadata\teamwork.json: The system
// cannot find the path specified.`）。

import (
	"context"
	"errors"
	"io/fs"
	"testing"
)

func TestAbsentHeadReadsReturnStateNotIOError(t *testing.T) {
	repository, key := boardFixture(t)
	ctx := context.Background()

	// ── ① 空值档：零值 + nil ────────────────────────────────────────────
	t.Run("空值档", func(t *testing.T) {
		if plan, err := repository.ReadTeamworkPlan(ctx, key); err != nil || !plan.IsEmpty() {
			t.Fatalf("ReadTeamworkPlan = (%+v, %v)，want 空计划 + nil", plan, err)
		}
		if events, err := repository.ReadTeamworkEvents(ctx, key); err != nil || len(events) != 0 {
			t.Fatalf("ReadTeamworkEvents = (%v, %v)，want 空集 + nil", events, err)
		}
		if meta, err := repository.ReadGoalBoard(ctx, key); err != nil || meta.Seq != 0 {
			t.Fatalf("ReadGoalBoard = (%+v, %v)，want 零值 + nil", meta, err)
		}
		if meta, err := repository.ReadTeamBoard(ctx, key); err != nil || meta.TeamID != "" {
			t.Fatalf("ReadTeamBoard = (%+v, %v)，want 零值 + nil", meta, err)
		}
		if snapshot, ok, err := repository.ReadToolRecordSnapshot(ctx, key); err != nil || ok || snapshot != nil {
			t.Fatalf("ReadToolRecordSnapshot = (%q, %v, %v)，want (nil, false, nil)", snapshot, ok, err)
		}
		if records, err := repository.ListNodeSessions(ctx, key); err != nil || len(records) != 0 {
			t.Fatalf("ListNodeSessions = (%v, %v)，want 空集 + nil", records, err)
		}
	})

	// ── ② 哨兵档：调用方点名了应当存在的东西 / 压缩链要区分"没有"与"读坏" ──
	t.Run("哨兵档", func(t *testing.T) {
		if _, err := repository.ReadNodeSession(ctx, key, "sub-1"); !errors.Is(err, ErrNodeSessionNotFound) {
			t.Fatalf("ReadNodeSession(缺失) = %v，want ErrNodeSessionNotFound", err)
		}
		if _, err := repository.readCheckpointLayout(key); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("readCheckpointLayout(缺失) = %v，want fs.ErrNotExist（压缩链据此区分「没有快照」与「快照读坏了」）", err)
		}
	})

	// ── ③ 退役档：这个面已经不存在，恒报 fs.ErrNotExist（不是"缺失即空"） ──
	t.Run("退役档", func(t *testing.T) {
		if _, err := repository.ReadState(ctx, key); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("ReadState = %v，want fs.ErrNotExist（state.json 已停读，D9/S20）", err)
		}
	})
}
