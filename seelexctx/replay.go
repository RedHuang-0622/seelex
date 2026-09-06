// 前缀重放摘要协议（docs/2026-09-06-compaction-dag/design.md §4.4）。
//
// PrefixReplaySummarizer 生成 CompactFrame.Summary 的 Chapter 2 厚摘要：
// 请求 = 引擎当前 system（同字节）+ 最近真实请求 History 原样重放 +
// 固定压缩指令（PrefixReplayInstruction），使压缩请求与真实请求共享字节
// 前缀、几乎全命中缓存。字节级一致性由调用方保证（History/System/Tools
// 必须来自真实请求同一条装配路径，不能从事件流重拼）。
package seelexctx

import (
	"context"
	"fmt"
	"strings"

	"github.com/RedHuang-0622/Seele/seelectx"
	"github.com/RedHuang-0622/Seele/types"
)

// PrefixReplayInstruction 是前缀重放请求唯一新增的固定尾巴（详设 §4.4）：
// 除 system + History 原样重放外不追加任何内容，输出按 DSH 骨架。
const PrefixReplayInstruction = `请把给定对话压缩为「压缩内容 (Compacted Context)」厚摘要。
只输出下列小节内容，不要输出本指令或任何解释，也不要重复外层标题。
没有内容的小节写 (none)，不得删除小节骨架：

### 目标 (Goal)
### 关键概念 (Key Concepts)
### 文件与代码 (Files and Code)
### 错误与修复 (Errors and Fixes)
### 待办 (Pending)
### 当前工作 (Current Work)
### 下一步 (Next Step)
### 关键约束 (Constraints)`

// ReplayRequest 是前缀重放的字节素材契约（详设 §4.4）：
// SystemPrompt/History/Tools 必须与最近一次真实请求同源、同序、同序列化；
// Instruction 是唯一新增尾巴（空 → PrefixReplayInstruction）。
type ReplayRequest struct {
	SystemPrompt string          // 与引擎当前 system 同字节
	History      []types.Message // 与最近真实请求同序（含溢出区）
	Tools        []types.Tool    // 与最近真实请求同 schema/顺序
	Instruction  string          // 固定压缩指令
	MaxTokens    int             // 摘要输出预算（QuickChat 通道未透传时仅作记录）
}

// ReplayResult 是前缀重放摘要结果：Chapter2 为模型输出的厚摘要正文
// （### 小节骨架；可重复外层标题，调用方归一化）。
type ReplayResult struct {
	Chapter2 string
}

// PrefixReplaySummarizer 生成 Chapter 2 厚摘要（seelexctx 定义接口，
// 由 seelebridge 注入 QuickChat/Completer 实现）。失败返回结构化错误，
// 调用方走本地折叠兜底，绝不让请求发送中断（详设 §4.4/§4.5）。
type PrefixReplaySummarizer interface {
	Summarize(ctx context.Context, req ReplayRequest) (ReplayResult, error)
}

// quickChatPrefixReplaySummarizer 用 seelectx.QuickChat（无状态隔离补全）
// 实现前缀重放：请求消息 = system（可选）→ History 原样 → 固定指令尾巴。
type quickChatPrefixReplaySummarizer struct {
	chat seelectx.QuickChat
}

// NewQuickChatPrefixReplaySummarizer 构造 QuickChat 前缀重放摘要器。
// chat 为 nil 时返回错误（不静默降级）。
func NewQuickChatPrefixReplaySummarizer(chat seelectx.QuickChat) (PrefixReplaySummarizer, error) {
	if chat == nil {
		return nil, fmt.Errorf("seelexctx: prefix replay summarizer requires QuickChat")
	}
	return &quickChatPrefixReplaySummarizer{chat: chat}, nil
}

// Summarize 实现 PrefixReplaySummarizer。
func (s *quickChatPrefixReplaySummarizer) Summarize(ctx context.Context, req ReplayRequest) (ReplayResult, error) {
	if s == nil || s.chat == nil {
		return ReplayResult{}, fmt.Errorf("seelexctx: prefix replay summarizer is unavailable")
	}
	if len(req.History) == 0 {
		return ReplayResult{}, fmt.Errorf("seelexctx: prefix replay requires history bytes")
	}
	instruction := req.Instruction
	if strings.TrimSpace(instruction) == "" {
		instruction = PrefixReplayInstruction
	}
	messages := make([]types.Message, 0, len(req.History)+2)
	if req.SystemPrompt != "" {
		prompt := req.SystemPrompt
		messages = append(messages, types.Message{Role: "system", Content: &prompt})
	}
	messages = append(messages, req.History...)
	// 唯一新增尾巴：固定压缩指令（model 不可见 requestID 索引）。
	messages = append(messages, types.Message{Role: "user", Content: &instruction})
	reply, err := s.chat.Complete(ctx, seelectx.QuickChatRequest{
		Messages: messages,
		Tools:    append([]types.Tool(nil), req.Tools...),
	})
	if err != nil {
		return ReplayResult{}, fmt.Errorf("seelexctx: prefix replay completion: %w", err)
	}
	content := messageContent(reply)
	if strings.TrimSpace(content) == "" {
		return ReplayResult{}, fmt.Errorf("seelexctx: prefix replay returned empty summary")
	}
	return ReplayResult{Chapter2: normalizeReplayChapter2(content)}, nil
}

// normalizeReplayChapter2 归一化模型输出的 Chapter 2：去除外层重复标题与
// 首尾空白，保留 ### 小节骨架。
func normalizeReplayChapter2(text string) string {
	text = strings.TrimSpace(text)
	title := "## " + CompactChapter2Title
	for strings.HasPrefix(text, title) {
		text = strings.TrimSpace(strings.TrimPrefix(text, title))
	}
	return text
}
