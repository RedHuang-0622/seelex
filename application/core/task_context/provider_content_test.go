package task_context

// 工具结果事件的「视图呈现 / provider wire 原文」分离（研究文档 §8 发现 2）：
// 记录侧 Content 供视图/轨迹读（应用分类呈现文本），ProviderContent 保存
// **已发出的字节**（框架 wire 形状），装配层（TranscriptTailHistory）投影时取
// 后者。两者混用会让下一轮重投影改写该消息、provider 前缀缓存自该点起失效。
//
// 运行：go test ./application/core/task_context -run ProviderContent -v -count=1

import (
	"errors"
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/application/model"
)

func providerContentCoordinator() *Coordinator {
	return &Coordinator{
		presentToolError: presentToolErrorForTest,
		isOversized:      func(content string, limit int) bool { return len(content) > limit },
		oversizedWarning: oversizedWarningForTest,
		tokenCounter:     NewCalibratedTokenCounter(),
	}
}

// presentToolErrorForTest 复刻生产呈现形状（模块/方法/摘要/下一步），只用于
// 断言「视图读呈现、wire 读原文」两侧不同。
func presentToolErrorForTest(toolName string, err error) string {
	return "【模块：工具执行｜方法：handleToolComplete(" + toolName + ")】\n" + err.Error() + "\n下一步：检查后重试。"
}

func oversizedWarningForTest(name, resultRef string) string {
	return "<seelex-tool-result-omitted>\ntool=" + name + "\nresult_ref=" + resultRef + "\n</seelex-tool-result-omitted>"
}

// TestProviderContentOnToolError 钉住失败工具的两侧正文：Content 是分类呈现
// 文本（视图），ProviderContent 是框架 wire 上原样发出的错误 JSON。
func TestProviderContentOnToolError(t *testing.T) {
	coordinator := providerContentCoordinator()
	toolErr := errors.New("project scope: no project is bound to this session")

	visible, _ := coordinator.RecordToolTranscriptLocked("session-1", "read_file", "call-1", `{"path":"x"}`, "", toolErr)

	event := transcriptToolEvent(t, coordinator, "session-1")
	if event.Content != visible || !strings.Contains(event.Content, "【模块：工具执行") {
		t.Fatalf("view content = %q (returned %q), want 分类呈现文本", event.Content, visible)
	}
	wantWire := `{"error": "project scope: no project is bound to this session"}`
	if event.ProviderContent != wantWire {
		t.Fatalf("provider content = %q, want wire 原文 %q", event.ProviderContent, wantWire)
	}
	if projected := providerContentForEvent(event); projected != wantWire {
		t.Fatalf("TranscriptTailHistory projection = %q, want wire 原文 %q", projected, wantWire)
	}
}

// TestProviderContentOnOversizedToolResult 钉住超限结果的两侧引用：视图呈现带
// 归档引用（tr-<digest>），wire 原文带处理器引用（result:<callID>）。
func TestProviderContentOnOversizedToolResult(t *testing.T) {
	coordinator := providerContentCoordinator()
	oversized := strings.Repeat("x", defaultToolResultLimit()+1)

	visible, resultRef := coordinator.RecordToolTranscriptLocked("session-2", "read_file", "call-2", `{"path":"x"}`, oversized, nil)

	event := transcriptToolEvent(t, coordinator, "session-2")
	if resultRef == "" || !strings.Contains(event.Content, "result_ref="+resultRef) {
		t.Fatalf("view warning = %q (ref %q), want 归档引用", event.Content, resultRef)
	}
	if !strings.Contains(event.ProviderContent, "result_ref=result:call-2") {
		t.Fatalf("provider content = %q, want wire 引用 result:call-2", event.ProviderContent)
	}
	if event.ProviderContent == event.Content {
		t.Fatalf("两类引用不应相同: %q", event.ProviderContent)
	}
	if !strings.Contains(visible, "省略") && !strings.Contains(visible, "seelex-tool-result-omitted") {
		t.Fatalf("visible warning = %q", visible)
	}
}

// TestProviderContentAbsentOnSuccessfulToolResult 钉住常规路径不分叉：成功的
// 工具结果 Content 就是 wire 字节，ProviderContent 留空（兜底取 Content）。
func TestProviderContentAbsentOnSuccessfulToolResult(t *testing.T) {
	coordinator := providerContentCoordinator()
	visible, _ := coordinator.RecordToolTranscriptLocked("session-3", "read_file", "call-3", `{"path":"x"}`, "file body\n", nil)

	event := transcriptToolEvent(t, coordinator, "session-3")
	if event.ProviderContent != "" {
		t.Fatalf("provider content = %q, want empty on non-divergent result", event.ProviderContent)
	}
	if event.Content != visible || event.Content != "file body\n" {
		t.Fatalf("content = %q (returned %q), want raw result", event.Content, visible)
	}
}

func transcriptToolEvent(t *testing.T, coordinator *Coordinator, sessionID string) model.TranscriptEvent {
	t.Helper()
	events := coordinator.TranscriptFor(sessionID)
	for index := len(events) - 1; index >= 0; index-- {
		if events[index].Role == "tool" {
			return events[index]
		}
	}
	t.Fatalf("no tool transcript event for session %q: %+v", sessionID, events)
	return model.TranscriptEvent{}
}
