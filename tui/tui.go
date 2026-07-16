// ── 主模型：Elm Architecture 事件驱动 ──────────────────────────
//
// 架构：
//   Event (tea.Msg) → Update → AppState → Widget Tree → Frame
//
// 运行时（Engine/ChatStream）通过 channel 桥接 goroutine → bubbletea

package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/RedHuang-0622/Seele/agent"
	"github.com/RedHuang-0622/Seele/agent/core/api"
	"github.com/RedHuang-0622/Seele/engine"
	"github.com/RedHuang-0622/Seele/seelectx/storage"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/RedHuang-0622/seelex/session"
	"github.com/RedHuang-0622/seelex/skill"
	tuiApprove "github.com/RedHuang-0622/seelex/tui/approve"
	"github.com/RedHuang-0622/seelex/tui/sugg"
)

// skillsNeedRefresh 由 Skill 管理命令设置，在 handleEnter 中消费
var skillsNeedRefresh bool

func signalSkillsRefresh() {
	skillsNeedRefresh = true
}

// ── 主模型 ───────────────────────────────────────────────────────

type Model struct {
	// ─ 应用状态（Elm Architecture State Store） ─
	state AppState

	// ─ 运行时引用 ─
	eng        *engine.Engine
	client     *api.ChatClient
	agt        *agent.Agent
	modelName  string
	sessionMgr *session.Manager
	skillReg   *skill.Registry
	SuggEng    *sugg.Engine

	// ─ 输入区 ─
	textarea  textarea.Model
	suggMode  bool
	suggIdx   int
	suggOffset int
	inputHist []string
	histIdx   int
	histDraft string

	// ─ 确认面板 ─
	prompting bool
	promptMsg string
	promptOpt []string
	promptSel int
	promptCh  chan string

	// ─ 审批面板（子包模块） ─
	ApproveMgr *tuiApprove.Manager

	// ─ 选择器（交互式列表） ─
	selState  selectState
	selTitle  string
	selItems  []selectItem
	selIdx    int

	// ─ Bubble Tea ─
	viewport viewport.Model
	ready    bool
	quitting bool
	width    int
	height   int
	showLogo bool

	// ─ 流式 ─
	streamCh  chan streamChunk
	lastInput string
	lastStart time.Time
}

// ── 工厂 ────────────────────────────────────────────────────────

func NewModel(
	eng *engine.Engine, modelName string,
	client *api.ChatClient, agt *agent.Agent,
	sessionMgr *session.Manager, skillReg *skill.Registry,
) Model {
	se := sugg.NewEngine(agt)
	skills := skillReg.All()
	ss := make([]sugg.Suggestion, 0, len(skills))
	for _, s := range skills {
		ss = append(ss, sugg.Suggestion{Text: s.Name, Description: s.Description, Kind: "skill"})
	}
	se.SetSkills(ss)
	se.RefreshTools()

	ta := textarea.New()
	ta.Placeholder = "输入消息…  /help 查看命令"
	ta.CharLimit = 0
	ta.SetWidth(80)
	ta.Focus()
	ta.ShowLineNumbers = false
	ta.KeyMap.InsertNewline.SetEnabled(false)

	return Model{
		state:      NewAppState(modelName),
		eng:        eng,
		client:     client,
		agt:        agt,
		modelName:  modelName,
		sessionMgr: sessionMgr,
		skillReg:   skillReg,
		SuggEng:    se,
		textarea:   ta,
		streamCh:   make(chan streamChunk, 256),
		showLogo:   true,
		ApproveMgr: tuiApprove.NewManager(),
		lastStart:  time.Now(),
	}
}

func (m Model) Init() tea.Cmd {
	// 启动时检查是否有待处理的审批定时器
	if m.ApproveMgr.Active && !m.ApproveMgr.State.Resolved {
		return tuiApprove.TickCmd()
	}
	return nil
}

// ── Update：事件入口（Elm Architecture）─────────────────────────

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	m.checkPrompt()
	m.ApproveMgr.CheckPending()

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.textarea.SetWidth(msg.Width - 4)
		vh := m.convHeight()
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

	case tea.MouseMsg:
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return m, cmd

	case streamChunk:
		return m.handleStreamChunk(msg)

	case tuiApprove.TickMsg:
		return m, m.ApproveMgr.HandleTick()

	default:
		return m, nil
	}
}

// ── 键盘 ──────────────────────────────────────────────────────

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.quitting {
		return m, tea.Quit
	}
	if handled, cmd := m.ApproveMgr.HandleKey(msg); handled {
		return m, cmd
	}
	if m.prompting {
		return m.handlePromptKey(msg)
	}
	// 选择器模式（交互式列表）
	if m.selState != selNone {
		return m.handleSelKey(msg)
	}
	if m.state.Streaming {
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
			if s := m.SuggEng.Suggest(m.textarea.Value()); len(s) > 0 {
				m.suggIdx = (m.suggIdx - 1 + len(s)) % len(s)
				if m.suggIdx < m.suggOffset {
					m.suggOffset = m.suggIdx
				}
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
			if s := m.SuggEng.Suggest(m.textarea.Value()); len(s) > 0 {
				m.suggIdx = (m.suggIdx + 1) % len(s)
				if m.suggIdx >= m.suggOffset+suggWindowSize {
					m.suggOffset = m.suggIdx - suggWindowSize + 1
				}
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
			if s := m.SuggEng.Suggest(m.textarea.Value()); len(s) > 0 && m.suggIdx < len(s) {
				m = m.acceptSugg(s[m.suggIdx])
			}
		}
		return m, nil

	// ── 视口滚动 ──
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

	case "home":
		if m.ready {
			m.viewport.GotoTop()
		}
		return m, nil

	case "end":
		if m.ready {
			m.viewport.GotoBottom()
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
	wasSugg := m.suggMode
	m.suggMode = (strings.HasPrefix(val, "/") || strings.HasPrefix(val, "#")) && !strings.Contains(val, " ")
	if m.suggMode && !wasSugg {
		m.suggIdx = 0
		m.suggOffset = 0
	}
	m.histIdx = -1
}

// ── Enter：提交输入 ────────────────────────────────────────────

func (m Model) handleEnter() (tea.Model, tea.Cmd) {
	input := strings.TrimSpace(m.textarea.Value())
	if m.state.Streaming {
		return m, nil
	}
	m.suggMode = false

	// Logo 状态：空白 Enter 也隐藏 Logo
	if m.showLogo {
		m.showLogo = false
		m.state.Conv.Clear()
		m.state.Conv.Add(Cell{Kind: CellSystem, Content: "/help 查看命令"})
		if input == "" {
			m.textarea.Reset()
			m.syncView()
			return m, nil
		}
	}

	if input == "" {
		return m, nil
	}

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
		// 交互式选择器命令
		cmdName := strings.ToLower(parts[0])
		if cmdName == "resume" && len(parts) == 1 {
			m.startSessionSelector()
			m.textarea.Reset()
			return m, nil
		}
		if cmdName == "pool" && len(parts) == 1 {
			m.startAccountSelector()
			m.textarea.Reset()
			return m, nil
		}
		// 普通命令
		if msg := executeCommand(input); msg != nil {
			if msg.content == "" && msg.role == "system" {
				m.quitting = true
				return m, tea.Quit
			}
			m.state.Conv.Add(Cell{Kind: CellSystem, Content: msg.content})
		}
		m.textarea.Reset()
		m.syncView()
		// Skill 变更后刷新建议引擎
		if skillsNeedRefresh {
			m.RefreshSkills()
			skillsNeedRefresh = false
		}
		return m, nil
	}

	// # → Skill 快速调用
	if strings.HasPrefix(input, "#") {
		name := strings.TrimPrefix(input, "#")
		if name != "" {
			if s, ok := m.skillReg.Get(name); ok {
				return m.execSkill(s, nil), nil
			}
			m.state.Conv.Add(Cell{Kind: CellSystem, Content: fmt.Sprintf("未知 Skill: %s", name)})
			m.textarea.Reset()
			m.syncView()
			return m, nil
		}
	}

	// 对话
	m.state.Conv.Add(Cell{Kind: CellUser, Content: input})
	m.state.Conv.Add(Cell{Kind: CellAssistant, Content: ""}) // 流式占位
	m.lastInput = input
	m.lastStart = time.Now()
	m.textarea.Reset()
	m.state.Streaming = true
	m.viewport.Height = m.convHeight()

	go m.doStream(input)
	return m, waitStream(m.streamCh)
}

// ── Skill ────────────────────────────────────────────────────────

func (m Model) execSkill(sk skill.Skill, args []string) tea.Model {
	p := sk.Prompt
	if len(args) > 0 {
		p += "\n\n" + strings.Join(args, " ")
	}
	m.eng.SetSystemPrompt(p)
	m.state.Conv.Add(Cell{Kind: CellSystem, Content: "加载 Skill: " + sk.Name})
	m.textarea.Reset()
	m.syncView()
	return m
}

// ── Skill 刷新 ───────────────────────────────────────────────

// RefreshSkills 从 Registry 重新加载 Skill 到建议引擎
func (m *Model) RefreshSkills() {
	skills := m.skillReg.All()
	ss := make([]sugg.Suggestion, 0, len(skills))
	for _, s := range skills {
		ss = append(ss, sugg.Suggestion{Text: s.Name, Description: s.Description, Kind: "skill"})
	}
	m.SuggEng.SetSkills(ss)
}

// ── 接受提示建议 ─────────────────────────────────────────────

func (m Model) acceptSugg(s sugg.Suggestion) Model {
	val := m.textarea.Value()
	if idx := strings.LastIndex(val, "/"); idx >= 0 {
		val = val[:idx+1]
	} else {
		val = ""
	}
	m.textarea.SetValue(val + s.Text + " ")
	m.textarea.CursorEnd()
	m.suggMode = false
	m.suggIdx = 0
	return m
}

// 编译期常量引用
var _ = storage.SessionMeta{}
