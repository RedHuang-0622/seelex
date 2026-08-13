package session

import (
	"context"
	"sync"
	"testing"

	frameworksession "github.com/RedHuang-0622/Seele/session"
	"github.com/RedHuang-0622/Seele/types"
)

// stubCompleter 是最小 ChatCompleter：组件测试不发起真实模型调用。
type stubCompleter struct{}

func (stubCompleter) Complete(context.Context, []types.Message, []types.Tool) (types.Message, error) {
	return types.Message{Role: "assistant"}, nil
}

func (stubCompleter) CompleteStream(context.Context, []types.Message, []types.Tool, func(string)) (string, string, []types.ToolCall, error) {
	return "", "", nil, nil
}

func (stubCompleter) CompleteStreamEvents(context.Context, []types.Message, []types.Tool, func(types.StreamEvent)) (string, string, []types.ToolCall, error) {
	return "", "", nil, nil
}

// stubSessionAgent 实现 session.Agent（ToolRuntime + LLM()）。
type stubSessionAgent struct {
	llm types.ChatCompleter
}

func (a stubSessionAgent) VisibleTools(context.Context) []types.Tool { return nil }
func (a stubSessionAgent) Dispatch(context.Context, string, string) (string, error) {
	return "", nil
}
func (a stubSessionAgent) LLM() types.ChatCompleter { return a.llm }

func newTestSubagentSession(t *testing.T, sessionID string) *frameworksession.Session {
	t.Helper()
	sess, err := frameworksession.NewSession(frameworksession.SessionComponents{
		Agent:     stubSessionAgent{llm: stubCompleter{}},
		SessionID: sessionID,
	})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	return sess
}

func TestSubagentSessionsRegisterConversationUnregister(t *testing.T) {
	store := NewSubagentSessions(nil)
	defer store.Close()

	sess := newTestSubagentSession(t, "s1")
	store.Register("n1", sess, "goal-one")

	msgs, ok := store.Conversation("n1")
	if !ok || msgs == nil {
		t.Fatalf("running conversation must be readable, ok=%v msgs=%v", ok, msgs)
	}
	snap, ok := store.ContextSnapshot("n1")
	if !ok || snap == nil {
		t.Fatalf("running context snapshot must be readable, ok=%v", ok)
	}
	if snap.Goal != "goal-one" {
		t.Fatalf("snapshot goal = %q, want goal-one", snap.Goal)
	}

	returned := store.Unregister("n1")
	if returned == nil {
		t.Fatal("unregister must return the end snapshot")
	}
	// 结束后：会话仍可读（留存快照），上下文快照保留。
	if _, ok := store.Conversation("n1"); !ok {
		t.Fatal("conversation must be readable after unregister (retained snapshot)")
	}
	if snap, ok := store.ContextSnapshot("n1"); !ok || snap == nil {
		t.Fatalf("context snapshot must be retained after unregister, ok=%v", ok)
	}
}

func TestSubagentSessionsUnregisterWithoutSessionIsNoop(t *testing.T) {
	store := NewSubagentSessions(nil)
	defer store.Close()

	if snap := store.Unregister("missing"); snap != nil {
		t.Fatalf("unregister without session must return nil, got %+v", snap)
	}
	if _, ok := store.Conversation("missing"); ok {
		t.Fatal("conversation for missing node must be not ok")
	}
}

func TestSubagentSessionsToolResultArchiverLazyAndReuse(t *testing.T) {
	store := NewSubagentSessions(nil)
	defer store.Close()

	arch := store.ToolResultArchiverFor("n1")
	if arch == nil {
		t.Fatal("archiver must be lazily created")
	}
	if again := store.ToolResultArchiverFor("n1"); again != arch {
		t.Fatal("archiver must be reused for the same node")
	}
	if other := store.ToolResultArchiverFor("n2"); other == arch {
		t.Fatal("different nodes must get different archivers")
	}

	if _, err := arch.Store(context.Background(), "call-1", "bash", "raw-output"); err != nil {
		t.Fatalf("Store: %v", err)
	}
	raw, ok := store.ToolResult("n1", "node:n1:call-1")
	if !ok || raw != "raw-output" {
		t.Fatalf("ToolResult = %q, %v; want raw-output, true", raw, ok)
	}
	if _, ok := store.ToolResult("n1", "node:n1:missing"); ok {
		t.Fatal("missing ref must not be readable")
	}
	if _, ok := store.ToolResult("other", "node:n1:call-1"); ok {
		t.Fatal("other node must not read this archiver")
	}
}

func TestSubagentSessionsConcurrentRace(t *testing.T) {
	store := NewSubagentSessions(nil)
	defer store.Close()

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			sess := newTestSubagentSession(t, id)
			store.Register(id, sess, "goal-"+id)
			_, _ = store.Conversation(id)
			_, _ = store.ContextSnapshot(id)
			_ = store.ToolResultArchiverFor(id)
			_, _ = store.ToolResult(id, "node:"+id+":x")
			store.Unregister(id)
		}(string(rune('a'+i%26)) + "-node")
	}
	wg.Wait()
}
