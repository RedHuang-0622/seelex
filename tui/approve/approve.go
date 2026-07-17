// Package approve 提供气泡终端审批面板组件。
//
// 作为 seelex/tui 的子包，实现基于 bubbletea 的审批交互。
// 复用 workplan/sugar/approve.Question / ChoiceOption / ApprovalGate。
//
// 架构：
//
//	外部 goroutine  →  Ask(Question) → 阻塞等待   ← 用户选择
//	                          ↓
//	                 pendingRequest (全局桥接)
//	                          ↓
//	                 Manager.CheckPending() → Update()
//	                          ↓
//	                 Manager.HandleKey() / View()
package approve

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// ─── 消息类型 ─────────────────────────────────────────────────────

// TickMsg 每秒发送一次，驱动倒计时渲染。
type TickMsg time.Time

// TickCmd 返回倒计时定时器命令。
func TickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return TickMsg(t)
	})
}

// ─── 全局桥接（goroutine → TUI Update） ─────────────────────────

var pendingRequest pendingReq

type pendingReq struct {
	q        Question
	ch       chan string
	risk     string
	preview  string
	toolName string
}

// Ask 发起一个审批问题，阻塞等待用户选择。
// 从任意 goroutine 调用（工具 handler / Permission Gate 回调）。
func Ask(q Question, risk, preview, toolName string) string {
	ch := make(chan string, 1)
	pendingRequest = pendingReq{q: q, ch: ch, risk: risk, preview: preview, toolName: toolName}
	return <-ch
}

// AskSimple 从简单字符串列表发起审批（兼容旧版 ask_approve 工具）。
func AskSimple(question string, choices []string) string {
	opts := make([]ChoiceOption, len(choices))
	for i, c := range choices {
		opts[i] = ChoiceOption{Key: c, Label: c}
		for _, b := range Choices(c) {
			if b.Key == c {
				opts[i] = b
				break
			}
		}
	}
	q := Question{
		ID:      "ask",
		Content: question,
		Options: opts,
	}
	return Ask(q, "low", "", "")
}

// ─── Manager：主 Model 嵌入的审批管理器 ─────────────────────────

// Manager 持有审批状态，主 TUI Model 通过它管理审批交互。
type Manager struct {
	// State 当前审批状态
	State State

	// Active 为 true 时表示审批面板激活中
	Active bool
}

// NewManager 创建审批管理器。
func NewManager() *Manager {
	return &Manager{}
}

// CheckPending 检查是否有待处理的审批请求，有则激活面板。
func (m *Manager) CheckPending() {
	if pendingRequest.ch == nil || m.Active {
		return
	}
	pq := pendingRequest
	pendingRequest = pendingReq{}
	m.State = newState(pq.q, pq.risk, pq.toolName, pq.preview)
	m.State.ch = pq.ch
	m.State.Selected = 0
	m.Active = true
}

// HandleKey 处理审批键盘事件。返回 true 表示已消费。
func (m *Manager) HandleKey(msg tea.KeyMsg) (handled bool, cmd tea.Cmd) {
	if !m.Active {
		return false, nil
	}
	if m.State.Resolved {
		return true, nil
	}
	opts := m.State.Options

	switch msg.String() {
	case "enter":
		if m.State.Selected >= 0 && m.State.Selected < len(opts) {
			m.resolve(opts[m.State.Selected].Key)
		}
		return true, nil
	case "up":
		if m.State.Selected > 0 {
			m.State.Selected--
		}
		return true, nil
	case "down":
		if m.State.Selected < len(opts)-1 {
			m.State.Selected++
		}
		return true, nil
	case "esc", "ctrl+c", "ctrl+d":
		m.resolve("__CANCEL__")
		return true, nil
	default:
		s := msg.String()
		if len(s) == 1 && s[0] >= '1' && s[0] <= '9' {
			if idx := int(s[0] - '1'); idx < len(opts) {
				m.State.Selected = idx
				m.resolve(opts[idx].Key)
			}
		}
		return true, nil
	}
}

// HandleTick 处理倒计时 tick。超时时自动取消。
func (m *Manager) HandleTick() (cmd tea.Cmd) {
	if !m.Active || m.State.Resolved {
		return nil
	}
	if m.State.isTimeout() {
		m.resolve("__TIMEOUT__")
		return nil
	}
	return TickCmd()
}

func (m *Manager) resolve(choice string) {
	m.State.Resolved = true
	m.State.Result = choice
	m.Active = false
	if m.State.ch != nil {
		select {
		case m.State.ch <- choice:
		default:
		}
	}
}
