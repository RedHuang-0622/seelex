package context_runtime

import (
	"reflect"
	"testing"

	"github.com/RedHuang-0622/seelex/application/contract"
)

// TestHasConversationBodyTable 钉住"引擎历史里有没有对话正文"这组形状。
// 与 seelexctx.HasConversationBody（seelexctx/replay_body_gate_test.go）是**同一组
// 形状**：两侧消息类型不同（contract.EngineMessage ↔ types.Message），规则本身
// 不允许分叉——一侧放宽、另一侧就会把"发不发重放请求"判成两回事。
func TestHasConversationBodyTable(t *testing.T) {
	systemPrompt := contract.EngineMessage{Role: "system", Content: "product system prompt", ContentSet: true}
	plan := contract.EngineMessage{Role: "system", Content: "plan context", ContentSet: true}
	blankUser := contract.EngineMessage{Role: "user", Content: "   ", ContentSet: true}
	declaration := contract.EngineMessage{Role: "assistant", ContentSet: true,
		ToolCalls: []contract.EngineToolCall{{ID: "t1", Name: "read_file", Arguments: "{}"}}}
	orphanResult := contract.EngineMessage{Role: "tool", ToolCallID: "t1", Name: "read_file", Content: "body", ContentSet: true}
	cases := []struct {
		name     string
		messages []contract.EngineMessage
		want     bool
	}{
		{name: "nil", messages: nil, want: false},
		{name: "只有产品指令", messages: []contract.EngineMessage{systemPrompt}, want: false},
		{name: "只有控制行", messages: []contract.EngineMessage{systemPrompt, plan}, want: false},
		{name: "空白 user 行", messages: []contract.EngineMessage{systemPrompt, blankUser}, want: false},
		{name: "user 正文", messages: []contract.EngineMessage{systemPrompt,
			{Role: "user", Content: "读一下 ports.go", ContentSet: true}}, want: true},
		{name: "assistant 正文", messages: []contract.EngineMessage{systemPrompt,
			{Role: "assistant", Content: "第一段正文", ContentSet: true}}, want: true},
		{name: "assistant 宣告工具调用", messages: []contract.EngineMessage{systemPrompt, declaration}, want: true},
		{name: "工具结果也是对话载荷", messages: []contract.EngineMessage{systemPrompt, orphanResult}, want: true},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := hasConversationBody(testCase.messages); got != testCase.want {
				t.Fatalf("hasConversationBody = %t, want %t（%+v）", got, testCase.want, testCase.messages)
			}
		})
	}
}

// TestReplayMaterialForFallbackTable：素材选择的两条事实——有正文时**逐字**用
// existing（前缀缓存口径：不改字节），没有正文时改取本次装配的 fullContext。
//
// 两个都没有正文时不在这里兜：装配层照常把 fullContext 交下去，由 DAG 的
// no-replay-material 闸拦下（这次压缩不调用模型、只留失败痕）——判据只有一条，
// 不在装配层再复制一份。
func TestReplayMaterialForFallbackTable(t *testing.T) {
	systemPrompt := contract.EngineMessage{Role: "system", Content: "product system prompt", ContentSet: true}
	existing := []contract.EngineMessage{systemPrompt,
		{Role: "user", Content: "上一次真实请求里的用户输入", ContentSet: true},
		{Role: "assistant", Content: "上一次真实请求里的回复", ContentSet: true}}
	fullContext := []contract.EngineMessage{systemPrompt,
		{Role: "user", Content: "本次装配投影出的用户输入", ContentSet: true}}
	cases := []struct {
		name     string
		existing []contract.EngineMessage
		full     []contract.EngineMessage
		want     []contract.EngineMessage
	}{
		{name: "引擎历史含正文 → 逐字用它（前缀缓存）", existing: existing, full: fullContext, want: existing},
		{name: "引擎历史为空 → 取本次装配", existing: nil, full: fullContext, want: fullContext},
		{name: "引擎历史只剩 system 行（现场 messages_before=1）→ 取本次装配",
			existing: []contract.EngineMessage{systemPrompt}, full: fullContext, want: fullContext},
		{name: "引擎历史只剩控制行 → 取本次装配",
			existing: []contract.EngineMessage{systemPrompt,
				{Role: "system", Content: "plan context", ContentSet: true}}, full: fullContext, want: fullContext},
		{name: "两边都没有正文 → 交 fullContext，由 DAG 闸拦下",
			existing: []contract.EngineMessage{systemPrompt},
			full:     []contract.EngineMessage{systemPrompt}, want: []contract.EngineMessage{systemPrompt}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got := replayMaterialFor(testCase.existing, testCase.full)
			if !reflect.DeepEqual(got, testCase.want) {
				t.Fatalf("replayMaterialFor = %+v, want %+v", got, testCase.want)
			}
			if testCase.name == "引擎历史含正文 → 逐字用它（前缀缓存）" && &got[0] != &testCase.existing[0] {
				t.Fatalf("含正文的引擎历史必须**原样**交出去（不重建切片，前缀缓存靠它）")
			}
		})
	}
}
