package seelexctx

import (
	"context"
	"strings"
	"testing"

	"github.com/RedHuang-0622/Seele/seelectx"
	"github.com/RedHuang-0622/Seele/types"
)

// wire_protocol_safety_test.go — provider 的 tool 配对协议在 seelex 这一侧的
// 出口保证（复现 2026-09-20 现场：会话循环第 15/22 轮中断于
//
//	HTTP 400 {"error":{"message":"Messages with role 'tool' must be a response
//	to a preceding message with 'tool_calls'","type":"invalid_request_error"}}
//
// ）。provider 校验的不是"历史里存在配对"，而是"每条 tool 消息必须紧跟那条
// 携带其 tool_calls 的 assistant 消息"：助手宣告与结果之间夹进任何消息、或
// 出现没有 assistant 宣告的孤儿 tool 行，整次请求 400。
//
// 框架侧有两条路会把这种历史送到 provider：
//  1. 控制器的窗口投影故意保留窗口内的非单元消息（孤儿 tool 行随窗口保留，
//     见 projectHistory 注释"审计 R3"），投影后孤儿可能落在**历史最前面**；
//  2. 乱序结果（assistant(tool_calls c1) → user → tool(c1)）此前无人搬运。
//
// 应用侧孪生实现（application/core/context_runtime RepairInterruptedToolChains）
// 早已按该协议剔除孤儿/搬回乱序行；框架侧（本包）只补缺失占位——两侧漂移，
// 于是这类历史在框架侧一路发到 provider。本文件把出口不变量钉死：
// AfterRepair 的历史与装配器产出的 wire 都必须是 provider 合法的。

// assertProviderToolProtocol 复刻 provider 对 tool 消息的校验规则：
//
//	每条 role=tool 消息都必须落在"声明了它的 assistant 行 + 1 .. + len(tool_calls)"
//	这一相邻结果块内，且同一 call_id 不许出现重复结果。
func assertProviderToolProtocol(t *testing.T, history []types.Message) {
	t.Helper()
	owner := map[string]int{}
	width := map[int]int{}
	for index, message := range history {
		if message.Role != "assistant" {
			continue
		}
		width[index] = len(message.ToolCalls)
		for _, call := range message.ToolCalls {
			if call.ID == "" {
				continue
			}
			if _, seen := owner[call.ID]; !seen {
				owner[call.ID] = index
			}
		}
	}
	results := map[string]int{}
	for index, message := range history {
		if message.Role != "tool" {
			continue
		}
		declaration, declared := owner[message.ToolCallID]
		if !declared {
			t.Fatalf("msg#%d role=tool tool_call_id=%q 没有前一条声明它的 assistant → provider 400 "+
				"(Messages with role 'tool' must be a response to a preceding message with 'tool_calls'): %s",
				index, message.ToolCallID, roleShape(history))
		}
		if index < declaration+1 || index > declaration+width[declaration] {
			t.Fatalf("msg#%d role=tool tool_call_id=%q 不在声明行 msg#%d 的相邻结果块 [%d,%d] 内 → provider 400: %s",
				index, message.ToolCallID, declaration, declaration+1, declaration+width[declaration],
				roleShape(history))
		}
		if first, duplicate := results[message.ToolCallID]; duplicate {
			t.Fatalf("msg#%d role=tool tool_call_id=%q 是重复结果（首个在 msg#%d）→ provider 400: %s",
				index, message.ToolCallID, first, roleShape(history))
		}
		results[message.ToolCallID] = index
	}
}

// roleShape 渲染历史的角色形状，失败信息里能一眼看出违规在哪。
func roleShape(history []types.Message) string {
	parts := make([]string, 0, len(history))
	for _, message := range history {
		part := message.Role
		if message.Role == "assistant" && len(message.ToolCalls) > 0 {
			ids := make([]string, 0, len(message.ToolCalls))
			for _, call := range message.ToolCalls {
				ids = append(ids, call.ID)
			}
			part += "(tool_calls:" + strings.Join(ids, ",") + ")"
		}
		if message.Role == "tool" {
			part += "(" + message.ToolCallID + ")"
		}
		parts = append(parts, part)
	}
	return "[" + strings.Join(parts, " → ") + "]"
}

// callMessage 构造携带单个工具调用的 assistant 消息。
func callMessage(id, name string) types.Message {
	return types.Message{
		Role:      "assistant",
		ToolCalls: []types.ToolCall{{ID: id, Function: types.ToolCallFunction{Name: name}}},
	}
}

// resultMessage 构造 tool 结果消息。
func resultMessage(id, name, content string) types.Message {
	return types.Message{Role: "tool", ToolCallID: id, Name: name, Content: &content}
}

// TestPrepareReplaceHistoryDropsUnownedOrphanToolResult 孤儿 tool 结果（没有
// 任何 assistant 宣告）必须被剔除：它没有任何可回应的前一条消息，原样发出
// 就是 400。现场正是这种形状——窗口投影把非单元消息留在窗口里，孤儿因此
// 落在投影后历史的最前面。
func TestPrepareReplaceHistoryDropsUnownedOrphanToolResult(t *testing.T) {
	history := []types.Message{
		textMessage("user", "在跑一轮"),
		textMessage("assistant", "好的"),
		resultMessage("ghost", "read_file", "孤儿结果：没有任何 assistant 宣告过它"),
		textMessage("user", "下一问"),
	}
	prepared := PrepareReplaceHistory(history)
	assertProviderToolProtocol(t, prepared)
	for _, message := range prepared {
		if message.Role == "tool" {
			t.Fatalf("孤儿 tool 结果必须被剔除，实际保留：%s", roleShape(prepared))
		}
	}
}

// TestPrepareReplaceHistoryRealignsMisorderedToolResult 乱序结果必须搬回
// 声明的相邻结果块：assistant(tool_calls c1) → user → tool(c1) 这种历史
// provider 同样 400（2026-09-17 应用侧实测；框架侧此前无人搬运）。
func TestPrepareReplaceHistoryRealignsMisorderedToolResult(t *testing.T) {
	history := []types.Message{
		textMessage("user", "读一下文件"),
		callMessage("c1", "read_file"),
		textMessage("user", "（用户抢话，结果还没回来就发了下一句）"),
		resultMessage("c1", "read_file", "文件内容"),
		textMessage("assistant", "读到了"),
	}
	prepared := PrepareReplaceHistory(history)
	assertProviderToolProtocol(t, prepared)
	found := false
	for index, message := range prepared {
		if message.Role == "tool" && message.ToolCallID == "c1" {
			found = true
			if index != 2 {
				t.Fatalf("c1 的结果必须紧跟声明行（msg#1）之后，实际在 msg#%d: %s", index, roleShape(prepared))
			}
		}
	}
	if !found {
		t.Fatalf("乱序结果必须被搬回而不是丢弃：%s", roleShape(prepared))
	}
}

// TestPrepareReplaceHistoryKeepsLegalHistoryUntouched 已经是合法协议的
// 历史必须逐字不动：投影要与已发出的字节一致（前缀缓存），修复层不许自作
// 聪明地重排、补占位或改写正文。
func TestPrepareReplaceHistoryLeavesLegalHistoryUntouched(t *testing.T) {
	history := []types.Message{
		textMessage("user", "读两个文件"),
		{Role: "assistant", Content: stringPtr("我来读这两个文件"), ToolCalls: []types.ToolCall{
			{ID: "c1", Function: types.ToolCallFunction{Name: "read_file"}},
			{ID: "c2", Function: types.ToolCallFunction{Name: "read_file"}},
		}},
		resultMessage("c1", "read_file", "内容1"),
		resultMessage("c2", "read_file", "内容2"),
		textMessage("assistant", "两个都读到了"),
	}
	prepared := PrepareReplaceHistory(history)
	assertProviderToolProtocol(t, prepared)
	if len(prepared) != len(history) {
		t.Fatalf("合法历史长度不得变化：%s", roleShape(prepared))
	}
	for index := range history {
		if prepared[index].Role != history[index].Role ||
			prepared[index].ToolCallID != history[index].ToolCallID ||
			prepared[index].Content == nil || history[index].Content == nil ||
			*prepared[index].Content != *history[index].Content {
			t.Fatalf("合法历史 msg#%d 被改写：%s", index, roleShape(prepared))
		}
	}
}

// TestAssemblerSanitizesWorkingHistoryToolProtocol 装配器是发往 provider 的
// 最后一跳：主会话/节点会话的 AssemblerOptions 都不设 Window，WorkingHistory
// 就是框架会话里的历史（含控制器投影保留的孤兒）——出口必须自己保证协议
// 合法，不能指望上游每个写入点都干净。
//
// 出口只做"剔除孤儿/重复结果 + 搬回乱序结果"，**不合成占位**：请求可能发生在
// 工具还没执行完的时刻（活跃 ReAct 中间态），给"即将执行"的调用补占位会污染
// 历史（与应用侧 PrepareNewHistoryContentFor 的保守口径一致）。
func TestAssemblerSanitizesWorkingHistoryToolProtocol(t *testing.T) {
	history := []types.Message{
		textMessage("user", "在跑一轮"),
		textMessage("assistant", "好的"),
		resultMessage("ghost", "read_file", "孤儿结果"),
		textMessage("user", "读文件"),
		callMessage("c1", "read_file"),
		textMessage("user", "抢话"),
		resultMessage("c1", "read_file", "文件内容"),
	}
	assembled, err := NewAssembler(AssemblerOptions{}).Assemble(context.Background(), seelectx.AssemblyRequest{
		WorkingHistory: history,
	})
	if err != nil {
		t.Fatal(err)
	}
	assertProviderToolProtocol(t, assembled.Messages)
	for _, message := range assembled.Messages {
		if message.Role == "tool" && strings.HasPrefix(messageContent(message), interruptedToolResultPrefix) {
			t.Fatalf("出口不得合成占位（活跃 ReAct 里调用可能只是还没执行）：%s", roleShape(assembled.Messages))
		}
	}
}

// TestControllerWindowProjectionKeepsProviderToolProtocol 复现现场那条链路：
// 窗口投影保留窗口内的非单元消息（孤儿 tool 行）→ 投影后历史以 tool 行开头
// → 控制器把它写回会话历史 → 下一次请求原样发往 provider → 400。
func TestControllerWindowProjectionKeepsProviderToolProtocol(t *testing.T) {
	controller := newController(1, NewMemoryCompactStack())
	big := strings.Repeat("数据内容", 50)
	history := []types.Message{
		textMessage("user", "轮0-用户"+big),
		textMessage("assistant", "轮0-回复"+big),
		textMessage("user", "轮1-用户"+big),
		textMessage("assistant", "轮1-回复"+big),
		resultMessage("ghost", "read_file", "孤儿结果：落在溢出边界与窗口之间"),
		textMessage("user", "轮2-用户"+big),
		textMessage("assistant", "轮2-回复"+big),
	}
	decision, err := controller.Handle(context.Background(), seelectx.ContextEvent{
		Kind: seelectx.ContextAfterAssistant, Turn: 1, Query: "", History: history,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !decision.ReplaceHistory {
		t.Fatal("窗口溢出必须压缩并替换历史")
	}
	assertProviderToolProtocol(t, decision.History)
}
