package goal

import (
	"context"
	"strings"
	"testing"
)

// tl_stream_test.go — 钉住「b 回合进行中」观察面的两条口径：
//
//  1. ctx 上的观察回调能取回（挂载点 = Supervisor.runRoundLocked，消费点 =
//     seelebridge 角色回合）；
//  2. 进行中正文是**有界近端**，且回合结束即清空（权威正文是裁决行，不是中间态）。
//
// 它不参与裁决：裁决仍只来自 Evaluate 的返回值。

// deltaEvaluator 是"b 回合产出流式分片"的桩：把分片喂给 ctx 上的观察回调。
type deltaEvaluator struct {
	chunks  []string
	sawSink bool
	reply   TLDirective
}

func (e *deltaEvaluator) Evaluate(ctx context.Context, _ TLSessionEmbed) (TLDirective, error) {
	if sink := TLDeltaSinkFrom(ctx); sink != nil {
		e.sawSink = true
		for _, chunk := range e.chunks {
			sink(chunk)
		}
	}
	reply := e.reply
	if reply.Kind == "" {
		reply = TLDirective{Kind: DirectiveVerdictNotDone, Content: "继续"}
	}
	return reply, nil
}

func TestTLDeltaSinkRoundTrip(t *testing.T) {
	if sink := TLDeltaSinkFrom(context.Background()); sink != nil {
		t.Fatal("未挂载时应返回 nil（执行面按不观察处理）")
	}
	if sink := TLDeltaSinkFrom(nil); sink != nil {
		t.Fatal("nil ctx 应返回 nil")
	}
	var got []string
	ctx := WithTLDeltaSink(context.Background(), func(delta string) { got = append(got, delta) })
	sink := TLDeltaSinkFrom(ctx)
	if sink == nil {
		t.Fatal("挂载后应能取回观察回调")
	}
	sink("片段")
	if strings.Join(got, ",") != "片段" {
		t.Fatalf("回调未收到分片: %v", got)
	}
	if WithTLDeltaSink(context.Background(), nil) != context.Background() {
		t.Fatal("nil 观察回调不应改变 ctx")
	}
}

// TestRunRoundInstallsInFlightSinkAndClears：b 回合进行中有观察回调，回合结束清空。
func TestRunRoundInstallsInFlightSinkAndClears(t *testing.T) {
	evaluator := &deltaEvaluator{chunks: []string{"先读证据", "再给结论"}}
	ctl := newTestController(t, DefaultStackDepth)
	if _, err := ctl.Begin(testCtx, BeginRequest{
		Title: "目标", Acceptance: []string{"有结论"},
	}); err != nil {
		t.Fatalf("begin: %v", err)
	}
	sup := NewSupervisor(ctl, evaluator, TechLeaderConfig{Enabled: true, EvalWindow: 0})
	if _, err := sup.RunEval(testCtx, "test:in_flight"); err != nil {
		t.Fatalf("RunEval: %v", err)
	}
	if !evaluator.sawSink {
		t.Fatal("b 回合没拿到 in-flight 观察回调：分片会被丢掉（渲染不及时的老病）")
	}
	if inFlight := sup.Snapshot().InFlight; inFlight != "" {
		t.Fatalf("回合结束应清空进行中正文（权威正文是裁决行），得 %q", inFlight)
	}
}

// TestNoteInFlightIsBoundedTail：进行中正文保留近端，且读数与内容一致。
func TestNoteInFlightIsBoundedTail(t *testing.T) {
	sup := NewSupervisor(newTestController(t, DefaultStackDepth), nil, TechLeaderConfig{})
	sup.noteInFlight("   ") // 空分片不上账
	if snap := sup.Snapshot(); snap.InFlight != "" || snap.InFlightChars != 0 {
		t.Fatalf("空分片不该产生进行中正文: %+v", snap.InFlight)
	}
	long := strings.Repeat("证", MaxInFlightRunes+200)
	sup.noteInFlight(long)
	snap := sup.Snapshot()
	runes := []rune(snap.InFlight)
	if len(runes) != MaxInFlightRunes+1 { // 近端上限 + 前置省略标记
		t.Fatalf("进行中正文应截到近端 %d rune，得 %d", MaxInFlightRunes, len(runes))
	}
	if !strings.HasPrefix(snap.InFlight, "…") {
		t.Fatal("截断时应前置省略标记（看的是当前位置）")
	}
	if snap.InFlightChars != len(runes) {
		t.Fatalf("字符读数应与内容一致: %d ≠ %d", snap.InFlightChars, len(runes))
	}
}
