package seelebridge

// runtime_seat_test.go — 钉住座位作业面在 Runtime 上的接线（M2 的最后一块）：
//   - RunSeat 未装配 SeatRoundRunner ⇒ **显式报错**（不静默降级成一个"空成功"的
//     作业：那会把治理静默掉，比失败更糟）；
//   - RunSeat 已装配 ⇒ 把载荷（会话归属 + 本轮正文）转发给 goal 域实现，并把它的
//     note 透传到作业输出面；
//   - 作业面装配前后的探针（SeatJobsAssembled）：未装配 ⇒ 不接线（goal 域走现状
//     同步循环）；装配后 DispatchSeat/JoinSeat 在 jobs.Manager 上跑完整条链
//     （seat 执行体 → SeatRoundRunner），终态按读数折回。

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RedHuang-0622/Seele/jobs"

	seeletelemetry "github.com/RedHuang-0622/seelex/seelebridge/internal/telemetry"
	"github.com/RedHuang-0622/seelex/seelebridge/teamwork"
)

// seatTestSink 是 jobs.Sink 的桩（执行体收敛终态用）。
type seatTestSink struct {
	mu          sync.Mutex
	notes       []string
	progress    []string
	exits       []int
	complete    []string
	completeCnt int
}

func (sink *seatTestSink) Note(text string) {
	sink.mu.Lock()
	defer sink.mu.Unlock()
	sink.notes = append(sink.notes, text)
}

func (sink *seatTestSink) SignalBytes() {}

func (sink *seatTestSink) Exit(code int) {
	sink.mu.Lock()
	defer sink.mu.Unlock()
	sink.exits = append(sink.exits, code)
}

func (sink *seatTestSink) Complete(state jobs.State, summary string) {
	sink.mu.Lock()
	defer sink.mu.Unlock()
	sink.completeCnt++
	sink.complete = append(sink.complete, string(state))
}

func (sink *seatTestSink) Progress(note string) {
	sink.mu.Lock()
	defer sink.mu.Unlock()
	sink.progress = append(sink.progress, note)
}

func (sink *seatTestSink) notedText() string {
	sink.mu.Lock()
	defer sink.mu.Unlock()
	return strings.Join(sink.notes, "")
}

func (sink *seatTestSink) completedStates() []string {
	sink.mu.Lock()
	defer sink.mu.Unlock()
	return append([]string(nil), sink.complete...)
}

// seatRoundCall 是一次 RunSeatRound 收到的入参。
type seatRoundCall struct {
	sessionID string
	detail    string
}

// recordingSeatRoundRunner 是 goal 域执行侧的桩：记录载荷、按脚本产出输出/报错。
type recordingSeatRoundRunner struct {
	err error

	mu    sync.Mutex
	calls []seatRoundCall
}

func (runner *recordingSeatRoundRunner) RunSeatRound(_ context.Context, sessionID, detail string, note func(string)) error {
	runner.mu.Lock()
	runner.calls = append(runner.calls, seatRoundCall{sessionID: sessionID, detail: detail})
	err := runner.err
	runner.mu.Unlock()
	if note != nil {
		note("goal 座位循环完成：round=1 seat=advisor-b\n")
	}
	return err
}

func (runner *recordingSeatRoundRunner) recorded() []seatRoundCall {
	runner.mu.Lock()
	defer runner.mu.Unlock()
	return append([]seatRoundCall(nil), runner.calls...)
}

// TestRunSeatWithoutRunnerFailsExplicitly：未装配执行侧 = 显式报错。
func TestRunSeatWithoutRunnerFailsExplicitly(t *testing.T) {
	r := newTestRuntime(t)
	defer r.Shutdown()

	err := r.RunSeat(context.Background(), teamwork.SeatRequest{SessionID: "s-seat"}, &seatTestSink{})
	if err == nil {
		t.Fatal("未装配 SeatRoundRunner 时 RunSeat 必须显式报错（不得静默成功）")
	}
	if !strings.Contains(err.Error(), "SeatRoundRunner") {
		t.Fatalf("错误应指明缺的是 SeatRoundRunner: %v", err)
	}
}

// TestRunSeatForwardsPayloadToRunner：RunSeat 把会话归属与本轮正文折成
// RunSeatRound 入参（作业 ctx 里没有它们），并把 note 透传到作业输出面。
func TestRunSeatForwardsPayloadToRunner(t *testing.T) {
	r := newTestRuntime(t)
	defer r.Shutdown()
	runner := &recordingSeatRoundRunner{}
	r.SetSeatRoundRunner(runner)

	sink := &seatTestSink{}
	if err := r.RunSeat(context.Background(), teamwork.SeatRequest{
		GoalID: "g-1", SessionID: "s-seat", Detail: "本轮工作正文",
	}, sink); err != nil {
		t.Fatalf("RunSeat: %v", err)
	}
	calls := runner.recorded()
	if len(calls) != 1 {
		t.Fatalf("执行侧应被调用一次，得到 %d 次: %+v", len(calls), calls)
	}
	if calls[0].sessionID != "s-seat" || calls[0].detail != "本轮工作正文" {
		t.Fatalf("载荷应原样转发（会话归属 + 正文）: %+v", calls[0])
	}
	if !strings.Contains(sink.notedText(), "座位循环") {
		t.Fatalf("执行侧的 note 应透传到作业输出面: %q", sink.notedText())
	}

	// 缺会话归属 = 显式报错（不能凭空跑在"没有会话"上）。
	if err := r.RunSeat(context.Background(), teamwork.SeatRequest{Detail: "x"}, &seatTestSink{}); err == nil {
		t.Fatal("载荷缺会话归属应显式报错")
	}
}

// TestSeatJobFaceRoundTrip：装配作业面后，座位作业经 jobs.Manager 跑完整条链——
// seat 执行体 → Runtime.RunSeat → SeatRoundRunner；终态按读数折回（done/failed）。
func TestSeatJobFaceRoundTrip(t *testing.T) {
	r := newTestRuntime(t)
	defer r.Shutdown()

	if r.SeatJobsAssembled() {
		t.Fatal("未注入 backend 时座位作业面不该报告已装配（goal 域据此走同步循环）")
	}
	if err := r.SetTeamworkBackend(teamworkTestBackend(&memPlanStore{}, "s-seat")); err != nil {
		t.Fatalf("SetTeamworkBackend: %v", err)
	}
	if !r.SeatJobsAssembled() {
		t.Fatal("注入 backend 后座位作业面应报告已装配")
	}

	runner := &recordingSeatRoundRunner{}
	r.SetSeatRoundRunner(runner)
	ctx := seeletelemetry.WithSessionID(context.Background(), "s-seat")

	handle, err := r.DispatchSeat(ctx, "s-seat", "本轮治理正文")
	if err != nil {
		t.Fatalf("DispatchSeat: %v", err)
	}
	outcome, err := r.JoinSeat(ctx, handle, 10*time.Second)
	if err != nil {
		t.Fatalf("JoinSeat: %v", err)
	}
	if !outcome.Known || outcome.State != string(jobs.StateDone) {
		t.Fatalf("座位作业应收敛为 done: %+v", outcome)
	}
	calls := runner.recorded()
	if len(calls) != 1 || calls[0].sessionID != "s-seat" || calls[0].detail != "本轮治理正文" {
		t.Fatalf("作业载荷应经执行体转发给 goal 域实现: %+v", calls)
	}

	// 执行侧报错 ⇒ 作业终态 failed，摘要把原因带回来（goal 域据此登记 RoundError）。
	runner.mu.Lock()
	runner.calls = nil
	runner.err = errors.New("ADVISOR 回合不可用")
	runner.mu.Unlock()
	handle, err = r.DispatchSeat(ctx, "s-seat", "第二轮治理正文")
	if err != nil {
		t.Fatalf("DispatchSeat(#2): %v", err)
	}
	outcome, err = r.JoinSeat(ctx, handle, 10*time.Second)
	if err != nil {
		t.Fatalf("JoinSeat(#2): %v", err)
	}
	if outcome.State != string(jobs.StateFailed) {
		t.Fatalf("执行侧报错应折成 failed: %+v", outcome)
	}
	if !strings.Contains(outcome.Summary, "ADVISOR 回合不可用") {
		t.Fatalf("失败摘要应带回原因: %+v", outcome)
	}
}

// TestDispatchSeatNeedsSession：会话归属是作业作用域的唯一事实，缺它必须显式拒绝
// （空作用域会让 Reclaim/Snapshot 找不到这条作业）。
func TestDispatchSeatNeedsSession(t *testing.T) {
	r := newTestRuntime(t)
	defer r.Shutdown()
	if err := r.SetTeamworkBackend(teamworkTestBackend(&memPlanStore{}, "s-seat")); err != nil {
		t.Fatalf("SetTeamworkBackend: %v", err)
	}
	if _, err := r.DispatchSeat(context.Background(), "  ", "正文"); err == nil {
		t.Fatal("缺会话归属应显式拒绝派发")
	}
}
