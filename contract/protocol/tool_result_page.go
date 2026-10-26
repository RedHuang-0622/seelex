package model

import (
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"
)

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
