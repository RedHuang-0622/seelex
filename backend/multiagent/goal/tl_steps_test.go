package goal

import (
	"context"
	"strings"
	"testing"
)

// tl_steps_test.go — 钉住 b（ADVISOR）回合的**过程观察面**三条口径：
//
//  1. ctx 上的步骤回调能取回（挂载点 = Supervisor.evaluateRound，消费点 =
//     seelebridge 角色回合的 ReAct 钩子）；
//  2. 步骤列表**有界**（保留最近 MaxRoundSteps 条）；
//  3. 步骤在回合**结束后保留**（下一轮开始才换代）——用户要能看见"刚刚评审
//     做了什么"，而不是只在评审那几秒；但两个回合的步骤不混。
//
// 步骤不参与裁决：裁决仍只来自 Evaluate 的返回值。

// stepEvaluator 是"b 回合调用只读工具"的桩：经 ctx 上的步骤回调上报步骤。
type stepEvaluator struct {
	steps   []TLStep
	sawSink bool
	reply   TLDirective
}

func (e *stepEvaluator) Evaluate(ctx context.Context, _ TLSessionEmbed) (TLDirective, error) {
	if sink := TLStepSinkFrom(ctx); sink != nil {
		e.sawSink = true
		for _, step := range e.steps {
			sink(step)
		}
	}
	reply := e.reply
	if reply.Kind == "" {
		reply = TLDirective{Kind: DirectiveVerdictNotDone, Content: "继续"}
	}
	return reply, nil
}

func TestTLStepSinkRoundTrip(t *testing.T) {
	if sink := TLStepSinkFrom(context.Background()); sink != nil {
		t.Fatal("未挂载时应返回 nil（执行面按不观察处理）")
	}
	if sink := TLStepSinkFrom(nil); sink != nil {
		t.Fatal("nil ctx 应返回 nil")
	}
	var got []TLStep
	ctx := WithTLStepSink(context.Background(), func(step TLStep) { got = append(got, step) })
	sink := TLStepSinkFrom(ctx)
	if sink == nil {
		t.Fatal("挂载后应能取回步骤回调")
	}
	sink(TLStep{Kind: "tool", Name: "read_file", Args: `{"path":"README.md"}`})
	if len(got) != 1 || got[0].Name != "read_file" {
		t.Fatalf("回调未收到步骤: %+v", got)
	}
	if WithTLStepSink(context.Background(), nil) != context.Background() {
		t.Fatal("nil 回调不应改变 ctx")
	}
}

// TestRunRoundKeepsRoundStepsAfterCommit：回合结束后步骤**仍在**（保留给面板），
// 且正文近端照旧清空——两者语义不同，不能一起清。
func TestRunRoundKeepsRoundStepsAfterCommit(t *testing.T) {
	evaluator := &stepEvaluator{steps: []TLStep{
		{Kind: "tool", Name: "read_file", Args: `{"path":"docs/x.md"}`},
		{Kind: "tool_result", Name: "read_file", Result: "命中 3 处"},
	}}
	ctl := newTestController(t, DefaultStackDepth)
	if _, err := ctl.Begin(testCtx, BeginRequest{Title: "目标", Acceptance: []string{"有结论"}}); err != nil {
		t.Fatalf("begin: %v", err)
	}
	sup := NewSupervisor(ctl, evaluator, TechLeaderConfig{Enabled: true, EvalWindow: 0})
	if _, err := sup.RunEval(testCtx, "test:steps"); err != nil {
		t.Fatalf("RunEval: %v", err)
	}
	if !evaluator.sawSink {
		t.Fatal("b 回合没拿到步骤回调：评审过程会完全不可见")
	}
	snap := sup.Snapshot()
	if len(snap.RoundSteps) != 2 {
		t.Fatalf("回合结束后步骤应保留（面板要显示最近一轮的过程），得 %+v", snap.RoundSteps)
	}
	if snap.RoundSteps[0].Name != "read_file" || snap.RoundSteps[1].Kind != "tool_result" {
		t.Fatalf("步骤顺序/形状不对: %+v", snap.RoundSteps)
	}
	if snap.InFlight != "" {
		t.Fatalf("正文近端仍应在回合结束清空（权威正文是裁决行），得 %q", snap.InFlight)
	}
}

// TestRoundStepsAreBounded：步骤列表保留最近 MaxRoundSteps 条（旧步骤先丢）。
func TestRoundStepsAreBounded(t *testing.T) {
	sup := NewSupervisor(newTestController(t, DefaultStackDepth), nil, TechLeaderConfig{})
	for i := 0; i < MaxRoundSteps+5; i++ {
		sup.noteStep(TLStep{Kind: "tool", Name: "glob", Args: strings.Repeat("g", i)})
	}
	snap := sup.Snapshot()
	if len(snap.RoundSteps) != MaxRoundSteps {
		t.Fatalf("步骤列表应有界到 %d 条，得 %d", MaxRoundSteps, len(snap.RoundSteps))
	}
	// 保留的是**最近**的：最后一条是最后写入的。
	last := snap.RoundSteps[len(snap.RoundSteps)-1]
	if last.Args != strings.Repeat("g", MaxRoundSteps+4) {
		t.Fatalf("应保留最近步骤（丢最旧），最后一条参数长度 = %d", len(last.Args))
	}
	if snap.RoundSteps[0].Kind != "tool" {
		t.Fatalf("空 kind 不应上账（Kind 会被规范化）: %+v", snap.RoundSteps[0])
	}
	sup.noteStep(TLStep{Kind: "  "})
	if len(sup.Snapshot().RoundSteps) != MaxRoundSteps {
		t.Fatal("空 kind 的步骤不应进入列表")
	}
}

// TestRoundStepsRotateOnNextRound：新一轮开始即换代——面板语义是"本轮/最近一轮"，
// 不把两个回合的步骤混在一起。
func TestRoundStepsRotateOnNextRound(t *testing.T) {
	evaluator := &stepEvaluator{steps: []TLStep{{Kind: "tool", Name: "glob"}}}
	ctl := newTestController(t, DefaultStackDepth)
	if _, err := ctl.Begin(testCtx, BeginRequest{Title: "目标", Acceptance: []string{"有结论"}}); err != nil {
		t.Fatalf("begin: %v", err)
	}
	sup := NewSupervisor(ctl, evaluator, TechLeaderConfig{Enabled: true, EvalWindow: 0})
	if _, err := sup.RunEval(testCtx, "round-1"); err != nil {
		t.Fatalf("第一轮 RunEval: %v", err)
	}
	if got := len(sup.Snapshot().RoundSteps); got != 1 {
		t.Fatalf("第一轮后应有 1 条步骤，得 %d", got)
	}
	evaluator.steps = []TLStep{
		{Kind: "tool", Name: "read_file"},
		{Kind: "tool_result", Name: "read_file", Result: "ok"},
	}
	if _, err := sup.RunEval(testCtx, "round-2"); err != nil {
		t.Fatalf("第二轮 RunEval: %v", err)
	}
	snap := sup.Snapshot()
	if len(snap.RoundSteps) != 2 {
		t.Fatalf("第二轮应换代（只剩本轮的 2 条），得 %d 条: %+v", len(snap.RoundSteps), snap.RoundSteps)
	}
	for _, step := range snap.RoundSteps {
		if step.Name == "glob" {
			t.Fatalf("上一轮的步骤不应残留: %+v", snap.RoundSteps)
		}
	}
}
