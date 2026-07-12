// ── 主 TUI 模型（装配件模式 — 组装各子组件）──────────────────────

package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/RedHuang-0622/Seele/agent"
	"github.com/RedHuang-0622/Seele/agent/core/api"
	"github.com/RedHuang-0622/Seele/engine"
	"github.com/RedHuang-0622/Seele/seelectx/tracer"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/RedHuang-0622/seelex/session"
	"github.com/RedHuang-0622/seelex/skill"
)

// ── 消息类型 ─────────────────────────────────────────────────────

type messageView struct {
	role    string // user | assistant | system | error
	content string
}

type streamChunk struct {
	text string
	done bool
	err  error
}

// ── 主模型（装配件 — 组合所有子组件）────────────────────────────

type model struct {
	// ── Core Seele（策略注入）──
	eng       *engine.Engine
	client    *api.ChatClient
	agt       *agent.Agent
	modelName string

	// ── 注入的装配件 ──
	sessionMgr *session.Manager
	skillReg   *skill.Registry
	suggEng    *suggestionEngine

	// ── 组件 ──
	viewport viewport.Model
	messages []messageView

	// ── 流式状态 ──
	streaming bool
	streamBuf string
	streamCh  chan streamChunk
	lastInput string
	lastStart time.Time

	// ── 输入状态 ──
	input    string
	suggMode bool // 正在提示模式 (/ @ #)
	suggIdx  int  // 选中提示索引

	// ── 窗口 ──
	width    int
	height   int
	ready    bool
	quitting bool
}

// ── 模型工厂 ─────────────────────────────────────────────────────

// initialModel 别名保持 main.go 兼容
func initialModel(
	eng *engine.Engine,
	modelName string,
	client *api.ChatClient,
	agt *agent.Agent,
	sessionMgr *session.Manager,
	skillReg *skill.Registry,
) model {
	return newModel(eng, modelName, client, agt, sessionMgr, skillReg)
}

func newModel(
	eng *engine.Engine,
	modelName string,
	client *api.ChatClient,
	agt *agent.Agent,
	sessionMgr *session.Manager,
	skillReg *skill.Registry,
) model {
	suggEng := newSuggestionEngine(agt)
	suggEng.RefreshTools()

	// 加载 skill 列表
	skills := skillReg.All()
	skillSuggs := make([]suggestion, 0, len(skills))
	for _, s := range skills {
		skillSuggs = append(skillSuggs, suggestion{
			text:        "#" + s.Name,
			description: s.Description,
			kind:        "skill",
		})
	}
	suggEng.SetSkills(skillSuggs)

	return model{
		eng:        eng,
		client:     client,
		agt:        agt,
		modelName:  modelName,
		sessionMgr: sessionMgr,
		skillReg:   skillReg,
		suggEng:    suggEng,
		streamCh:   make(chan streamChunk, 256),
		messages:   []messageView{{role: "system", content: fmt.Sprintf("Seele CLI — %s", modelName)}},
	}
}

func (m model) Init() tea.Cmd {
	return nil
}

// ── Update（委托组件处理）────────────────────────────────────────

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		vpHeight := m.height - 5
		if !m.ready {
			m.viewport = viewport.New(msg.Width, vpHeight)
			m.ready = true
		} else {
			m.viewport.Width = msg.Width
			m.viewport.Height = vpHeight
		}
		return m, nil

	case tea.KeyMsg:
		return m.handleKeyMsg(msg)

	case streamChunk:
		if msg.done {
			m.streaming = false
			if msg.err != nil {
				m.messages = append(m.messages, messageView{role: "error", content: msg.err.Error()})
			} else {
				m.messages = append(m.messages, messageView{role: "assistant", content: m.streamBuf})
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

// ── 键盘消息处理 ───────────────────────────────────────────────

func (m model) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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
			last := m.input[len(m.input)-1]
			m.input = m.input[:len(m.input)-1]
			// 退出提示模式
			if last == '/' || last == '@' || last == '#' {
				m.suggEng = newSuggestionEngine(m.agt)
				m.suggEng.RefreshTools()
				m.suggMode = false
			}
		}
		return m, nil

	case "tab":
		if m.suggMode {
			suggs := m.suggEng.Suggest(m.input)
			if len(suggs) > 0 {
				if m.suggIdx < len(suggs) {
					m = m.acceptSuggestion(suggs[m.suggIdx])
				}
			}
		}
		return m, nil

	case "up":
		if m.suggMode {
			suggs := m.suggEng.Suggest(m.input)
			if m.suggIdx > 0 {
				m.suggIdx--
			} else {
				m.suggIdx = len(suggs) - 1
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
			if len(s) == 1 && s[0] >= 0x20 && s[0] != 0x7f {
				m.input += s
				// 检测提示模式触发
				if s == "/" || s == "@" || s == "#" {
					m.suggEng.RefreshTools()
					m.suggMode = true
					m.suggIdx = 0
				} else if m.suggMode {
					// 继续提示
				} else {
					m.suggMode = false
				}
			}
		}
		return m, nil
	}
}

// ── Enter 处理 ─────────────────────────────────────────────────

func (m model) handleEnter() (tea.Model, tea.Cmd) {
	input := strings.TrimSpace(m.input)
	if input == "" || m.streaming {
		return m, nil
	}
	m.suggMode = false

	// 命令处理
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

	// Skill 调用 (#skill_name)
	if strings.HasPrefix(input, "#") {
		return m.handleSkillInvocation(input), nil
	}

	// 正常 Chat
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

func (m model) handleSkillInvocation(input string) tea.Model {
	name := strings.TrimPrefix(input, "#")
	parts := strings.Fields(name)
	if len(parts) == 0 {
		m.messages = append(m.messages, messageView{role: "system", content: "用法: #skill_name [参数]"})
		return m
	}
	skillName := parts[0]

	s, ok := m.skillReg.Get(skillName)
	if !ok {
		m.messages = append(m.messages, messageView{role: "system", content: fmt.Sprintf("未知 Skill: %s", skillName)})
		return m
	}

	// 将 Skill prompt 作为 system prompt 注入后发起 Chat
	prompt := s.Prompt
	if len(parts) > 1 {
		prompt += "\n\n用户参数: " + strings.Join(parts[1:], " ")
	}

	m.eng.SetSystemPrompt(prompt)
	m.messages = append(m.messages, messageView{role: "system", content: fmt.Sprintf("已加载 Skill: %s", skillName)})
	m.input = ""
	m.refreshView()
	return m
}

// ── 接受提示建议 ───────────────────────────────────────────────

func (m model) acceptSuggestion(s suggestion) model {
	// 替换当前输入中的最后一个触发词
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

// ── View（组合各组件输出）────────────────────────────────────────

func (m model) View() string {
	if !m.ready {
		return "\n  Loading..."
	}
	var b strings.Builder

	// Banner
	b.WriteString(styleBanner.Render(" Seele CLI"))
	b.WriteString(styleMuted.Render(fmt.Sprintf("  %s", m.modelName)))
	if sid := m.eng.SessionID(); sid != "" {
		b.WriteString("  ")
		b.WriteString(styleSessionID.Render(sid[len(sid)-8:]))
	}
	b.WriteString("\n")

	// Viewport（消息区）
	m.viewport.SetContent(m.renderMessages())
	b.WriteString(m.viewport.View())
	b.WriteString(styleSep.Render(strings.Repeat("─", m.width)))
	b.WriteString("\n")

	// 提示面板（在输入框上方）
	if m.suggMode {
		suggs := m.suggEng.Suggest(m.input)
		if len(suggs) > 0 {
			b.WriteString(renderSuggestions(suggs, m.suggIdx, m.width))
		}
	}

	// 输入行
	if m.streaming {
		b.WriteString("  ")
		b.WriteString(m.streamBuf)
	} else {
		b.WriteString(stylePrompt.Render("> "))
		b.WriteString(m.input)
	}
	b.WriteString("\n")

	// 状态栏
	b.WriteString(m.renderStatus())

	return b.String()
}

// ── 渲染辅助 ─────────────────────────────────────────────────────

func (m model) renderMessages() string {
	var b strings.Builder
	for _, msg := range m.messages {
		switch msg.role {
		case "user":
			b.WriteString(styleUser.Render("  You"))
			b.WriteString("\n  ")
			b.WriteString(msg.content)
			b.WriteString("\n\n")
		case "assistant":
			b.WriteString(styleAssistant.Render("  Seele"))
			b.WriteString("\n  ")
			b.WriteString(msg.content)
			b.WriteString("\n\n")
		case "system":
			b.WriteString(styleSystem.Render("  ● " + msg.content))
			b.WriteString("\n")
		case "error":
			b.WriteString(styleError.Render("  ✖ " + msg.content))
			b.WriteString("\n")
		}
	}
	if m.streaming && m.streamBuf != "" {
		b.WriteString(styleAssistant.Render("  Seele"))
		b.WriteString("\n")
		b.WriteString(styleStream.Render("  " + m.streamBuf))
		b.WriteString("\n")
	}
	return b.String()
}

func (m model) refreshView() {
	m.viewport.SetContent(m.renderMessages())
	m.viewport.GotoBottom()
}

// ── 状态栏 ─────────────────────────────────────────────────────

func (m model) renderStatus() string {
	tokens := tokensFromEngine(m.eng)
	elapsed := time.Since(m.lastStart).Round(time.Second)
	pf := m.client.ProviderFilter()
	plugin := m.agt.Tools().ActivePlugin()

	pluginStr := ""
	if plugin != "" && plugin != "default" {
		pluginStr = " [" + plugin + "]"
	}
	right := fmt.Sprintf("%s%s  tok:%s  %s", pf, pluginStr, tokens, elapsed)
	padding := max(m.width-lipgloss.Width(right)-2, 0)
	return styleStatus.Render("  " + strings.Repeat(" ", padding) + right)
}

// ── 流式调用 ─────────────────────────────────────────────────────

func (m model) doStream(input string) {
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

// Compile guards
var _ = agent.Options{}
var _ = session.Manager{}
var _ = skill.Registry{}
