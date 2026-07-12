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
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/RedHuang-0622/seelex/session"
	"github.com/RedHuang-0622/seelex/skill"
)

// ── 全局 Program 引用（用于 tea.Println 输出）────────────────

var prog *tea.Program

func SetProgram(p *tea.Program) { prog = p }

func PrintOutput(kind, text string) {
	if prog == nil {
		return
	}
	switch kind {
	case "tool_call":
		prog.Println(StyleToolCall.Render("  ✎ " + text))
	case "tool_result":
		prog.Println(StyleToolResult.Render("    " + text))
	case "chunk":
		fmt.Print(text) // 流式文本直接 stdout
	case "done":
		prog.Println("")
	}
}

// ── 渐变色 SEELEX 艺术字 ──────────────────────────────────────

var gradientSeelex = lipgloss.JoinVertical(lipgloss.Left,
	lipgloss.NewStyle().Foreground(lipgloss.Color("#7C3AED")).Render(`███████╗███████╗███████╗██╗     ███████╗██╗  ██╗`),
	lipgloss.NewStyle().Foreground(lipgloss.Color("#8B5CF6")).Render(`██╔════╝██╔════╝██╔════╝██║     ██╔════╝╚██╗██╔╝`),
	lipgloss.NewStyle().Foreground(lipgloss.Color("#A78BFA")).Render(`███████╗█████╗  █████╗  ██║     █████╗   ╚███╔╝ `),
	lipgloss.NewStyle().Foreground(lipgloss.Color("#34D399")).Render(`╚════██║██╔══╝  ██╔══╝  ██║     ██╔══╝   ██╔██╗ `),
	lipgloss.NewStyle().Foreground(lipgloss.Color("#10B981")).Render(`███████║███████╗███████╗███████╗███████╗██╔╝ ██╗`),
	lipgloss.NewStyle().Foreground(lipgloss.Color("#059669")).Render(`╚══════╝╚══════╝╚══════╝╚══════╝╚══════╝╚═╝  ╚═╝`),
)

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
	role    string // "user" | "assistant" | "system" | "diff"
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
	textarea textarea.Model
	messages []messageView

	streaming bool
	streamCh  chan StreamEvent
	lastStart time.Time

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
	showLogo bool
}

// ── 工厂 ─────────────────────────────────────────────────────────

func NewModel(
	eng *engine.Engine, modelName string,
	client *api.ChatClient, agt *agent.Agent,
	sessionMgr *session.Manager, skillReg *skill.Registry,
) Model {
	se := newSuggestionEngine(agt)
	skills := skillReg.All()
	ss := make([]suggestion, 0, len(skills))
	for _, s := range skills {
		ss = append(ss, suggestion{text: s.Name, description: s.Description, kind: "skill"})
	}
	se.SetSkills(ss)

	ta := textarea.New()
	ta.Placeholder = "输入消息…  /help 查看命令"
	ta.CharLimit = 0
	ta.SetWidth(80)
	ta.Focus()
	ta.ShowLineNumbers = false
	ta.KeyMap.InsertNewline.SetEnabled(false)

	return Model{
		eng: eng, client: client, agt: agt, modelName: modelName,
		sessionMgr: sessionMgr, skillReg: skillReg, suggEng: se,
		textarea:  ta,
		streamCh:  make(chan StreamEvent, 256),
		showLogo:  true,
		lastStart: time.Now(),
	}
}

func (m Model) Init() tea.Cmd { return nil }

func (m Model) suggLines() int {
	if !m.suggMode {
		return 0
	}
	s := m.suggEng.Suggest(m.textarea.Value())
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
	return max(m.height-fixed, 4)
}

// ── Update ───────────────────────────────────────────────────────

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.showLogo && m.ready {
		m.showLogo = false
		m.messages = []messageView{
			{role: "system", content: "/help 查看命令"},
		}
	}
	m.checkPrompt()

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.textarea.SetWidth(msg.Width - 4)
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
	case "done", "reload":
		m.streaming = false
		if evt.Kind == "reload" {
			m.rebuildMessages()
			m.syncView()
		}
	}
	return m, nil
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
	case "enter":
		return m.handleEnter()

	case "ctrl+c", "ctrl+d":
		m.quitting = true
		return m, tea.Quit

	case "up":
		if m.suggMode {
			if s := m.suggEng.Suggest(m.textarea.Value()); len(s) > 0 {
				m.suggIdx = (m.suggIdx - 1 + len(s)) % len(s)
			}
		} else if len(m.inputHist) > 0 {
			if m.histIdx == -1 {
				m.histDraft = m.textarea.Value()
				m.histIdx = len(m.inputHist) - 1
			} else if m.histIdx > 0 {
				m.histIdx--
			}
			m.textarea.SetValue(m.inputHist[m.histIdx])
			m.textarea.CursorEnd()
		}
		return m, nil

	case "down":
		if m.suggMode {
			if s := m.suggEng.Suggest(m.textarea.Value()); len(s) > 0 {
				m.suggIdx = (m.suggIdx + 1) % len(s)
			}
		} else if m.histIdx != -1 {
			m.histIdx++
			if m.histIdx >= len(m.inputHist) {
				m.histIdx = -1
				m.textarea.SetValue(m.histDraft)
				m.histDraft = ""
			} else {
				m.textarea.SetValue(m.inputHist[m.histIdx])
			}
			m.textarea.CursorEnd()
		}
		return m, nil

	case "tab":
		if m.suggMode {
			if s := m.suggEng.Suggest(m.textarea.Value()); len(s) > 0 && m.suggIdx < len(s) {
				m = m.acceptSugg(s[m.suggIdx])
			}
		}
		return m, nil

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

	default:
		var cmd tea.Cmd
		m.textarea, cmd = m.textarea.Update(msg)
		m.afterInput()
		return m, cmd
	}
}

func (m *Model) afterInput() {
	val := m.textarea.Value()
	m.suggMode = strings.HasPrefix(val, "/") && !strings.Contains(val, " ")
	m.histIdx = -1
}

// ── 确认键盘 ──────────────────────────────────────────────────

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
	m.promptCh <- choice
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
	input := strings.TrimSpace(m.textarea.Value())
	if input == "" || m.streaming {
		return m, nil
	}
	m.suggMode = false

	if input != "" && (len(m.inputHist) == 0 || m.inputHist[len(m.inputHist)-1] != input) {
		m.inputHist = append(m.inputHist, input)
	}
	m.histIdx = -1

	// 命令
	if strings.HasPrefix(input, "/") {
		if input == "/" {
			m.textarea.Reset()
			return m, nil
		}
		parts := strings.Fields(input[1:])
		if len(parts) > 0 {
			if s, ok := m.skillReg.Get(parts[0]); ok {
				return m.execSkill(s, parts[1:]), nil
			}
		}
		if msg := executeCommand(input); msg != nil {
			if msg.content == "" && msg.role == "system" {
				m.quitting = true
				return m, tea.Quit
			}
			m.messages = append(m.messages, *msg)
		}
		m.textarea.Reset()
		m.syncView()
		return m, nil
	}

	// 对话 — Q 加入 TUI, 流式输出走 tea.Println
	m.messages = append(m.messages, messageView{role: "user", content: input})
	if prog != nil {
		prog.Println(StyleUser.Render("  Q: " + input))
	}
	m.lastStart = time.Now()
	m.textarea.Reset()
	m.streaming = true

	eventCh := make(chan StreamEvent, 256)
	m.streamCh = eventCh
	currentStreamCh = eventCh

	go m.doStream(input, eventCh)
	return m, waitStream(m.streamCh)
}

// ── Skill ────────────────────────────────────────────────────────

func (m Model) execSkill(sk skill.Skill, args []string) tea.Model {
	p := sk.Prompt
	if len(args) > 0 {
		p += "\n\n" + strings.Join(args, " ")
	}
	m.eng.SetSystemPrompt(p)
	m.messages = append(m.messages, messageView{role: "system", content: "加载 Skill: " + sk.Name})
	m.textarea.Reset()
	m.syncView()
	return m
}

func (m Model) acceptSugg(s suggestion) Model {
	val := m.textarea.Value()
	if idx := strings.LastIndex(val, "/"); idx >= 0 {
		val = val[:idx+1]
	} else {
		val = ""
	}
	m.textarea.SetValue(val + s.text + " ")
	m.textarea.CursorEnd()
	m.suggMode = false
	m.suggIdx = 0
	return m
}

// ── 流式 ────────────────────────────────────────────────────────

func (m Model) doStream(input string, eventCh chan StreamEvent) {
	ctx := context.Background()
	_, err := m.eng.ChatStream(ctx, input, func(chunk string) {
		fmt.Print(chunk)
	})
	fmt.Println()

	// 流结束后打印 A: 摘要
	hist := m.eng.History()
	for i := len(hist) - 1; i >= 0; i-- {
		if hist[i].Role == "assistant" && hist[i].Content != nil {
			if prog != nil {
				prog.Println(StyleAssistant.Render("  A: " + *hist[i].Content))
			}
			break
		}
	}
	currentStreamCh = nil
	if err != nil && prog != nil {
		prog.Println(StyleError.Render("  ✖ " + err.Error()))
	}

	// 发送 reload 事件让 TUI 重建 messages + 复位 streaming
	select {
	case eventCh <- StreamEvent{Kind: "reload"}:
	default:
	}
}

func waitStream(ch chan StreamEvent) tea.Cmd {
	return func() tea.Msg {
		return <-ch
	}
}

// ── View ────────────────────────────────────────────────────────

func (m Model) View() string {
	if !m.ready {
		return gradientSeelex + "\n\n  Loading...\n"
	}
	if m.showLogo {
		return gradientSeelex + "\n" +
			StyleMuted.Render("  " + m.modelName) + "\n\n" +
			StyleMuted.Render("  loading...")
	}

	var b strings.Builder

	// Banner
	b.WriteString(StyleBanner.Render(" SEELEX"))
	b.WriteString(StyleMuted.Render(fmt.Sprintf("  %s", m.modelName)))
	b.WriteString("\n")

	// Viewport — 仅显示 Q&A + diff
	m.viewport.SetContent(m.renderMessages())
	b.WriteString(m.viewport.View())

	// 分隔线
	b.WriteString(StyleSep.Render(strings.Repeat("─", m.width)))
	b.WriteString("\n")

	// 确认面板
	if m.prompting {
		b.WriteString(m.renderPrompt())
	}

	// 提示面板
	if m.suggMode {
		if s := m.suggEng.Suggest(m.textarea.Value()); len(s) > 0 {
			b.WriteString(renderSuggestions(s, m.suggIdx, m.width))
		}
	}

	// 输入框
	if !m.streaming && !m.prompting {
		b.WriteString(m.textarea.View())
	}

	// 状态栏
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

// ── 从 Engine History 重建 Messages ─────────────────────────

func (m *Model) rebuildMessages() {
	hist := m.eng.History()
	m.messages = nil
	var lastQ string
	for _, h := range hist {
		switch h.Role {
		case "user":
			if h.Content != nil {
				lastQ = *h.Content
				m.messages = append(m.messages, messageView{role: "user", content: lastQ})
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
				role: "tool_result", content: content,
			})
		}
	}
}

// ── 消息渲染（仅 Q/A/工具链/diff）───────────────────────────

func (m Model) renderMessages() string {
	var b strings.Builder
	for _, msg := range m.messages {
		switch msg.role {
		case "user":
			b.WriteString(StyleUser.Render("  Q: "))
			b.WriteString(msg.content)
			b.WriteString("\n\n")
		case "assistant":
			b.WriteString(StyleAssistant.Render("  A: "))
			b.WriteString(msg.content)
			b.WriteString("\n\n")
		case "tool_call":
			b.WriteString(StyleToolCall.Render("  " + msg.content))
			b.WriteString("\n")
		case "tool_result":
			b.WriteString(StyleToolResult.Render("  " + msg.content))
			b.WriteString("\n")
		case "diff":
			for _, line := range strings.Split(msg.content, "\n") {
				if strings.HasPrefix(line, "+") {
					b.WriteString(StyleDiffAdd.Render("  " + line))
				} else if strings.HasPrefix(line, "-") {
					b.WriteString(StyleDiffDel.Render("  " + line))
				} else {
					b.WriteString("  " + line)
				}
				b.WriteString("\n")
			}
			b.WriteString("\n")
		}
	}
	return b.String()
}

func (m *Model) syncView() {
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

// ── 工具链 hooks ──────────────────────────────────────────────

func CreateToolHooks() *engine.LoopHooks {
	return &engine.LoopHooks{
		OnToolStart: func(_ context.Context, info engine.ToolCallInfo) {
			if prog == nil {
				return
			}
			prog.Println(StyleToolCall.Render("  ✎ " + info.Name + "(" + info.Arguments + ")"))
		},
		OnToolComplete: func(_ context.Context, info engine.ToolCallInfo) {
			if prog == nil {
				return
			}
			result := info.Result
			if len(result) > 200 {
				result = result[:200] + "..."
			}
			if info.Error != nil {
				result = info.Error.Error()
			}
			prog.Println(StyleToolResult.Render("    " + result))
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
