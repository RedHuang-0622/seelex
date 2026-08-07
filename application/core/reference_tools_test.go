package core

import (
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/application/model"
)

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
		nodeID, ok := nodeResultRef(tc.ref)
		if nodeID != tc.nodeID || ok != tc.ok {
			t.Fatalf("nodeResultRef(%q) = (%q,%v), want (%q,%v)", tc.ref, nodeID, ok, tc.nodeID, tc.ok)
		}
	}
}

// TestReadToolResultResolvesNodeRef 验证 P1 桥：read_tool_result 对
// node:<nodeID>: 前缀的 ref 经引擎桥读回节点归档内容（子代理 ref 可读）。
func TestReadToolResultResolvesNodeRef(t *testing.T) {
	engine := &fakeEngine{}
	engine.nodeToolResultFn = func(nodeID, ref string) (string, bool) {
		if nodeID == "agent-1" && ref == "node:agent-1:result:call-1" {
			return "子代理工具输出内容-0123456789", true
		}
		return "", false
	}
	service := newTestService(t, engine)

	page, err := service.ReadToolResultHandler(t.Context(), `{"result_ref":"node:agent-1:result:call-1","offset":0,"limit":64}`)
	if err != nil {
		t.Fatalf("read node ref: %v", err)
	}
	if !strings.Contains(page, "子代理工具输出内容") {
		t.Fatalf("page must contain node tool content, got: %s", page)
	}
	if !strings.Contains(page, `"result_ref":"node:agent-1:result:call-1"`) {
		t.Fatalf("page must echo node ref, got: %s", page)
	}

	// 未知节点 ref → 显式错误（不静默空结果）。
	if _, err := service.ReadToolResultHandler(t.Context(), `{"result_ref":"node:ghost:result:x"}`); err == nil {
		t.Fatal("unknown node ref must error")
	}
}

func TestEncodeToolResultPageNodeContent(t *testing.T) {
	result := StoredToolResult{
		ToolResultRef: model.ToolResultRef{Ref: "node:a:result:b", Tool: "read_file"},
		Content:       strings.Repeat("x", 100),
	}
	page, err := encodeToolResultPage(result, 0, 32, "")
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	for _, want := range []string{`"has_more":true`, `"total_bytes":100`, `"next_offset":32`} {
		if !strings.Contains(page, want) {
			t.Fatalf("page must contain %q, got: %s", want, page)
		}
	}
}
