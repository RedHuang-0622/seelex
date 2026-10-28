package integration

import (
	"context"
	"strings"
	"testing"

	assembly "github.com/RedHuang-0622/seelex/application/assembly"
)

func TestAppendDeltaDoesNotExposeThoughtContent(t *testing.T) {
	service := newTestService(t, &fakeEngine{chunks: []string{"answer<think>private reasoning</think> done"}})
	components := assembly.ComponentsFor(service)
	if err := components.Conversation.Submission.Submit(context.Background(), "show the answer"); err != nil {
		t.Fatal(err)
	}
	if err := components.Runtime.Lifecycle.WaitForIdle(context.Background()); err != nil {
		t.Fatal(err)
	}
	snapshot := components.Session.Snapshot.Snapshot()
	content := snapshot.Conversation[len(snapshot.Conversation)-1].Content
	if content != "answer done" || strings.Contains(content, "think") || strings.Contains(content, "private") {
		t.Fatalf("visible content = %q", content)
	}
}
