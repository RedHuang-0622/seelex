package seelexctx

import (
	"context"
	"strings"
	"testing"

	"github.com/RedHuang-0622/Seele/seelectx"
)

func TestProcessorOversizedResultArchived(t *testing.T) {
	archiver := NewInMemoryToolResultArchiver()
	processor := NewToolResultProcessor(100, archiver)
	raw := strings.Repeat("oversized", 100) // 800 字符 > 100 预算
	view, err := processor.Process(context.Background(), seelectx.ToolResult{
		CallID: "call-1", Name: "bash", Arguments: "{}", Raw: raw,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(view.Content, ToolResultOmittedPrefix) {
		t.Fatalf("oversized result must be omitted, got prefix %q", view.Content[:30])
	}
	if !strings.Contains(view.Content, "result_ref=result:call-1") {
		t.Fatalf("omitted view must carry result_ref: %s", view.Content)
	}
	if !strings.Contains(view.Content, "read_tool_result") {
		t.Fatalf("omitted view must point to read_tool_result pagination: %s", view.Content)
	}
	stored, ok := archiver.Read("result:call-1")
	if !ok || stored != raw {
		t.Fatal("archived raw content must be readable by result_ref")
	}
}

func TestProcessorSmallResultPassthrough(t *testing.T) {
	processor := NewToolResultProcessor(100, nil)
	view, err := processor.Process(context.Background(), seelectx.ToolResult{
		CallID: "call-2", Name: "read_file", Raw: "小结果",
	})
	if err != nil {
		t.Fatal(err)
	}
	if view.Content != "小结果" {
		t.Fatalf("small result must pass through, got %q", view.Content)
	}
}

func TestProcessorErrorResultPassthrough(t *testing.T) {
	processor := NewToolResultProcessor(10, nil)
	view, err := processor.Process(context.Background(), seelectx.ToolResult{
		CallID: "call-3", Name: "bash", Raw: `{"error": "boom"}`,
		Err: errTestTool,
	})
	if err != nil {
		t.Fatal(err)
	}
	// 工具错误原样透传（错误保持可见，不省略）。
	if view.Content != `{"error": "boom"}` {
		t.Fatalf("error result must pass through, got %q", view.Content)
	}
}

var errTestTool = &windowPolicyError{"test tool error"}

func TestProcessorTruncatedMarkerOversized(t *testing.T) {
	processor := NewToolResultProcessor(10_000, nil)
	view, err := processor.Process(context.Background(), seelectx.ToolResult{
		CallID: "call-4", Name: "bash", Raw: "部分内容\n...[truncated]",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(view.Content, ToolResultOmittedPrefix) {
		t.Fatal("framework truncated marker must count as oversized")
	}
}

func TestProcessorArchiverIdempotentByCallID(t *testing.T) {
	archiver := NewInMemoryToolResultArchiver()
	processor := NewToolResultProcessor(5, archiver)
	raw := "很长很长的工具输出"
	if _, err := processor.Process(context.Background(), seelectx.ToolResult{
		CallID: "call-5", Name: "bash", Raw: raw,
	}); err != nil {
		t.Fatal(err)
	}
	// 同一调用 ID 重复归档（控制器硬阈值路径兜底）→ 幂等返回同一引用。
	ref, err := archiver.Store(context.Background(), "call-5", "bash", raw)
	if err != nil {
		t.Fatal(err)
	}
	if ref != "result:call-5" {
		t.Fatalf("duplicate archive ref = %q, want idempotent result:call-5", ref)
	}
}

func TestIsOversizedToolResult(t *testing.T) {
	if !IsOversizedToolResult(strings.Repeat("a", 101), 100) {
		t.Fatal("long content must be oversized")
	}
	if IsOversizedToolResult("abc", 100) {
		t.Fatal("short content must not be oversized")
	}
	if !IsOversizedToolResult("x\n...[truncated]", 10_000) {
		t.Fatal("framework truncated marker must be oversized")
	}
}

func TestProcessorDefaultLimitUsesSeelexDefault(t *testing.T) {
	// limit=0 → seelex 生效默认 DefaultToolResultLimit()（20000）。
	processor := NewToolResultProcessor(0, nil)
	view, err := processor.Process(context.Background(), seelectx.ToolResult{
		CallID: "call-6", Name: "bash", Raw: strings.Repeat("x", 25_000),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(view.Content, ToolResultOmittedPrefix) {
		t.Fatal("25000 chars must exceed the seelex default limit")
	}
	// 低于默认（如 10000）的结果原样透传——默认已从框架 4000 提高。
	view, err = processor.Process(context.Background(), seelectx.ToolResult{
		CallID: "call-7", Name: "read_file", Raw: strings.Repeat("x", 10_000),
	})
	if err != nil {
		t.Fatal(err)
	}
	if view.Content != strings.Repeat("x", 10_000) {
		t.Fatal("10000 chars must pass through under the seelex default limit")
	}
}
