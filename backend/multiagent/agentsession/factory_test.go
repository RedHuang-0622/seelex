package agentsession

import (
	"context"
	"strings"
	"sync"
	"testing"

	frameworksession "github.com/RedHuang-0622/Seele/session"
	"github.com/RedHuang-0622/Seele/types"
	"github.com/RedHuang-0622/seelex/store"
)

type testCompleter struct{}

func (testCompleter) Complete(context.Context, []types.Message, []types.Tool) (types.Message, error) {
	content := "ok"
	return types.Message{Role: "assistant", Content: &content}, nil
}

func (testCompleter) CompleteStream(context.Context, []types.Message, []types.Tool, func(string)) (string, string, []types.ToolCall, error) {
	return "ok", "", nil, nil
}

func (testCompleter) CompleteStreamEvents(context.Context, []types.Message, []types.Tool, func(types.StreamEvent)) (string, string, []types.ToolCall, error) {
	return "ok", "", nil, nil
}

type testAgent struct{}

func (testAgent) VisibleTools(context.Context) []types.Tool { return nil }
func (testAgent) Dispatch(context.Context, string, string) (string, error) {
	return "", nil
}
func (testAgent) LLM() types.ChatCompleter { return testCompleter{} }

func testDeps() Deps {
	return Deps{
		Components: func() frameworksession.SessionComponents {
			return frameworksession.SessionComponents{Agent: testAgent{}}
		},
		ModelName: func() string { return "test-model" },
	}
}

func TestFactoryRequiresExplicitSessionID(t *testing.T) {
	factory := New(testDeps())
	if _, err := factory.NewWithID("  ", nil); err == nil || !strings.Contains(err.Error(), "session ID") {
		t.Fatalf("empty session ID must fail: %v", err)
	}
	var nilFactory *Factory
	if _, err := nilFactory.NewWithID("session", nil); err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Fatalf("nil factory must fail explicitly: %v", err)
	}
}

func TestFactoryBuildsFrameworkSessionWithRequestedIdentity(t *testing.T) {
	factory := New(testDeps())
	sess, err := factory.NewWithID("work-item-session", nil)
	if err != nil {
		t.Fatalf("NewWithID: %v", err)
	}
	if sess.SessionID() != "work-item-session" {
		t.Fatalf("SessionID = %q", sess.SessionID())
	}
}

type captureCompleter struct {
	mu       sync.Mutex
	messages []types.Message
}

func (c *captureCompleter) capture(messages []types.Message) {
	c.mu.Lock()
	c.messages = append([]types.Message(nil), messages...)
	c.mu.Unlock()
}

func (c *captureCompleter) Complete(_ context.Context, messages []types.Message, _ []types.Tool) (types.Message, error) {
	c.capture(messages)
	content := "resumed"
	return types.Message{Role: "assistant", Content: &content}, nil
}

func (c *captureCompleter) CompleteStream(_ context.Context, messages []types.Message, _ []types.Tool, _ func(string)) (string, string, []types.ToolCall, error) {
	c.capture(messages)
	return "resumed", "", nil, nil
}

func (c *captureCompleter) CompleteStreamEvents(_ context.Context, messages []types.Message, _ []types.Tool, _ func(types.StreamEvent)) (string, string, []types.ToolCall, error) {
	c.capture(messages)
	return "resumed", "", nil, nil
}

type captureAgent struct{ completer *captureCompleter }

func (a captureAgent) VisibleTools(context.Context) []types.Tool { return nil }
func (a captureAgent) Dispatch(context.Context, string, string) (string, error) {
	return "", nil
}
func (a captureAgent) LLM() types.ChatCompleter { return a.completer }

func TestFactorySeedsColdSessionWithInterruptedSnapshot(t *testing.T) {
	router, err := store.NewRouter(t.TempDir())
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	defer router.Close()
	completer := &captureCompleter{}
	old := "中断前已经完成的需求"
	newInput := "从中断处继续"
	factory := New(Deps{
		Components: func() frameworksession.SessionComponents {
			return frameworksession.SessionComponents{Agent: captureAgent{completer: completer}}
		},
		NewHistory: func(sessionID string) PreparedHistory {
			return store.NewDurableHistory(router, sessionID)
		},
		InitialHistory: func(string) []types.Message {
			return []types.Message{{Role: "user", Content: &old}}
		},
	})
	sess, err := factory.NewWithID("work-item-resume", nil)
	if err != nil {
		t.Fatalf("NewWithID: %v", err)
	}
	if _, err := sess.ChatStream(context.Background(), newInput, nil); err != nil {
		t.Fatalf("ChatStream: %v", err)
	}
	completer.mu.Lock()
	seen := append([]types.Message(nil), completer.messages...)
	completer.mu.Unlock()
	var texts []string
	for _, message := range seen {
		if message.Content != nil {
			texts = append(texts, *message.Content)
		}
	}
	joined := strings.Join(texts, "\n")
	if !strings.Contains(joined, old) || !strings.Contains(joined, newInput) {
		t.Fatalf("冷恢复必须把中断前快照和本轮输入一起交给模型: %q", joined)
	}
}
