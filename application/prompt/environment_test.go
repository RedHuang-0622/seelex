package prompt

import (
	"strings"
	"testing"

	protocol "github.com/RedHuang-0622/seelex/contract/protocol"
)

func TestRenderEnvironmentTraceBlockUsesStableOrderAndMarks(t *testing.T) {
	block := RenderEnvironmentTraceBlock("/workspace/app", "dev", "test", []protocol.WorkspaceEnvironmentInfo{
		{Name: "prod", Host: "prod.example.com", Port: 2222, User: "deploy"},
		{Name: "dev"},
		{Name: "test", Host: "test.example.com", Port: 22, User: "qa"},
	})
	if !strings.HasPrefix(block, EnvironmentTraceMarkerOpen) || !strings.HasSuffix(block, EnvironmentTraceMarkerClose) {
		t.Fatalf("missing markers: %q", block)
	}
	dev := strings.Index(block, "- dev ·")
	testIndex := strings.Index(block, "- test ·")
	prod := strings.Index(block, "- prod ·")
	if dev < 0 || testIndex < 0 || prod < 0 || !(dev < prod && prod < testIndex) {
		t.Fatalf("entries are not sorted: %q", block)
	}
	for _, want := range []string{"当前固定", "项目默认", "本会话不可切换", "subagent / teammate"} {
		if !strings.Contains(block, want) {
			t.Fatalf("missing %q: %q", want, block)
		}
	}
}
