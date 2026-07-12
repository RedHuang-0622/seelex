package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/RedHuang-0622/Seele/agent"
	"github.com/RedHuang-0622/Seele/agent/core/api"
	"github.com/RedHuang-0622/Seele/engine"
	"github.com/RedHuang-0622/Seele/seelectx/tracer"
	"github.com/RedHuang-0622/Seele/types"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/RedHuang-0622/seelex/session"
	"github.com/RedHuang-0622/seelex/skill"
)

const bannerArt = `
    ╔═╗╔═╗╔═╗╔═╗╔╗╔╔╦╗
    ╚═╗║ ║║ ║║ ║║║║ ║
    ╚═╝╚═╝╚═╝╚═╝╝╚╝ ╩`

// ── 流式事件 ─────────────────────────────────────────────────

type StreamEvent struct {
	Kind     string
	Text     string
	Extra    string
	Duration time.Duration
	Err      error
}

var currentStreamCh chan StreamEvent

// ── 消息 ────────────────────────────────────────────────────────

type messageView struct {
	role    string
	content string
	extra   string
}

// ── 确认 ────────────────────────────────────────────────────────

var (
	approvalCh    chan string
	pendingPrompt promptRequest
)

type promptRequest struct {
	question string
	choices  []string
	ch       chan string
}

func initApproval() chan string {
	if approvalCh == nil {
		approvalCh = make(chan string, 1)
	}
	return approvalCh
}

// ── 主模型 ───────────────────────────────────────────────────────

type Model struct {
	eng       *engine.Engine
	client    *api.ChatClient
	agt       *agent.Agent
	modelName string

	sessionMgr *session.Manager
	skillReg   *skill.Registry
	suggEng    *suggestionEngine

	viewport viewport.Model
	messages []messageView

	streaming bool
	streamBuf string
	streamCh  chan StreamEvent
	lastInput string
	lastStart time.Time

	input    string
	suggMode bool
	suggIdx  int

	inputHist []string
	histIdx   int
	histDraft string

	prompting bool
	promptMsg string
	promptOpt []string
	promptSel int
	promptCh  chan string

	width    int
	height   int
	ready    bool
	quitting bool
	firstRun bool
}

// ── 工厂 ─────────────────────────────────────────────────────────

func NewModel(
	eng *engine.Engine, modelName string,
	client *api.ChatClient, agt *agent.Agent,
	sessionMgr *session.Manager, skillReg *skill.Registry,
) Model {
	se := newSuggestionEngine(agt)
	se.RefreshTools()
	skills := skillReg.All()
	ss := make([]suggestion, 0, len(skills))
	for _, s := range skills {
		ss = append(ss, suggestion{text: "#" + s.Name, description: s.Description, kind: "skill"})
	}
	se.SetSkills(ss)
	return Model{
		eng: eng, client: client, agt: agt, modelName: modelName,
		sessionMgr: sessionMgr, skillReg: skillReg, suggEng: se,
		streamCh:  make(chan StreamEvent, 256),
		firstRun:  true,
		lastStart: time.Now(),
	}
}

func (m Model) Init() tea.Cmd { return nil }

func (m Model) suggLines() int {
	if !m.suggMode {
		return 0
	}
	s := m.suggEng.Suggest(m.input)
	if len(s) == 0 {
		return 0
	}
	return min(len(s), 8)
}

func (m Model) promptLines() int {
	if !m.prompting {
		return 0
	}
	return len(m.promptOpt) + 3
}

func (m Model) viewportHeight() int {
	fixed := 6
	if m.suggMode {
		fixed += m.suggLines()
	}
	if m.prompting {
		fixed += m.promptLines()
	}
	return max(m.height-fixed, 3)
}

// ── Update ───────────────────────────────────────────────────────

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.firstRun && m.ready {
		m.firstRun = false
		m.messages = []messageView{
			{role: "system", content: fmt.Sprintf("Seele CLI — %s", m.modelName)},
			{role: "system", content: "/help @工具 #Skill ↑↓历史"},
		}
	}
	m.checkPrompt()

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		vh := m.viewportHeight()
		if !m.ready {
			m.viewport = viewport.New(msg.Width, vh)
			m.ready = true
		} else {
			m.viewport.Width = msg.Width
			m.viewport.Height = vh
		}
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	case StreamEvent:
		return m.handleStreamEvent(msg)

	default:
		return m, nil
	}
}

// ── 流式事件处理 ──────────────────────────────────────────────

func (m Model) handleStreamEvent(evt StreamEvent) (tea.Model, tea.Cmd) {
	switch evt.Kind {
	case "chunk":
		m.streamBuf += evt.Text
		m.syncViewport()
		return m, waitStream(m.streamCh)

	case "tool_call":
		args := evt.Extra
		if len(args) > 60 {
			args = args[:60] + "..."
		}
		m.messages = append(m.messages, messageView{
			role: "tool_call", content: fmt.Sprintf("✎ %s(%s)", evt.Text, args),
		})
		m.syncViewport()
		return m, waitStream(m.streamCh)

	case "tool_result":
		label := fmt.Sprintf("✓ %s", evt.Extra)
		if evt.Duration > 0 {
			label += fmt.Sprintf(" (%s)", evt.Duration.Round(time.Millisecond))
		}
		m.messages = append(m.messages, messageView{
			role: "tool_result", content: evt.Text, extra: label,
		})
		m.syncViewport()
		return m, waitStream(m.streamCh)

	case "done":
		m.streaming = false
		m.streamBuf = ""
		currentStreamCh = nil
		if evt.Err != nil {
			m.messages = append(m.messages, messageView{role: "error", content: evt.Err.Error()})
		}
		m.rebuildFromHistory()
		m.syncViewport()
		return m, nil

	default:
		return m, nil
	}
}

// ── 键盘 ──────────────────────────────────────────────────────

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.quitting {
		return m, tea.Quit
	}
	if m.prompting {
		return m.handlePromptKey(msg)
	}
	if m.streaming {
		if msg.String() == "ctrl+c" {
			m.quitting = true
			return m, tea.Quit
		}
		return m, nil
	}

	switch msg.String() {
	case "ctrl+c", "ctrl+d":
		m.quitting = true
		return m, tea.Quit

	case "enter":
		return m.handleEnter()

	case "backspace":
		if len(m.input) > 0 {
			rs := []rune(m.input)
			last := string(rs[len(rs)-1])
			m.input = string(rs[:len(rs)-1])
			if last == "/" || last == "@" || last == "#" {
				se := newSuggestionEngine(m.agt)
				se.RefreshTools()
				m.suggEng = se
				m.suggMode = false
			}
		}
		return m, nil

	// ↑↓ = 输入历史
	case "up":
		if m.suggMode {
			if s := m.suggEng.Suggest(m.input); len(s) > 0 {
				m.suggIdx = (m.suggIdx - 1 + len(s)) % len(s)
			}
		} else if len(m.inputHist) > 0 {
			if m.histIdx == -1 {
				m.histDraft = m.input
				m.histIdx = len(m.inputHist) - 1
			} else if m.histIdx > 0 {
				m.histIdx--
			}
			m.input = m.inputHist[m.histIdx]
		}
		return m, nil

	case "down":
		if m.suggMode {
			if s := m.suggEng.Suggest(m.input); len(s) > 0 {
				m.suggIdx = (m.suggIdx + 1) % len(s)
			}
		} else if m.histIdx != -1 {
			m.histIdx++
			if m.histIdx >= len(m.inputHist) {
				m.histIdx = -1
				m.input = m.histDraft
				m.histDraft = ""
			} else {
				m.input = m.inputHist[m.histIdx]
			}
		}
		return m, nil

	// PgUp/PgDn = viewport 滚动
	case "pgup":
		if m.ready {
			m.viewport.HalfPageUp()
		}
		return m, nil
	case "pgdown":
		if m.ready {
			m.viewport.HalfPageDown()
		}
		return m, nil

	case "tab":
		if m.suggMode {
			if s := m.suggEng.Suggest(m.input); len(s) > 0 && m.suggIdx < len(s) {
				m = m.acceptSugg(s[m.suggIdx])
			}
		}
		return m, nil

	default:
		s := msg.String()
		if len(s) > 0 && s[0] >= 0x20 && s[0] != 0x7f {
			m.input += s
			m.histIdx = -1
			switch s {
			case "/", "@", "#":
				m.suggEng.RefreshTools()
				m.suggMode = true
				m.suggIdx = 0
			}
		}
		if m.suggMode {
			m.viewport.Height = m.viewportHeight()
		}
		return m, nil
	}
}

func (m Model) handlePromptKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		if 0 <= m.promptSel && m.promptSel < len(m.promptOpt) {
			m.resolvePrompt(m.promptOpt[m.promptSel])
		}
		return m, nil
	case "up":
		m.promptSel = max(m.promptSel-1, 0)
		return m, nil
	case "down":
		m.promptSel = min(m.promptSel+1, len(m.promptOpt)-1)
		return m, nil
	case "ctrl+c", "ctrl+d":
		m.resolvePrompt("__CANCEL__")
		return m, nil
	default:
		s := msg.String()
		if len(s) == 1 && s[0] >= '1' && s[0] <= '9' {
			if idx := int(s[0] - '1'); idx < len(m.promptOpt) {
				m.resolvePrompt(m.promptOpt[idx])
			}
		}
		return m, nil
	}
}

func (m *Model) resolvePrompt(choice string) {
	m.prompting = false
	if choice != "__CANCEL__" {
		m.messages = append(m.messages, messageView{role: "system", content: "✓ 已选择: " + choice})
	}
	m.promptCh <- choice
	m.syncViewport()
}

func (m *Model) checkPrompt() {
	if pendingPrompt.ch != nil && !m.prompting {
		m.prompting = true
		m.promptMsg = pendingPrompt.question
		m.promptOpt = pendingPrompt.choices
		m.promptSel = 0
		m.promptCh = pendingPrompt.ch
		pendingPrompt = promptRequest{}
	}
}

// ── Enter ───────────────────────────────────────────────────────

func (m Model) handleEnter() (tea.Model, tea.Cmd) {
	input := strings.TrimSpace(m.input)
	if input == "" || m.streaming {
		return m, nil
	}
	m.suggMode = false

	if input != "" && (len(m.inputHist) == 0 || m.inputHist[len(m.inputHist)-1] != input) {
		m.inputHist = append(m.inputHist, input)
	}
	m.histIdx = -1

	if strings.HasPrefix(input, "/") {
		if input == "/" {
			m.input = ""
			return m, nil
		}
		if msg := executeCommand(input); msg != nil {
			if msg.content == "" && msg.role == "system" {
				m.quitting = true
				return m, tea.Quit
			}
			m.messages = append(m.messages, *msg)
		}
		m.input = ""
		m.syncViewport()
		return m, nil
	}

	if strings.HasPrefix(input, "#") {
		return m.skillCall(input), nil
	}

	m.messages = append(m.messages, messageView{role: "user", content: input})
	m.lastInput = input
	m.lastStart = time.Now()
	m.input = ""
	m.streaming = true
	m.streamBuf = ""

	eventCh := make(chan StreamEvent, 256)
	m.streamCh = eventCh
	currentStreamCh = eventCh
	go m.doStream(input, eventCh)
	return m, waitStream(m.streamCh)
}

// ── Skill ────────────────────────────────────────────────────────

func (m Model) skillCall(input string) tea.Model {
	parts := strings.Fields(strings.TrimPrefix(input, "#"))
	if len(parts) == 0 {
		m.messages = append(m.messages, messageView{role: "system", content: "用法: #skill_name [参数]"})
		return m
	}
	sk, ok := m.skillReg.Get(parts[0])
	if !ok {
		m.messages = append(m.messages, messageView{role: "system", content: "未知 Skill: " + parts[0]})
		return m
	}
	p := sk.Prompt
	if len(parts) > 1 {
		p += "\n\n用户参数: " + strings.Join(parts[1:], " ")
	}
	m.eng.SetSystemPrompt(p)
	m.messages = append(m.messages, messageView{role: "system", content: "加载 Skill: " + parts[0]})
	m.input = ""
	m.syncViewport()
	return m
}

func (m Model) acceptSugg(s suggestion) Model {
	for _, c := range []string{"/", "@", "#"} {
		if idx := strings.LastIndex(m.input, c); idx >= 0 {
			m.input = m.input[:idx+1]
			break
		}
	}
	m.input += s.text + " "
	m.suggMode = false
	m.suggIdx = 0
	return m
}

// ── 流式 goroutine ──────────────────────────────────────────────

func (m Model) doStream(input string, eventCh chan StreamEvent) {
	ctx := context.Background()
	_, err := m.eng.ChatStream(ctx, input, func(chunk string) {
		select {
		case eventCh <- StreamEvent{Kind: "chunk", Text: chunk}:
		default:
		}
	})
	select {
	case eventCh <- StreamEvent{Kind: "done", Err: err}:
	default:
	}
}

func waitStream(ch chan StreamEvent) tea.Cmd {
	return func() tea.Msg {
		return <-ch
	}
}

// ── 从 History 重建 ──────────────────────────────────────────────

func (m *Model) rebuildFromHistory() {
	hist := m.eng.History()
	if len(hist) == 0 {
		return
	}
	welcome := "Seele CLI — " + m.modelName
	if len(m.messages) > 0 {
		welcome = m.messages[0].content
	}
	m.messages = []messageView{{role: "system", content: welcome}}
	for _, h := range hist {
		switch h.Role {
		case "system":
			if h.Content != nil {
				m.messages = append(m.messages, messageView{role: "system", content: *h.Content})
			}
		case "user":
			if h.Content != nil {
				m.messages = append(m.messages, messageView{role: "user", content: *h.Content})
			}
		case "assistant":
			if h.Content != nil && *h.Content != "" {
				m.messages = append(m.messages, messageView{role: "assistant", content: *h.Content})
			}
			for _, tc := range h.ToolCalls {
				args := tc.Function.Arguments
				if len(args) > 80 {
					args = args[:80] + "..."
				}
				m.messages = append(m.messages, messageView{
					role: "tool_call", content: fmt.Sprintf("✎ %s(%s)", tc.Function.Name, args),
				})
			}
		case "tool":
			content := ""
			if h.Content != nil {
				content = *h.Content
				if len(content) > 120 {
					content = content[:120] + "..."
				}
			}
			m.messages = append(m.messages, messageView{
				role: "tool_result", content: content, extra: "✓ " + h.Name,
			})
		}
	}
}

// ── View ────────────────────────────────────────────────────────

func (m Model) View() string {
	if !m.ready {
		return StyleBanner.Render(bannerArt) + "\n\n  Loading...\n"
	}
	var b strings.Builder

	b.WriteString(StyleBanner.Render(" ◆ Seele"))
	b.WriteString(StyleMuted.Render(fmt.Sprintf("  %s", m.modelName)))
	b.WriteString("\n")

	m.viewport.SetContent(m.renderMessages())
	b.WriteString(m.viewport.View())

	b.WriteString(StyleSep.Render(strings.Repeat("─", m.width)))
	b.WriteString("\n")

	if m.prompting {
		b.WriteString(m.renderPrompt())
	}
	if m.suggMode {
		if s := m.suggEng.Suggest(m.input); len(s) > 0 {
			b.WriteString(renderSuggestions(s, m.suggIdx, m.width))
		}
	}

	if !m.streaming && !m.prompting {
		prompt := StyleInputPrompt.Render(">")
		cursor := " "
		if time.Now().UnixMilli()/500%2 == 0 {
			cursor = StyleInputPrompt.Render("▎")
		}
		b.WriteString(StyleInputBox.Width(m.width-2).Render(
			fmt.Sprintf("%s %s%s", prompt, m.input, cursor),
		))
	}

	b.WriteString("\n")
	b.WriteString(m.renderStatus())
	return b.String()
}

// ── 确认面板 ─────────────────────────────────────────────────

func (m Model) renderPrompt() string {
	var b strings.Builder
	b.WriteString(StyleConfirm.Render("  " + m.promptMsg))
	b.WriteString("\n")
	for i, opt := range m.promptOpt {
		prefix := "  "
		sty := StyleChoiceInactive
		if i == m.promptSel {
			prefix = " ▸ "
			sty = StyleChoiceActive
		}
		b.WriteString(sty.Render(fmt.Sprintf("%s%d. %s", prefix, i+1, opt)))
		b.WriteString("\n")
	}
	b.WriteString(StyleMuted.Render("  ↑↓ Enter 数字键"))
	return b.String()
}

// ── 消息渲染（含工具链实时展示）────────────────────────────────

func (m Model) renderMessages() string {
	var b strings.Builder
	for _, msg := range m.messages {
		switch msg.role {
		case "user":
			b.WriteString(StyleUser.Render("  You"))
			b.WriteString("\n  ")
			b.WriteString(msg.content)
			b.WriteString("\n\n")
		case "assistant":
			b.WriteString(StyleAssistant.Render("  Seele"))
			b.WriteString("\n  ")
			b.WriteString(msg.content)
			b.WriteString("\n\n")
		case "tool_call":
			b.WriteString(StyleToolCall.Render("  " + msg.content))
			b.WriteString("\n")
		case "tool_result":
			if msg.extra != "" {
				b.WriteString("  ")
				b.WriteString(StyleConfirm.Render(msg.extra))
				b.WriteString("\n")
			}
			if msg.content != "" {
				display := msg.content
				if len(display) > 200 {
					display = display[:200] + "..."
				}
				b.WriteString(StyleToolResult.Render("    " + display))
				b.WriteString("\n")
			}
		case "system":
			b.WriteString(StyleSystem.Render("  ● " + msg.content))
			b.WriteString("\n")
		case "error":
			b.WriteString(StyleError.Render("  ✖ " + msg.content))
			b.WriteString("\n")
		}
	}

	if m.streaming && m.streamBuf != "" {
		b.WriteString(StyleAssistant.Render("  Seele"))
		b.WriteString("\n")
		b.WriteString(StyleStream.Render("  " + m.streamBuf))
		b.WriteString("\n")
	}
	return b.String()
}

func (m *Model) syncViewport() {
	m.viewport.SetContent(m.renderMessages())
	m.viewport.GotoBottom()
}

// ── 状态栏 ─────────────────────────────────────────────────────

func (m Model) renderStatus() string {
	tokens := tokensFromEngine(m.eng)
	elapsed := time.Since(m.lastStart).Round(time.Second)
	pf := string(m.client.ProviderFilter())
	plugin := m.agt.Tools().ActivePlugin()

	left := fmt.Sprintf(" %s", pf)
	if pf == "" {
		left = " round-robin"
	}
	if plugin != "" && plugin != "default" {
		left += fmt.Sprintf(" [%s]", plugin)
	}
	if m.streaming {
		left += " …"
	}
	right := fmt.Sprintf("tok:%s  %s", tokens, elapsed)
	padding := max(m.width-lipgloss.Width(right)-lipgloss.Width(left)-4, 0)
	return StyleStatus.Render(left + strings.Repeat(" ", padding) + right)
}

// ── Token ───────────────────────────────────────────────────────

func tokensFromEngine(eng *engine.Engine) string {
	if eng == nil {
		return "0"
	}
	tree := eng.ExportTrace()
	if tree == nil || tree.Root == nil {
		return "0"
	}
	for _, c := range tree.Root.Children {
		if c.Kind == tracer.SpanLLMCall {
			if t, ok := c.Attrs["total_tokens"]; ok {
				return t
			}
		}
	}
	return "0"
}

// ── 工具链 hooks 工厂 ─────────────────────────────────────────

func CreateToolHooks() *engine.LoopHooks {
	return &engine.LoopHooks{
		OnToolStart: func(_ context.Context, info engine.ToolCallInfo) {
			if currentStreamCh == nil {
				return
			}
			select {
			case currentStreamCh <- StreamEvent{
				Kind: "tool_call", Text: info.Name, Extra: info.Arguments,
			}:
			default:
			}
		},
		OnToolComplete: func(_ context.Context, info engine.ToolCallInfo) {
			if currentStreamCh == nil {
				return
			}
			result := info.Result
			if len(result) > 120 {
				result = result[:120] + "..."
			}
			if info.Error != nil {
				result = info.Error.Error()
			}
			select {
			case currentStreamCh <- StreamEvent{
				Kind: "tool_result", Text: result, Extra: info.Name,
				Duration: info.Duration,
			}:
			default:
			}
		},
	}
}

// ── Approve ───────────────────────────────────────────────────

func HandleApproval(question string, choices []string) string {
	ch := initApproval()
	pendingPrompt = promptRequest{question: question, choices: choices, ch: ch}
	return <-ch
}

var _ = types.Message{}
