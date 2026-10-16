package seelexctx

import (
	"context"
	"strings"
	"testing"

	"github.com/RedHuang-0622/Seele/seelectx"
	"github.com/RedHuang-0622/Seele/types"
)

// wire_pairing_gap_test.go — provider 的第二半措辞（宣告了但相邻结果块里凑不齐
// 回执）在 seelex 这一侧的出口保证：
//
//	HTTP 400 {"error":{"message":"An assistant message with 'tool_calls' must be
//	followed by tool messages responding to each 'tool_call_id'. (insufficient
//	tool messages following tool_calls message)","type":"invalid_request_error"}}
//
// 现场（用户报告）：session loop 0 直接 400 —— 会话循环里第一次请求就带着一条
// "宣告了却没回执"的 assistant 行。两种形状都会这样，且旧口径两种都没有处置：
//
//  1. 同一个 call_id 被两条 assistant 各自宣告一次（中断后重发复用 ID）。旧口径
//     "每个 ID 只认首个结果"：第二条同名结果被当重复行丢掉、第二条声明也不算缺
//     回执 → 后一条声明在自己的相邻块里一条结果都没有；
//  2. 声明里带空 ID / 行内重复 ID 的调用。旧口径判"空 ID 无法配对，链保持原样"，
//     而 provider 是按宣告条数数回执的，这类调用永远配不上。
//
// 应用侧孪生实现同步修（application/core/context_runtime
// RepairInterruptedToolChains + history_pairing_gap_test.go）。

// TestPrepareReplaceHistoryPairsRepeatedDeclarationOccurrence：ReplaceHistory
// 路径（fabricate=true）下，第二次声明必须补到回执，历史对 provider 合法。
func TestPrepareReplaceHistoryPairsRepeatedDeclarationOccurrence(t *testing.T) {
	history := []types.Message{
		textMessage("user", "读一下文件"),
		callMessage("c1", "read_file"),
		resultMessage("c1", "read_file", "内容"),
		textMessage("assistant", "被打断了"),
		textMessage("user", "再试一次"),
		callMessage("c1", "read_file"), // 重发复用了同一个 call_id
	}
	prepared := PrepareReplaceHistory(history)
	assertProviderToolProtocol(t, prepared)
	synthetic := 0
	for _, message := range prepared {
		if message.Role == "tool" && messageContent(message) != "内容" {
			synthetic++
			if !strings.HasPrefix(messageContent(message), interruptedToolResultPrefix) {
				t.Fatalf("补出来的必须是合成占位（明示可能没执行）：%q", messageContent(message))
			}
		}
	}
	if synthetic != 1 {
		t.Fatalf("恰好补一条占位给第二次声明：%s", roleShape(prepared))
	}
	// 幂等：改好的历史再走一次逐字不变。
	again := PrepareReplaceHistory(prepared)
	if len(again) != len(prepared) {
		t.Fatalf("重复修复不得增删消息：%s", roleShape(again))
	}
}

// TestPrepareReplaceHistoryKeepsRepeatedResultWithRepeatedDeclaration：两条同名
// 结果各自紧跟自己的声明，是**合法**历史（每条声明都有相邻回执）。旧口径把第二条
// 当重复行丢掉，反而把合法历史改成缺回执的非法历史 —— 这里钉住"不许丢"。
func TestPrepareReplaceHistoryKeepsRepeatedResultWithRepeatedDeclaration(t *testing.T) {
	history := []types.Message{
		textMessage("user", "读一下文件"),
		callMessage("c1", "read_file"),
		resultMessage("c1", "read_file", "第一次内容"),
		textMessage("assistant", "结果作废"),
		textMessage("user", "重试"),
		callMessage("c1", "read_file"),
		resultMessage("c1", "read_file", "第二次内容"),
	}
	prepared := PrepareReplaceHistory(history)
	assertProviderToolProtocol(t, prepared)
	var contents []string
	for _, message := range prepared {
		if message.Role == "tool" {
			contents = append(contents, messageContent(message))
		}
	}
	if len(contents) != 2 || contents[0] != "第一次内容" || contents[1] != "第二次内容" {
		t.Fatalf("两条真结果都必须保留且顺序不变：%v（%s）", contents, roleShape(prepared))
	}
}

// TestAssemblerRepairsRepeatedDeclarationAtWire：wire 出口（fabricate=false）也
// 必须让请求合法。重复宣告不属于"可能正在执行的调用"（它已经拿到过一次回执），
// 不补占位就是必然 400 —— 保守口径在这里只会把请求打死。
func TestAssemblerRepairsRepeatedDeclarationAtWire(t *testing.T) {
	history := []types.Message{
		textMessage("user", "读一下文件"),
		callMessage("c1", "read_file"),
		resultMessage("c1", "read_file", "内容"),
		textMessage("assistant", "被打断了"),
		textMessage("user", "再试一次"),
		callMessage("c1", "read_file"),
		textMessage("assistant", "结束"),
	}
	assembled, err := NewAssembler(AssemblerOptions{}).Assemble(context.Background(), seelectx.AssemblyRequest{
		WorkingHistory: history,
	})
	if err != nil {
		t.Fatal(err)
	}
	assertProviderToolProtocol(t, assembled.Messages)
}

// TestAssemblerDropsUnpairableCallDeclarations：空 ID 与行内重复 ID 的调用在 wire
// 出口从声明里剔除（provider 按宣告条数数回执，它们永远配不上）。首次宣告且全篇
// 无结果的调用仍不补占位（活跃 ReAct 中间态口径，见
// TestAssemblerSanitizesWorkingHistoryToolProtocol）。
func TestAssemblerDropsUnpairableCallDeclarations(t *testing.T) {
	history := []types.Message{
		textMessage("user", "搜一下"),
		{Role: "assistant", ToolCalls: []types.ToolCall{
			{ID: "", Function: types.ToolCallFunction{Name: "read_file"}},
			{ID: "c1", Function: types.ToolCallFunction{Name: "grep_search"}},
			{ID: "c1", Function: types.ToolCallFunction{Name: "grep_search"}},
		}},
		resultMessage("c1", "grep_search", "命中"),
		textMessage("assistant", "结束"),
	}
	assembled, err := NewAssembler(AssemblerOptions{}).Assemble(context.Background(), seelectx.AssemblyRequest{
		WorkingHistory: history,
	})
	if err != nil {
		t.Fatal(err)
	}
	assertProviderToolProtocol(t, assembled.Messages)
	for _, message := range assembled.Messages {
		if message.Role != "assistant" || len(message.ToolCalls) == 0 {
			continue
		}
		if len(message.ToolCalls) != 1 || message.ToolCalls[0].ID != "c1" {
			t.Fatalf("无法配对的宣告必须剔除：%s", roleShape(assembled.Messages))
		}
	}
}
