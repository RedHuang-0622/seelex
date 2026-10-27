package reference

import (
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/contract/protocol"
)

// 本文件原在 `application/assembly/reference_tools_test.go`，随判据（M5-R-tool-reference）一起归位；
// 留在 core 的是 Service 级集成用例（引擎桥取节点归档、result:call_ 别名映射）。

func TestNodeResultRefParser(t *testing.T) {
	cases := []struct {
		ref    string
		nodeID string
		ok     bool
	}{
		{"node:agent-1:result:call-1", "agent-1", true},
		{"node:a:b:c", "a", true},
		{"result:call-1", "", false},
		{"node:", "", false},
		{"node::result:x", "", false},
		{"", "", false},
	}
	for _, tc := range cases {
		nodeID, ok := NodeResultRef(tc.ref)
		if nodeID != tc.nodeID || ok != tc.ok {
			t.Fatalf("NodeResultRef(%q) = (%q,%v), want (%q,%v)", tc.ref, nodeID, ok, tc.nodeID, tc.ok)
		}
	}
}

func TestEncodePageNodeContent(t *testing.T) {
	result := model.StoredToolResult{
		ToolResultRef: model.ToolResultRef{Ref: "node:a:result:b", Tool: "read_file"},
		Content:       strings.Repeat("x", 100),
	}
	page, err := EncodePage(result, 0, 32, "")
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	for _, want := range []string{`"has_more":true`, `"total_bytes":100`, `"next_offset":32`} {
		if !strings.Contains(page, want) {
			t.Fatalf("page must contain %q, got: %s", want, page)
		}
	}
}

// TestBuildPageAlignsUTF8Boundaries：分页边界必须落在 rune 起点（否则前端与模型会看到半个汉字）。
func TestBuildPageAlignsUTF8Boundaries(t *testing.T) {
	content := strings.Repeat("中", 10) // 每个 3 字节
	result := model.StoredToolResult{
		ToolResultRef: model.ToolResultRef{Ref: "tr-1", Tool: "read_file"},
		Content:       content,
	}
	page := BuildPage(result, 0, 4, "") // 4 落在第一个汉字的中间
	if page.Offset != 0 || page.NextOffset != 3 {
		t.Fatalf("边界应对齐到 rune 起点：offset=%d next=%d", page.Offset, page.NextOffset)
	}
	if page.Content != "中" {
		t.Fatalf("内容应是一个完整汉字，got %q", page.Content)
	}
	page = BuildPage(result, 28, 4, "")
	if page.Content != "中" || page.TotalBytes != len(content) {
		t.Fatalf("末页也应对齐：%+v", page)
	}
}

// TestBuildPageContainsFiltersLines：contains 过滤按行保留匹配行，分页在过滤后的内容上算。
func TestBuildPageContainsFiltersLines(t *testing.T) {
	result := model.StoredToolResult{
		ToolResultRef: model.ToolResultRef{Ref: "tr-2", Tool: "bash"},
		Content:       "alpha\nneedle one\nbeta\nneedle two",
	}
	page := BuildPage(result, 0, 1000, " needle ")
	if page.Content != "needle one\nneedle two" {
		t.Fatalf("过滤后内容 = %q", page.Content)
	}
	if page.TotalBytes != len("needle one\nneedle two") {
		t.Fatalf("total_bytes 应基于过滤后内容，got %d", page.TotalBytes)
	}
	// offset 超出过滤后内容：空内容 + 原样 offset（调用方据此报错）。
	page = BuildPage(result, 999, 10, "needle")
	if page.Content != "" || page.Offset != 999 {
		t.Fatalf("越界页应为空内容且保留 offset，got %+v", page)
	}
}

// TestEncodePageMakesProgressAcrossUTF8Boundaries：模型面一次只取 1 字节时，
// 必须给出一个完整 rune 并让 next_offset 前进（否则模型会卡在半个汉字上）。
// 原在 `application/assembly/session_archive_test.go`，随判据一起归位。
func TestEncodePageMakesProgressAcrossUTF8Boundaries(t *testing.T) {
	result := model.StoredToolResult{
		ToolResultRef: model.ToolResultRef{Ref: "tr-unicode", Tool: "read"},
		Content:       "中文",
	}
	page, err := EncodePage(result, 0, 1, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(page, `"content":"中"`) || !strings.Contains(page, `"next_offset":3`) {
		t.Fatalf("unicode page = %s", page)
	}
}
