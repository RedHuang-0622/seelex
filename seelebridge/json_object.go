package seelebridge

// json_object.go — 从 LLM 原文里取 JSON 对象的容错层（只修语法，不改语义）。
//
// 为什么需要它（证据）：2026-09-16 22:06 的真实 goal 收口回合里，ADVISOR 的裁决
// 原文在 content 值里带了**未转义的内层引号**（"定义—执行—验证"）。旧实现
// parseGoalDirective 只做"第一个 { 到最后一个 } → json.Unmarshal"，解码因此停在
// 提前闭合的字符串处：
//
//	goal TL 输出非 JSON: invalid character 'å' after object key:value pair
//
// （'å' = 0xE5，正是紧随其后那个中文字的 UTF-8 首字节——Go 的 encoding/json 用 %q
// 打印非法字节，看起来才像拉丁字母。）
//
// 后果不是"少一条日志"：gate 把评估器的任何错误都归进 B4 缺席矩阵，于是一条内容上
// 已经 verdict_done 的裁决被判成"缺席（429/超时）"，goal 保持 active 并转人工，
// 面向用户的收口说明与实际状态不一致（同一次运行里 goal 随后被 directive 路径收口
// 为 completed）。**一条裁决该不该收口，不该由转义细节决定。**
//
// 边界：本层只做两件事——补引号/控制字符的转义，再取首个**括号平衡**的对象。
// 不改字段名、不补字段、不猜 kind；语义校验仍在 goal 域的 TLDirective.Validate()。

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// ErrNoJSONObject 表示原文里找不到任何可解码的 JSON 对象。
var ErrNoJSONObject = errors.New("no JSON object found")

// decodeJSONObjectLenient 取 raw 里第一个平衡的 JSON 对象并解码到 out。
// 三档依次尝试，任一成功即返回：严格 → 提取 → 修复后提取。
func decodeJSONObjectLenient(raw string, out any) error {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ErrNoJSONObject
	}
	var lastErr error
	if object, ok := firstBalancedJSONObject(trimmed, false); ok {
		if err := json.Unmarshal([]byte(object), out); err != nil {
			lastErr = err
		} else {
			return nil
		}
	}
	if object, ok := firstBalancedJSONObject(trimmed, true); ok {
		if err := json.Unmarshal([]byte(object), out); err != nil {
			// 修复档已经消掉了最常见的两类违约，它的错误更能说明问题。
			lastErr = err
		} else {
			return nil
		}
	}
	if lastErr != nil {
		return lastErr
	}
	return ErrNoJSONObject
}

// firstBalancedJSONObject 返回 raw 里第一个花括号平衡的 JSON 对象文本。
//
// repair=false：原样切片（先让严格解析去判）。
// repair=true ：对**字符串值内部**的两类语法违约做最小修复后再切片：
//   - 裸控制字符（字面换行/制表）—— JSON 不允许，转成 \n / \t / \u00XX；
//   - 未转义的引号 —— 只有它**确实是字符串结束**时才当结束：看它后面第一个非空白
//     字节是 , } ] :（或输入结束）才算结束，否则是值里的字面引号，补成 \"；
//   - 非法转义序列（如 Windows 路径 C:\Users）—— 把反斜杠自身转义。
//
// 修复只发生在字符串内部；对象结构（键、括号、逗号）一字不动。
func firstBalancedJSONObject(raw string, repair bool) (string, bool) {
	start := strings.IndexByte(raw, '{')
	if start < 0 {
		return "", false
	}
	var out strings.Builder
	if !repair {
		out.Grow(len(raw) - start)
	}
	depth := 0
	inString := false
	escaped := false
	for i := start; i < len(raw); i++ {
		c := raw[i]
		if inString {
			switch {
			case escaped:
				out.WriteByte(c)
				escaped = false
			case c == '\\':
				if !repair || validJSONEscapeAt(raw, i) {
					out.WriteByte(c)
					escaped = true
				} else {
					out.WriteString(`\\`)
				}
			case c == '"':
				if !repair || jsonStringEndsAt(raw, i) {
					out.WriteByte(c)
					inString = false
				} else {
					out.WriteString(`\"`)
				}
			case c < 0x20:
				if repair {
					writeEscapedControl(&out, c)
				} else {
					out.WriteByte(c)
				}
			default:
				out.WriteByte(c)
			}
			continue
		}
		switch c {
		case '"':
			inString = true
			out.WriteByte(c)
		case '{':
			depth++
			out.WriteByte(c)
		case '}':
			depth--
			out.WriteByte(c)
			if depth == 0 {
				return out.String(), true
			}
		default:
			out.WriteByte(c)
		}
	}
	return "", false
}

// jsonStringEndsAt 判断 raw[i] 处的引号是不是字符串的结束引号：只有紧随其后的
// 第一个非空白字节属于 JSON 结构符（, } ] :）或输入已结束，它才可能是结束。
func jsonStringEndsAt(raw string, quote int) bool {
	for i := quote + 1; i < len(raw); i++ {
		switch raw[i] {
		case ' ', '\t', '\r', '\n':
			continue
		case ',', '}', ']', ':':
			return true
		default:
			return false
		}
	}
	return true
}

// validJSONEscapeAt 判断 raw[i] == '\\' 是否开启一个合法 JSON 转义序列。
func validJSONEscapeAt(raw string, i int) bool {
	if i+1 >= len(raw) {
		return false
	}
	return strings.IndexByte(`"\/bfnrtu`, raw[i+1]) >= 0
}

// writeEscapedControl 把 JSON 不允许的裸控制字节写成转义形式。
func writeEscapedControl(out *strings.Builder, c byte) {
	switch c {
	case '\n':
		out.WriteString(`\n`)
	case '\r':
		out.WriteString(`\r`)
	case '\t':
		out.WriteString(`\t`)
	case '\b':
		out.WriteString(`\b`)
	case '\f':
		out.WriteString(`\f`)
	default:
		fmt.Fprintf(out, `\u%04x`, c)
	}
}
