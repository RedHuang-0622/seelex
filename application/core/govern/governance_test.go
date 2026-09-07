package govern

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
)

// stubSeat 是可脚本化的治理座位：按预置序列返回 TurnAction。
type stubSeat struct {
	name string
	kind AgentKind
	mu   sync.Mutex
	calls []TurnAction
	acts  int
}

func newStubSeat(name string, kind AgentKind, calls ...TurnAction) *stubSeat {
	return &stubSeat{name: name, kind: kind, calls: append([]TurnAction(nil), calls...)}
}

func (s *stubSeat) Name() string { return s.name }
func (s *stubSeat) Kind() AgentKind { return s.kind }

func (s *stubSeat) Act(_ context.Context) (TurnAction, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.acts++
	if len(s.calls) == 0 {
		return TurnAction{Note: "ok"}, nil
	}
	next := s.calls[0]
	s.calls = s.calls[1:]
	return next, nil
}

func (s *stubSeat) acted() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.acts
}

func TestTurnGovernorAlternatesSeatsByOrder(t *testing.T) {
	exec := newStubSeat("exec-a", AgentKindExec)
	advisor := newStubSeat("advisor-b", AgentKindAdvisor)
	g := NewTurnGovernor([]Seat{exec, advisor}, 0)

	ctx := context.Background()
	// 第 1 轮：exec → advisor。
	if current := g.Current(); current != "exec-a" {
		t.Fatalf("第 1 回合应轮到 exec-a, 得 %q", current)
	}
	more, err := g.Next(ctx)
	if err != nil || !more {
		t.Fatalf("Next(exec) 应推进并继续: more=%v err=%v", more, err)
	}
	if current := g.Current(); current != "advisor-b" {
		t.Fatalf("第 2 回合应轮到 advisor-b, 得 %q", current)
	}
	more, err = g.Next(ctx)
	if err != nil || !more {
		t.Fatalf("Next(advisor) 应推进并继续: more=%v err=%v", more, err)
	}
	// 第 2 轮开始。
	if g.Round() != 1 {
		t.Fatalf("一轮完成后 round 应为 1, 得 %d", g.Round())
	}
	if current := g.Current(); current != "exec-a" {
		t.Fatalf("第 2 轮应回到 exec-a, 得 %q", current)
	}
	if exec.acted() != 1 || advisor.acted() != 1 {
		t.Fatalf("首轮各座位应行动 1 次: exec=%d advisor=%d", exec.acted(), advisor.acted())
	}
}

func TestTurnGovernorBreaksWhenSeatRequests(t *testing.T) {
	exec := newStubSeat("exec-a", AgentKindExec)
	advisor := newStubSeat("advisor-b", AgentKindAdvisor,
		TurnAction{BreakLoop: true, Note: "verdict_done: 全绿收口"})
	g := NewTurnGovernor([]Seat{exec, advisor}, 0)

	ctx := context.Background()
	if _, err := g.Next(ctx); err != nil {
		t.Fatalf("Next(exec): %v", err)
	}
	more, err := g.Next(ctx)
	if err != nil {
		t.Fatalf("Next(advisor): %v", err)
	}
	if more {
		t.Fatal("advisor 请求打破循环后 Next 应返回 false")
	}
	broken, reason := g.Broken()
	if !broken || !strings.Contains(reason, "verdict_done") {
		t.Fatalf("循环应被打破并记录原因: broken=%v reason=%q", broken, reason)
	}
	snap := SnapshotOf(g)
	if !snap.Broken || snap.BreakReason != reason {
		t.Fatalf("快照应带断环状态: %+v", snap)
	}
}

func TestTurnGovernorMaxRoundsStopsLoop(t *testing.T) {
	exec := newStubSeat("exec-a", AgentKindExec)
	advisor := newStubSeat("advisor-b", AgentKindAdvisor)
	g := NewTurnGovernor([]Seat{exec, advisor}, 2)

	ctx := context.Background()
	for i := 0; i < 4; i++ { // 2 轮 × 2 座位
		more, err := g.Next(ctx)
		if err != nil {
			t.Fatalf("Next#%d: %v", i, err)
		}
		if !more {
			t.Fatalf("前 4 次推进都应继续（轮次护栏未到），第 %d 次却停", i)
		}
	}
	more, err := g.Next(ctx)
	if err != nil {
		t.Fatalf("Next 超护栏: %v", err)
	}
	if more {
		t.Fatal("到达 maxRounds 后 Next 应返回 false")
	}
	if g.Round() != 2 {
		t.Fatalf("护栏轮次应为 2, 得 %d", g.Round())
	}
}

func TestTurnGovernorExternalBreak(t *testing.T) {
	exec := newStubSeat("exec-a", AgentKindExec)
	g := NewTurnGovernor([]Seat{exec}, 0)
	g.Break("user interrupt")

	ctx := context.Background()
	more, err := g.Next(ctx)
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if more {
		t.Fatal("外部 Break 后 Next 不应推进")
	}
	broken, reason := g.Broken()
	if !broken || reason != "user interrupt" {
		t.Fatalf("断环状态异常: broken=%v reason=%q", broken, reason)
	}
}

func TestTurnGovernorSeatErrorStops(t *testing.T) {
	boom := errors.New("boom")
	failing := &failingSeat{name: "exec-a", err: boom}
	g := NewTurnGovernor([]Seat{failing}, 0)

	ctx := context.Background()
	_, err := g.Next(ctx)
	if !errors.Is(err, boom) {
		t.Fatalf("座位错误应透传, 得 %v", err)
	}
	broken, _ := g.Broken()
	if broken {
		t.Fatal("座位错误不应标成主动断环（调用方决定如何处理）")
	}
}

type failingSeat struct {
	name string
	err  error
}

func (f *failingSeat) Name() string { return f.name }
func (f *failingSeat) Kind() AgentKind { return AgentKindExec }
func (f *failingSeat) Act(context.Context) (TurnAction, error) {
	return TurnAction{}, f.err
}
