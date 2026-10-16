package seelexctx

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/RedHuang-0622/Seele/seelectx"
	"github.com/RedHuang-0622/Seele/types"
)

// recordingQuickChat 记录每次补全请求（前缀重放测试替身）。
type recordingQuickChat struct {
	mu       sync.Mutex
	requests []seelectx.QuickChatRequest
	reply    string
	err      error
}

func (c *recordingQuickChat) Complete(_ context.Context, request seelectx.QuickChatRequest) (types.Message, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.requests = append(c.requests, request)
	if c.err != nil {
		return types.Message{}, c.err
	}
	content := c.reply
	return types.Message{Role: "assistant", Content: &content}, nil
}

func (c *recordingQuickChat) snapshot() []seelectx.QuickChatRequest {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]seelectx.QuickChatRequest(nil), c.requests...)
}

// TestQuickChatPrefixReplayByteOrder 请求 = system（可选）→ History 原样 →
// 固定指令尾巴；Tools 透传；返回正文归一化（剥外层重复标题）。
func TestQuickChatPrefixReplayByteOrder(t *testing.T) {
	chat := &recordingQuickChat{reply: "## " + CompactChapter2Title + "\n### 目标 (Goal)\n迁移完成"}
	summarizer, err := NewQuickChatPrefixReplaySummarizer(chat)
	if err != nil {
		t.Fatal(err)
	}
	history := []types.Message{
		textMessage("user", "迁移模块 X"),
		textMessage("assistant", "开始"),
	}
	tools := []types.Tool{{Type: "function", Function: types.ToolFunction{Name: "read_file"}}}
	result, err := summarizer.Summarize(context.Background(), ReplayRequest{
		SystemPrompt: "system-bytes",
		History:      history,
		Tools:        tools,
		MaxTokens:    2048,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Chapter2 != "### 目标 (Goal)\n迁移完成" {
		t.Fatalf("chapter2 = %q", result.Chapter2)
	}
	requests := chat.snapshot()
	if len(requests) != 1 {
		t.Fatalf("requests = %d, want 1", len(requests))
	}
	request := requests[0]
	wantRoles := []string{"system", "user", "assistant", "user"}
	if len(request.Messages) != len(wantRoles) {
		t.Fatalf("messages = %d, want %d", len(request.Messages), len(wantRoles))
	}
	for index, role := range wantRoles {
		if request.Messages[index].Role != role {
			t.Fatalf("message[%d].role = %q, want %q", index, request.Messages[index].Role, role)
		}
	}
	// 前缀字节：History 原样（第二、三条）；尾巴固定指令。
	if *request.Messages[1].Content != "迁移模块 X" || *request.Messages[2].Content != "开始" {
		t.Fatal("history bytes must be replayed in original order")
	}
	if !strings.Contains(*request.Messages[3].Content, "### 目标 (Goal)") {
		t.Fatal("last message must carry the fixed compression instruction")
	}
	if len(request.Tools) != 1 || request.Tools[0].Function.Name != "read_file" {
		t.Fatalf("tools = %+v", request.Tools)
	}
}

// TestQuickChatPrefixReplayFallbackErrors 输入/通道错误显式返回：
// 空 history、补全失败、空回复、nil QuickChat。
func TestQuickChatPrefixReplayFallbackErrors(t *testing.T) {
	if _, err := NewQuickChatPrefixReplaySummarizer(nil); err == nil {
		t.Fatal("nil QuickChat must fail construction")
	}
	chat := &recordingQuickChat{reply: "ok"}
	summarizer, err := NewQuickChatPrefixReplaySummarizer(chat)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := summarizer.Summarize(context.Background(), ReplayRequest{}); err == nil {
		t.Fatal("empty history must fail")
	}
	history := []types.Message{textMessage("user", "x")}
	chat.err = errors.New("completion boom")
	if _, err := summarizer.Summarize(context.Background(), ReplayRequest{History: history}); err == nil {
		t.Fatal("completion error must propagate")
	}
	chat.err = nil
	chat.reply = "   "
	if _, err := summarizer.Summarize(context.Background(), ReplayRequest{History: history}); err == nil {
		t.Fatal("empty reply must fail")
	}
}

// TestQuickChatPrefixReplayDefaultInstruction 未给 Instruction 时使用固定
// 压缩指令（含 DSH 小节骨架）。
func TestQuickChatPrefixReplayDefaultInstruction(t *testing.T) {
	chat := &recordingQuickChat{reply: "### 目标 (Goal)\n完成"}
	summarizer, err := NewQuickChatPrefixReplaySummarizer(chat)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := summarizer.Summarize(context.Background(), ReplayRequest{
		History: []types.Message{textMessage("user", "x")},
	}); err != nil {
		t.Fatal(err)
	}
	request := chat.snapshot()[0]
	tail := *request.Messages[len(request.Messages)-1].Content
	if tail != PrefixReplayInstruction {
		t.Fatalf("tail must equal fixed instruction when unset")
	}
}
