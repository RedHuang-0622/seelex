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

// TestReadToolResultResolvesCallAlias 验证 result:call_<callID> 别名映射：
// 工具结果过大被省略归档后，模型自造 result:call_... 引用（而非占位中
// 的 tr- ref）仍能经 resultRefsByToolCallID 映射回真实 ref 读回。
func TestReadToolResultResolvesCallAlias(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	raw := strings.Repeat("x", 4000) + "alias-target-content"
	stored := service.components.tasks.storeToolResultLocked("fork_subagents", raw)
	service.resultRefsByToolCallID["call_00_Ohu6emGEZr0gV4m2JU4t0987"] = stored.Ref

	page, err := service.ReadToolResultHandler(t.Context(),
		`{"result_ref":"result:call_00_Ohu6emGEZr0gV4m2JU4t0987","offset":4000,"limit":64}`)
	if err != nil {
		t.Fatalf("read alias ref: %v", err)
	}
	if !strings.Contains(page, `"result_ref":"`+stored.Ref+`"`) {
		t.Fatalf("page must echo the real tr- ref, got: %s", page)
	}
	if !strings.Contains(page, "alias-target-content") {
		t.Fatalf("page must contain archived content, got: %s", page)
	}

	// 未知 callID：别名保留原值 → 仍返回明确错误（不静默）。
	if _, err := service.ReadToolResultHandler(t.Context(),
		`{"result_ref":"result:call_ghost","limit":32}`); err == nil {
		t.Fatal("unknown call alias must error")
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
