package context_runtime

import (
	"testing"

	"github.com/RedHuang-0622/seelex/application/contract"
)

// history_pairing_gap_test.go — provider 的第二半措辞
//
//	HTTP 400 {"error":{"message":"An assistant message with 'tool_calls' must be
//	followed by tool messages responding to each 'tool_call_id'. (insufficient
//	tool messages following tool_calls message)","type":"invalid_request_error"}}
//
// 与「孤儿 tool 行」那一半（见 TestRepairInterruptedToolChainsDropsOrphanResult）
// 不同：这一半是"宣告了但相邻结果块里没凑齐回执"。provider 按**宣告条数**数回执，
// 因此下面两种形状即使历史里"看起来有配对"也照样 400：
//
//   - 同一个 call_id 被两条 assistant 各自宣告一次（中断后重发复用 ID）：一条结果
//     只能回应一条声明，后一条声明在自己的相邻块里一条回执都没有；
//   - 声明里带空 ID 的调用：provider 数为一条 tool_call，而任何结果都无法回应它。
//
// 两条用例都用 looksLikeProviderValidToolPairs（严格相邻 + 逐条回执）判红绿。

// TestRepairInterruptedToolChainsPairsRepeatedDeclarationOccurrence：同一 call_id
// 被两次声明时，结果必须按**出现次序**逐条配对（第 k 次声明 ↔ 第 k 条结果），
// 后一条声明拿不到真结果时补合成占位 —— 不能让它在 provider 那边算作"缺回执"。
func TestRepairInterruptedToolChainsPairsRepeatedDeclarationOccurrence(t *testing.T) {
	history := []contract.EngineMessage{
		{Role: "user", Content: "读一下文件", ContentSet: true},
		{Role: "assistant", ToolCalls: []contract.EngineToolCall{{ID: "c1", Name: "read_file"}}},
		{Role: "tool", ToolCallID: "c1", Name: "read_file", Content: "内容", ContentSet: true},
		{Role: "assistant", Content: "被打断了", ContentSet: true},
		{Role: "user", Content: "再试一次", ContentSet: true},
		// 重发复用了同一个 call_id：这条声明自己也必须拿到一条回执。
		{Role: "assistant", ToolCalls: []contract.EngineToolCall{{ID: "c1", Name: "read_file"}}},
		{Role: "assistant", Content: "结束", ContentSet: true},
	}
	prepared, repaired := RepairInterruptedToolChains(history)
	if !repaired {
		t.Fatal("第二次声明缺回执必须被修复")
	}
	if !looksLikeProviderValidToolPairs(prepared) {
		t.Fatalf("修复后仍会 400（insufficient tool messages following tool_calls message）：%+v", prepared)
	}
	// 首个结果保持真内容；第二次声明补的是合成占位，且明示"可能没执行"。
	if prepared[2].Content != "内容" {
		t.Fatalf("首个真实结果不得被改写：%+v", prepared[2])
	}
	if prepared[6].Role != "tool" || prepared[6].ToolCallID != "c1" || !IsProviderOnlyHistoryContent(prepared[6].Content) {
		t.Fatalf("第二次声明必须补一条合成占位回执：%+v", prepared)
	}
	// 幂等：再跑一次逐字不变。
	again, againRepaired := RepairInterruptedToolChains(prepared)
	if againRepaired || len(again) != len(prepared) {
		t.Fatalf("重复宣告的修复必须幂等：%+v", again)
	}
}

// TestRepairInterruptedToolChainsKeepsRepeatedResultWithRepeatedDeclaration：反过来
// 的现场形状 —— 重发的调用**有自己的结果**（两条同名结果，各自紧跟自己的声明）。
//
// 旧口径把"同一 call_id 的第二条结果"一律当重复行丢掉（每个 ID 只认首个结果），
// 丢掉之后后一条声明在自己的相邻块里就没有回执了，provider 数下来正是
// "insufficient tool messages following tool_calls message"。逐次配对后两条声明
// 各自拿到自己的结果，历史逐字不变（前缀缓存口径）。
func TestRepairInterruptedToolChainsKeepsRepeatedResultWithRepeatedDeclaration(t *testing.T) {
	history := []contract.EngineMessage{
		{Role: "user", Content: "读一下文件", ContentSet: true},
		{Role: "assistant", ToolCalls: []contract.EngineToolCall{{ID: "c1", Name: "read_file"}}},
		{Role: "tool", ToolCallID: "c1", Name: "read_file", Content: "第一次内容", ContentSet: true},
		{Role: "assistant", Content: "结果作废", ContentSet: true},
		{Role: "user", Content: "重试", ContentSet: true},
		{Role: "assistant", ToolCalls: []contract.EngineToolCall{{ID: "c1", Name: "read_file"}}},
		{Role: "tool", ToolCallID: "c1", Name: "read_file", Content: "第二次内容", ContentSet: true},
	}
	prepared, repaired := RepairInterruptedToolChains(history)
	if repaired {
		t.Fatalf("两条声明各自有相邻回执的历史是合法的，不该被改写：%+v", prepared)
	}
	if !looksLikeProviderValidToolPairs(prepared) {
		t.Fatalf("修复后仍会 400（insufficient tool messages following tool_calls message）：%+v", prepared)
	}
	// 两条真结果都必须在，且先后顺序不变；没有合成占位混进来。
	var contents []string
	for _, message := range prepared {
		if message.Role == "tool" && message.ToolCallID == "c1" {
			contents = append(contents, message.Content)
			if IsProviderOnlyHistoryContent(message.Content) {
				t.Fatalf("两条结果都是真结果，不该出现合成占位：%+v", prepared)
			}
		}
	}
	if len(contents) != 2 || contents[0] != "第一次内容" || contents[1] != "第二次内容" {
		t.Fatalf("两条真结果的先后顺序必须保持：%v（%+v）", contents, prepared)
	}
}

// TestRepairInterruptedToolChainsDropsEmptyCallIDDeclaration：声明里的空 ID 调用
// 永远拿不到回执（provider 按 tool_calls 条数数），投影时从声明里剔除；同一行里
// 重复 ID 的调用同属记录异常，同样剔除 —— 否则声明数恒大于回执数，必然 400。
func TestRepairInterruptedToolChainsDropsEmptyCallIDDeclaration(t *testing.T) {
	history := []contract.EngineMessage{
		{Role: "user", Content: "读文件", ContentSet: true},
		{Role: "assistant", ToolCalls: []contract.EngineToolCall{
			{ID: "", Name: "read_file"},
			{ID: "c1", Name: "grep_search"},
			{ID: "c1", Name: "grep_search"},
		}},
		{Role: "tool", ToolCallID: "c1", Name: "grep_search", Content: "命中", ContentSet: true},
		{Role: "assistant", Content: "结束", ContentSet: true},
	}
	prepared, repaired := RepairInterruptedToolChains(history)
	if !repaired {
		t.Fatal("空 ID / 行内重复 ID 的声明必须被规整")
	}
	if !looksLikeProviderValidToolPairs(prepared) {
		t.Fatalf("修复后仍会 400（insufficient tool messages following tool_calls message）：%+v", prepared)
	}
	if len(prepared[1].ToolCalls) != 1 || prepared[1].ToolCalls[0].ID != "c1" {
		t.Fatalf("声明必须只留可配对的调用：%+v", prepared[1].ToolCalls)
	}
	if len(prepared) != len(history) {
		t.Fatalf("规整不得增删消息：%+v", prepared)
	}
}
