package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/atotto/clipboard"
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
	SwitchEffort(context.Context, string) error
	// LoadMoreHistory 加载更早的消息到 Conversation，返回是否还有更多。
	LoadMoreHistory(limit int) error
}

const maxPasteChars = 200 // 超过此字符数视为粘贴

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
	textareaHeight int
	pasteBuffer    string // 折叠粘贴时暂存真实内容
	pasteSeq       int    // 折叠计数器
}

func NewModel(app AppController) Model {
	input := textarea.New()
	input.Placeholder = "输入消息…  /help 查看命令"
	input.CharLimit = 0
	input.SetWidth(80)
	input.SetHeight(1)
	input.Focus()
	input.ShowLineNumbers = false
	return Model{app: app, snapshot: app.Snapshot(), subscription: app.Subscribe(256), textarea: input, histIdx: -1, showLogo: true, textareaHeight: 1}
}

func (model Model) Init() tea.Cmd { return waitApplicationEvent(model.subscription) }

func (model Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case tea.WindowSizeMsg:
		model.width, model.height = message.Width, message.Height
		model.textarea.SetWidth(max(message.Width-4, 1))
		model = model.autoResizeTextarea()
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
		newModel, cmd := model.handleKey(message)
		if m, ok := newModel.(Model); ok {
			newModel = m.autoResizeTextarea()
		}
		return newModel, cmd
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
		switch message.String() {
		case "ctrl+c":
			model.app.CancelChat(model.snapshot.Chat.RequestID)
			return model, nil
		case "alt+e":
			return model, func() tea.Msg {
				return submitResultMsg{err: model.app.SwitchEffort(context.Background(), "cycle")}
			}
		case "enter":
			input := strings.TrimSpace(model.textarea.Value())
			if input == "" {
				return model, nil
			}
			model.textarea.Reset()
			return model, submitInput(model.app, input)
		default:
			var cmd tea.Cmd
			model.textarea, cmd = model.textarea.Update(message)
			model.afterInput()
			return model, cmd
		}
	}
	switch message.String() {
	case "enter":
		return model.handleEnter()
	case "ctrl+q":
		model.quitting = true
		return model, tea.Quit
	case "ctrl+c":
		return model, copyLastResponse(model)
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
	case "alt+e":
		return model, func() tea.Msg {
			return submitResultMsg{err: model.app.SwitchEffort(context.Background(), "cycle")}
		}
	default:
		old := model.textarea.Value()
		var command tea.Cmd
		model.textarea, command = model.textarea.Update(message)
		model.foldPaste(old, model.textarea.Value())
		model.afterInput()
		return model, command
	}
}

// copyLastResponse 复制最后一条 assistant 回复到系统剪贴板。
func copyLastResponse(model Model) tea.Cmd {
	return func() tea.Msg {
		var last string
		for i := len(model.snapshot.Conversation) - 1; i >= 0; i-- {
			msg := model.snapshot.Conversation[i]
			if msg.Role == "assistant" && msg.Content != "" {
				last = msg.Content
				break
			}
		}
		if last == "" {
			return submitResultMsg{err: nil}
		}
		// 分离思考内容与正文（以 "以以以" 或 "---" 为界）
		if idx := strings.LastIndex(last, "---"); idx > 0 {
			last = strings.TrimSpace(last[idx+3:])
		}
		if err := clipboard.WriteAll(last); err != nil {
			// 失败不阻塞，复制是辅助功能
			return submitResultMsg{err: nil}
		}
		return submitResultMsg{err: nil}
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

// resolvePaste 如果当前输入是折叠的粘贴占位符，用真实内容替换。
func (model *Model) resolvePaste() string {
	if model.pasteBuffer != "" {
		buf := model.pasteBuffer
		model.pasteBuffer = ""
		model.textarea.SetValue(buf)
		model.textarea.CursorEnd()
		model.autoResizeTextarea()
		return buf
	}
	return model.textarea.Value()
}

// foldPaste 检测粘贴行为，超过阈值时折叠为占位符。
func (model *Model) foldPaste(oldValue, newValue string) {
	if model.pasteBuffer != "" {
		return // 已有折叠内容，不再处理
	}
	if len(newValue) <= len(oldValue)+maxPasteChars {
		return // 变化量小，不是粘贴
	}
	// 检查增量部分是否来自 oldValue 末尾的追加（粘贴通常追加到末尾）
	added := newValue[len(oldValue):]
	lines := strings.Count(added, "\n") + 1
	if lines < 5 && len(added) < maxPasteChars {
		return // 行数少且字符数少，不是粘贴
	}
	model.pasteSeq++
	model.pasteBuffer = newValue
	placeholder := fmt.Sprintf("[Pasted text #%d +%d lines]", model.pasteSeq, lines)
	model.textarea.SetValue(placeholder)
	model.textarea.CursorEnd()
}

func (model *Model) afterInput() {
	value := model.textarea.Value()
	wasSuggestion := model.suggMode
	model.suggMode = (strings.HasPrefix(value, "/") || strings.HasPrefix(value, "#") || strings.HasPrefix(value, "@")) && !strings.Contains(value, " ")
	if model.suggMode && !wasSuggestion {
		model.suggIdx, model.suggOffset = 0, 0
	}
	// 如果用户编辑了折叠占位符，清除 pasteBuffer
	if model.pasteBuffer != "" && !strings.HasPrefix(value, "[Pasted text #") {
		model.pasteBuffer = ""
	}
	model.histIdx = -1
}

func (model Model) autoResizeTextarea() Model {
	lines := model.textarea.LineCount()
	if lines < 1 {
		lines = 1
	}
	if lines > 10 {
		lines = 10
	}
	if lines != model.textareaHeight {
		model.textareaHeight = lines
		model.textarea.SetHeight(lines)
		if model.ready {
			model.viewport.Height = model.convHeight()
		}
	}
	return model
}

func (model Model) acceptSuggestion(suggestion application.Suggestion) Model {
	trigger := "/"
	switch {
	case strings.HasPrefix(model.textarea.Value(), "#"):
		trigger = "#"
	case strings.HasPrefix(model.textarea.Value(), "@"):
		trigger = "@"
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
