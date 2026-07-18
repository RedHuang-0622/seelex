package tui

import (
	"context"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/RedHuang-0622/seelex/application"
)

type AppController interface {
	Snapshot() application.Snapshot
	Subscribe(buffer int) application.Subscription
	Submit(context.Context, string) error
	CancelChat(requestID string) bool
	Suggestions(input string) []application.Suggestion
	ResolveInteraction(context.Context, string, string) error
	SelectAccount(context.Context, string) error
	SwitchPlugin(context.Context, string) error
	// LoadMoreHistory 加载更早的消息到 Conversation，返回是否还有更多。
	LoadMoreHistory(limit int) error
}

type Model struct {
	app            AppController
	snapshot       application.Snapshot
	subscription   application.Subscription
	textarea       textarea.Model
	viewport       viewport.Model
	suggMode       bool
	suggIdx        int
	suggOffset     int
	inputHist      []string
	histIdx        int
	histDraft      string
	interactionID  string
	interactionSel int
	ready          bool
	quitting       bool
	width          int
	height         int
	showLogo       bool
	uiError        string
}

func NewModel(app AppController) Model {
	input := textarea.New()
	input.Placeholder = "输入消息…  /help 查看命令"
	input.CharLimit = 0
	input.SetWidth(80)
	input.Focus()
	input.ShowLineNumbers = false
	input.KeyMap.InsertNewline.SetEnabled(false)
	return Model{app: app, snapshot: app.Snapshot(), subscription: app.Subscribe(256), textarea: input, histIdx: -1, showLogo: true}
}

func (model Model) Init() tea.Cmd { return waitApplicationEvent(model.subscription) }

func (model Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case tea.WindowSizeMsg:
		model.width, model.height = message.Width, message.Height
		model.textarea.SetWidth(max(message.Width-4, 1))
		height := model.convHeight()
		if !model.ready {
			model.viewport = viewport.New(message.Width, height)
			model.ready = true
		} else {
			model.viewport.Width, model.viewport.Height = message.Width, height
		}
		model.syncView()
		return model, nil
	case tea.KeyMsg:
		return model.handleKey(message)
	case tea.MouseMsg:
		var command tea.Cmd
		model.viewport, command = model.viewport.Update(message)
		return model, command
	case applicationEventMsg:
		model.snapshot = model.app.Snapshot()
		model.uiError = ""
		model.syncInteractionSelection()
		model.syncView()
		if message.event.Kind == application.EventExitRequested {
			model.quitting = true
			return model, tea.Quit
		}
		return model, waitApplicationEvent(model.subscription)
	case submitResultMsg:
		if message.err != nil {
			model.uiError = message.err.Error()
			model.syncView()
		}
		return model, waitApplicationEvent(model.subscription)
	default:
		return model, nil
	}
}

func (model Model) handleKey(message tea.KeyMsg) (tea.Model, tea.Cmd) {
	if model.quitting {
		return model, tea.Quit
	}
	if model.snapshot.Interaction != nil {
		return model.handleInteractionKey(message)
	}
	if model.snapshot.Chat.Running {
		if message.String() == "ctrl+c" {
			model.app.CancelChat(model.snapshot.Chat.RequestID)
		}
		return model, nil
	}
	switch message.String() {
	case "enter":
		return model.handleEnter()
	case "ctrl+c", "ctrl+d":
		model.quitting = true
		return model, tea.Quit
	case "up":
		if model.suggMode {
			suggestions := model.app.Suggestions(model.textarea.Value())
			if len(suggestions) > 0 {
				model.suggIdx = (model.suggIdx - 1 + len(suggestions)) % len(suggestions)
				if model.suggIdx < model.suggOffset {
					model.suggOffset = model.suggIdx
				}
			}
		} else if len(model.inputHist) > 0 {
			if model.histIdx == -1 {
				model.histDraft = model.textarea.Value()
				model.histIdx = len(model.inputHist) - 1
			} else if model.histIdx > 0 {
				model.histIdx--
			}
			model.textarea.SetValue(model.inputHist[model.histIdx])
			model.textarea.CursorEnd()
		}
		return model, nil
	case "down":
		if model.suggMode {
			suggestions := model.app.Suggestions(model.textarea.Value())
			if len(suggestions) > 0 {
				model.suggIdx = (model.suggIdx + 1) % len(suggestions)
				if model.suggIdx >= model.suggOffset+suggWindowSize {
					model.suggOffset = model.suggIdx - suggWindowSize + 1
				}
			}
		} else if model.histIdx != -1 {
			model.histIdx++
			if model.histIdx >= len(model.inputHist) {
				model.histIdx = -1
				model.textarea.SetValue(model.histDraft)
				model.histDraft = ""
			} else {
				model.textarea.SetValue(model.inputHist[model.histIdx])
			}
			model.textarea.CursorEnd()
		}
		return model, nil
	case "tab":
		if model.suggMode {
			suggestions := model.app.Suggestions(model.textarea.Value())
			if len(suggestions) > 0 && model.suggIdx < len(suggestions) {
				model = model.acceptSuggestion(suggestions[model.suggIdx])
			}
		}
		return model, nil
	case "pgup":
		if model.ready {
			model.viewport.HalfPageUp()
			if model.viewport.AtTop() && model.snapshot.HasMoreHistory {
				return model, loadMoreHistory(model.app, 0)
				}
		}
		return model, nil
	case "pgdown":
		if model.ready {
			model.viewport.HalfPageDown()
		}
		return model, nil
	case "home":
		if model.ready {
			model.viewport.GotoTop()
			if model.snapshot.HasMoreHistory {
				return model, loadMoreHistory(model.app, 0)
			}
		}
		return model, nil
	case "end":
		if model.ready {
			model.viewport.GotoBottom()
		}
		return model, nil
	default:
		var command tea.Cmd
		model.textarea, command = model.textarea.Update(message)
		model.afterInput()
		return model, command
	}
}

func (model Model) handleEnter() (tea.Model, tea.Cmd) {
	input := strings.TrimSpace(model.textarea.Value())
	model.suggMode = false
	if model.showLogo {
		model.showLogo = false
		if input == "" {
			model.textarea.Reset()
			return model, nil
		}
	}
	if input == "" {
		return model, nil
	}
	if len(model.inputHist) == 0 || model.inputHist[len(model.inputHist)-1] != input {
		model.inputHist = append(model.inputHist, input)
	}
	model.histIdx = -1
	model.textarea.Reset()
	return model, submitInput(model.app, input)
}

func (model *Model) afterInput() {
	value := model.textarea.Value()
	wasSuggestion := model.suggMode
	model.suggMode = (strings.HasPrefix(value, "/") || strings.HasPrefix(value, "#") || strings.HasPrefix(value, "@")) && !strings.Contains(value, " ")
	if model.suggMode && !wasSuggestion {
		model.suggIdx, model.suggOffset = 0, 0
	}
	model.histIdx = -1
}

func (model Model) acceptSuggestion(suggestion application.Suggestion) Model {
	trigger := "/"
	if strings.HasPrefix(model.textarea.Value(), "#") {
		trigger = "#"
	}
	model.textarea.SetValue(trigger + suggestion.Text + " ")
	model.textarea.CursorEnd()
	model.suggMode, model.suggIdx = false, 0
	return model
}

func (model *Model) syncInteractionSelection() {
	if model.snapshot.Interaction == nil {
		model.interactionID, model.interactionSel = "", 0
		return
	}
	if model.interactionID != model.snapshot.Interaction.ID {
		model.interactionID, model.interactionSel = model.snapshot.Interaction.ID, 0
	}
	if model.interactionSel >= len(model.snapshot.Interaction.Options) {
		model.interactionSel = max(len(model.snapshot.Interaction.Options)-1, 0)
	}
}
