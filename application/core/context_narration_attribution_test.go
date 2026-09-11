package core

// 工具轮说明正文的**归属**守卫（红灯用例：修复前叙述会落到别的轮次/迭代上）。
//
// 背景：框架在 wire 上丢弃工具轮正文（Seele `session/loop.go:564`），说明文本
// 只经 onChunk 进可见视图。record / 重启恢复要保留它，就必须把它归位到**产生
// 它的那一次迭代**的 assistant(tool_calls) 事件上。
//
// 旧实现在回合收尾做"事后填充"（`mergeStreamedToolNarration` → 视图全部
// assistant 正文 → 从 transcript 头部找第一个空事件填）：每多一轮，叙述就
// 往更早的轮次上再叠一层（见
// docs/research/2026-09-11-seelex-vs-codex-context-strategy-control-group.md §4）。
//
// 本用例断言：每个工具轮事件的正文**恰好等于**它自己那次迭代的说明文本。
//
// 运行：go test ./application/core -run ToolNarration -v -count=1

import (
	"fmt"
	"testing"

	"github.com/RedHuang-0622/Seele/types"
)

func TestToolNarrationStaysWithOwningIteration(t *testing.T) {
	harness := newPrefixProbeHarness(t, 1_000_000)
	harness.startStream()

	const turns = 3
	const rounds = 2
	want := make(map[string]string, turns*rounds)
	for turn := 1; turn <= turns; turn++ {
		harness.beginTurn(fmt.Sprintf("第 %d 轮输入", turn))
		for round := 1; round <= rounds; round++ {
			narration := fmt.Sprintf("T%d 第%d次工具前的说明。", turn, round)
			callID := fmt.Sprintf("call-%d-%d", turn, round)
			arguments := fmt.Sprintf(`{"path":"file-%d-%d.go"}`, turn, round)
			want[callID] = narration
			harness.streamText(narration)
			harness.llmIteration("", "读取装配入口。", []types.ToolCall{{
				ID: callID, Type: "function",
				Function: types.ToolCallFunction{Name: "read_file", Arguments: arguments},
			}})
			harness.toolRound("read_file", callID, arguments, fmt.Sprintf("round-%d tool %d result", turn, round))
		}
		reply := fmt.Sprintf("T%d 结论：本轮结束。", turn)
		harness.streamText(reply)
		harness.llmIteration(reply, "整理结论。", nil)
		harness.finishTurn(reply)
	}

	checked := 0
	for _, event := range harness.service.components.tasks.TranscriptFor(harness.session) {
		if event.Role != "assistant" || len(event.ToolCalls) == 0 {
			continue
		}
		callID := event.ToolCalls[0].ID
		expected, known := want[callID]
		if !known {
			t.Fatalf("出现了未预期的工具轮事件 %s（content=%q）", callID, event.Content)
		}
		checked++
		if event.Content != expected {
			t.Fatalf("工具轮事件 %s 的说明正文 = %q，期望 %q —— 叙述被写到了别的迭代/轮次上",
				callID, event.Content, expected)
		}
	}
	if checked != turns*rounds {
		t.Fatalf("工具轮事件数 = %d，期望 %d", checked, turns*rounds)
	}
}
