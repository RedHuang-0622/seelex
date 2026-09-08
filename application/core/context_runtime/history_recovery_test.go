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

// TestRepairInterruptedToolChainsSkipsWhenResultExistsLater：缺失 ID 的结果
// 若在后文存在（乱序历史）则不注入占位，避免重复/破坏既有配对。
func TestRepairInterruptedToolChainsSkipsWhenResultExistsLater(t *testing.T) {
	history := []contract.EngineMessage{
		{Role: "user", Content: "task", ContentSet: true},
		{Role: "assistant", ToolCalls: []contract.EngineToolCall{{ID: "c1", Name: "bash"}}},
		{Role: "user", Content: "inline note", ContentSet: true},
		{Role: "tool", ToolCallID: "c1", Name: "bash", Content: "later", ContentSet: true},
	}
	prepared, repaired := RepairInterruptedToolChains(history)
	if repaired {
		t.Fatal("must not inject a placeholder when the result exists later")
	}
	if len(prepared) != len(history) {
		t.Fatalf("length changed: %d -> %d", len(history), len(prepared))
	}
}
