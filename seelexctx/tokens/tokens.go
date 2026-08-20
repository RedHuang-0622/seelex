// Package tokens 提供无外部依赖的脚本感知 token 估算。
//
// 背景：len/3（字节数/3）在中文、标点与工具参数混合场景误差大
// （调研见 docs/research/agent-harness-research-report.md P2-10）。本包按
// 脚本类型分别估值，方向偏保守（宁高勿低），避免请求超出上下文窗口：
//
//   - CJK（中文/日文假名/韩文）：1 字符 ≈ 1 token（多数模型实际 0.6~0.9）
//   - ASCII 字母/数字：4 字符 ≈ 1 token（英文实际约 3.5~4.5 字符/token）
//   - 非 ASCII 字母/数字（西里尔等）：1 字符 ≈ 1 token（保守）
//   - 其余符号/空白/emoji：2 字符 ≈ 1 token（标点常与词合并，个别独立成 token）
//
// 该估算用于事前决策（窗口/压缩预算）；真实计数由 provider usage 在事后
// 记入 TokenAudit，并可反馈校准（见 application/core/token_counter.go）。
package tokens

import (
	"unicode"

	"github.com/RedHuang-0622/Seele/types"
)

// Count 估算单段文本的 token 数；空串返回 0。结果确定、无外部依赖。
func Count(text string) int {
	if text == "" {
		return 0
	}
	var cjk, ascii, other int
	for _, r := range text {
		switch {
		case isCJKRune(r):
			cjk++
		case r < utf8RuneSelf && (unicode.IsLetter(r) || unicode.IsDigit(r)):
			ascii++
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			cjk++ // 非 ASCII 文字按 1 token/字符（保守）
		default:
			other++
		}
	}
	// 各段取上限，保证偏保守；最小返回 1（非空文本至少占 1 token）。
	return cjk + (ascii+3)/4 + (other+1)/2
}

// utf8RuneSelf 是 ASCII 与非 ASCII 的分界（0x80）。
const utf8RuneSelf = 0x80

// isCJKRune 判断是否为中日韩统一表意/假名/谚文。
func isCJKRune(r rune) bool {
	return unicode.Is(unicode.Han, r) ||
		unicode.Is(unicode.Hiragana, r) ||
		unicode.Is(unicode.Katakana, r) ||
		unicode.Is(unicode.Hangul, r)
}

// CountMessage 估算单条协议消息的 token（含 role 与结构开销，与
// seelexctx ConservativeTokenCounter.CountMessage 同形）。
func CountMessage(message types.Message) int {
	tokens := 4 + Count(message.Role)
	if message.Content != nil {
		tokens += Count(*message.Content)
	}
	tokens += Count(message.ReasoningContent)
	if message.ToolCallID != "" {
		tokens += 2 + Count(message.ToolCallID)
	}
	if message.Name != "" {
		tokens += 2 + Count(message.Name)
	}
	for _, call := range message.ToolCalls {
		tokens += 8 + Count(call.ID) + Count(call.Function.Name) + Count(call.Function.Arguments)
	}
	return tokens
}

// CountHistory 估算整段历史消息的 token 数。
func CountHistory(messages []types.Message) int {
	total := 0
	for _, message := range messages {
		total += CountMessage(message)
	}
	return total
}
