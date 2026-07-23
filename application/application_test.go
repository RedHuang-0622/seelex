package application

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RedHuang-0622/Seele/engine"
)

type fakeEngine struct {
	mu        sync.Mutex
	history   []EngineMessage
	chunks    []string
	prompt    string
	chatErr   error
	cleared   bool
	sessionID string
	lastInput string
	maxLoops  int
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
	engine.lastInput = input
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
func (engine *fakeEngine) SessionID() string {
	engine.mu.Lock()
	defer engine.mu.Unlock()
	if engine.sessionID == "" {
		return "session-1"
	}
	return engine.sessionID
}
func (engine *fakeEngine) StartSession() string {
	engine.mu.Lock()
	defer engine.mu.Unlock()
	engine.sessionID = "session-new"
	engine.history = nil
	engine.cleared = true
	return engine.sessionID
}
func (engine *fakeEngine) ReplaceHistory(sessionID string, history []EngineMessage) error {
	engine.mu.Lock()
	defer engine.mu.Unlock()
	engine.sessionID = sessionID
	engine.history = append([]EngineMessage(nil), history...)
	return nil
}
func (engine *fakeEngine) SetSystemPrompt(prompt string) {
	engine.mu.Lock()
	engine.prompt = prompt
	engine.mu.Unlock()
}
func (engine *fakeEngine) SetMaxLoops(maxLoops int) {
	engine.mu.Lock()
	engine.maxLoops = maxLoops
	engine.mu.Unlock()
}
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

type trackingSessions struct {
	fakeSessions
	mu       sync.Mutex
	savedIDs []string
}

func (sessions *trackingSessions) SaveCurrent(sessionID string) error {
	sessions.mu.Lock()
	sessions.savedIDs = append(sessions.savedIDs, sessionID)
	sessions.mu.Unlock()
	return nil
}

func newTestService(engine *fakeEngine) *Service {
	return New(Dependencies{Engine: engine, Runtime: &fakeRuntime{}, Plugins: &fakePlugins{current: PluginInfo{Name: "default"}}, Skills: fakeSkills{}, Sessions: fakeSessions{}})
}

func TestSnapshotIncludesPersistedSessions(t *testing.T) {
	t.Parallel()
	service := newTestService(&fakeEngine{})
	defer service.Shutdown()

	snapshot := service.Snapshot()
	if len(snapshot.Sessions) != 1 {
		t.Fatalf("sessions = %d, want 1", len(snapshot.Sessions))
	}
	if snapshot.Sessions[0].ID != "saved" || snapshot.Sessions[0].TokenCount != 4 {
		t.Fatalf("unexpected session metadata: %+v", snapshot.Sessions[0])
	}
}

func TestResumedChatPersistsToSelectedSession(t *testing.T) {
	engine := &fakeEngine{}
	sessions := &trackingSessions{}
	service := New(Dependencies{
		Engine: engine, Runtime: &fakeRuntime{},
		Plugins: &fakePlugins{current: PluginInfo{Name: "default"}},
		Skills:  fakeSkills{}, Sessions: sessions,
	})
	defer service.Shutdown()

	if err := service.Submit(context.Background(), "/resume saved"); err != nil {
		t.Fatal(err)
	}
	if err := service.Submit(context.Background(), "continue"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for service.Snapshot().Chat.Running && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	sessions.mu.Lock()
	defer sessions.mu.Unlock()
	if len(sessions.savedIDs) != 1 || sessions.savedIDs[0] != "saved" {
		t.Fatalf("saved session IDs = %v, want [saved]", sessions.savedIDs)
	}
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
	if event.ProtocolVersion != ProtocolVersion {
		t.Fatalf("protocol version = %d, want %d", event.ProtocolVersion, ProtocolVersion)
	}
}

func TestMessageDeltaIncludesStableMessageID(t *testing.T) {
	service := newTestService(&fakeEngine{})
	defer service.Shutdown()
	subscription := service.Subscribe(1)
	defer subscription.Close()

	service.mu.Lock()
	service.snapshot.Chat = ChatState{Running: true, RequestID: "request-1"}
	message := service.appendMessageLocked("assistant", "", nil)
	messageID := message.ID
	service.mu.Unlock()

	service.appendDelta("request-1", "next")
	event := <-subscription.Events
	var delta MessageDelta
	if err := json.Unmarshal(event.Payload, &delta); err != nil {
		t.Fatal(err)
	}
	if delta.MessageID != messageID || delta.Delta != "next" {
		t.Fatalf("unexpected delta payload: %+v", delta)
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
	waitForChatCompletion(t, service)
	engine.mu.Lock()
	prompt := engine.prompt
	modelInput := engine.lastInput
	engine.mu.Unlock()
	if strings.Contains(prompt, "review prompt") || strings.Contains(prompt, "strict") {
		t.Fatalf("system prompt contains Skill/user request: %q", prompt)
	}
	if !strings.Contains(prompt, "Seelex") {
		t.Fatalf("prompt missing identity: %q", prompt)
	}
	if !strings.Contains(prompt, "high-effort") {
		t.Fatalf("prompt missing effort: %q", prompt)
	}
	for _, expected := range []string{"- name: review", "review prompt", "## User Request\n/review strict"} {
		if !strings.Contains(modelInput, expected) {
			t.Fatalf("slash Skill model input missing %q: %q", expected, modelInput)
		}
	}
	if err := service.Submit(context.Background(), "#review focused"); err != nil {
		t.Fatal(err)
	}
	waitForChatCompletion(t, service)
	engine.mu.Lock()
	modelInput = engine.lastInput
	engine.mu.Unlock()
	for _, expected := range []string{"- name: review", "review prompt", "## User Request\n#review focused"} {
		if !strings.Contains(modelInput, expected) {
			t.Fatalf("hash Skill model input missing %q: %q", expected, modelInput)
		}
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

func waitForChatCompletion(t *testing.T, service *Service) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if !service.Snapshot().Chat.Running {
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

func TestToolHookBridgeAssignsUniqueStableIDs(t *testing.T) {
	service := newTestService(&fakeEngine{})
	defer service.Shutdown()
	bridge := NewToolHookBridge()
	bridge.Bind(service)
	hooks := bridge.Hooks()
	info := engine.ToolCallInfo{Turn: 1, Name: "read", Arguments: `{"path":"a"}`}

	hooks.OnToolStart(context.Background(), info)
	hooks.OnToolComplete(context.Background(), info)
	hooks.OnToolStart(context.Background(), info)
	hooks.OnToolComplete(context.Background(), info)

	ids := make([]string, 0, 2)
	for _, message := range service.Snapshot().Conversation {
		if message.Role == "tool" && message.Tool != nil {
			ids = append(ids, message.Tool.ID)
		}
	}
	if len(ids) != 2 || ids[0] == ids[1] {
		t.Fatalf("tool IDs = %v, want two unique IDs", ids)
	}
}

func TestLoadMoreHistoryAssignsStableMessageIDs(t *testing.T) {
	service := newTestService(&fakeEngine{})
	defer service.Shutdown()
	service.mu.Lock()
	service.snapshot.HistoryOffset = 1
	service.mu.Unlock()

	if err := service.LoadMoreHistory(1); err != nil {
		t.Fatal(err)
	}
	conversation := service.Snapshot().Conversation
	if len(conversation) == 0 || conversation[0].ID == "" {
		t.Fatalf("loaded history message has no stable ID: %#v", conversation)
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

func TestResumeCommandOpensSessionInteraction(t *testing.T) {
	service := newTestService(&fakeEngine{})
	defer service.Shutdown()

	if err := service.Submit(context.Background(), "/resume"); err != nil {
		t.Fatal(err)
	}
	snapshot := service.Snapshot()
	if snapshot.Interaction == nil || snapshot.Interaction.Kind != "session" {
		t.Fatalf("resume should open a session interaction: %#v", snapshot.Interaction)
	}
}
