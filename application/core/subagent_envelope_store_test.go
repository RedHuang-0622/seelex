package core

import (
	"context"
	"strings"
	"testing"
)

// inheritedContextEnvelopeFixture 模拟 AgentNode.mergeBack 实际入队的载荷
// （seelexctx/snapshot.Format 产物）：仅含“来源会话/导出时间/消息数/目标”
// 装配信息与续作提示，是内部上下文信封，不是子代理独立结论。
const inheritedContextEnvelopeFixture = "## 继承上下文 (Inherited Context)\n" +
	"> 来源会话: sess-parent | 导出时间: 14:24:25 | 消息数: 20\n\n" +
	"### 目标 (Goal)\n" +
	"两个子代理：一个不调用工具写人生的意义，一个调用工具读项目 README。\n\n" +
	"---\n" +
	"以上为继承的上下文。请基于这些信息继续工作，在决策时引用上述目标和约束。\n"

// TestInheritedContextEnvelopeNotEnteringContextStores 复现：子代理 merge-back
// 队列里只有“继承上下文”信封时，收尾排空不得把它写进引擎历史、可见会话或
// transcript（运行实例实测：信封以 [子代理产出] 用户消息落在最终答复之后，
// 并进入 record/transcript/provider history）。
func TestInheritedContextEnvelopeNotEnteringContextStores(t *testing.T) {
	engine := &fakeEngine{}
	runtime := &fakeRuntime{mailbox: []string{inheritedContextEnvelopeFixture}}
	service := newTestService(t, engine, withTestRuntime(runtime))

	if err := service.Submit(context.Background(), "main task"); err != nil {
		t.Fatal(err)
	}
	if err := service.WaitForIdle(context.Background()); err != nil {
		t.Fatalf("wait idle: %v", err)
	}

	snapshot := service.Snapshot()
	for _, message := range snapshot.Conversation {
		if strings.Contains(message.Content, "以上为继承的上下文") {
			t.Fatalf("inherited-context envelope leaked into visible conversation: %+v", message)
		}
	}
	for _, message := range engine.History() {
		if strings.Contains(message.Content, "以上为继承的上下文") {
			t.Fatalf("inherited-context envelope leaked into engine history: %+v", message)
		}
	}
	for _, event := range service.components.tasks.TranscriptFor(snapshot.Session.ID) {
		if strings.Contains(event.Content, "以上为继承的上下文") {
			t.Fatalf("inherited-context envelope leaked into transcript: %+v", event)
		}
	}
}
