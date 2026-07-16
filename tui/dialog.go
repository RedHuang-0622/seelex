// ── 交互式对话框：选择器 + 审批面板 ─────────────────────────
//
// 选择器：会话恢复 / 账号切换（↑↓ Enter 选择）
// 审批：  复用 workplan/sugar/approve.Question + TUI 桥接

package tui

import (
	"fmt"

	"github.com/RedHuang-0622/Seele/agent/core/api"
	tea "github.com/charmbracelet/bubbletea"

	tuiApprove "github.com/RedHuang-0622/seelex/tui/approve"
)

// ── 选择器键盘 ──────────────────────────────────────────────

func (m Model) handleSelKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up":
		if m.selIdx > 0 {
			m.selIdx--
		}
		return m, nil
	case "down":
		if m.selIdx < len(m.selItems)-1 {
			m.selIdx++
		}
		return m, nil
	case "enter":
		return m.selConfirm()
	case "esc", "ctrl+c", "ctrl+d":
		m.selState = selNone
		m.selItems = nil
		return m, nil
	default:
		s := msg.String()
		if len(s) == 1 && s[0] >= '1' && s[0] <= '9' {
			if idx := int(s[0] - '1'); idx < len(m.selItems) {
				m.selIdx = idx
				return m.selConfirm()
			}
		}
		return m, nil
	}
}

func (m Model) selConfirm() (tea.Model, tea.Cmd) {
	if m.selIdx < 0 || m.selIdx >= len(m.selItems) {
		m.selState = selNone
		return m, nil
	}
	item := m.selItems[m.selIdx]
	state := m.selState
	m.selState = selNone
	m.selItems = nil

	switch state {
	case selSession:
		if err := m.sessionMgr.Resume(item.id); err != nil {
			m.state.Conv.Add(Cell{Kind: CellSystem, Content: "恢复失败: " + err.Error()})
		} else {
			m.eng.ClearHistory()
			msgs, loadErr := m.sessionMgr.LoadHistory(item.id)
			m.state.Conv.Clear()
			m.state.Conv.Add(Cell{Kind: CellSystem, Content: fmt.Sprintf("已恢复会话: %s", item.label)})
			if loadErr == nil {
				for _, h := range msgs {
					m.addHistoryMessage(h)
				}
			}
		}
		m.syncView()
	case selAccount:
		m.client.SetProviderFilter(api.ProviderType(item.id))
		m.state.Conv.Add(Cell{Kind: CellSystem, Content: fmt.Sprintf("已切换账号: %s", item.label)})
		m.syncView()
	}
	return m, nil
}

// ── 选择器数据 ──────────────────────────────────────────────

func (m *Model) startSessionSelector() {
	metas := m.sessionMgr.List()
	if len(metas) == 0 {
		m.state.Conv.Add(Cell{Kind: CellSystem, Content: "暂无持久化会话"})
		m.syncView()
		return
	}
	m.selState = selSession
	m.selTitle = "选择会话"
	m.selIdx = 0
	m.selItems = make([]selectItem, 0, len(metas))
	for _, meta := range metas {
		label := meta.SessionID
		if len(label) > 16 {
			label = label[:16]
		}
		m.selItems = append(m.selItems, selectItem{
			id:    meta.SessionID,
			label: label,
			desc:  fmt.Sprintf("tok:%d  %s", meta.TokenCount, meta.UpdatedAt.Format("01-02 15:04")),
		})
	}
}

func (m *Model) startAccountSelector() {
	pool := m.client.AccountPool()
	if pool == nil {
		m.state.Conv.Add(Cell{Kind: CellSystem, Content: "无账号池"})
		m.syncView()
		return
	}
	all := pool.All()
	if len(all) == 0 {
		m.state.Conv.Add(Cell{Kind: CellSystem, Content: "无可用账号"})
		m.syncView()
		return
	}
	m.selState = selAccount
	m.selTitle = "切换账号"
	m.selIdx = 0
	m.selItems = make([]selectItem, 0, len(all))
	for _, a := range all {
		status := ""
		if a.Disabled {
			status = " [禁用]"
		}
		m.selItems = append(m.selItems, selectItem{
			id:    string(a.Provider),
			label: a.Name + status,
			desc:  fmt.Sprintf("%s %s", a.Provider, a.Model),
		})
	}
}

// ── 旧版审批键盘（兼容 promptRequest 模式） ───────────────

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
	select {
	case m.promptCh <- choice:
	default:
	}
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

// ── 旧版 Approve 入口（向后兼容） ─────────────────────────

var (
	pendingPrompt promptRequest
)

func init() {
	// 确保 pendingPrompt 可用
}

func HandleApproval(question string, choices []string) string {
	return tuiApprove.AskSimple(question, choices)
}
