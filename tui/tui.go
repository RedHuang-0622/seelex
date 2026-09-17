package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/RedHuang-0622/seelex/application"
	"github.com/RedHuang-0622/seelex/application/contract/dto"
)

type AppController interface {
	Snapshot() application.Snapshot
	Subscribe(buffer int) application.Subscription
	// SubscribeSession 订阅指定会话的事件；sessionID 为空表示跟随当前视图
	// 会话（含草稿物化后的新会话），归属由 application 在投递端过滤。
	SubscribeSession(sessionID string, buffer int) (application.Subscription, error)
	Submit(context.Context, string) error
	CancelChat(requestID string) bool
	Suggestions(input string) []application.Suggestion
	ResolveInteraction(context.Context, string, string) error
	SelectAccount(context.Context, string) error
	SwitchPlugin(context.Context, string) error
	SwitchEffort(context.Context, string) error
	// LoadMoreHistory 加载更早的消息到 Conversation（limit<=0 = 一整窗）。
	LoadMoreHistory(limit int) error
	// LoadLatestHistory 把可见窗口拉回最新一页（回看历史后的出口）。
	LoadLatestHistory() error
}

// queueController 是 AppController 的可选排队输入编辑面（调换顺序 / 撤回
// 到输入框）。只在回合运行期间生效（队列只承载运行中接受的输入）；未实现时
// 队列编辑按键给出明确提示，不静默改本地顺序。
type queueController interface {
	// ReorderQueuedInput 把排队输入从 from 位置移动到 to 位置（空 sessionID
	// = 当前视图会话）。
	ReorderQueuedInput(sessionID string, from, to int) error
	// RecallQueuedInput 撤回一条排队输入并返回其展示原文。
	RecallQueuedInput(sessionID string, index int) (string, error)
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
	pasteBuffer    string    // 折叠粘贴时暂存真实内容
	pasteSeq       int       // 折叠计数器
	lastKeyTime    time.Time // 上次按键时间，用于检测粘贴爆发
	queueFocus     bool      // 队列焦点：按键改走队列编辑（↑↓ 选择 / Shift+↑↓ 调换 / Alt+R 撤回）
	queueSel       int       // 队列焦点下的选中行
	// 只读面板（goalteam.go）：panel 是当前打开的面板，teamView 是异步取回的
	// 团队视图（nil = 未取/取失败），teamLoading/teamErr 是其加载态。
	panel       string
	teamView    *dto.TeamView
	teamLoading bool
	teamErr     string
}

func NewModel(app AppController) Model {
	input := textarea.New()
	input.Placeholder = "输入消息…  /help 查看命令"
	input.CharLimit = 0
	input.SetWidth(80)
	input.SetHeight(1)
	input.Focus()
	input.ShowLineNumbers = false
	// "" = 跟随当前视图会话：草稿物化成真实会话后事件自动可达，TUI 与 GUI
	// 因此共用 application 投递端这一套会话归属判定，不再各自过滤。
	subscription, err := app.SubscribeSession("", 256)
	if err != nil {
		subscription = app.Subscribe(256)
	}
	return Model{app: app, snapshot: app.Snapshot(), subscription: subscription, textarea: input, histIdx: -1, showLogo: true, textareaHeight: 1}
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
		// team.changed = 后端会话团队事实变了（装配/工作顺序/入职/编辑成员）。团队
		// 面板是**按需读面**（数据不在快照里），不重取就会停在旧成员表上——此前只能
		// 靠用户再按一次 Alt+T 才发现。GUI 的 status 子页同一口径（team.changed →
		// 作废面板缓存，见 gui/frontend/dist/app.js 的 invalidateAgentTeam）。
		var teamRefresh tea.Cmd
		if model.panel == panelTeam && message.event.Kind == application.EventTeamChanged {
			model.teamLoading = true
			teamRefresh = fetchTeamView(model.app, model.snapshot.Session.ID)
		}
		if model.snapshot.Chat.Running {
			return model, tea.Batch(teamRefresh, waitApplicationEvent(model.subscription), tickEvery(3*time.Second))
		}
		model.queueFocus = false
		return model, tea.Batch(teamRefresh, waitApplicationEvent(model.subscription))
	case tickMsg:
		model.snapshot = model.app.Snapshot()
		model.syncView()
		if model.snapshot.Chat.Running {
			return model, tickEvery(3 * time.Second)
		}
		model.queueFocus = false
		return model, nil
	case submitResultMsg:
		if message.err != nil {
			model.uiError = message.err.Error()
			model.syncView()
		}
		return model, waitApplicationEvent(model.subscription)
	case teamViewMsg:
		model = model.applyTeamView(message)
		model.syncView()
		return model, waitApplicationEvent(model.subscription)
	default:
		return model, nil
	}
}

// windowed 报告可见窗口是否已离开尾部（用户在回看更早历史）：口径与前端
// historyWindowed 一致——durable 条数不计 system 引导消息。

func (model Model) windowed() bool {
	total := model.snapshot.TotalMessages
	if total <= 0 {
		return false
	}
	visible := 0
	for _, message := range model.snapshot.Conversation {
		if message.Role != "system" {
			visible++
		}
	}
	return model.snapshot.HistoryOffset+visible < total
}

func (model Model) handleKey(message tea.KeyMsg) (tea.Model, tea.Cmd) {
	if model.quitting {
		return model, tea.Quit
	}
	// 只读面板的开关键先于运行态/审批态处理：它不改任何后端状态。
	if handled, updated, command := model.handlePanelKey(message); handled {
		return updated, command
	}
	if model.snapshot.Interaction != nil {
		return model.handleInteractionKey(message)
	}
	if model.snapshot.Chat.Running {
		if model.queueFocus {
			if handled, updated, command := model.handleQueueKey(message); handled {
				return updated, command
			}
			// 未识别按键：先退出队列焦点，再按常规运行态处理——Ctrl+C 这类
			// 全局键不被焦点吞掉。
			model.queueFocus = false
		}
		switch message.String() {
		case "ctrl+c":
			model.app.CancelChat(model.snapshot.Chat.RequestID)
			return model, nil
		case "alt+e":
			return model, func() tea.Msg {
				return submitResultMsg{err: model.app.SwitchEffort(context.Background(), "cycle")}
			}
		case "alt+q":
			if len(model.snapshot.Chat.InputQueue) == 0 {
				model.uiError = "当前没有排队中的输入"
				return model, nil
			}
			model.queueFocus = true
			model.queueSel = 0
			model.uiError = ""
			return model, nil
		case "enter":
			if model.checkPaste() {
				return model, nil
			}
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
			// 窗口已锚定在更早历史（回看中）时，end 还要求「回到最新」：分页
			// 不再把窗口拽回尾部，回看期间的新内容只能由这条出口带回。
			if model.windowed() {
				return model, loadLatestHistory(model.app)
			}
		}
		return model, nil
	case "alt+e":
		return model, func() tea.Msg {
			return submitResultMsg{err: model.app.SwitchEffort(context.Background(), "cycle")}
		}
	default:
		model.lastKeyTime = time.Now()
		var command tea.Cmd
		old := model.textarea.Value()
		model.textarea, command = model.textarea.Update(message)
		model.foldPaste(old, model.textarea.Value())
		model.afterInput()
		return model, command
	}
}

// handleQueueKey 处理「队列焦点」下的按键。handled=false 表示该键不属于队列
// 编辑面，调用方退出焦点后按常规运行态继续处理。
func (model Model) handleQueueKey(message tea.KeyMsg) (bool, tea.Model, tea.Cmd) {
	queue := model.snapshot.Chat.InputQueue
	if len(queue) == 0 {
		model.queueFocus = false
		return true, model, nil
	}
	selected := model.queueSelection()
	switch message.String() {
	case "esc", "alt+q":
		model.queueFocus = false
		return true, model, nil
	case "up":
		model.queueSel = max(selected-1, 0)
		return true, model, nil
	case "down":
		model.queueSel = min(selected+1, len(queue)-1)
		return true, model, nil
	case "shift+up":
		updated, command := model.reorderQueued(selected, selected-1)
		return true, updated, command
	case "shift+down":
		updated, command := model.reorderQueued(selected, selected+1)
		return true, updated, command
	case "alt+r":
		updated, command := model.recallQueued(selected)
		return true, updated, command
	}
	return false, model, nil
}

// reorderQueued 调换排队顺序（from → to）：顺序事实源在后端会话队列，TUI 不
// 本地重排；成功后选中行跟随被移动的那条。
func (model Model) reorderQueued(from, to int) (Model, tea.Cmd) {
	length := len(model.snapshot.Chat.InputQueue)
	if from < 0 || from >= length || to < 0 || to >= length || from == to {
		return model, nil
	}
	controller, ok := model.app.(queueController)
	if !ok {
		model.uiError = "当前前端不支持队列编辑"
		return model, nil
	}
	if err := controller.ReorderQueuedInput(model.snapshot.Session.ID, from, to); err != nil {
		model.uiError = err.Error()
		return model, nil
	}
	model.queueSel = to
	model.uiError = ""
	return model, nil
}

// recallQueued 撤回排队输入到输入框（原文追加在当前草稿之后，不覆盖），并
// 退出队列焦点交回文本输入。
func (model Model) recallQueued(index int) (Model, tea.Cmd) {
	controller, ok := model.app.(queueController)
	if !ok {
		model.uiError = "当前前端不支持队列编辑"
		return model, nil
	}
	text, err := controller.RecallQueuedInput(model.snapshot.Session.ID, index)
	if err != nil {
		model.uiError = err.Error()
		return model, nil
	}
	existing := model.textarea.Value()
	if strings.TrimSpace(existing) != "" && strings.TrimSpace(text) != "" {
		text = strings.TrimRight(existing, "\n") + "\n" + text
	} else if strings.TrimSpace(existing) != "" {
		text = existing
	}
	model.textarea.SetValue(text)
	model.textarea.CursorEnd()
	model.afterInput()
	model.queueFocus = false
	model.uiError = ""
	return model, nil
}

// queueSelection 返回队列焦点下的有效选中行（钳到 [0, len-1]）。
func (model Model) queueSelection() int {
	length := len(model.snapshot.Chat.InputQueue)
	if length == 0 {
		return 0
	}
	return min(max(model.queueSel, 0), length-1)
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
		// Content 已由 application 剥掉思考块（reasoning 在 Message.ReasoningContent
		// 单独字段），终端不再自行按分隔符猜正文边界。
		if err := clipboard.WriteAll(last); err != nil {
			// 失败不阻塞，复制是辅助功能
			return submitResultMsg{err: nil}
		}
		return submitResultMsg{err: nil}
	}
}

func (model Model) handleEnter() (tea.Model, tea.Cmd) {
	if model.pasteBuffer == "" && time.Since(model.lastKeyTime) < 50*time.Millisecond {
		model.lastKeyTime = time.Now()
		return model, nil
	}
	if model.checkPaste() {
		return model, nil
	}
	input := strings.TrimSpace(model.textarea.Value())
	// 粘贴已折叠为占位符时，Enter 提交真实粘贴内容（而不是吞掉按键）：
	// 折叠只做 UI 显示（防长文本刷屏），提交语义保持直觉 —— 粘贴 → 回车 → 发送。
	if model.pasteBuffer != "" {
		input = strings.TrimSpace(model.pasteBuffer)
		model.pasteBuffer = ""
	}
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

// checkPaste 检测 textarea 是否包含多行粘贴内容，是则折叠为占位符。
// 返回 false 让提交继续：已折叠时 handleEnter 会用 pasteBuffer 的真实内容
// 提交（粘贴 → 回车 → 发送），不再吞掉 Enter。
func (model *Model) checkPaste() bool {
	if model.pasteBuffer != "" {
		return false // 已折叠，提交逻辑用 pasteBuffer
	}
	val := model.textarea.Value()
	if val == "" {
		return false
	}
	lines := strings.Count(val, "\n") + 1
	if lines < 2 && len(val) < maxPasteChars {
		return false // 单行短文本，不是粘贴
	}
	model.pasteSeq++
	model.pasteBuffer = val
	placeholder := fmt.Sprintf("[Pasted text #%d +%d lines]", model.pasteSeq, lines)
	model.textarea.SetValue(placeholder)
	model.textarea.CursorEnd()
	return true
}

// foldPaste 检测单次按键中是否插入了大量文本（逐字符粘贴特征），是则折叠为占位符。
func (model *Model) foldPaste(old, newVal string) {
	if model.pasteBuffer != "" {
		return
	}
	if old == newVal {
		return
	}
	oldLines := strings.Count(old, "\n")
	newLines := strings.Count(newVal, "\n")
	if newLines-oldLines < 2 && len(newVal)-len(old) < maxPasteChars {
		return
	}
	model.pasteSeq++
	model.pasteBuffer = newVal
	placeholder := fmt.Sprintf("[Pasted text #%d +%d lines]", model.pasteSeq, newLines-oldLines+1)
	model.textarea.SetValue(placeholder)
	model.textarea.CursorEnd()
}

func (model *Model) afterInput() {
	value := model.textarea.Value()
	wasSuggestion := model.suggMode
	model.suggMode = application.HasSigilPrefix(value) && !strings.Contains(value, " ")
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
	// 前缀取自输入本身（sigil 契约是 application 的事实源，TUI 不再自持一份字符表）；
	// 输入前缀不可解析时退回 `/`（与面板默认一致）。
	trigger := application.SigilOf(model.textarea.Value())
	if trigger == "" {
		trigger = application.SigilCommand
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
