package context_runtime

import (
	"reflect"
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/application/contract"
)

func TestRepairEmptyHistoryContentKeepsToolCallAssistantContentEmpty(t *testing.T) {
	history := []contract.EngineMessage{
		{Role: "assistant", ToolCalls: []contract.EngineToolCall{{ID: "call-1", Name: "read_file", Arguments: `{"path":"a.go"}`}}},
		// durable 转写可能带着回合收尾补写的视图流式正文：投影时必须归零，
		// 否则下一轮请求字节与已发出字节分叉。
		{Role: "assistant", Content: "我先读取装配入口。", ContentSet: true,
			ToolCalls: []contract.EngineToolCall{{ID: "call-2", Name: "grep_search", Arguments: `{"pattern":"x"}`}}},
		// 空工具结果：wire 上也是空正文（框架把工具返回值原样发出），同样不得
		// 补占位 —— 否则下一轮重投影 ≠ 已发出（2026-09-11 空工具结果修复，
		// 见 TestContextPrefixInvariant_EmptyToolResult）。
		{Role: "tool", ToolCallID: "call-1", Name: "read_file", Content: ""},
		{Role: "assistant", Content: ""},
	}
	prepared, repaired := RepairEmptyHistoryContent(history)
	if !repaired {
		t.Fatal("expected empty non-protocol messages to be repaired")
	}
	for _, index := range []int{0, 1} {
		if prepared[index].Content != "" || prepared[index].ContentSet {
			t.Fatalf("tool-call assistant %d kept provider content %q (set=%v), want empty: wire never carries text with tool calls",
				index, prepared[index].Content, prepared[index].ContentSet)
		}
		if len(prepared[index].ToolCalls) != 1 {
			t.Fatalf("tool-call assistant %d lost its protocol data: %+v", index, prepared[index])
		}
	}
	if prepared[2].Content != "" || prepared[2].ContentSet {
		t.Fatalf("empty tool result was given placeholder content %q (set=%v): the wire carries the tool result verbatim, empty stays empty",
			prepared[2].Content, prepared[2].ContentSet)
	}
	if prepared[3].Content != MissingHistoryContent || !prepared[3].ContentSet {
		t.Fatalf("message 3 was not repaired: %+v", prepared[3])
	}
}

func TestRetainedSystemHistoryKeepsStablePrefixAndSettledContext(t *testing.T) {
	history := []contract.EngineMessage{
		{Role: "system", Content: "product instruction", ContentSet: true},
		{Role: "user", Content: "settled request", ContentSet: true},
		{Role: "assistant", Content: "settled answer", ContentSet: true},
		{Role: "user", Content: planContextPrefix + "\n{}", ContentSet: true},
	}
	retained := RetainedSystemHistory(history)
	// 稳定前缀（system）+ 已定稿轮次保留；动态尾部（plan 消息）剔除。
	if got := retainedContents(retained); !reflect.DeepEqual(got, []string{"product instruction", "settled request", "settled answer"}) {
		t.Fatalf("retained history = %v, want stable prefix + settled context", got)
	}
	systemOnly := RetainedSystemOnly(history)
	if got := retainedContents(systemOnly); !reflect.DeepEqual(got, []string{"product instruction"}) {
		t.Fatalf("recovery retention = %v, want first system instruction only", got)
	}
}

// TestAutonomousCompactionMessageIsBoundedAndDynamicTail：自主压缩帧带协议
// 前缀、正文由 ContextSummary 提供（空摘要也给出显式说明），且属动态尾部——
// 保留段（稳定前缀 + 已定稿累积段）不得把它当作可复用前缀携带。
func TestAutonomousCompactionMessageIsBoundedAndDynamicTail(t *testing.T) {
	frame := AutonomousCompactionMessage("objective: inspect\nstatus: running")
	if !strings.HasPrefix(frame, AutonomousCompactionPrefix) {
		t.Fatalf("frame = %q, want prefix %q", frame, AutonomousCompactionPrefix)
	}
	if !strings.Contains(frame, "objective: inspect") {
		t.Fatalf("frame lost the bounded checkpoint summary: %q", frame)
	}
	if empty := AutonomousCompactionMessage("   "); !strings.HasPrefix(empty, AutonomousCompactionPrefix) {
		t.Fatalf("empty summary frame = %q, want prefix %q", empty, AutonomousCompactionPrefix)
	}
	history := []contract.EngineMessage{
		{Role: "system", Content: "product instruction", ContentSet: true},
		{Role: "user", Content: "settled request", ContentSet: true},
		{Role: "system", Content: frame, ContentSet: true},
	}
	retained := RetainedSystemHistory(history)
	if got := retainedContents(retained); !reflect.DeepEqual(got, []string{"product instruction", "settled request"}) {
		t.Fatalf("retained history = %v, want the compaction frame dropped", got)
	}
}

// TestCompactionFrameNeverReentersCompactionInput：帧是**终态**——一次压缩产生的帧绝不
// 能再被聚合成下一次压缩的输入（对摘要再摘要会丢事实，且失真不可追溯）。
// 两道闸门各自兜底：RetainedSystemHistory 丢掉动态尾（帧属动态尾），
// RetainedSystemOnly 只留首条产品指令。这里把第二道闸门也钉死，并明确压缩输入的
// 形状：产品指令 + 任务证据摘要 + plan + 当前输入，不含任何已落帧。
func TestCompactionFrameNeverReentersCompactionInput(t *testing.T) {
	frame := AutonomousCompactionMessage("objective: chunk-1 evidence")
	history := []contract.EngineMessage{
		{Role: "system", Content: "product instruction", ContentSet: true},
		{Role: "user", Content: "settled chunk material", ContentSet: true},
		{Role: "system", Content: frame, ContentSet: true},
	}
	retained := RetainedSystemHistory(history)
	if got := retainedContents(retained); !reflect.DeepEqual(got, []string{"product instruction", "settled chunk material"}) {
		t.Fatalf("保留段不得携带压缩帧，got %v", got)
	}
	only := RetainedSystemOnly(retained)
	if got := retainedContents(only); !reflect.DeepEqual(got, []string{"product instruction"}) {
		t.Fatalf("压缩输入只能带首条产品指令，不得携带帧，got %v", got)
	}
}

func retainedContents(history []contract.EngineMessage) []string {
	contents := make([]string, 0, len(history))
	for _, message := range history {
		contents = append(contents, message.Content)
	}
	return contents
}

// TestRetainedSystemHistoryKeepsActiveSkillEvent：激活技能事件是 append-only
// 定稿轮次（internal 标记），保留段必须照常携带并计数——它随 transcript
// 前缀一起缓存，不是每轮重建的动态尾部消息。
func TestRetainedSystemHistoryKeepsActiveSkillEvent(t *testing.T) {
	skill := ActiveSkillPrefix + "\n## Trusted Active Skill: review\nbody"
	history := []contract.EngineMessage{
		{Role: "system", Content: "product instruction", ContentSet: true},
		{Role: "user", Content: skill, ContentSet: true},
		{Role: "user", Content: "settled request", ContentSet: true},
		{Role: "assistant", Content: "settled answer", ContentSet: true},
	}
	retained := RetainedSystemHistory(history)
	if got := retainedContents(retained); !reflect.DeepEqual(got, []string{"product instruction", skill, "settled request", "settled answer"}) {
		t.Fatalf("retained history = %v, want stable prefix + settled context including the skill event", got)
	}
	if !IsActiveSkillContent(skill) {
		t.Fatal("IsActiveSkillContent must recognize the active-skill internal marker")
	}
	if isDynamicTailMessage(history[1]) {
		t.Fatal("active-skill event is a settled append-only turn, not a dynamic tail message")
	}
}
