package goal

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/application/core/govern"
)

// TestHeadlessGovernRPC 验证 goal headless 治理测试面：
// goal_gov_next 推进 EXEC→TL 回合，goal_gov_snapshot 返回治理读面。
// 用确定性 stub 评估器（非真实 API），保证单元层可重复。
func TestHeadlessGovernRPC(t *testing.T) {
	ctl := newTestController(t, DefaultStackDepth)
	stub := newStubEvaluator(
		TLDirective{Kind: DirectiveCorrect, Content: "先补测试"},
		TLDirective{Kind: DirectiveVerdictNotDone, Content: "未全绿"},
	)
	sup := NewSupervisor(ctl, stub, TechLeaderConfig{Enabled: true, EvalWindow: 0})
	if _, err := ctl.Begin(testCtx, BeginRequest{Title: "发布 v1"}); err != nil {
		t.Fatalf("begin: %v", err)
	}

	var execRounds int
	gov := NewTurnGovernorForDSA2A("exec-a", func(ctx context.Context) (govern.TurnAction, error) {
		execRounds++
		if err := sup.Notify(ctx, TLEvalSignal{Kind: SignalStepCheckpoint, Source: "n1"}); err != nil {
			return govern.TurnAction{}, err
		}
		return govern.TurnAction{Note: "exec advance"}, nil
	}, sup, 3)

	server := httptest.NewServer(NewServer(ctl).WithTechLeader(sup).WithGovernor(gov).Handler())
	t.Cleanup(server.Close)
	client := NewClient(server.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	// 推进两轮（exec+advisor 各两次）→ 轮次护栏应仍允许。
	for i := 0; i < 4; i++ {
		var out struct {
			More bool `json:"more"`
		}
		if err := client.Call(ctx, "goal_gov_next", nil, &out); err != nil {
			t.Fatalf("goal_gov_next#%d: %v", i, err)
		}
		if !out.More {
			t.Fatalf("第 %d 次推进不应收束", i)
		}
	}
	var snap govern.Snapshot
	if err := client.Call(ctx, "goal_gov_snapshot", nil, &snap); err != nil {
		t.Fatalf("goal_gov_snapshot: %v", err)
	}
	if snap.Round != 2 || snap.CurrentSeat != "exec-a" || len(snap.Seats) != 2 {
		t.Fatalf("治理快照异常: %+v", snap)
	}
	if execRounds != 2 {
		t.Fatalf("EXEC 应行动 2 轮, 得 %d", execRounds)
	}
	// 外部断环经 RPC 可见。
	if err := client.Call(ctx, "goal_gov_break", struct {
		Reason string `json:"reason"`
	}{Reason: "user stop"}, nil); err != nil {
		t.Fatalf("goal_gov_break: %v", err)
	}
	if err := client.Call(ctx, "goal_gov_snapshot", nil, &snap); err != nil {
		t.Fatalf("goal_gov_snapshot(2): %v", err)
	}
	if !snap.Broken || snap.BreakReason != "user stop" {
		t.Fatalf("断环快照异常: %+v", snap)
	}
}

// TestHeadlessGovernUnwired 验证未装配治理循环时 goal_gov_* 显式拒绝。
func TestHeadlessGovernUnwired(t *testing.T) {
	ctl := newTestController(t, DefaultStackDepth)
	server := httptest.NewServer(NewServer(ctl).Handler())
	t.Cleanup(server.Close)
	client := NewClient(server.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var out struct {
		More bool `json:"more"`
	}
	if err := client.Call(ctx, "goal_gov_next", nil, &out); err == nil {
		t.Fatal("未装配治理循环时 goal_gov_next 应报错")
	}
}
