package model

import (
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"
)

// WireToolResultRefPrefix 是引擎 ToolResultProcessor 与模型自造引用共用的
// wire 前缀：wire 引用形如 "result:<callID>"，而记录侧归档引用是 "tr-<digest>"。
// 两者的唯一判据在 WireToolResultRef / WireToolResultCallID，禁止各处再写字面量。
const WireToolResultRefPrefix = "result:"

// WireToolResultRef 由工具调用 ID 构造 wire 结果引用。
func WireToolResultRef(callID string) string {
	return WireToolResultRefPrefix + callID
}

// WireToolResultCallID 解析 wire 结果引用；非 wire 形状或空调用 ID 一律返回 false，
// 调用方据此原样保留引用，不得把「解析不出来」降级成空串。
func WireToolResultCallID(ref string) (string, bool) {
	if !strings.HasPrefix(ref, WireToolResultRefPrefix) {
		return "", false
	}
	callID := strings.TrimPrefix(ref, WireToolResultRefPrefix)
	if callID == "" {
		return "", false
	}
	return callID, true
}

// NodeResultRef parses a `node:<nodeID>:...` subagent result reference.
func NodeResultRef(ref string) (string, bool) {
	const prefix = "node:"
	if !strings.HasPrefix(ref, prefix) {
		return "", false
	}
	rest := strings.TrimPrefix(ref, prefix)
	sep := strings.IndexByte(rest, ':')
	if sep <= 0 {
		return "", false
	}
	return rest[:sep], true
}

// BuildPage applies contains filtering, byte pagination and UTF-8 boundary
// alignment to one archived tool result.
func BuildPage(result StoredToolResult, offset, limit int, contains string) ToolResultPage {
	content := result.Content
	if contains = strings.TrimSpace(contains); contains != "" {
		lines := strings.Split(content, "\n")
		matched := lines[:0]
		for _, line := range lines {
			if strings.Contains(line, contains) {
				matched = append(matched, line)
			}
		}
		content = strings.Join(matched, "\n")
	}
	if offset > len(content) {
		return ToolResultPage{
			ResultRef: result.Ref, Tool: result.Tool, Digest: result.Digest,
			Offset: offset, TotalBytes: len(content), Content: "",
		}
	}
	end := offset + limit
	if end > len(content) {
		end = len(content)
	}
	offset = utf8Start(content, offset)
	end = utf8End(content, end)
	if end == offset && offset < len(content) {
		_, size := utf8.DecodeRuneInString(content[offset:])
		end = offset + size
	}
	return ToolResultPage{
		ResultRef: result.Ref, Tool: result.Tool, Digest: result.Digest,
		Offset: offset, NextOffset: end, TotalBytes: len(content),
		HasMore: end < len(content), Content: content[offset:end],
	}
}

// EncodePage keeps the model-facing read_tool_result JSON shape.
func EncodePage(result StoredToolResult, offset, limit int, contains string) (string, error) {
	page := BuildPage(result, offset, limit, contains)
	if page.Offset > page.TotalBytes {
		return "", errors.New("read_tool_result: offset exceeds filtered content")
	}
	encoded, err := json.Marshal(page)
	return string(encoded), err
}

func utf8Start(value string, index int) int {
	for index > 0 && index < len(value) && !utf8.RuneStart(value[index]) {
		index--
	}
	return index
}

func utf8End(value string, index int) int {
	for index < len(value) && index > 0 && !utf8.RuneStart(value[index]) {
		index--
	}
	return index
}
