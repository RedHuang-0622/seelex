package sessionhistory

import (
	"strings"
	"testing"

	context_runtime "github.com/RedHuang-0622/seelex/application/conversation/context"
	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/contract/dto"
	kernelcontext "github.com/RedHuang-0622/seelex/kernel/context"
)

func TestRuntimeHistoryVisibilityAndMessageProjection(t *testing.T) {
	runtime := &Runtime{displayInput: strings.TrimSpace}
	user := contract.EngineMessage{Role: "user", Content: "  hello  "}
	if !runtime.IsVisibleHistoryMessage(user) {
		t.Fatal("user row should be visible")
	}
	message := runtime.AdaptEngineMessage(user)
	if message.Content != "hello" {
		t.Fatalf("content = %q", message.Content)
	}
	if runtime.IsVisibleHistoryMessage(contract.EngineMessage{Role: "system", Content: "prompt"}) {
		t.Fatal("system row must remain hidden")
	}
}

// hidingDisplay 是「用户输入归一化后为空」的显示面：它只负责把内部信封折叠成
// 空串，除此之外原样返回（测试里不复制真实 skill/recovery 信封的解析规则）。
func hidingDisplay(value string) string {
	if value == "internal-envelope" {
		return ""
	}
	return value
}

// TestAdaptEngineMessageAppliesDisplayOnlyToUserRows 钉住 adaptEngineMessage 的
// 角色归一化方向：**只有** user 行经过 display 归一化，assistant/tool 只走
// thought block 剥离，system 两者都不走。
func TestAdaptEngineMessageAppliesDisplayOnlyToUserRows(t *testing.T) {
	stripThoughtBlocks := func(value string) string {
		return strings.ReplaceAll(value, "<thought>plan</thought>", "")
	}
	display := func(value string) string { return strings.TrimSpace(hidingDisplay(value)) }

	cases := []struct {
		name    string
		message contract.EngineMessage
		want    string
	}{
		{
			name:    "user 行经过 display 归一化",
			message: contract.EngineMessage{Role: "user", Content: "  typed  "},
			want:    "typed",
		},
		{
			name:    "user 行归一化后为空即空正文",
			message: contract.EngineMessage{Role: "user", Content: "internal-envelope"},
			want:    "",
		},
		{
			name:    "user 行不剥离 thought block",
			message: contract.EngineMessage{Role: "user", Content: "keep<thought>plan</thought>"},
			want:    "keep<thought>plan</thought>",
		},
		{
			name:    "assistant 行剥离 thought block",
			message: contract.EngineMessage{Role: "assistant", Content: "answer<thought>plan</thought>"},
			want:    "answer",
		},
		{
			name:    "tool 行剥离 thought block",
			message: contract.EngineMessage{Role: "tool", Content: "result<thought>plan</thought>"},
			want:    "result",
		},
		{
			name:    "system 行两条归一化都不走",
			message: contract.EngineMessage{Role: "system", Content: "internal-envelope<thought>plan</thought>"},
			want:    "internal-envelope<thought>plan</thought>",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got := adaptEngineMessage(testCase.message, stripThoughtBlocks, display)
			if got.Content != testCase.want {
				t.Fatalf("content = %q, want %q", got.Content, testCase.want)
			}
			if got.Role != testCase.message.Role {
				t.Fatalf("role = %q, want %q", got.Role, testCase.message.Role)
			}
			if got.ReasoningContent != testCase.message.ReasoningContent {
				t.Fatalf("reasoning = %q, want %q", got.ReasoningContent, testCase.message.ReasoningContent)
			}
		})
	}
}

// TestAdaptEngineMessageToleratesNilStripThoughtBlocks 钉住 stripThoughtBlocks 的
// 空值语义：nil 等价于「不剥离」，不得回退成 panic 或被当作空正文。
func TestAdaptEngineMessageToleratesNilStripThoughtBlocks(t *testing.T) {
	for _, role := range []string{"assistant", "tool"} {
		message := adaptEngineMessage(contract.EngineMessage{Role: role, Content: "text<thought>plan</thought>"}, nil, hidingDisplay)
		if message.Content != "text<thought>plan</thought>" {
			t.Fatalf("%s row content = %q, want 原样（nil strip 表示不剥离）", role, message.Content)
		}
	}
}

// TestAdaptEngineMessageClearsProviderOnlyContent 钉住 provider-only 修复文本不得
// 作为正文渲染：归一化（display 或 strip）之后再判一次，命中即清空。
func TestAdaptEngineMessageClearsProviderOnlyContent(t *testing.T) {
	providerOnly := []string{
		kernelcontext.MissingHistoryContent,
		kernelcontext.ToolCallHistoryContent,
	}
	for _, content := range providerOnly {
		if !context_runtime.IsProviderOnlyHistoryContent(content) {
			t.Fatalf("%q 已不再是 provider-only 文本，本用例的输入需要同步更新", content)
		}
		for _, role := range []string{"assistant", "user"} {
			message := adaptEngineMessage(contract.EngineMessage{Role: role, Content: content}, nil, hidingDisplay)
			if message.Content != "" {
				t.Fatalf("%s 行的 provider-only 正文未清空：%q", role, message.Content)
			}
		}
	}
	// 对照：真实正文（不是 provider-only）不得被误清。
	if message := adaptEngineMessage(contract.EngineMessage{Role: "assistant", Content: "真实回复"}, nil, hidingDisplay); message.Content != "真实回复" {
		t.Fatalf("真实正文被清空：%q", message.Content)
	}
}

// TestAdaptEngineMessageMapsToolCalls 钉住 tool_calls 的投影：引擎历史的每次
// 工具调用都要转成一条成功态的 model.ToolCall，否则恢复后工具卡片凭空消失。
func TestAdaptEngineMessageMapsToolCalls(t *testing.T) {
	message := adaptEngineMessage(contract.EngineMessage{
		Role:    "assistant",
		Content: "调用工具",
		ToolCalls: []contract.EngineToolCall{
			{ID: "call-1", Name: "read_file", Arguments: `{"path":"a.go"}`},
		},
	}, nil, hidingDisplay)
	if message.Tool == nil {
		t.Fatal("tool_calls 未投影成 model.ToolCall")
	}
	if message.Tool.ID != "call-1" || message.Tool.Name != "read_file" || message.Tool.Arguments != `{"path":"a.go"}` {
		t.Fatalf("tool call = %#v", message.Tool)
	}
	if message.Tool.Status != dto.ToolEventSuccess {
		t.Fatalf("tool status = %q, want %q", message.Tool.Status, dto.ToolEventSuccess)
	}
	if plain := adaptEngineMessage(contract.EngineMessage{Role: "assistant", Content: "无工具"}, nil, hidingDisplay); plain.Tool != nil {
		t.Fatalf("无 tool_calls 的行不该带工具卡片：%#v", plain.Tool)
	}
}

// TestIsVisibleHistoryMessageJudgesDisplayedUserContent 钉住可见性判据：system 恒
// 不可见；user 以**归一化后**的正文判空；assistant/tool 恒可见（正文由投影清空，
// 行本身仍占位，否则工具卡片与其结果会丢）。
func TestIsVisibleHistoryMessageJudgesDisplayedUserContent(t *testing.T) {
	cases := []struct {
		name    string
		message contract.EngineMessage
		want    bool
	}{
		{"system 恒不可见", contract.EngineMessage{Role: "system", Content: "prompt"}, false},
		{"user 归一化后非空可见", contract.EngineMessage{Role: "user", Content: "question"}, true},
		{"user 归一化后为空不可见", contract.EngineMessage{Role: "user", Content: "internal-envelope"}, false},
		{"user 空白归一化后为空不可见", contract.EngineMessage{Role: "user", Content: "   "}, false},
		{"assistant 空正文仍可见", contract.EngineMessage{Role: "assistant", Content: ""}, true},
		{"tool 行可见", contract.EngineMessage{Role: "tool", Content: ""}, true},
	}
	display := func(value string) string { return strings.TrimSpace(hidingDisplay(value)) }
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := isVisibleHistoryMessage(testCase.message, display); got != testCase.want {
				t.Fatalf("isVisibleHistoryMessage(%#v) = %v, want %v", testCase.message, got, testCase.want)
			}
		})
	}
}

// TestRuntimeHistorySemanticsReachExportedEntry 是装配面的一致性：Runtime 的导出
// 入口必须与包内判据同语义（displayInput 由装配注入、strip 由 conversationStreams
// 提供，未装配时等价于不剥离）。
func TestRuntimeHistorySemanticsReachExportedEntry(t *testing.T) {
	runtime := NewRuntime(RuntimeDeps{DisplayInput: hidingDisplay})
	hidden := contract.EngineMessage{Role: "user", Content: "internal-envelope"}
	if runtime.IsVisibleHistoryMessage(hidden) {
		t.Fatal("归一化后为空的 user 行经导出入口仍被判可见")
	}
	if content := runtime.AdaptEngineMessage(hidden).Content; content != "" {
		t.Fatalf("导出入口未清空归一化后的空正文：%q", content)
	}
	visible := contract.EngineMessage{Role: "assistant", Content: "answer"}
	if !runtime.IsVisibleHistoryMessage(visible) {
		t.Fatal("assistant 行经导出入口被判不可见")
	}
	if content := runtime.AdaptEngineMessage(visible).Content; content != "answer" {
		t.Fatalf("未装配 conversationStreams 时正文被改写：%q", content)
	}
}
