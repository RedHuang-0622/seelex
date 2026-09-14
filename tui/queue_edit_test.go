package tui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/RedHuang-0622/seelex/application"
)

// queueFakeApp 是队列编辑面的 TUI 假实现：AppController + 可选
// queueController（与真实 application.Service 的能力面一致）。
type queueFakeApp struct {
	snapshot   application.Snapshot
	hub        *application.EventHub
	reorders   [][2]int
	recalled   int
	recallText string
	cancelled  string
	reorderErr error
	recallErr  error
}

func newQueueFakeApp(queue []string) *queueFakeApp {
	return &queueFakeApp{
		snapshot: application.Snapshot{
			Runtime: application.RuntimeState{Model: "model"},
			Chat:    application.ChatState{Running: true, RequestID: "chat-1", InputQueue: queue, QueuedCount: len(queue)},
		},
		hub: application.NewEventHub(),
	}
}

func (app *queueFakeApp) Snapshot() application.Snapshot { return app.snapshot }
func (app *queueFakeApp) Subscribe(buffer int) application.Subscription {
	return app.hub.Subscribe(buffer)
}
func (app *queueFakeApp) SubscribeSession(sessionID string, buffer int) (application.Subscription, error) {
	return app.hub.SubscribeSession(sessionID, buffer), nil
}
func (*queueFakeApp) Submit(context.Context, string) error { return nil }
func (app *queueFakeApp) CancelChat(id string) bool        { app.cancelled = id; return true }
func (*queueFakeApp) Suggestions(string) []application.Suggestion {
	return nil
}
func (*queueFakeApp) ResolveInteraction(context.Context, string, string) error { return nil }
func (*queueFakeApp) SelectAccount(context.Context, string) error              { return nil }
func (*queueFakeApp) SwitchPlugin(context.Context, string) error               { return nil }
func (*queueFakeApp) SwitchEffort(context.Context, string) error               { return nil }
func (*queueFakeApp) LoadMoreHistory(int) error                                { return nil }
func (*queueFakeApp) LoadLatestHistory() error                                 { return nil }
func (app *queueFakeApp) ReorderQueuedInput(_ string, from, to int) error {
	app.reorders = append(app.reorders, [2]int{from, to})
	return app.reorderErr
}
func (app *queueFakeApp) RecallQueuedInput(_ string, index int) (string, error) {
	app.recalled = index
	return app.recallText, app.recallErr
}

func altKey(r rune) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}, Alt: true}
}

// TestQueueFocusReorderRoutesThroughApplication Alt+Q 进入队列焦点，Shift+↓
// 调换顺序：顺序事实源在应用层（TUI 不本地重排），选中行跟随被移动的条目。
func TestQueueFocusReorderRoutesThroughApplication(t *testing.T) {
	app := newQueueFakeApp([]string{"one", "two"})
	model := NewModel(app)
	model.showLogo = false

	updated, _ := model.handleKey(altKey('q'))
	model = updated.(Model)
	if !model.queueFocus || model.queueSel != 0 {
		t.Fatalf("Alt+Q focus = %v sel = %d, want true/0", model.queueFocus, model.queueSel)
	}

	updated, _ = model.handleKey(tea.KeyMsg{Type: tea.KeyShiftDown})
	model = updated.(Model)
	if len(app.reorders) != 1 || app.reorders[0] != [2]int{0, 1} {
		t.Fatalf("reorder calls = %v, want [[0 1]]", app.reorders)
	}
	if model.queueSel != 1 {
		t.Fatalf("selection after reorder = %d, want 1", model.queueSel)
	}

	// 边界：末行再下移不发命令（不产生越界调用）。
	updated, _ = model.handleKey(tea.KeyMsg{Type: tea.KeyShiftDown})
	model = updated.(Model)
	if len(app.reorders) != 1 {
		t.Fatalf("boundary Shift+Down must not call the application: %v", app.reorders)
	}
}

// TestQueueFocusRecallWritesComposer Alt+R 撤回选中项：原文进输入框（不覆盖
// 已有草稿），焦点交回文本输入。
func TestQueueFocusRecallWritesComposer(t *testing.T) {
	app := newQueueFakeApp([]string{"one", "two"})
	app.recallText = "two"
	model := NewModel(app)
	model.showLogo = false
	model.textarea.SetValue("draft")

	updated, _ := model.handleKey(altKey('q'))
	model = updated.(Model)
	updated, _ = model.handleKey(tea.KeyMsg{Type: tea.KeyDown})
	model = updated.(Model)
	updated, _ = model.handleKey(altKey('r'))
	model = updated.(Model)

	if app.recalled != 1 {
		t.Fatalf("recalled index = %d, want 1", app.recalled)
	}
	if model.queueFocus {
		t.Fatal("recall must leave queue focus")
	}
	if got := model.textarea.Value(); got != "draft\ntwo" {
		t.Fatalf("composer after recall = %q, want %q", got, "draft\ntwo")
	}
}

// TestQueueFocusDoesNotSwallowGlobalKeys 队列焦点不吞全局键：Ctrl+C 仍是停止
// 当前回合，Esc 退出焦点。
func TestQueueFocusDoesNotSwallowGlobalKeys(t *testing.T) {
	app := newQueueFakeApp([]string{"one", "two"})
	model := NewModel(app)
	model.showLogo = false

	updated, _ := model.handleKey(altKey('q'))
	model = updated.(Model)
	updated, _ = model.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	model = updated.(Model)
	if model.queueFocus {
		t.Fatal("Esc must leave queue focus")
	}

	updated, _ = model.handleKey(altKey('q'))
	model = updated.(Model)
	updated, _ = model.handleKey(tea.KeyMsg{Type: tea.KeyCtrlC})
	model = updated.(Model)
	if app.cancelled != "chat-1" {
		t.Fatalf("cancelled = %q, want chat-1", app.cancelled)
	}
	if model.queueFocus {
		t.Fatal("Ctrl+C must leave queue focus")
	}
}

// TestQueueFocusRejectedWhenQueueEmpty 没有排队输入时不进入焦点，给明确提示。
func TestQueueFocusRejectedWhenQueueEmpty(t *testing.T) {
	app := newQueueFakeApp(nil)
	model := NewModel(app)
	model.showLogo = false

	updated, _ := model.handleKey(altKey('q'))
	model = updated.(Model)
	if model.queueFocus {
		t.Fatal("empty queue must not enter focus")
	}
	if model.uiError == "" {
		t.Fatal("empty queue must surface a hint")
	}
}

// TestRenderQueueHighlightsSelection 队列焦点下选中行高亮 + 快捷键提示；
// 只读态（未聚焦）不出现选择标记。
func TestRenderQueueHighlightsSelection(t *testing.T) {
	app := newQueueFakeApp([]string{"one", "two"})
	model := NewModel(app)
	model.showLogo = false

	readOnly := model.renderQueue()
	if !strings.Contains(readOnly, "queue:") || !strings.Contains(readOnly, "1. one") {
		t.Fatalf("read-only queue = %q", readOnly)
	}
	if strings.Contains(readOnly, "▸") {
		t.Fatalf("read-only queue must not render a selection marker: %q", readOnly)
	}

	model.queueFocus = true
	model.queueSel = 1
	focused := model.renderQueue()
	if !strings.Contains(focused, "▸ 2. two") {
		t.Fatalf("focused queue must highlight the selected row: %q", focused)
	}
	if !strings.Contains(focused, "Alt+R 撤回") {
		t.Fatalf("focused queue must advertise the queue shortcuts: %q", focused)
	}
}
