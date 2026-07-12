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

// ── ASCII 启动横幅 ──────────────────────────────────────────────

const bannerArt = `
    ╔═╗╔═╗╔═╗╔═╗╔╗╔╔╦╗
    ╚═╗║ ║║ ║║ ║║║║ ║
    ╚═╝╚═╝╚═╝╚═╝╝╚╝ ╩`

// ── 消息类型 ─────────────────────────────────────────────────────

type messageView struct {
	role    string
	content string
	extra   string
}

type streamChunk struct {
	text string
	done bool
	err  error
}

// ── 确认通道（全局，跨 goroutine）─────────────────────────────

var (
	approvalCh   chan string
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
	streamCh  chan streamChunk
	lastInput string
	lastStart time.Time

	input    string
	suggMode bool
	suggIdx  int

	prompting bool
	promptMsg string
	promptOpt []string
	promptSel int
	promptCh  chan string

	width    int
	height   int
	ready    bool
	quitting bool
	firstRun bool // 首次显示横幅
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
		streamCh:  make(chan streamChunk, 256),
		firstRun:  true,
		lastStart: time.Now(),
	}
}

func (m Model) Init() tea.Cmd { return nil }

// ── 高度计算 ─────────────────────────────────────────────────────

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

// viewportHeight = 总高 - 固定元素(6) - 浮动面板
// 固定: banner(1) + sep(1) + input边框上(1) + input内容(1) + input边框下(1) + status(1) = 6
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
	// 首次显示时隐藏横幅
	if m.firstRun && m.ready {
		m.firstRun = false
		m.messages = []messageView{
			{role: "system", content: fmt.Sprintf("Seele CLI — %s", m.modelName)},
			{role: "system", content: "输入 /help 查看命令, @ 查看工具, # 查看 Skill"},
		}
	}

	// 检查待处理确认
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

	case streamChunk:
		if msg.done {
			m.streaming = false
			m.streamBuf = ""
			if msg.err != nil {
				m.messages = append(m.messages, messageView{role: "error", content: msg.err.Error()})
			}
			// 流结束后从 history 重建完整视图（含工具链）
			m.rebuildFromHistory()
			m.syncViewport()
			return m, nil
		}
		m.streamBuf += msg.text
		m.syncViewport()
		return m, waitStream(m.streamCh)

	default:
		return m, nil
	}
}

// ── 键盘───────────────────────────────────────────────────────

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.quitting {
		return m, tea.Quit
	}

	// 确认模式
	if m.prompting {
		return m.handlePromptKey(msg)
	}

	// 流式模式 — 只允许退出
	if m.streaming {
		if msg.String() == "ctrl+c" {
			m.quitting = true
			return m, tea.Quit
		}
		return m, nil
	}

	// ── 正常模式 ──────────────────────────────────────────────
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

	// ── Viewport 滚动键（不在 sugg 模式时转发）───────────
	case "pgup":
		if !m.suggMode && m.ready {
			m.viewport.HalfPageUp()
		}
		return m, nil
	case "pgdown":
		if !m.suggMode && m.ready {
			m.viewport.HalfPageDown()
		}
		return m, nil
	case "up":
		if m.suggMode {
			if s := m.suggEng.Suggest(m.input); len(s) > 0 {
				m.suggIdx = (m.suggIdx - 1 + len(s)) % len(s)
			}
		} else if m.ready {
			m.viewport.ScrollUp(1)
		}
		return m, nil
	case "down":
		if m.suggMode {
			if s := m.suggEng.Suggest(m.input); len(s) > 0 {
				m.suggIdx = (m.suggIdx + 1) % len(s)
			}
		} else if m.ready {
			m.viewport.ScrollDown(1)
		}
		return m, nil

	case "tab":
		if m.suggMode {
			if s := m.suggEng.Suggest(m.input); len(s) > 0 && m.suggIdx < len(s) {
				m = m.acceptSugg(s[m.suggIdx])
			}
		}
		return m, nil

	// ── 字符输入 ─────────────────────────────────────────────
	default:
		s := msg.String()
		if len(s) > 0 && s[0] >= 0x20 && s[0] != 0x7f {
			m.input += s
			switch s {
			case "/", "@", "#":
				m.suggEng.RefreshTools()
				m.suggMode = true
				m.suggIdx = 0
			}
		}
		// 输入变化时更新 vp 高度（提示面板可能弹出/收起）
		if m.suggMode {
			m.viewport.Height = m.viewportHeight()
		}
		return m, nil
	}
}

// ── 确认键盘 ──────────────────────────────────────────────────

func (m Model) handlePromptKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		if m.promptSel >= 0 && m.promptSel < len(m.promptOpt) {
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
			idx := int(s[0] - '1')
			if idx < len(m.promptOpt) {
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

// ── 检查待确认 ───────────────────────────────────────────────

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

	// 命令
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

	// Skill
	if strings.HasPrefix(input, "#") {
		return m.skillCall(input), nil
	}

	// 对话
	m.messages = append(m.messages, messageView{role: "user", content: input})
	m.lastInput = input
	m.lastStart = time.Now()
	m.input = ""
	m.streaming = true
	m.streamBuf = ""
	go m.doStream(input)
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

// ── 接受提示 ──────────────────────────────────────────────────

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

// ── 从 History 重建（含工具链）──────────────────────────────────

func (m *Model) rebuildFromHistory() {
	hist := m.eng.History()
	if len(hist) == 0 {
		return
	}

	// 保留欢迎消息
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
			// 文本回复
			if h.Content != nil && *h.Content != "" {
				m.messages = append(m.messages, messageView{role: "assistant", content: *h.Content})
			}
			// 工具调用链（每个工具一行）
			for _, tc := range h.ToolCalls {
				args := tc.Function.Arguments
				if len(args) > 80 {
					args = args[:80] + "..."
				}
				m.messages = append(m.messages, messageView{
					role: "tool_call",
					content: fmt.Sprintf("%s(%s)", tc.Function.Name, args),
					extra: tc.ID,
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
				role: "tool_result",
				content: content,
				extra: h.Name,
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

	// ── Banner ─────────────────────────────────────────────────
	b.WriteString(StyleBanner.Render(" ◆ Seele"))
	b.WriteString(StyleMuted.Render(fmt.Sprintf("  %s", m.modelName)))
	b.WriteString("\n")

	// ── Viewport ──────────────────────────────────────────────
	m.viewport.SetContent(m.renderMessages())
	b.WriteString(m.viewport.View())

	// ── 分隔线 ─────────────────────────────────────────────────
	b.WriteString(StyleSep.Render(strings.Repeat("─", m.width)))
	b.WriteString("\n")

	// ── 确认面板 ──────────────────────────────────────────────
	if m.prompting {
		b.WriteString(m.renderPrompt())
	}

	// ── 提示面板 ──────────────────────────────────────────────
	if m.suggMode {
		if s := m.suggEng.Suggest(m.input); len(s) > 0 {
			b.WriteString(renderSuggestions(s, m.suggIdx, m.width))
		}
	}

	// ── 输入框 ───────────────────────────────────────────────
	if !m.streaming && !m.prompting {
		prompt := StyleInputPrompt.Render(">")
		cursor := " "
		if time.Now().UnixMilli()/500%2 == 0 {
			cursor = StyleInputPrompt.Render("▎")
		}
		b.WriteString(StyleInputBox.Width(m.width - 2).Render(
			fmt.Sprintf("%s %s%s", prompt, m.input, cursor),
		))
	}

	// ── 状态栏 ─────────────────────────────────────────────────
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

// ── 消息渲染（含工具链）────────────────────────────────────────

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
			// 工具调用链：金色 → 工具名(参数)
			b.WriteString("  ")
			b.WriteString(StyleToolCall.Render("→ "))
			b.WriteString(StyleToolCall.Render(msg.content))
			b.WriteString("\n")

		case "tool_result":
			// 工具返回：灰色缩进
			b.WriteString("    ")
			b.WriteString(StyleToolResult.Render("↳ "))
			if msg.extra != "" {
				b.WriteString(StyleToolResult.Render(fmt.Sprintf("[%s] ", msg.extra)))
			}
			b.WriteString(StyleToolResult.Render(msg.content))
			b.WriteString("\n")

		case "system":
			b.WriteString(StyleSystem.Render("  ● " + msg.content))
			b.WriteString("\n")

		case "error":
			b.WriteString(StyleError.Render("  ✖ " + msg.content))
			b.WriteString("\n")
		}
	}

	// 流式文本（仅在这里展示，输入区不重复）
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
	plugin := m.agt.Tools().ActivePlugin()
	pf := string(m.client.ProviderFilter())

	left := fmt.Sprintf(" %s %s", pf, plugin)
	if pf == "" {
		left = " round-robin"
	}
	if plugin == "" || plugin == "default" {
		left = fmt.Sprintf(" %s", pf)
		if pf == "" {
			left = " round-robin"
		}
	}
	if m.streaming {
		left += " …"
	}

	right := fmt.Sprintf("tok:%s  %s", tokens, elapsed)
	padding := max(m.width-lipgloss.Width(right)-lipgloss.Width(left)-4, 0)

	return StyleStatus.Render(left + strings.Repeat(" ", padding) + right)
}

// ── 流式 ────────────────────────────────────────────────────────

func (m Model) doStream(input string) {
	ctx := context.Background()
	_, err := m.eng.ChatStream(ctx, input, func(chunk string) {
		select {
		case m.streamCh <- streamChunk{text: chunk}:
		default:
		}
	})
	m.streamCh <- streamChunk{done: true, err: err}
}

func waitStream(ch chan streamChunk) tea.Cmd {
	return func() tea.Msg {
		return <-ch
	}
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

// ── Approve 工具接口 ─────────────────────────────────────────

func HandleApproval(question string, choices []string) string {
	ch := initApproval()
	pendingPrompt = promptRequest{
		question: question,
		choices:  choices,
		ch:       ch,
	}
	return <-ch
}

// 编译守卫
var _ = types.Message{}
