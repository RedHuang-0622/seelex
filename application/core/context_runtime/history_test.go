package context_runtime

import (
	"reflect"
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
	for _, index := range []int{2, 3} {
		if prepared[index].Content != MissingHistoryContent || !prepared[index].ContentSet {
			t.Fatalf("message %d was not repaired: %+v", index, prepared[index])
		}
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
