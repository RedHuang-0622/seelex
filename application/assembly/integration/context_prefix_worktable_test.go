//go:build !extended

package integration

import (
	"strings"
	"testing"

	"github.com/RedHuang-0622/Seele/types"
	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/kernel/context/prefixprobe"
)

// 字节冻结不变量（I1）：装配把动态尾块（工作打点表 / 环境表）拼进当前输入后发给
// provider，**必须把这一份已发出字节写回 transcript**；下一轮重投影按它重建，
// 才能与上一次请求逐字节一致。
//
// 历史（同一夹具的两个阶段）：
//   - M0（修复前）：`turn=2 prefix BREAK byte=16766 char=16688 msg#0 role=user`，
//     该请求只复用 80.5%（16688/20724）——动态块污染了承载它的那条 user 消息，
//     而历史整体排在 system 之后，所以**整段历史**从该点重算。
//   - M1（修复后，本用例当前断言）：`turn=2 prefix OK hit=20503/20867 (98.3%)`，
//     新增的 364 字符全部来自真实新内容（上一轮答复 + 本轮输入）。
//
// 修复接缝：`application/conversation.TurnInputApplication.Prepare` 在归一化后
// 调 `RecordWire` → `tasks.RecordUserWireContentLocked`（详见
// docs/arch/context-prefix-stability-l1.md §6.3 / M1）。
//
// 运行：go test ./application/assembly -run ContextPrefixWorktable -v -count=1

func TestContextPrefixWorktable_PrefixIntact(t *testing.T) {
	harness := newPrefixProbeHarness(t, 1_000_000)
	harness.startStream()

	// 回合开始前工作打点表即非空：生产最常见形状（上一轮遗留 / 工具中途写入），
	// 也正是修复前打穿前缀的那个条件。
	harness.runtime.tasks = map[string]dto.TaskRecord{
		"todo:1": {ID: "todo:1", Phase: dto.TaskPhaseTasklist, Task: "核对前缀失效区间", Status: dto.TaskRunning, Kind: "todo"},
	}

	turnOneInput := "第 1 轮：核对装配路径的前缀稳定性。"
	turnOneReply := "第 1 轮结论：装配把可复用段拼为保留前缀。"
	toolResult := strings.Repeat("coordinator.go: PrepareExecutionContextFor 保留段重建路径证据行；", 60)

	harness.beginTurn(turnOneInput)
	harness.capture(1, 1, "t1.iter1")

	harness.streamText("我先读取装配入口。")
	harness.llmIteration("我先读取装配入口。", "需要先看清装配顺序。", []types.ToolCall{{
		ID: "call-read-1", Type: "function",
		Function: types.ToolCallFunction{Name: "read_file", Arguments: `{"path":"application/assembly/context_runtime/coordinator.go"}`},
	}})
	harness.toolRound("read_file", "call-read-1", `{"path":"application/assembly/context_runtime/coordinator.go"}`, toolResult)
	harness.capture(1, 2, "t1.iter2")

	harness.streamText(turnOneReply)
	harness.llmIteration(turnOneReply, "整理结论。", nil)
	harness.finishTurn(turnOneReply)

	harness.beginTurn("第 2 轮：继续。")
	harness.capture(2, 1, "t2.iter1")

	// M0 阶段还没有真实 prefix_key（M2 落地），此处用零值键表达"键未变"，
	// 于是任何断开都会被归类为 UnexpectedMiss——本用例要求它为 0。
	var key prefixprobe.Key
	probe := prefixprobe.New()
	for _, call := range harness.calls {
		segments, refs := probeSegmentsAndMessages(systemPromptFor(harness.service), call.messages, call.bytes)
		res := probe.Observe(uint64(call.turn), prefixprobe.Sample{
			Wire: call.bytes, Segments: segments, Messages: refs, Key: key,
		})
		t.Logf("  %s", res.Summary())
		if !res.Intact() {
			t.Fatalf("请求前缀被打破（I1 字节冻结失效）：%s\n  sent=%q\n  rebuilt=%q",
				res.Summary(),
				summarizeProbeText(probeMessageContentAt(call.messages, res.Divergence.MessageIndex)),
				summarizeProbeText(""))
		}
	}

	cum := probe.Cumulative()
	t.Logf("== 累计: requests=%d hit=%d/%d (%.1f%%) unexpected=%d expected=%d ==",
		cum.Requests, cum.HitChars, cum.TotalChars, cum.Ratio()*100,
		cum.UnexpectedMiss, cum.ExpectedMiss)
	if cum.UnexpectedMiss != 0 {
		t.Fatalf("键未变却断开：UnexpectedMiss=%d，字节冻结被打破", cum.UnexpectedMiss)
	}
	// 跨轮那条必须复用掉上一条请求的全部字节（只允许尾部新增）。
	last := probe.Last()
	if newChars := last.TotalChars - last.HitChars; newChars <= 0 {
		t.Fatalf("跨轮请求应当有真实新增内容，新增字符=%d 说明夹具退化了", newChars)
	}
}
