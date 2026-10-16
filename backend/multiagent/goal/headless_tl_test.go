package goal

import (
	"context"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// startTLHeadlessServer 起带 TL 监督器的 goal Headless 控制面。
func startTLHeadlessServer(t *testing.T, ctl *Controller, replies ...TLDirective) (*httptest.Server, *Supervisor, *stubEvaluator) {
	t.Helper()
	stub := newStubEvaluator(replies...)
	sup := NewSupervisor(ctl, stub, TechLeaderConfig{Enabled: true, EvalWindow: 0})
	server := httptest.NewServer(NewServer(ctl).WithTechLeader(sup).Handler())
	t.Cleanup(server.Close)
	return server, sup, stub
}

// TestHeadlessTLE2E 覆盖完整 A2A 链路（外部驱动视角）：
// begin → tail → notify(step) 自动回合 → drain 指令 → goal 指令环 →
// propose_finish（verdict_not_done 拦截 → verdict_done 收口出栈）。
func TestHeadlessTLE2E(t *testing.T) {
	ctl := newTestController(t, DefaultStackDepth)
	server, _, _ := startTLHeadlessServer(t, ctl,
		TLDirective{Kind: DirectiveCorrect, Content: "跳过验证了，先补 go test"},
		TLDirective{Kind: DirectiveVerdictNotDone, Content: "测试未全绿，回 active"},
		TLDirective{Kind: DirectiveVerdictDone, Content: "全绿，收口"},
	)
	client := NewClient(server.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 1) begin。
	var record GoalRecord
	if err := client.Call(ctx, "goal_begin", BeginRequest{
		Title: "发布 v1", Acceptance: []string{"go test 全绿"},
	}, &record); err != nil {
		t.Fatalf("goal_begin: %v", err)
	}

	// 2) step_checkpoint 信号（DS-A2A：b 回合前自动 bind 锚点 + append 触发帧）→ b 回合。
	if err := client.Call(ctx, "goal_tl_notify", TLEvalSignal{
		Kind: SignalStepCheckpoint, Source: "n-verify", Detail: "完成实现，未跑测试",
	}, nil); err != nil {
		t.Fatalf("goal_tl_notify: %v", err)
	}
	var snapshot TLState
	if err := client.Call(ctx, "goal_tl_snapshot", nil, &snapshot); err != nil {
		t.Fatalf("goal_tl_snapshot: %v", err)
	}
	if snapshot.EvalCount != 1 || snapshot.ActiveGoalTitle != "发布 v1" {
		t.Fatalf("回合后快照异常: %+v", snapshot)
	}
	if snapshot.Peer != PeerBound && snapshot.Peer != PeerAdvisoryPending {
		t.Fatalf("b 应处于 bound/advisory_pending, 得 %s", snapshot.Peer)
	}
	if snapshot.RoundCount != 1 || snapshot.FrameCount == 0 {
		t.Fatalf("b 上下文应有 1 回合与帧账本: %+v", snapshot)
	}
	// 3) 排空指令（b→a corr 信封；EXEC 下一轮受信领取）。
	var directives []TLDirective
	if err := client.Call(ctx, "goal_tl_directives", nil, &directives); err != nil {
		t.Fatalf("goal_tl_directives: %v", err)
	}
	if len(directives) != 1 || directives[0].Kind != DirectiveCorrect || !strings.Contains(directives[0].Content, "go test") {
		t.Fatalf("指令异常: %+v", directives)
	}
	if directives[0].Corr == "" || directives[0].Corr != snapshot.Rounds[0].Corr {
		t.Fatalf("指令 corr 信封缺失/不一致: %+v vs %+v", directives[0], snapshot.Rounds[0])
	}
	// DS-A2A：b 产物不进 goal 共享状态（旧"TL 摘要写 goal 指令环"已移除）。
	var status StatusView
	if err := client.Call(ctx, "goal_status", nil, &status); err != nil {
		t.Fatalf("goal_status: %v", err)
	}
	if len(status.Active.Directives) != 0 {
		t.Fatalf("b 回合不应写 goal 指令环: %+v", status.Active.Directives)
	}

	// 4) propose_finish #1 → b verdict_not_done → 拦截（仍 active）。
	var proposal FinishProposalResult
	if err := client.Call(ctx, "goal_propose_finish", FinishRequest{Result: "做完了"}, &proposal); err != nil {
		t.Fatalf("goal_propose_finish#1: %v", err)
	}
	if proposal.Outcome != OutcomeNotDone {
		t.Fatalf("应被拦截: %+v", proposal)
	}
	if projection := ctl.Projection(); projection.Active == nil {
		t.Fatal("拦截后应有 active goal")
	}

	// 5) propose_finish #2 → b verdict_done → 收口出栈 + b unbind(reason=done)。
	if err := client.Call(ctx, "goal_propose_finish", FinishRequest{Result: "补完测试"}, &proposal); err != nil {
		t.Fatalf("goal_propose_finish#2: %v", err)
	}
	if proposal.Outcome != OutcomeCompleted || proposal.Goal == nil || proposal.Goal.Status != StatusCompleted {
		t.Fatalf("应收口: %+v", proposal)
	}
	if projection := ctl.Projection(); projection.Active != nil || len(projection.Goals) != 0 {
		t.Fatalf("收口后投影应为空: %+v", projection)
	}
	if err := client.Call(ctx, "goal_tl_snapshot", nil, &snapshot); err != nil {
		t.Fatalf("goal_tl_snapshot(收口后): %v", err)
	}
	if snapshot.Peer != PeerReaped || snapshot.UnbindReason != "done" {
		t.Fatalf("收口后 b 应 reaped(reason=done): %+v", snapshot)
	}
}

// TestHeadlessTLErrNotWired 验证未装配 TL 时 goal_tl_* RPC 报可读错误。
func TestHeadlessTLErrNotWired(t *testing.T) {
	ctl := newTestController(t, DefaultStackDepth)
	server := startHeadlessServer(t, ctl) // 无 WithTechLeader
	client := NewClient(server.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := client.Call(ctx, "goal_tl_snapshot", nil, nil)
	if err == nil || !strings.Contains(err.Error(), "techleader 未装配") {
		t.Fatalf("未装配应报错, 得 %v", err)
	}
}

// TestHeadlessApprovalPreScreen 验证审批预筛经 headless：low 代答、high 转人工。
func TestHeadlessApprovalPreScreen(t *testing.T) {
	ctl := newTestController(t, DefaultStackDepth)
	server, _, _ := startTLHeadlessServer(t, ctl,
		TLDirective{Kind: DirectiveApprove, Content: "低风险读，放行"},
	)
	client := NewClient(server.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Call(ctx, "goal_begin", BeginRequest{Title: "目标"}, nil); err != nil {
		t.Fatalf("goal_begin: %v", err)
	}
	var verdict ApprovalVerdict
	if err := client.Call(ctx, "goal_prescreen", ApprovalScreenRequest{
		Summary: "读 README", RiskLevel: "low",
	}, &verdict); err != nil {
		t.Fatalf("goal_prescreen: %v", err)
	}
	if verdict.Outcome != ApprovalOutcomeApproved {
		t.Fatalf("low 应 approve 代答: %+v", verdict)
	}
	if err := client.Call(ctx, "goal_prescreen", ApprovalScreenRequest{
		Summary: "写账号配置", RiskLevel: "high",
	}, &verdict); err != nil {
		t.Fatalf("goal_prescreen(high): %v", err)
	}
	if verdict.Outcome != ApprovalOutcomeEscalate {
		t.Fatalf("high 应 escalate: %+v", verdict)
	}
}

// TestConcurrentNotifyRace 在 -race 下验证多 goroutine 信号/回合无竞态。
func TestConcurrentNotifyRace(t *testing.T) {
	ctl := newTestController(t, DefaultStackDepth)
	if _, err := ctl.Begin(testCtx, BeginRequest{Title: "目标"}); err != nil {
		t.Fatalf("begin: %v", err)
	}
	stub := newStubEvaluator()
	sup := NewSupervisor(ctl, stub, TechLeaderConfig{Enabled: true, EvalWindow: 0})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var wait sync.WaitGroup
	for w := 0; w < 8; w++ {
		wait.Add(1)
		go func(worker int) {
			defer wait.Done()
			for round := 0; round < 20; round++ {
				kind := SignalStepCheckpoint
				if round%3 == 0 {
					kind = SignalTurnCompleted
				}
				if err := sup.Notify(ctx, TLEvalSignal{Kind: kind, Source: "race"}); err != nil {
					t.Errorf("notify: %v", err)
					return
				}
			}
		}(w)
	}
	wait.Wait()
	_ = sup.Snapshot()
}
