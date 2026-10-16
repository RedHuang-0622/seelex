package goal

import (
	"fmt"
	"strings"
	"testing"
)

// 这些用例钉住「EXEC 的工作进展确实进入 ADVISOR 输入」这条接线：
//   - turn_completed 携带 Detail（本轮工作正文摘要）→ 待抽帧缓冲 → b 回合前抽成
//     work.progress 帧，b 的输入正文（RenderText）里能看到它；
//   - 不带 Detail 的 turn_completed 仍是纯跳帧（不产生帧，b 上下文不膨胀）；
//   - 缓冲有界（MaxWorkFrames）且同一内容连续上报只留一条。

// TestTurnCompletedDetailBecomesWorkProgressFrame 是 ① 的核心断言：
// ADVISOR 下一回合的输入里必须出现 EXEC 本轮的工作正文摘要。
func TestTurnCompletedDetailBecomesWorkProgressFrame(t *testing.T) {
	ctl := newTestController(t, DefaultStackDepth)
	sup, stub := newTestSupervisor(t, ctl, 0,
		TLDirective{Kind: DirectiveCheckpointOK, Content: "ok"})
	if _, err := ctl.Begin(testCtx, BeginRequest{Title: "接线验证"}); err != nil {
		t.Fatalf("begin: %v", err)
	}
	detail := "WORK-CONTENT-MARKER-42 | tools: bash, read_file"
	if err := sup.Notify(testCtx, TLEvalSignal{
		Kind: SignalTurnCompleted, Source: "chat_end", Detail: detail,
	}); err != nil {
		t.Fatalf("notify: %v", err)
	}
	// turn_completed 本身不触发回合（跳帧语义保持不变）。
	if stub.evalCount() != 0 {
		t.Fatalf("turn_completed 不应触发评估, 得 %d", stub.evalCount())
	}
	if _, err := sup.RunEval(testCtx, "govern:advisor-b"); err != nil {
		t.Fatalf("RunEval: %v", err)
	}

	embed := stub.last()
	if embed == nil {
		t.Fatal("b 未执行回合")
	}
	found := false
	for _, frame := range embed.Frames {
		if frame.Kind == FrameWorkProgress {
			found = true
			if frame.Detail != detail {
				t.Fatalf("work.progress 帧 detail = %q, want %q", frame.Detail, detail)
			}
		}
	}
	if !found {
		t.Fatalf("b 输入帧账本应含 work.progress: %+v", embed.Frames)
	}
	// RenderText 就是送给 LLM 的全文：工作正文必须在里面（这条是"接线"的判据）。
	text := embed.RenderText()
	if !strings.Contains(text, "work.progress") || !strings.Contains(text, "WORK-CONTENT-MARKER-42") {
		t.Fatalf("ADVISOR 输入正文缺少 EXEC 工作进展:\n%s", text)
	}
	if !strings.Contains(text, "bash") {
		t.Fatalf("ADVISOR 输入正文缺少工具名摘要:\n%s", text)
	}
	// 帧账本仍须严格单调 + 合法（协议不变量不因新增帧而破坏）。
	for index := 1; index < len(embed.Frames); index++ {
		if embed.Frames[index].RefSeq <= embed.Frames[index-1].RefSeq {
			t.Fatalf("帧 ref_seq 须单调: %+v", embed.Frames)
		}
	}
	if err := embed.Validate(); err != nil {
		t.Fatalf("回合输入不合法: %v", err)
	}
	// 回合后缓冲清空：同一细节不会被两个回合重复下发。
	if snapshot := sup.Snapshot(); snapshot.FrameCount != len(embed.Frames) {
		t.Fatalf("帧账本应与下发一致: %+v", snapshot)
	}
	if _, err := sup.RunEval(testCtx, "govern:advisor-b"); err != nil {
		t.Fatalf("RunEval(2): %v", err)
	}
	second := stub.last()
	if countWorkProgress(second.Frames) != 1 {
		t.Fatalf("第二轮不应重复 work.progress（缓冲已消费，仅保留账本里那一帧）: %+v", second.Frames)
	}
}

// TestTurnCompletedWithoutDetailStaysSkipped 钉住跳帧语义：没有工作正文摘要的
// turn_completed（旧生产者/无产出的回合）不得凭空产生帧。
func TestTurnCompletedWithoutDetailStaysSkipped(t *testing.T) {
	ctl := newTestController(t, DefaultStackDepth)
	sup, stub := newTestSupervisor(t, ctl, 0)
	if _, err := ctl.Begin(testCtx, BeginRequest{Title: "跳帧"}); err != nil {
		t.Fatalf("begin: %v", err)
	}
	for index := 0; index < 3; index++ {
		if err := sup.Notify(testCtx, TLEvalSignal{Kind: SignalTurnCompleted}); err != nil {
			t.Fatalf("notify: %v", err)
		}
	}
	if _, err := sup.RunEval(testCtx, "govern:advisor-b"); err != nil {
		t.Fatalf("RunEval: %v", err)
	}
	if countWorkProgress(stub.last().Frames) != 0 {
		t.Fatalf("无 detail 的 turn 不应产生 work.progress: %+v", stub.last().Frames)
	}
	if snapshot := sup.Snapshot(); snapshot.FrameCount != 1 {
		t.Fatalf("仅触发帧应入账: %+v", snapshot)
	}
}

// TestWorkProgressBufferBoundedAndDeduped 钉住有界性：内容级去重（同一轮被
// iteration_complete 与 chat_end 各报一次）+ 超限丢最旧（b 上下文按帧数有界）。
func TestWorkProgressBufferBoundedAndDeduped(t *testing.T) {
	// 去重：同一轮被两个生产者各报一次（内容相同）→ 只留一条。
	ctl := newTestController(t, DefaultStackDepth)
	sup, stub := newTestSupervisor(t, ctl, 0)
	if _, err := ctl.Begin(testCtx, BeginRequest{Title: "去重"}); err != nil {
		t.Fatalf("begin: %v", err)
	}
	for index := 0; index < 2; index++ {
		if err := sup.Notify(testCtx, TLEvalSignal{
			Kind: SignalTurnCompleted, Source: "iteration_complete", Detail: "同一轮的工作正文",
		}); err != nil {
			t.Fatalf("notify: %v", err)
		}
	}
	if _, err := sup.RunEval(testCtx, "govern:advisor-b"); err != nil {
		t.Fatalf("RunEval: %v", err)
	}
	if got := countWorkProgress(stub.last().Frames); got != 1 {
		t.Fatalf("同一内容只应产生 1 帧, 得 %d: %+v", got, stub.last().Frames)
	}

	// 有界：新会话报 MaxWorkFrames+3 条不同内容 → 只下发最后 MaxWorkFrames 条。
	ctl2 := newTestController(t, DefaultStackDepth)
	sup2, stub2 := newTestSupervisor(t, ctl2, 0)
	if _, err := ctl2.Begin(testCtx, BeginRequest{Title: "有界"}); err != nil {
		t.Fatalf("begin: %v", err)
	}
	for index := 0; index < MaxWorkFrames+3; index++ {
		if err := sup2.Notify(testCtx, TLEvalSignal{
			Kind: SignalTurnCompleted, Detail: fmt.Sprintf("turn-%02d 工作正文", index),
		}); err != nil {
			t.Fatalf("notify(%d): %v", index, err)
		}
	}
	if _, err := sup2.RunEval(testCtx, "govern:advisor-b"); err != nil {
		t.Fatalf("RunEval: %v", err)
	}
	if got := countWorkProgress(stub2.last().Frames); got != MaxWorkFrames {
		t.Fatalf("work.progress 帧数 = %d, want %d", got, MaxWorkFrames)
	}
	text := stub2.last().RenderText()
	if strings.Contains(text, "turn-00") || strings.Contains(text, "turn-02") {
		t.Fatalf("丢最旧语义被破坏（最旧 3 条不应下发）:\n%s", text)
	}
	if !strings.Contains(text, fmt.Sprintf("turn-%02d", MaxWorkFrames+2)) {
		t.Fatalf("最新一条工作正文必须下发:\n%s", text)
	}
}

func countWorkProgress(frames []Frame) int {
	count := 0
	for _, frame := range frames {
		if frame.Kind == FrameWorkProgress {
			count++
		}
	}
	return count
}
