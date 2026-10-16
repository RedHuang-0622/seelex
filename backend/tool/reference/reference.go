// Package reference 是"工具结果引用与分页"的判据落点：ref 前缀解析（节点引用）、
// contains 过滤 → offset/limit 分页 → UTF-8 边界对齐，以及模型面 JSON 编码。
//
// 落点沿革：2026-10-16 随 M5-R-tool-reference **接收者转移**从
// `application/core/reference_tools.go` 归位（plan §2.6 的 `reference_tools.go` → `backend/tool/reference/`）。
// 门面（`application/core`）仍持有 handler 接线与"从哪读"的编排：
// pending（内存态）→ 会话落盘存储 → 引擎桥的节点归档；本包只管**解析与分页判据**。
//
// 一条口径：GUI 的 `ToolResultContent` 与模型面 `read_tool_result` 共用 `BuildPage`，
// 保证两条通道读到同一份归档、得到同一页边界。
package reference

import (
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/RedHuang-0622/seelex/middle/protocol/model"
)

// NodeResultRef 解析 `node:<nodeID>:...` 前缀的子代理结果引用；非节点引用 → false。
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

// BuildPage 计算工具结果的一页（contains 过滤 → offset/limit 分页 → UTF-8 边界对齐）。
// GUI 的 ToolResultContent 与模型面 read_tool_result 共用同一页结构。
func BuildPage(result model.StoredToolResult, offset, limit int, contains string) model.ToolResultPage {
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
		return model.ToolResultPage{ResultRef: result.Ref, Tool: result.Tool, Digest: result.Digest,
			Offset: offset, TotalBytes: len(content), Content: ""}
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
	return model.ToolResultPage{
		ResultRef: result.Ref, Tool: result.Tool, Digest: result.Digest,
		Offset: offset, NextOffset: end, TotalBytes: len(content),
		HasMore: end < len(content), Content: content[offset:end],
	}
}

// EncodePage 是模型面 read_tool_result 的 JSON 编码（保留既有载荷形状；
// 由 BuildPage 计算）。
func EncodePage(result model.StoredToolResult, offset, limit int, contains string) (string, error) {
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
