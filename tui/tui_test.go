package tui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/RedHuang-0622/seelex/application"
)

type fakeApp struct {
	snapshot  application.Snapshot
	hub       *application.EventHub
	submitted string
	resolved  string
	cancelled string
	loadLimit int
	submitErr error
}

func newFakeApp() *fakeApp {
	return &fakeApp{snapshot: application.Snapshot{Runtime: application.RuntimeState{Model: "model"}}, hub: application.NewEventHub()}
}
func (app *fakeApp) Snapshot() application.Snapshot                { return app.snapshot }
func (app *fakeApp) Subscribe(buffer int) application.Subscription { return app.hub.Subscribe(buffer) }
func (app *fakeApp) Submit(_ context.Context, input string) error {
	app.submitted = input
	return app.submitErr
}
func (app *fakeApp) CancelChat(id string) bool { app.cancelled = id; return true }
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
func (app *fakeApp) LoadMoreHistory(limit int) error {
	app.loadLimit = limit
	return nil
}

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

func TestPlanNodeLifecycleIconsAreDefined(t *testing.T) {
	for _, status := range []application.NodeStatus{
		application.NodeQueued,
		application.NodeCanceled,
		application.NodePanicked,
	} {
		if planNodeIcon(status) == "?" {
			t.Fatalf("missing icon for %q", status)
		}
	}
}

func TestFoldedPasteSubmitsOriginalContent(t *testing.T) {
	app := newFakeApp()
	model := NewModel(app)
	model.showLogo = false
	original := "first\nsecond\nthird"
	model.foldPaste("", original)
	if model.pasteBuffer != original || !strings.HasPrefix(model.textarea.Value(), "[Pasted text #1") {
		t.Fatalf("paste was not folded: value=%q buffer=%q", model.textarea.Value(), model.pasteBuffer)
	}
	updated, command := model.handleEnter()
	if command == nil {
		t.Fatal("expected submit command")
	}
	command()
	if app.submitted != original {
		t.Fatalf("submitted %q, want original paste", app.submitted)
	}
	if updated.(Model).pasteBuffer != "" {
		t.Fatal("paste buffer was not cleared after submit")
	}
}

func TestCheckPasteFoldsLongSingleLine(t *testing.T) {
	model := NewModel(newFakeApp())
	model.textarea.SetValue(strings.Repeat("x", maxPasteChars))
	if !model.checkPaste() {
		t.Fatal("expected long input to be folded")
	}
	if len(model.pasteBuffer) != maxPasteChars || !strings.Contains(model.textarea.Value(), "+1 lines") {
		t.Fatalf("folded paste state = %q, %d", model.textarea.Value(), len(model.pasteBuffer))
	}
}

func TestEditingPastePlaceholderClearsHiddenBuffer(t *testing.T) {
	model := NewModel(newFakeApp())
	model.pasteBuffer = "secret pasted content"
	model.textarea.SetValue("edited")
	model.afterInput()
	if model.pasteBuffer != "" {
		t.Fatal("edited placeholder retained stale paste content")
	}
}

func TestInputHistoryRestoresDraft(t *testing.T) {
	model := NewModel(newFakeApp())
	model.inputHist = []string{"one", "two"}
	model.textarea.SetValue("draft")

	updated, _ := model.handleKey(tea.KeyMsg{Type: tea.KeyUp})
	model = updated.(Model)
	if got := model.textarea.Value(); got != "two" {
		t.Fatalf("first history value = %q", got)
	}
	updated, _ = model.handleKey(tea.KeyMsg{Type: tea.KeyUp})
	model = updated.(Model)
	if got := model.textarea.Value(); got != "one" {
		t.Fatalf("second history value = %q", got)
	}
	updated, _ = model.handleKey(tea.KeyMsg{Type: tea.KeyDown})
	model = updated.(Model)
	updated, _ = model.handleKey(tea.KeyMsg{Type: tea.KeyDown})
	model = updated.(Model)
	if got := model.textarea.Value(); got != "draft" || model.histIdx != -1 {
		t.Fatalf("draft restore = %q, histIdx=%d", got, model.histIdx)
	}
}

func TestSuggestionNavigationAndAcceptance(t *testing.T) {
	model := NewModel(newFakeApp())
	model.textarea.SetValue("/")
	model.afterInput()
	if !model.suggMode {
		t.Fatal("suggestion mode did not activate")
	}
	updated, _ := model.handleKey(tea.KeyMsg{Type: tea.KeyTab})
	model = updated.(Model)
	if got := model.textarea.Value(); got != "/help " || model.suggMode {
		t.Fatalf("accepted suggestion = %q, mode=%v", got, model.suggMode)
	}
}

func TestApplicationEventRefreshesSnapshot(t *testing.T) {
	app := newFakeApp()
	model := NewModel(app)
	app.snapshot.Runtime.Model = "updated"
	updated, command := model.Update(applicationEventMsg{event: application.Event{Kind: application.EventRuntimeChanged}})
	model = updated.(Model)
	if model.snapshot.Runtime.Model != "updated" || command == nil {
		t.Fatalf("event refresh model=%q command=%v", model.snapshot.Runtime.Model, command)
	}
}

func TestExitEventQuits(t *testing.T) {
	model := NewModel(newFakeApp())
	updated, command := model.Update(applicationEventMsg{event: application.Event{Kind: application.EventExitRequested}})
	if !updated.(Model).quitting || command == nil {
		t.Fatal("exit event did not request quit")
	}
}

func TestSubmitErrorBecomesUIError(t *testing.T) {
	model := NewModel(newFakeApp())
	updated, _ := model.Update(submitResultMsg{err: errors.New("submit failed")})
	if got := updated.(Model).uiError; got != "submit failed" {
		t.Fatalf("ui error = %q", got)
	}
}

func TestWindowSizeInitializesViewport(t *testing.T) {
	model := NewModel(newFakeApp())
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	model = updated.(Model)
	if !model.ready || model.width != 100 || model.height != 30 || model.textarea.Width() < 1 {
		t.Fatalf("window state = ready:%v width:%d height:%d", model.ready, model.width, model.height)
	}
}

func TestLoadMoreHistoryCommandUsesRequestedLimit(t *testing.T) {
	app := newFakeApp()
	message := loadMoreHistory(app, 42)()
	if result, ok := message.(loadMoreMsg); !ok || result.err != nil || app.loadLimit != 42 {
		t.Fatalf("load result=%#v limit=%d", message, app.loadLimit)
	}
}

func TestTickRefreshesRunningSnapshot(t *testing.T) {
	app := newFakeApp()
	app.snapshot.Chat = application.ChatState{Running: true}
	model := NewModel(app)
	updated, command := model.Update(tickMsg(time.Now()))
	if !updated.(Model).snapshot.Chat.Running || command == nil {
		t.Fatal("running tick did not schedule next refresh")
	}
}
