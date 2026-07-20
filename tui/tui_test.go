package tui

import (
	"context"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/RedHuang-0622/seelex/application"
)

type fakeApp struct {
	snapshot  application.Snapshot
	hub       *application.EventHub
	submitted string
	resolved  string
	cancelled string
}

func newFakeApp() *fakeApp {
	return &fakeApp{snapshot: application.Snapshot{Runtime: application.RuntimeState{Model: "model"}}, hub: application.NewEventHub()}
}
func (app *fakeApp) Snapshot() application.Snapshot                { return app.snapshot }
func (app *fakeApp) Subscribe(buffer int) application.Subscription { return app.hub.Subscribe(buffer) }
func (app *fakeApp) Submit(_ context.Context, input string) error  { app.submitted = input; return nil }
func (app *fakeApp) CancelChat(id string) bool                     { app.cancelled = id; return true }
func (*fakeApp) Suggestions(string) []application.Suggestion {
	return []application.Suggestion{{Text: "help", Kind: "command"}}
}
func (app *fakeApp) ResolveInteraction(_ context.Context, id, option string) error {
	app.resolved = id + ":" + option
	return nil
}
func (*fakeApp) SelectAccount(context.Context, string) error { return nil }
func (*fakeApp) SwitchPlugin(context.Context, string) error  { return nil }
func (*fakeApp) SwitchEffort(context.Context, string) error  { return nil }
func (*fakeApp) LoadMoreHistory(int) error                   { return nil }

func TestEnterSubmitsRawInput(t *testing.T) {
	app := newFakeApp()
	model := NewModel(app)
	model.showLogo = false
	model.textarea.SetValue("hello")
	updated, command := model.handleEnter()
	if command == nil {
		t.Fatal("expected submit command")
	}
	_ = updated
	message := command()
	result, ok := message.(submitResultMsg)
	if !ok || result.err != nil {
		t.Fatalf("unexpected result %#v", message)
	}
	if app.submitted != "hello" {
		t.Fatalf("submitted %q", app.submitted)
	}
}

func TestInteractionKeyResolvesThroughApplication(t *testing.T) {
	app := newFakeApp()
	app.snapshot.Interaction = &application.Interaction{ID: "account-1", Options: []application.InteractionOption{{ID: "primary", Label: "Primary"}}}
	model := NewModel(app)
	model.snapshot = app.snapshot
	_, command := model.handleInteractionKey(tea.KeyMsg{Type: tea.KeyEnter})
	if command == nil {
		t.Fatal("expected resolve command")
	}
	command()
	if app.resolved != "account-1:primary" {
		t.Fatalf("resolved %q", app.resolved)
	}
}

func TestCtrlCCancelsActiveChat(t *testing.T) {
	app := newFakeApp()
	app.snapshot.Chat = application.ChatState{Running: true, RequestID: "chat-1"}
	model := NewModel(app)
	model.snapshot = app.snapshot
	model.handleKey(tea.KeyMsg{Type: tea.KeyCtrlC})
	if app.cancelled != "chat-1" {
		t.Fatalf("cancelled %q", app.cancelled)
	}
}
