package gui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/application"
)

type emittedEvent struct {
	name    string
	payload any
}

type fakeApplication struct {
	hub              *application.EventHub
	snapshot         application.Snapshot
	submitted        string
	cancelled        string
	resolvedID       string
	resolvedOption   string
	selectedAccount  string
	selectedEffort   string
	selectedPlugin   string
	loadedHistory    int
	suggestionsInput string
}

func newFakeApplication() *fakeApplication {
	return &fakeApplication{
		hub: application.NewEventHub(),
		snapshot: application.Snapshot{
			ProtocolVersion: application.ProtocolVersion,
			Revision:        1,
			Runtime:         application.RuntimeState{Model: "test-model"},
		},
	}
}

func (fake *fakeApplication) Snapshot() application.Snapshot { return fake.snapshot }
func (fake *fakeApplication) Subscribe(buffer int) application.Subscription {
	return fake.hub.Subscribe(buffer)
}
func (fake *fakeApplication) Submit(_ context.Context, text string) error {
	fake.submitted = text
	return nil
}
func (fake *fakeApplication) CancelChat(requestID string) bool {
	fake.cancelled = requestID
	return true
}
func (fake *fakeApplication) ResolveInteraction(_ context.Context, id, optionID string) error {
	fake.resolvedID, fake.resolvedOption = id, optionID
	return nil
}
func (fake *fakeApplication) SelectAccount(_ context.Context, name string) error {
	fake.selectedAccount = name
	return nil
}
func (fake *fakeApplication) SwitchEffort(_ context.Context, level string) error {
	fake.selectedEffort = level
	return nil
}
func (fake *fakeApplication) SwitchPlugin(_ context.Context, name string) error {
	fake.selectedPlugin = name
	return nil
}
func (fake *fakeApplication) LoadMoreHistory(limit int) error {
	fake.loadedHistory = limit
	return nil
}
func (fake *fakeApplication) Suggestions(input string) []application.Suggestion {
	fake.suggestionsInput = input
	return []application.Suggestion{{Text: "help", Kind: "command"}}
}

func TestNewBridgeRequiresApplication(t *testing.T) {
	t.Parallel()
	if _, err := NewBridge(nil, Options{}); err == nil {
		t.Fatal("NewBridge accepted a nil application")
	}
}

func TestBridgeDiscoversProjectSources(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("project"), 0o600); err != nil {
		t.Fatal(err)
	}
	bridge, err := NewBridge(newFakeApplication(), Options{ProjectRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	info := bridge.Info()
	if info.Project.Root != root || len(info.Project.Sources) != 1 || info.Project.Sources[0].Path != "README.md" {
		t.Fatalf("unexpected project info: %+v", info.Project)
	}
}

func TestBridgeForwardsCommands(t *testing.T) {
	t.Parallel()
	fake := newFakeApplication()
	bridge, err := NewBridge(fake, Options{Title: "Seelex Test", Version: "test"})
	if err != nil {
		t.Fatal(err)
	}

	if err := bridge.Submit("hello"); err != nil {
		t.Fatal(err)
	}
	if !bridge.CancelChat("request-1") {
		t.Fatal("CancelChat returned false")
	}
	if err := bridge.ResolveInteraction("approval-1", "allow"); err != nil {
		t.Fatal(err)
	}
	if err := bridge.SelectAccount("main"); err != nil {
		t.Fatal(err)
	}
	if err := bridge.SwitchEffort("high"); err != nil {
		t.Fatal(err)
	}
	if err := bridge.SwitchPlugin("default"); err != nil {
		t.Fatal(err)
	}
	if err := bridge.LoadMoreHistory(50); err != nil {
		t.Fatal(err)
	}
	suggestions := bridge.Suggestions("/he")

	if fake.submitted != "hello" || fake.cancelled != "request-1" {
		t.Fatalf("chat commands were not forwarded: %#v", fake)
	}
	if fake.resolvedID != "approval-1" || fake.resolvedOption != "allow" {
		t.Fatalf("interaction was not forwarded: %#v", fake)
	}
	if fake.selectedAccount != "main" || fake.selectedEffort != "high" || fake.selectedPlugin != "default" {
		t.Fatalf("runtime commands were not forwarded: %#v", fake)
	}
	if fake.loadedHistory != 50 || fake.suggestionsInput != "/he" || len(suggestions) != 1 {
		t.Fatalf("history or suggestions were not forwarded: %#v", fake)
	}
	if bridge.Info().Title != "Seelex Test" || bridge.Snapshot().Runtime.Model != "test-model" {
		t.Fatal("bridge metadata or snapshot mismatch")
	}
}

func TestBridgeRelaysApplicationEvents(t *testing.T) {
	t.Parallel()
	fake := newFakeApplication()
	bridge, err := NewBridge(fake, Options{})
	if err != nil {
		t.Fatal(err)
	}

	emitted := make(chan emittedEvent, 4)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	bridge.start(ctx, func(_ context.Context, name string, payload any) {
		emitted <- emittedEvent{name: name, payload: payload}
	})
	defer bridge.stop()

	ready := waitEmitted(t, emitted)
	if ready.name != "seelex:ready" {
		t.Fatalf("first event = %q, want seelex:ready", ready.name)
	}

	published := fake.hub.Publish(application.EventRuntimeChanged, 2, "", map[string]string{"plugin": "default"})
	relayed := waitEmitted(t, emitted)
	if relayed.name != eventName {
		t.Fatalf("relayed event name = %q, want %q", relayed.name, eventName)
	}
	event, ok := relayed.payload.(application.Event)
	if !ok {
		t.Fatalf("relayed payload type = %T", relayed.payload)
	}
	if event.ProtocolVersion != application.ProtocolVersion || event.Seq != published.Seq || event.Kind != published.Kind {
		t.Fatalf("relayed event = %#v, want %#v", event, published)
	}
}

func TestEmbeddedFrontendExists(t *testing.T) {
	t.Parallel()
	for _, name := range []string{
		"frontend/dist/index.html", "frontend/dist/app.js", "frontend/dist/components.js",
		"frontend/dist/protocol.js", "frontend/dist/client-state.js", "frontend/dist/conversation-view.js",
		"frontend/dist/chat-view.js", "frontend/dist/styles.css",
	} {
		if _, err := embeddedFrontend.ReadFile(name); err != nil {
			t.Fatalf("embedded frontend %q: %v", name, err)
		}
	}
	index, err := embeddedFrontend.ReadFile("frontend/dist/index.html")
	if err != nil {
		t.Fatal(err)
	}
	script, err := embeddedFrontend.ReadFile("frontend/dist/app.js")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(index), `id="session-list"`) || !strings.Contains(string(script), "renderSessions") {
		t.Fatal("embedded frontend does not include the session list")
	}
	components, err := embeddedFrontend.ReadFile("frontend/dist/components.js")
	if err != nil {
		t.Fatal(err)
	}
	componentSource := string(components)
	if !strings.Contains(componentSource, `renderIOPanel("IN"`) || !strings.Contains(componentSource, `renderIOPanel("OUT"`) || !strings.Contains(componentSource, "2400, 40") {
		t.Fatal("tool component must split IN/OUT and limit default output")
	}
	if !strings.Contains(string(index), `data-icon="command"`) || !strings.Contains(string(index), `data-icon="send"`) {
		t.Fatal("primary GUI actions must use icon controls")
	}
	html := string(index)
	leftStart := strings.Index(html, `<aside class="left-panel panel">`)
	rightStart := strings.Index(html, `<aside class="right-panel panel">`)
	if leftStart < 0 || rightStart < 0 {
		t.Fatal("embedded frontend sidebars are incomplete")
	}
	leftEnd := strings.Index(html[leftStart:], `</aside>`)
	rightEnd := strings.Index(html[rightStart:], `</aside>`)
	runtimeStart := strings.Index(html, `id="runtime-modal"`)
	if leftEnd < 0 || rightEnd < 0 || runtimeStart < 0 {
		t.Fatal("embedded frontend layout regions are incomplete")
	}
	leftPanel := html[leftStart : leftStart+leftEnd]
	rightPanel := html[rightStart : rightStart+rightEnd]
	runtimeModal := html[runtimeStart:]
	if strings.Contains(leftPanel, `id="plugin-list"`) || strings.Contains(rightPanel, `id="plugin-list"`) || !strings.Contains(runtimeModal, `id="plugin-list"`) {
		t.Fatal("plugins must be rendered in the runtime modal")
	}
	if !strings.Contains(rightPanel, `id="project-status"`) || !strings.Contains(rightPanel, `id="project-sources"`) {
		t.Fatal("right sidebar must render project status and sources")
	}
	if !strings.Contains(html, `id="command-modal"`) || !strings.Contains(string(script), "updateInlineSuggestions") {
		t.Fatal("embedded frontend does not include GUI command mode")
	}
}

func waitEmitted(t *testing.T, events <-chan emittedEvent) emittedEvent {
	t.Helper()
	select {
	case event := <-events:
		return event
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for GUI event")
		return emittedEvent{}
	}
}
