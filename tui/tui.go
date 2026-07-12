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

// ── 消息类型 ─────────────────────────────────────────────────────

type messageView struct {
	role    string // user | assistant | system | error | tool_call | tool_result
	content string
	extra   string // 额外信息（如工具名）
}

type streamChunk struct {
	text string
	done bool
	err  error
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

	width    int
	height   int
	ready    bool
	quitting bool
}

// ── 模型工厂 ─────────────────────────────────────────────────────

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
		streamCh: make(chan streamChunk, 256),
		messages: []messageView{
			{role: "system", content: fmt.Sprintf("Seele CLI — %s", modelName)},
		},
		lastStart: time.Now(),
	}
}

func (m Model) Init() tea.Cmd { return nil }

// ── Viewport 高度 ────────────────────────────────────────────────

func (m Model) suggLines() int {
	if !m.suggMode {
		return 0
	}
	s := m.suggEng.Suggest(m.input)
	if len(s) == 0 {
		return 0
	}
	return min(len(s), 8) + 2
}

func (m Model) viewportHeight() int {
	fixed := 5 // banner(1) + sep(1) + input-box(2) + status(1)
	if m.suggMode {
		fixed += m.suggLines()
	}
	return max(m.height-fixed, 3)
}

// ── Update ───────────────────────────────────────────────────────

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
			if msg.err != nil {
				m.messages = append(m.messages, messageView{role: "error", content: msg.err.Error()})
			} else {
				// 从 engine history 重建带工具链的消息视图
				m.rebuildFromHistory()
			}
			m.streamBuf = ""
			m.refreshView()
			return m, nil
		}
		m.streamBuf += msg.text
		m.refreshView()
		return m, waitStream(m.streamCh)

	default:
		return m, nil
	}
}

// ── 从 History 重建消息（展示工具链）────────────────────────────

func (m *Model) rebuildFromHistory() {
	hist := m.eng.History()
	if len(hist) == 0 {
		return
	}

	m.messages = nil
	// 保留欢迎消息
	m.messages = append(m.messages, messageView{
		role: "system", content: fmt.Sprintf("Seele CLI — %s", m.modelName),
	})

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
			// 工具调用链
			for _, tc := range h.ToolCalls {
				args := tc.Function.Arguments
				if len(args) > 60 {
					args = args[:60] + "..."
				}
				m.messages = append(m.messages, messageView{
					role:    "tool_call",
					content: fmt.Sprintf("%s(%s)", tc.Function.Name, args),
					extra:   tc.ID,
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
				role:    "tool_result",
				content: content,
				extra:   h.Name,
			})
		}
	}
}

// ── 键盘处理 ─────────────────────────────────────────────────────

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.quitting {
		return m, tea.Quit
	}
	switch msg.String() {
	case "ctrl+c", "ctrl+d":
		m.quitting = true
		if !m.streaming {
			return m, tea.Quit
		}
		return m, nil

	case "enter":
		return m.handleEnter()

	case "backspace":
		if len(m.input) > 0 && !m.streaming {
			rs := []rune(m.input)
			last := string(rs[len(rs)-1])
			m.input = string(rs[:len(rs)-1])
			if last == "/" || last == "@" || last == "#" {
				m.suggEng = newSuggestionEngine(m.agt)
				m.suggEng.RefreshTools()
				m.suggMode = false
			}
		}
		return m, nil

	case "tab":
		if m.suggMode {
			if s := m.suggEng.Suggest(m.input); len(s) > 0 && m.suggIdx < len(s) {
				m = m.acceptSugg(s[m.suggIdx])
			}
		}
		return m, nil

	case "up":
		if m.suggMode {
			if s := m.suggEng.Suggest(m.input); len(s) > 0 {
				m.suggIdx = (m.suggIdx - 1 + len(s)) % len(s)
			}
		}
		return m, nil

	case "down":
		if m.suggMode {
			m.suggIdx++
		}
		return m, nil

	default:
		if !m.streaming {
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
			m.viewport.Height = m.viewportHeight()
		}
		return m, nil
	}
}

// ── Enter ───────────────────────────────────────────────────────

func (m Model) handleEnter() (tea.Model, tea.Cmd) {
	input := strings.TrimSpace(m.input)
	if input == "" || m.streaming {
		return m, nil
	}
	m.suggMode = false

	if strings.HasPrefix(input, "/") {
		msg := executeCommand(input)
		if msg != nil {
			if msg.content == "" && msg.role == "system" {
				m.quitting = true
				return m, tea.Quit
			}
			m.messages = append(m.messages, *msg)
		}
		m.input = ""
		m.refreshView()
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
	go m.doStream(input)
	return m, waitStream(m.streamCh)
}

// ── Skill 调用 ─────────────────────────────────────────────────

func (m Model) skillCall(input string) tea.Model {
	name := strings.TrimPrefix(input, "#")
	parts := strings.Fields(name)
	if len(parts) == 0 {
		m.messages = append(m.messages, messageView{role: "system", content: "用法: #skill_name [参数]"})
		return m
	}
	sk, ok := m.skillReg.Get(parts[0])
	if !ok {
		m.messages = append(m.messages, messageView{role: "system", content: fmt.Sprintf("未知 Skill: %s", parts[0])})
		return m
	}
	p := sk.Prompt
	if len(parts) > 1 {
		p += "\n\n用户参数: " + strings.Join(parts[1:], " ")
	}
	m.eng.SetSystemPrompt(p)
	m.messages = append(m.messages, messageView{role: "system", content: fmt.Sprintf("加载 Skill: %s", parts[0])})
	m.input = ""
	m.refreshView()
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

// ── View ────────────────────────────────────────────────────────

func (m Model) View() string {
	if !m.ready {
		return "\n  Loading..."
	}
	var b strings.Builder

	// Banner
	b.WriteString(StyleBanner.Render(" ◆ Seele"))
	if sid := m.eng.SessionID(); len(sid) > 8 {
		b.WriteString(StyleMuted.Render(fmt.Sprintf("  %s  ", m.modelName)))
		b.WriteString(StyleSessionID.Render(sid[len(sid)-8:]))
	} else {
		b.WriteString(StyleMuted.Render(fmt.Sprintf("  %s", m.modelName)))
	}
	b.WriteString("\n")

	// Viewport
	m.viewport.SetContent(m.renderMessages())
	b.WriteString(m.viewport.View())

	// 分隔线
	b.WriteString(StyleSep.Render(strings.Repeat("─", m.width)))
	b.WriteString("\n")

	// 提示面板
	if m.suggMode {
		if s := m.suggEng.Suggest(m.input); len(s) > 0 {
			b.WriteString(renderSuggestions(s, m.suggIdx, m.width))
		}
	}

	// 输入框
	if m.streaming {
		b.WriteString("  ")
		b.WriteString(m.streamBuf)
	} else {
		prompt := StyleInputPrompt.Render(">")
		cursor := " "
		if time.Now().UnixMilli()/500%2 == 0 {
			cursor = StyleInputPrompt.Render("▎")
		}
		inputLine := fmt.Sprintf("%s %s%s", prompt, m.input, cursor)
		b.WriteString(StyleInputBox.Width(m.width - 2).Render(inputLine))
	}
	b.WriteString("\n")

	// 状态栏
	b.WriteString(m.renderStatus())
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
			// 工具调用链展示
			b.WriteString(StyleToolCall.Render("  → "))
			b.WriteString(StyleToolCall.Render(msg.content))
			b.WriteString("\n")

		case "tool_result":
			// 工具返回结果
			b.WriteString(StyleToolResult.Render("    ↳ "))
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

	// 流式输出
	if m.streaming && m.streamBuf != "" {
		b.WriteString(StyleAssistant.Render("  Seele"))
		b.WriteString("\n")
		b.WriteString(StyleStream.Render("  " + m.streamBuf))
		b.WriteString("\n")
	}
	return b.String()
}

func (m *Model) refreshView() {
	m.viewport.SetContent(m.renderMessages())
	m.viewport.GotoBottom()
}

// ── 状态栏 ─────────────────────────────────────────────────────

func (m Model) renderStatus() string {
	tokens := tokensFromEngine(m.eng)
	elapsed := time.Since(m.lastStart).Round(time.Second)
	plugin := m.agt.Tools().ActivePlugin()
	sid := m.eng.SessionID()
	pf := string(m.client.ProviderFilter())

	parts := []string{pf}
	if pf == "" {
		parts[0] = "round-robin"
	}
	if plugin != "" && plugin != "default" {
		parts = append(parts, plugin)
	}
	parts = append(parts, fmt.Sprintf("tok:%s", tokens))
	parts = append(parts, elapsed.String())
	if len(sid) > 8 {
		parts = append(parts, sid[len(sid)-8:])
	}

	right := strings.Join(parts, "  ")
	padding := max(m.width-lipgloss.Width(right)-2, 0)
	return StyleStatus.Render("  " + strings.Repeat(" ", padding) + right)
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

// ── Token 提取 ─────────────────────────────────────────────────

func tokensFromEngine(eng *engine.Engine) string {
	if tree := eng.ExportTrace(); tree != nil && tree.Root != nil {
		for _, c := range tree.Root.Children {
			if c.Kind == tracer.SpanLLMCall {
				if t, ok := c.Attrs["total_tokens"]; ok {
					return t
				}
			}
		}
	}
	return "?"
}

// 编译守卫
var _ = types.Message{}
