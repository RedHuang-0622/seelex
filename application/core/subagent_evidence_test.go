package core

import (
	"context"
	"strings"
	"testing"
)

// 子代理 merge-back 结果链路（2026-09-08 改造）：工作树 git merge 完成后，
// 子代理结论经工具结果（tool_result / summary / NodeSemanticResult）回传父
// 代理；Runtime mailbox 只是有界暂存，Application 排空后整体丢弃——既不
// 注入引擎历史，也不写入可见会话/transcript/record（前端不表现 mailbox
// 内容）。真实结论与“继承上下文”信封都不例外。

func TestSubagentMailboxContentDoesNotReachFrontendOrStorage(t *testing.T) {
	engine := &fakeEngine{}
	runtime := &fakeRuntime{mailbox: []string{
		"子代理发现：模块 A 与 B 存在重叠依赖",
		"## 继承上下文 (Inherited Context)\n> 来源会话: sess-parent\n---\n以上为继承的上下文。",
	}}
	service := newTestService(t, engine, withTestRuntime(runtime))

	if err := service.Submit(context.Background(), "main task"); err != nil {
		t.Fatal(err)
	}
	if err := service.WaitForIdle(context.Background()); err != nil {
		t.Fatalf("wait idle: %v", err)
	}
	if pending := runtime.DrainSubagentContexts(); len(pending) != 0 {
		t.Fatalf("mailbox was not drained by application: %q", pending)
	}

	snapshot := service.Snapshot()
	for _, message := range snapshot.Conversation {
		if strings.HasPrefix(message.Content, "[子代理产出]") ||
			strings.Contains(message.Content, "重叠依赖") ||
			strings.Contains(message.Content, "以上为继承的上下文") {
			t.Fatalf("mailbox content leaked into visible conversation: %+v", message)
		}
	}
	for _, message := range engine.History() {
		if strings.Contains(message.Content, "重叠依赖") ||
			strings.Contains(message.Content, "以上为继承的上下文") {
			t.Fatalf("mailbox content leaked into engine history: %+v", message)
		}
	}
	for _, event := range service.components.tasks.TranscriptFor(snapshot.Session.ID) {
		if strings.Contains(event.Content, "重叠依赖") ||
			strings.Contains(event.Content, "以上为继承的上下文") {
			t.Fatalf("mailbox content leaked into transcript: %+v", event)
		}
	}
}
