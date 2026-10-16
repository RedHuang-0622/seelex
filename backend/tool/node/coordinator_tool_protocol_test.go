package node

import (
	"context"
	"testing"

	"github.com/RedHuang-0622/Seele/seelectx"
	"github.com/RedHuang-0622/Seele/types"
)

// coordinator_tool_protocol_test.go — 节点子代理会话的 wire 出口协议。
//
// 节点装配器（ScopeAssembler）**不经** seelexctx 的装配器：它合并块之后直接
// 委托 DefaultRequestAssembler，所以 seelexctx 装配器里的出口规整对它无效，
// 必须显式调用 seelexctx.SanitizeProviderToolProtocol。2026-09-20 现场有一条
// 就落在节点会话上（账号角色 goalplan-1）：控制器投影刻意保留窗口内的非单元
// 消息（孤儿 tool 行），出口不规整 → provider 400
// "Messages with role 'tool' must be a response to a preceding message with
// 'tool_calls'"，节点会话循环中断。

// TestScopeAssemblerSanitizesWorkingHistoryToolProtocol 孤儿 tool 结果（没有
// assistant 宣告）不得进入 provider 请求；合法消息保序保留。
func TestScopeAssemblerSanitizesWorkingHistoryToolProtocol(t *testing.T) {
	orphan := "孤儿结果：没有任何 assistant 宣告过它"
	history := []types.Message{
		{Role: "user", Content: stringPtr("在跑一轮")},
		{Role: "assistant", Content: stringPtr("好的")},
		{Role: "tool", ToolCallID: "ghost", Name: "read_file", Content: &orphan},
		{Role: "user", Content: stringPtr("下一问")},
	}
	assembled, err := ScopeAssembler{}.Assemble(context.Background(), seelectx.AssemblyRequest{
		WorkingHistory: history,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, message := range assembled.Messages {
		if message.Role == "tool" {
			t.Fatalf("孤儿 tool 结果必须被出口剔除，实际消息序: %#v", assembled.Messages)
		}
	}
	if len(assembled.Messages) != 3 {
		t.Fatalf("合法消息必须保序保留（3 条），实际 %d 条: %#v", len(assembled.Messages), assembled.Messages)
	}
	if assembled.Messages[0].Role != "user" || assembled.Messages[1].Role != "assistant" ||
		assembled.Messages[2].Role != "user" {
		t.Fatalf("消息顺序被改动: %#v", assembled.Messages)
	}
}

// TestScopeAssemblerLeavesLegalHistoryUntouched 合法协议的历史逐字不动
// （投影 == 已发出字节，前缀缓存不失效）。
func TestScopeAssemblerLeavesLegalHistoryUntouched(t *testing.T) {
	call := types.Message{Role: "assistant", Content: stringPtr("我来读"), ToolCalls: []types.ToolCall{
		{ID: "c1", Function: types.ToolCallFunction{Name: "read_file"}},
	}}
	result := "内容"
	history := []types.Message{
		{Role: "user", Content: stringPtr("读文件")},
		call,
		{Role: "tool", ToolCallID: "c1", Name: "read_file", Content: &result},
	}
	assembled, err := ScopeAssembler{}.Assemble(context.Background(), seelectx.AssemblyRequest{
		WorkingHistory: history,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(assembled.Messages) != len(history) {
		t.Fatalf("合法历史长度不得变化：%d != %d", len(assembled.Messages), len(history))
	}
	for index := range history {
		if assembled.Messages[index].Role != history[index].Role ||
			assembled.Messages[index].ToolCallID != history[index].ToolCallID {
			t.Fatalf("合法历史 msg#%d 被改动: %#v", index, assembled.Messages)
		}
	}
}
