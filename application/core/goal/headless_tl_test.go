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

	// 2) 喂会话尾窗（有界嵌入素材）。
	if err := client.Call(ctx, "goal_tl_tail", []TurnBrief{
		{Role: "mainagent", Summary: "改代码"},
		{Role: "tool", Summary: "跑 go test", Tool: "bash"},
	}, nil); err != nil {
		t.Fatalf("goal_tl_tail: %v", err)
	}

	// 3) step_checkpoint 信号 → 自动 TL 回合（eval_window=0）。
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
	// 4) 排空指令（mainagent 下一轮领取）。
	var directives []TLDirective
	if err := client.Call(ctx, "goal_tl_directives", nil, &directives); err != nil {
		t.Fatalf("goal_tl_directives: %v", err)
	}
	if len(directives) != 1 || directives[0].Kind != DirectiveCorrect || !strings.Contains(directives[0].Content, "go test") {
		t.Fatalf("指令异常: %+v", directives)
	}
	// goal 指令环（TLMemory/Goal 帧素材）已写入。
	var status StatusView
	if err := client.Call(ctx, "goal_status", nil, &status); err != nil {
		t.Fatalf("goal_status: %v", err)
	}
	if len(status.Active.Directives) != 1 || !strings.Contains(status.Active.Directives[0], "correct") {
		t.Fatalf("goal 指令环未写入: %+v", status.Active.Directives)
	}

	// 5) propose_finish #1 → TL verdict_not_done → 拦截（仍 active）。
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

	// 6) propose_finish #2 → TL verdict_done → 收口出栈。
	if err := client.Call(ctx, "goal_propose_finish", FinishRequest{Result: "补完测试"}, &proposal); err != nil {
		t.Fatalf("goal_propose_finish#2: %v", err)
	}
	if proposal.Outcome != OutcomeCompleted || proposal.Goal == nil || proposal.Goal.Status != StatusCompleted {
		t.Fatalf("应收口: %+v", proposal)
	}
	if projection := ctl.Projection(); projection.Active != nil || len(projection.Goals) != 0 {
		t.Fatalf("收口后投影应为空: %+v", projection)
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
