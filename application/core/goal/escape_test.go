package goal

// escape_test.go — 环逃生收口的语义：逃生 = 这一轮 goal 结束（收口 + 归档 + reap）。
//
// 这一组用例钉三件事，缺一件就会回到"loop 停了但没人知道"的状态：
//  1. 逃生后 goal 必须进终态（不是继续挂在 active 等一个不会来的 ADVISOR 回合）；
//  2. b 侧会话历史必须先归档留痕（它只在进程内 AdvisorSession 里，reap 就没了）；
//  3. reap 必须真的发生——下一个 goal 要拿到**全新**的 b peer（清空锚点/帧/回合），
//     否则新 goal 会带着上一轮 goal 的审查上下文跑（headless/直接终态化路径的老问题）。

import (
	"context"
	"strings"
	"testing"
)

// recordingRecorder 同时实现 TLRoundRecorder 与 TLHistoryArchiver（逃生归档经
// 类型断言接进来，所以"只实现前者"的测试替身也合法）。
type recordingRecorder struct {
	rounds     []TLRoundRecord
	archives   []TLArchiveRecord
	archiveErr error
}

func (r *recordingRecorder) RecordTLRound(_ context.Context, record TLRoundRecord) error {
	r.rounds = append(r.rounds, record)
	return nil
}

func (r *recordingRecorder) RecordMainTurn(context.Context, MainTurnRecord) error { return nil }

func (r *recordingRecorder) ArchiveTLHistory(_ context.Context, record TLArchiveRecord) error {
	if r.archiveErr != nil {
		return r.archiveErr
	}
	r.archives = append(r.archives, record)
	return nil
}

// escapingRecorder 只实现 TLRoundRecorder：证明归档是可选增强，不逼所有记录器跟上。
type escapingRecorder struct{ rounds int }

func (r *escapingRecorder) RecordTLRound(context.Context, TLRoundRecord) error {
	r.rounds++
	return nil
}
func (r *escapingRecorder) RecordMainTurn(context.Context, MainTurnRecord) error {
	return nil
}

func escapeTestFixture(t *testing.T) (*Controller, *Supervisor, *stubEvaluator, *recordingRecorder) {
	t.Helper()
	ctl := newTestController(t, DefaultStackDepth)
	sup, stub := newTestSupervisor(t, ctl, 0)
	recorder := &recordingRecorder{}
	sup.SetRoundRecorder(recorder)
	if _, err := ctl.Begin(testCtx, BeginRequest{Title: "逃生目标"}); err != nil {
		t.Fatalf("begin: %v", err)
	}
	// 制造一次真实回合 + 一帧工作进展，让 b 侧确实有"历史"可归档。
	if err := sup.Notify(testCtx, TLEvalSignal{Kind: SignalTurnCompleted, Detail: "已改完 runtime.go"}); err != nil {
		t.Fatalf("notify: %v", err)
	}
	if _, err := sup.RunEval(testCtx, "manual"); err != nil {
		t.Fatalf("runEval: %v", err)
	}
	return ctl, sup, stub, recorder
}

// TestAbortOnEscapeClosesGoalAndArchivesTLHistory：逃生必须收口 goal + 归档 b 历史 + reap。
func TestAbortOnEscapeClosesGoalAndArchivesTLHistory(t *testing.T) {
	ctl, sup, _, recorder := escapeTestFixture(t)

	if active, ok := ctl.ActiveGoal(); !ok || active == nil {
		t.Fatal("前置条件：逃生前应有 active goal")
	}

	result, err := sup.AbortOnEscape(testCtx, "round_limit")
	if err != nil {
		t.Fatalf("AbortOnEscape: %v", err)
	}
	if !result.Closed || result.Goal == nil {
		t.Fatalf("逃生必须收口 active goal: %+v", result)
	}
	if result.Goal.Status != StatusAborted {
		t.Fatalf("逃生收口后的状态 = %q, want %q（逃生是「未完成即终止」，不是 completed）", result.Goal.Status, StatusAborted)
	}
	if _, ok := ctl.ActiveGoal(); ok {
		t.Fatal("逃生后不应还有 active goal（否则面板会一直显示一个不会推进的循环）")
	}

	// 归档：一次、且带上"这次 goal 到此为止"的归因。
	if !result.Archived {
		t.Fatal("逃生必须归档 b 侧会话历史（否则这次 goal 的审查过程彻底查不到）")
	}
	if len(recorder.archives) != 1 {
		t.Fatalf("归档调用次数 = %d, want 1", len(recorder.archives))
	}
	archive := recorder.archives[0]
	if archive.Reason != "round_limit" || archive.Kind != "round_limit" {
		t.Fatalf("归档必须带上逃生原因: %+v", archive)
	}
	if archive.Rounds < 1 {
		t.Fatalf("归档应包含 b 的回合数，得到 %d", archive.Rounds)
	}
	if !strings.Contains(archive.Content, "[goal-anchor]") {
		t.Fatalf("归档正文应是 b 实际看到的上下文（含锚点）:\n%s", archive.Content)
	}

	// reap：终态化必须把 peer 标成 reaped，并留下原因。
	snapshot := sup.Snapshot()
	if snapshot.Peer != PeerReaped {
		t.Fatalf("逃生后 b peer 状态 = %q, want %q", snapshot.Peer, PeerReaped)
	}
	if snapshot.UnbindReason != "round_limit" {
		t.Fatalf("reap 原因 = %q, want round_limit", snapshot.UnbindReason)
	}
}

// TestAbortOnEscapeIsIdempotentAndKeepsRingSafe：逃生可能被多处触发（环记账 +
// 上层观察），重复调用不得报错、不得再归档、不得动已收口的 goal。
func TestAbortOnEscapeIsIdempotentAndKeepsRingSafe(t *testing.T) {
	ctl, sup, _, recorder := escapeTestFixture(t)

	if _, err := sup.AbortOnEscape(testCtx, "no_progress"); err != nil {
		t.Fatalf("首次逃生: %v", err)
	}
	second, err := sup.AbortOnEscape(testCtx, "no_progress")
	if err != nil {
		t.Fatalf("重复逃生必须幂等，得到错误: %v", err)
	}
	if second.Closed {
		t.Fatal("没有 active goal 时重复逃生不应报告收口")
	}
	if len(recorder.archives) != 1 {
		t.Fatalf("重复逃生不应重复归档，归档次数 = %d", len(recorder.archives))
	}
	if _, ok := ctl.ActiveGoal(); ok {
		t.Fatal("重复逃生不应把 goal 复活")
	}
}

// TestEscapeDoesNotLeakTLHistoryIntoNextGoal：逃生收口后，下一个 goal 的 b peer
// 必须是全新的（这一条是"归档"的对偶面：归档是为了不再复用）。
//
// 反例（修复前的行为）：直接 Controller.Abort 不走 Supervisor，peer 不被 reap，
// 新 goal 会沿用旧 peer 的 anchor/frames/rounds——新 goal 的 ADVISOR 看到的是
// 上一轮 goal 的审查上下文。
func TestEscapeDoesNotLeakTLHistoryIntoNextGoal(t *testing.T) {
	ctl, sup, _, _ := escapeTestFixture(t)

	if snapshot := sup.Snapshot(); snapshot.RoundCount != 1 {
		t.Fatalf("前置条件：第一个 goal 应有 1 个 b 回合，得到 %d", snapshot.RoundCount)
	}
	if _, err := sup.AbortOnEscape(testCtx, "no_progress"); err != nil {
		t.Fatalf("逃生: %v", err)
	}
	if _, err := ctl.Begin(testCtx, BeginRequest{Title: "下一个目标"}); err != nil {
		t.Fatalf("begin 第二个 goal: %v", err)
	}
	if _, err := sup.RunEval(testCtx, "manual"); err != nil {
		t.Fatalf("第二个 goal 的 runEval: %v", err)
	}
	snapshot := sup.Snapshot()
	if snapshot.ActiveGoalTitle != "下一个目标" {
		t.Fatalf("b peer 应重新锚定到新 goal，得到 %q", snapshot.ActiveGoalTitle)
	}
	if snapshot.RoundCount != 1 {
		t.Fatalf("第二个 goal 的 b 回合数 = %d, want 1：上一轮 goal 的回合记忆泄漏进了新 goal", snapshot.RoundCount)
	}
	if snapshot.Peer == PeerReaped {
		t.Fatalf("新 goal 应重建 b peer，得到 %q（逃生 reap 没有随新 goal 复位）", snapshot.Peer)
	}
}

// TestAbortOnEscapeWithoutArchiverStillCloses：未实现归档面时收口照常发生
// （归档是增强项，不是收口的前置条件）。
func TestAbortOnEscapeWithoutArchiverStillCloses(t *testing.T) {
	ctl := newTestController(t, DefaultStackDepth)
	sup, _ := newTestSupervisor(t, ctl, 0)
	sup.SetRoundRecorder(&escapingRecorder{})
	if _, err := ctl.Begin(testCtx, BeginRequest{Title: "无归档面"}); err != nil {
		t.Fatalf("begin: %v", err)
	}
	result, err := sup.AbortOnEscape(testCtx, "empty_ring")
	if err != nil {
		t.Fatalf("AbortOnEscape: %v", err)
	}
	if !result.Closed || result.Archived {
		t.Fatalf("无归档面时应收口但不报告归档: %+v", result)
	}
}

// TestAbortOnEscapeWithoutGoalIsNoop：没有 active goal 时逃生是 no-op（不报错）。
func TestAbortOnEscapeWithoutGoalIsNoop(t *testing.T) {
	ctl := newTestController(t, DefaultStackDepth)
	sup, _ := newTestSupervisor(t, ctl, 0)
	result, err := sup.AbortOnEscape(testCtx, "round_limit")
	if err != nil {
		t.Fatalf("无 goal 时逃生不应报错: %v", err)
	}
	if result.Closed || result.Archived {
		t.Fatalf("无 goal 时逃生应为 no-op: %+v", result)
	}
}
