package core

// 跨轮前缀不变量守卫（红灯用例：修复前必红，修复后转绿）。
//
// 不变量：**每一条发给 provider 的请求，其字节流必须以「更早发出的某条请求」
// 为前缀**（相邻请求即上一条）。这是 provider 前缀缓存命中的必要条件，也是
// Codex `core/tests/suite/prompt_caching.rs` 断言同款性质。
//
// 生产实测在「回合边界」被**事后改写**打穿（wire 上的字节从来没变，变的是
// 下一轮重投影时被改写的那份记录）：
//
//  1. 工具轮 assistant 正文：wire 上恒为空（Seele `session/loop.go:564`
//     在 tool_calls 时 `Content: nil`），但回合收尾
//     `mergeStreamedToolNarration` 把视图里的流式正文补写进 durable 转写；
//     下一轮从转写重投影 → 该消息字节 ≠ 已发出字节。
//  2. 同一轮"无说明文本"的工具轮 assistant：durable 里正文为空，下一轮装配
//     时 `RepairEmptyHistoryContent` 注入 `ToolCallHistoryContent` 占位 →
//     同样分叉（同一条规则已覆盖）。
//  3. **空工具结果**：工具返回空串时 wire 上该 tool 消息正文为空，下一轮重投影
//     却被补成 `MissingHistoryContent` 占位 → 同类分叉（独立用例守住）。
//
// 断言到「相邻两条请求」的粒度，首条不满足即失败，并打印首个差异消息的
// role / tool_calls / 两侧正文，使红灯原因可归因。
//
// 运行：go test ./application/core -run ContextPrefixInvariant -v -count=1

import (
	"strings"
	"testing"

	"github.com/RedHuang-0622/Seele/types"
)

func TestContextPrefixInvariant_CrossTurn(t *testing.T) {
	harness := newPrefixProbeHarness(t, 1_000_000)
	harness.startStream()

	narration := "我先读取装配入口与回合收尾代码，核对工具轮的前缀语义。"
	reply := "第 1 轮结论：装配把可复用段拼为保留前缀，工具轮正文需要单独核对。"
	toolResultOne := strings.Repeat("coordinator.go: PrepareExecutionContextFor 保留段重建路径证据行；", 120)
	toolResultTwo := strings.Repeat("chat.go: mergeStreamedToolNarration 视图正文合并证据行；", 80)

	// ── 第 1 轮：i1（工具）→ i2（工具）→ i3（终答），与生产 ReAct 形状一致 ──
	harness.beginTurn("第 1 轮：请核对 context_runtime 装配路径的前缀稳定性，并给出证据。")
	harness.capture(1, 1, "t1.iter1")

	harness.streamText(narration)
	harness.llmIteration("", "需要先看清装配顺序。", []types.ToolCall{{
		ID: "call-read-1", Type: "function",
		Function: types.ToolCallFunction{Name: "read_file", Arguments: `{"path":"application/core/context_runtime/coordinator.go"}`},
	}})
	harness.toolRound("read_file", "call-read-1", `{"path":"application/core/context_runtime/coordinator.go"}`, toolResultOne)
	harness.capture(1, 2, "t1.iter2")

	// 本轮无说明文本（生产里常见：模型直接发起第二个工具调用）。
	harness.llmIteration("", "核对收尾三连。", []types.ToolCall{{
		ID: "call-grep-2", Type: "function",
		Function: types.ToolCallFunction{Name: "grep_search", Arguments: `{"pattern":"mergeStreamedToolNarration"}`},
	}})
	harness.toolRound("grep_search", "call-grep-2", `{"pattern":"mergeStreamedToolNarration"}`, toolResultTwo)
	harness.capture(1, 3, "t1.iter3")

	harness.streamText(reply)
	harness.llmIteration(reply, "整理结论。", nil)
	harness.finishTurn(reply)

	// ── 第 2 轮：首个请求即"从 durable 转写重投影"出来的那条 ──
	harness.beginTurn("第 2 轮：继续，并说明上一轮工具结果的失效影响。")
	harness.capture(2, 1, "t2.iter1")

	probeAssertPrefixInvariant(t, harness)
}

// TestContextPrefixInvariant_EmptyToolResult 覆盖第 3 处事后改写：工具返回空
// 结果时，wire 上该 tool 消息没有正文；下一轮重投影若补 `MissingHistoryContent`
// 占位，字节就与已发出请求分叉（provider 前缀缓存自该消息起失效）。
func TestContextPrefixInvariant_EmptyToolResult(t *testing.T) {
	harness := newPrefixProbeHarness(t, 1_000_000)
	harness.startStream()

	toolReply := "第 1 轮结论：该工具没有任何输出。"
	harness.beginTurn("第 1 轮：跑一个只读、无输出的工具。")
	harness.capture(1, 1, "t1.iter1")

	harness.streamText("我先跑一次只读命令。")
	harness.llmIteration("", "只读命令无副作用。", []types.ToolCall{{
		ID: "call-empty-1", Type: "function",
		Function: types.ToolCallFunction{Name: "bash", Arguments: `{"command":"true"}`},
	}})
	harness.toolRound("bash", "call-empty-1", `{"command":"true"}`, "")
	harness.capture(1, 2, "t1.iter2")

	harness.streamText(toolReply)
	harness.llmIteration(toolReply, "整理结论。", nil)
	harness.finishTurn(toolReply)

	harness.beginTurn("第 2 轮：继续，上一轮工具没有输出。")
	harness.capture(2, 1, "t2.iter1")

	probeAssertPrefixInvariant(t, harness)
}

// probeAssertPrefixInvariant 断言相邻请求保持字节前缀；首条违反即失败并归因。
func probeAssertPrefixInvariant(t *testing.T, harness *prefixProbeHarness) {
	t.Helper()
	for index := 1; index < len(harness.calls); index++ {
		previous, current := harness.calls[index-1], harness.calls[index]
		if strings.HasPrefix(current.bytes, previous.bytes) {
			t.Logf("prefix OK   %-9s → %-9s (%d B ⊑ %d B)",
				previous.label, current.label, len(previous.bytes), len(current.bytes))
			continue
		}
		shared := lcpBytes(previous.bytes, current.bytes)
		messageIndex, role := prefixProbeFirstDiff(previous.messages, current.messages)
		reason := probeFirstDiffKind(previous.messages, current.messages, messageIndex)
		t.Logf("prefix BREAK %-9s → %-9s: shared=%d/%d B, first_diff=msg#%d(role=%s, tool_calls=%s), reason=%s",
			previous.label, current.label, shared, len(previous.bytes), messageIndex, role,
			probeCallIDs(probeMessageAt(previous.messages, messageIndex).ToolCalls), reason)
		t.Logf("  sent    = %q", summarizeProbeText(probeMessageContentAt(previous.messages, messageIndex)))
		t.Logf("  rebuilt = %q", summarizeProbeText(probeMessageContentAt(current.messages, messageIndex)))
		t.Fatalf("请求前缀不变量被打破：%s(%d B) 不是 %s(%d B) 的前缀（首个差异消息 msg#%d/%s，%s）—— provider 前缀缓存自该字节起全部失效",
			previous.label, len(previous.bytes), current.label, len(current.bytes), messageIndex, role, reason)
	}
}

// probeMessageAt 返回序列化消息快照中的第 index 条（越界返回空消息）。
func probeMessageAt(messages []EngineMessage, index int) EngineMessage {
	if index < 0 || index >= len(messages) {
		return EngineMessage{}
	}
	return messages[index]
}

func probeMessageContentAt(messages []EngineMessage, index int) string {
	return probeMessageAt(messages, index).Content
}

// probeFirstDiffKind 归因首个差异消息的成因——只在两侧 role / tool_calls ID
// 相同时才判定为"同一位置被改写"，否则归因为"消息流错位"。
func probeFirstDiffKind(previous, current []EngineMessage, index int) string {
	before, after := probeMessageAt(previous, index), probeMessageAt(current, index)
	if before.Role != after.Role || probeCallIDs(before.ToolCalls) != probeCallIDs(after.ToolCalls) {
		return "消息流错位（该位置不是同一条消息）"
	}
	switch {
	case before.Content == "" && after.Content != "":
		return "已发出字节为空、重投影时被补写正文（事后改写）"
	case before.Content != "" && after.Content == "":
		return "已发出字节有正文、重投影时被清空（事后改写）"
	default:
		return "内容被改写"
	}
}
