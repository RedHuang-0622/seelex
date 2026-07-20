package application

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeEngine struct {
	mu      sync.Mutex
	history []EngineMessage
	chunks  []string
	prompt  string
	chatErr error
	cleared bool
}

func (engine *fakeEngine) ChatStream(ctx context.Context, input string, onChunk func(string)) (string, error) {
	for _, chunk := range engine.chunks {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
			onChunk(chunk)
		}
	}
	engine.mu.Lock()
	engine.history = []EngineMessage{{Role: "user", Content: input}, {Role: "assistant", Content: "answer"}}
	err := engine.chatErr
	engine.mu.Unlock()
	return "answer", err
}
func (engine *fakeEngine) History() []EngineMessage {
	engine.mu.Lock()
	defer engine.mu.Unlock()
	return append([]EngineMessage(nil), engine.history...)
}
func (engine *fakeEngine) ClearHistory() {
	engine.mu.Lock()
	engine.history = nil
	engine.cleared = true
	engine.mu.Unlock()
}
func (*fakeEngine) SessionID() string { return "session-1" }
func (engine *fakeEngine) SetSystemPrompt(prompt string) {
	engine.mu.Lock()
	engine.prompt = prompt
	engine.mu.Unlock()
}
func (*fakeEngine) SetMaxLoops(int)    {}
func (*fakeEngine) TraceText() string  { return "trace" }
func (*fakeEngine) TokenCount() string { return "12" }

type fakeRuntime struct{ account string }

func (*fakeRuntime) Model() string    { return "test-model" }
func (*fakeRuntime) Provider() string { return "test-provider" }
func (*fakeRuntime) Accounts() []AccountInfo {
	return []AccountInfo{{Name: "primary", Provider: "test", Model: "m"}}
}
func (runtime *fakeRuntime) SelectAccount(name string) bool {
	if name != "primary" {
		return false
	}
	runtime.account = name
	return true
}
func (*fakeRuntime) VisibleTools(context.Context) []Tool {
	return []Tool{{Name: "read", Description: "read files"}}
}
func (*fakeRuntime) ActivePlugin() string { return "default" }

type fakePlugins struct{ current PluginInfo }

func (*fakePlugins) All() []PluginInfo {
	return []PluginInfo{{Name: "default", Description: "default"}, {Name: "code", Description: "coding", Prompt: "code prompt"}}
}
func (plugins *fakePlugins) Activate(_ context.Context, name string) error {
	if name != "code" && name != "default" {
		return errors.New("missing plugin")
	}
	plugins.current = PluginInfo{Name: name, Prompt: name + " prompt"}
	return nil
}
func (plugins *fakePlugins) Deactivate(context.Context) error {
	plugins.current = PluginInfo{}
	return nil
}
func (plugins *fakePlugins) Current() (PluginInfo, bool) {
	return plugins.current, plugins.current.Name != ""
}

type fakeSkills struct{}

func (fakeSkills) All() []SkillInfo {
	return []SkillInfo{{Name: "review", Description: "review code", Prompt: "review prompt"}}
}
func (fakeSkills) Get(name string) (SkillInfo, bool) {
	if name != "review" {
		return SkillInfo{}, false
	}
	return SkillInfo{Name: "review", Prompt: "review prompt"}, true
}

type fakeSessions struct{}

func (fakeSessions) SaveCurrent(string) error { return nil }
func (fakeSessions) Resume(string) error      { return errors.New("resume unsupported") }
func (fakeSessions) List() []SessionInfo {
	return []SessionInfo{{ID: "saved", UpdatedAt: time.Unix(1, 0), TokenCount: 4}}
}
func (fakeSessions) LoadHistory(string) ([]EngineMessage, error) {
	return []EngineMessage{{Role: "assistant", Content: "saved answer"}}, nil
}
func (fakeSessions) LoadHistoryRange(string, int, int) ([]EngineMessage, int, error) {
	return []EngineMessage{{Role: "assistant", Content: "saved answer"}}, 1, nil
}
func (fakeSessions) MessageCount(string) (int, error) { return 1, nil }

func newTestService(engine *fakeEngine) *Service {
	return New(Dependencies{Engine: engine, Runtime: &fakeRuntime{}, Plugins: &fakePlugins{current: PluginInfo{Name: "default"}}, Skills: fakeSkills{}, Sessions: fakeSessions{}})
}

func TestEventHubOrdersAndResyncs(t *testing.T) {
	hub := NewEventHub()
	subscription := hub.Subscribe(1)
	defer subscription.Close()
	hub.Publish(EventMessageAdded, 1, "", nil)
	hub.Publish(EventMessageDelta, 2, "", nil)
	event := <-subscription.Events
	if event.Kind != EventResyncRequired || event.Seq != 2 {
		t.Fatalf("expected resync at seq 2, got %#v", event)
	}
}

func TestSuggestionsAndSkillRouting(t *testing.T) {
	engine := &fakeEngine{}
	service := newTestService(engine)
	defer service.Shutdown()
	suggestions := service.Suggestions("/R")
	if len(suggestions) != 3 || suggestions[0].Kind != "command" || suggestions[1].Kind != "tool" || suggestions[2].Kind != "skill" {
		t.Fatalf("unexpected suggestions: %#v", suggestions)
	}
	if err := service.Submit(context.Background(), "/review strict"); err != nil {
		t.Fatal(err)
	}
	engine.mu.Lock()
	prompt := engine.prompt
	engine.mu.Unlock()
	if !strings.Contains(prompt, "review prompt") || !strings.Contains(prompt, "strict") {
		t.Fatalf("prompt missing skill content: %q", prompt)
	}
	if !strings.Contains(prompt, "Seelex") {
		t.Fatalf("prompt missing identity: %q", prompt)
	}
	if !strings.Contains(prompt, "high-effort") {
		t.Fatalf("prompt missing effort: %q", prompt)
	}
	if err := service.Submit(context.Background(), "#review focused"); err != nil {
		t.Fatal(err)
	}
	engine.mu.Lock()
	prompt = engine.prompt
	engine.mu.Unlock()
	if !strings.Contains(prompt, "review prompt") || !strings.Contains(prompt, "focused") {
		t.Fatalf("hash skill prompt missing content: %q", prompt)
	}
}

func TestChatPublishesSnapshotWithoutUI(t *testing.T) {
	engine := &fakeEngine{chunks: []string{"an", "swer"}}
	service := newTestService(engine)
	defer service.Shutdown()
	if err := service.Submit(context.Background(), "hello"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		snapshot := service.Snapshot()
		if !snapshot.Chat.Running {
			if len(snapshot.Conversation) < 3 || snapshot.Conversation[len(snapshot.Conversation)-1].Content != "answer" {
				t.Fatalf("unexpected conversation: %#v", snapshot.Conversation)
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("chat did not complete")
}

func TestToolEventsUpdateSnapshot(t *testing.T) {
	service := newTestService(&fakeEngine{})
	defer service.Shutdown()
	service.handleToolStart("read", "read-1", `{"path":"a"}`)
	service.handleToolComplete("read", "read-1", "ok", nil, time.Second)
	snapshot := service.Snapshot()
	found := false
	for _, message := range snapshot.Conversation {
		if message.Tool != nil && message.Tool.ID == "read-1" && message.Tool.Status == "success" {
			found = true
		}
	}
	if !found {
		t.Fatalf("completed tool call not found: %#v", snapshot.Conversation)
	}
}

func TestApprovalBrokerResolve(t *testing.T) {
	hub := NewEventHub()
	broker := NewApprovalBroker(hub)
	result := make(chan ApprovalDecision, 1)
	go func() {
		decision, err := broker.Request(context.Background(), ApprovalRequest{ID: "approval-1", Question: "continue?", Options: []InteractionOption{{ID: "yes", Label: "Yes"}}})
		if err == nil {
			result <- decision
		}
	}()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		broker.mu.Lock()
		_, pending := broker.pending["approval-1"]
		broker.mu.Unlock()
		if pending {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if err := broker.Resolve("approval-1", ApprovalDecision{OptionID: "yes"}); err != nil {
		t.Fatal(err)
	}
	select {
	case decision := <-result:
		if decision.OptionID != "yes" {
			t.Fatalf("unexpected decision %#v", decision)
		}
	case <-time.After(time.Second):
		t.Fatal("approval did not resolve")
	}
}

func TestResumeCommandReportsUnavailableCapability(t *testing.T) {
	service := newTestService(&fakeEngine{})
	defer service.Shutdown()

	if err := service.Submit(context.Background(), "/resume"); err != nil {
		t.Fatal(err)
	}
	snapshot := service.Snapshot()
	if snapshot.Interaction != nil {
		t.Fatalf("resume should not open an interaction when unsupported: %#v", snapshot.Interaction)
	}
	last := snapshot.Conversation[len(snapshot.Conversation)-1]
	if !strings.Contains(last.Content, "会话恢复暂不可用") {
		t.Fatalf("unexpected resume notice %q", last.Content)
	}
}
