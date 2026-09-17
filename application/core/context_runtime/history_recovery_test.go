package context_runtime

import (
	"reflect"
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/application/contract"
)

// interruptedChainHistory 构造残缺工具链 provider 历史（同三处探针语义）：
// user 任务 → assistant 请求 t1/t2 → 只记录 t1 结果；withSuffixText 时其后
// 跟同轮 assistant 文本答复。
func interruptedChainHistory(withSuffixText, atTail bool) []contract.EngineMessage {
	history := []contract.EngineMessage{
		{Role: "user", Content: "任务1：重构并验证", ContentSet: true},
		{Role: "assistant", ToolCalls: []contract.EngineToolCall{
			{ID: "t1", Name: "read_file", Arguments: `{"path":"a.go"}`},
			{ID: "t2", Name: "grep_search", Arguments: `{"pattern":"x"}`},
		}},
		{Role: "tool", ToolCallID: "t1", Name: "read_file", Content: "file body", ContentSet: true},
	}
	if withSuffixText {
		history = append(history, contract.EngineMessage{Role: "assistant", Content: "已完成第一步，继续", ContentSet: true})
	}
	if !atTail {
		history = append(history,
			contract.EngineMessage{Role: "user", Content: "任务2：继续", ContentSet: true},
			contract.EngineMessage{Role: "assistant", Content: "任务2完成", ContentSet: true},
		)
	}
	return history
}

// TestRepairInterruptedToolChainsFillsMissingResultBeforeSuffixText：
// 残缺工具链（缺 t2 结果）必须在断裂点（后缀文本前）插入合成 tool 占位，
// 使发送 provider 的序列 assistant/tool 配对合法 —— 重启后 continue 时模型
// 能看到原任务、已完成结果与"t2 状态未知"的显式信号。
func TestRepairInterruptedToolChainsFillsMissingResultBeforeSuffixText(t *testing.T) {
	history := interruptedChainHistory(true, false)
	prepared, repaired := RepairInterruptedToolChains(history)
	if !repaired {
		t.Fatal("interrupted chain must be repaired")
	}
	if len(prepared) != 7 {
		t.Fatalf("prepared length = %d, want 7 (user, tc, t1, t2 recovery, text, task2 user, task2 assistant)", len(prepared))
	}
	if prepared[3].Role != "tool" || prepared[3].ToolCallID != "t2" || prepared[3].Name != "grep_search" {
		t.Fatalf("missing result must be filled as tool t2 before suffix text: %+v", prepared[3])
	}
	if !strings.HasPrefix(prepared[3].Content, InterruptedToolResultPrefix) {
		t.Fatalf("synthetic tool result must carry the recovery prefix: %q", prepared[3].Content)
	}
	if !IsProviderOnlyHistoryContent(prepared[3].Content) {
		t.Fatal("synthetic tool result must be recognized as provider-only")
	}
	if prepared[4].Role != "assistant" || prepared[4].Content != "已完成第一步，继续" {
		t.Fatalf("suffix text must stay after the filled result: %+v", prepared[4])
	}
}

// TestRepairInterruptedToolChainsFillsAllMissingAtTail：会话尾以残缺链收尾
// （Case B，无文本终止点）时补 t2 于链尾，冷加载后 continue 上下文完整。
func TestRepairInterruptedToolChainsFillsAllMissingAtTail(t *testing.T) {
	history := interruptedChainHistory(false, true)
	prepared, repaired := RepairInterruptedToolChains(history)
	if !repaired {
		t.Fatal("tail interrupted chain must be repaired")
	}
	if len(prepared) != 4 {
		t.Fatalf("prepared length = %d, want 4 (user, tc, t1, t2 recovery)", len(prepared))
	}
	last := prepared[len(prepared)-1]
	if last.Role != "tool" || last.ToolCallID != "t2" {
		t.Fatalf("tail must end with the t2 recovery tool result: %+v", last)
	}
}

// TestRepairInterruptedToolChainsSkipsCompleteChainsAndIsIdempotent：
// 完整链不改动（repaired=false）；补过的链再跑不重复注入。
func TestRepairInterruptedToolChainsSkipsCompleteChainsAndIsIdempotent(t *testing.T) {
	complete := []contract.EngineMessage{
		{Role: "assistant", ToolCalls: []contract.EngineToolCall{{ID: "c1", Name: "read_file"}}},
		{Role: "tool", ToolCallID: "c1", Name: "read_file", Content: "ok", ContentSet: true},
	}
	prepared, repaired := RepairInterruptedToolChains(complete)
	if repaired {
		t.Fatal("complete chain must not be repaired")
	}
	if !reflect.DeepEqual(prepared, complete) {
		t.Fatalf("complete chain changed: %+v", prepared)
	}

	broken := interruptedChainHistory(false, true)
	once, _ := RepairInterruptedToolChains(broken)
	twice, _ := RepairInterruptedToolChains(once)
	if !reflect.DeepEqual(once, twice) {
		t.Fatalf("repair is not idempotent:\nonce=%+v\ntwice=%+v", once, twice)
	}
}

// TestRepairInterruptedToolChainsReordersResultBackToDeclaration：缺失 ID 的
// 结果若出现在后文（乱序历史），必须搬回声明之后而不是留在原处。
//
// 这条用例原先是「后文存在结果就不动」，那个口径会让
// `assistant(tool_calls c1) → user → tool(c1)` 原样发给 provider，而 provider
// 的校验是「每条 tool 消息必须紧跟在携带其 tool_calls 的 assistant 之后」——
// 直接 400（`Messages with role 'tool' must be a response to a preceding
// message with 'tool_calls'`，2026-09-17 实测）。搬回后序列合法，且没有丢内容。
func TestRepairInterruptedToolChainsReordersResultBackToDeclaration(t *testing.T) {
	history := []contract.EngineMessage{
		{Role: "user", Content: "task", ContentSet: true},
		{Role: "assistant", ToolCalls: []contract.EngineToolCall{{ID: "c1", Name: "bash"}}},
		{Role: "user", Content: "inline note", ContentSet: true},
		{Role: "tool", ToolCallID: "c1", Name: "bash", Content: "later", ContentSet: true},
	}
	prepared, repaired := RepairInterruptedToolChains(history)
	if !repaired {
		t.Fatal("out-of-order (non-adjacent) result must be repaired")
	}
	if len(prepared) != len(history) {
		t.Fatalf("reorder must not change length: %d -> %d", len(history), len(prepared))
	}
	if prepared[1].Role != "assistant" || prepared[2].Role != "tool" || prepared[2].ToolCallID != "c1" {
		t.Fatalf("result must sit right after its declaration: %+v", prepared)
	}
	if prepared[2].Content != "later" {
		t.Fatalf("reorder must preserve the real result content: %q", prepared[2].Content)
	}
	if prepared[3].Role != "user" || prepared[3].Content != "inline note" {
		t.Fatalf("the intervening message must survive the reorder: %+v", prepared[3])
	}
	if !looksLikeProviderValidToolPairs(prepared) {
		t.Fatalf("repaired history still violates the provider tool-pair rule: %+v", prepared)
	}
	// 幂等：重排后的序列再跑一次不变。
	again, againRepaired := RepairInterruptedToolChains(prepared)
	if againRepaired {
		t.Fatal("canonical order must not report further repair")
	}
	if !reflect.DeepEqual(prepared, again) {
		t.Fatalf("repair is not idempotent:\nonce=%+v\ntwice=%+v", prepared, again)
	}
}

// TestRepairInterruptedToolChainsDropsOrphanResult：没有任何 assistant 宣告该
// call_id 的 tool 行是无法满足 provider 协议的孤儿（它必须回应一条带 tool_calls
// 的前一条消息），投影时必须剔除而不是原样发出。
func TestRepairInterruptedToolChainsDropsOrphanResult(t *testing.T) {
	history := []contract.EngineMessage{
		{Role: "user", Content: "task", ContentSet: true},
		{Role: "assistant", Content: "先说一句", ContentSet: true},
		{Role: "tool", ToolCallID: "ghost", Name: "bash", Content: "orphan", ContentSet: true},
		{Role: "assistant", Content: "继续", ContentSet: true},
	}
	prepared, repaired := RepairInterruptedToolChains(history)
	if !repaired {
		t.Fatal("orphan tool row must be repaired (dropped)")
	}
	if len(prepared) != 3 {
		t.Fatalf("orphan tool row must be dropped: %+v", prepared)
	}
	for _, message := range prepared {
		if message.Role == "tool" {
			t.Fatalf("orphan tool row survived: %+v", message)
		}
	}
	if !looksLikeProviderValidToolPairs(prepared) {
		t.Fatalf("repaired history still violates the provider tool-pair rule: %+v", prepared)
	}
}

// TestRepairInterruptedToolChainsDropsDuplicateResult：同一 call_id 的第二个结果
// 同样没有可回应的声明位置（一条调用只有一个结果），provider 会拒绝，投影时按
// 重复丢弃；首个结果保持原位、内容不变。
func TestRepairInterruptedToolChainsDropsDuplicateResult(t *testing.T) {
	history := []contract.EngineMessage{
		{Role: "assistant", ToolCalls: []contract.EngineToolCall{{ID: "c1", Name: "bash"}}},
		{Role: "tool", ToolCallID: "c1", Name: "bash", Content: "first", ContentSet: true},
		{Role: "tool", ToolCallID: "c1", Name: "bash", Content: "duplicate", ContentSet: true},
		{Role: "assistant", Content: "done", ContentSet: true},
	}
	prepared, repaired := RepairInterruptedToolChains(history)
	if !repaired {
		t.Fatal("duplicate result must be repaired (dropped)")
	}
	if len(prepared) != 3 {
		t.Fatalf("duplicate result must be dropped: %+v", prepared)
	}
	if prepared[1].Role != "tool" || prepared[1].Content != "first" {
		t.Fatalf("the first result must survive untouched: %+v", prepared[1])
	}
	if !looksLikeProviderValidToolPairs(prepared) {
		t.Fatalf("repaired history still violates the provider tool-pair rule: %+v", prepared)
	}
	again, againRepaired := RepairInterruptedToolChains(prepared)
	if againRepaired || !reflect.DeepEqual(prepared, again) {
		t.Fatalf("duplicate repair must be idempotent: %+v", again)
	}
}

// looksLikeProviderValidToolPairs 是 provider 工具配对规则的本地校验器：
//
//  1. 每条 `tool` 消息的前一条必须是携带该 call_id 的 `assistant` 消息；
//  2. 每条携带 `tool_calls` 的 `assistant` 消息之后必须依次跟齐全部结果。
//
// 用它把「修完之后仍然会被 provider 拒绝」变成可断言的红灯，而不是等线上 HTTP 400。
func looksLikeProviderValidToolPairs(history []contract.EngineMessage) bool {
	expected := make([]string, 0, 4)
	for _, message := range history {
		if message.Role == "tool" {
			if len(expected) == 0 || expected[0] != message.ToolCallID {
				return false
			}
			expected = expected[1:]
			continue
		}
		if len(expected) > 0 {
			return false // 结果没跟齐就出现别的消息
		}
		for _, call := range message.ToolCalls {
			expected = append(expected, call.ID)
		}
	}
	return len(expected) == 0
}
