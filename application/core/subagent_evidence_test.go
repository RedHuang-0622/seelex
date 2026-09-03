package core

import (
	"context"
	"strings"
	"testing"
)

// evidenceRuntime 是带 merge-back 邮箱的 fakeRuntime：DrainSubagentContexts
// 返回预先入队的子代理证据文本（模拟节点执行结束后合并回父）。
type evidenceRuntime struct {
	*fakeRuntime
	contexts []string
}

func (runtime *evidenceRuntime) DrainSubagentContexts() []string {
	out := runtime.contexts
	runtime.contexts = nil
	return out
}

// TestSubagentMergeBackEvidenceVisibleAndDurable（G4/INV-G12）：子代理合并回
// 父的证据以一条可见记录进入会话视图，并写 transcript（持久化事实源），
// 不再被可见区丢弃。
func TestSubagentMergeBackEvidenceVisibleAndDurable(t *testing.T) {
	runtime := &evidenceRuntime{
		fakeRuntime: &fakeRuntime{},
		contexts:    []string{"子代理发现：模块 A 与 B 存在重叠依赖"},
	}
	service := newTestService(t, &fakeEngine{}, withTestRuntime(runtime))

	if err := service.Submit(context.Background(), "main task"); err != nil {
		t.Fatal(err)
	}
	if err := service.WaitForIdle(context.Background()); err != nil {
		t.Fatal(err)
	}
	snapshot := service.Snapshot()
	foundVisible := false
	for _, message := range snapshot.Conversation {
		if message.Role == "user" && strings.HasPrefix(message.Content, "[子代理产出] ") && strings.Contains(message.Content, "重叠依赖") {
			foundVisible = true
			break
		}
	}
	if !foundVisible {
		t.Fatalf("merge-back evidence missing from visible conversation: %+v", snapshot.Conversation)
	}

	sessionID := snapshot.Session.ID
	events := service.components.tasks.TranscriptFor(sessionID)
	foundDurable := false
	for _, event := range events {
		if strings.HasPrefix(event.Content, "[子代理产出] ") && strings.Contains(event.Content, "重叠依赖") {
			foundDurable = true
			break
		}
	}
	if !foundDurable {
		t.Fatalf("merge-back evidence missing from transcript: %+v", events)
	}
}
