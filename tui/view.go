// ── Widget 渲染层：AppState → Widget Tree → Frame ──────────
//
// 每个 render* 方法负责一个独立面板，最终由 View() 组合。

package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/RedHuang-0622/seelex/tui/splash"
)

// ── 布局计算 ────────────────────────────────────────────────────

const (
	shortcutsBarH = 1
)

func (m Model) convHeight() int {
	fixed := m.topPanelH() + m.midPanelH() + m.bottomPanelH()
	return max(m.height-fixed, 4)
}

func (m Model) topPanelH() int { return 1 + 1 }

func (m Model) midPanelH() int {
	h := 0
	if m.suggMode {
		if s := m.SuggEng.Suggest(m.textarea.Value()); len(s) > 0 {
			h += suggWindowSize + 3 // 窗口大小 + 上下滚动提示 + 间距
		}
	}

	if m.state.Streaming {
		h += 1 // 流式状态指示行
	}
	return h
}

func (m Model) bottomPanelH() int {
	return 3 + shortcutsBarH + 2
}

// ── View 主入口 ────────────────────────────────────────────────

func (m Model) View() string {
	if !m.ready {
		return splash.Gradient + "\n\n  Loading...\n"
	}
	if m.showLogo {
		return splash.Render(m.width, m.height, m.modelName)
	}

	var b strings.Builder

	// ═══ 顶部状态栏 ═══
	b.WriteString(m.renderStatusBar())
	b.WriteString("\n")

	// ═══ 对话视口 或 选择器 ═══
	if m.selState != selNone {
		m.viewport.SetContent(m.renderSelector())
	} else {
		m.viewport.SetContent(m.renderConversation())
	}
	b.WriteString(m.viewport.View())
	b.WriteString("\n")

// ═══ 输入框上方分隔线 ═══
	b.WriteString(StyleSep.Render(strings.Repeat("─", m.width)))
	b.WriteString("\n")

	// ═══ 流式状态指示（仅流式中显示） ═══
	if m.state.Streaming {
		elapsed := time.Since(m.lastStart).Round(time.Millisecond * 100)
		b.WriteString(StyleStatus.Render(fmt.Sprintf("  ● receiving  %s", elapsed)))
		b.WriteString("\n")
	}

	// ═══ 建议面板 ═══
	if m.suggMode {
		if s := m.SuggEng.Suggest(m.textarea.Value()); len(s) > 0 {
			b.WriteString(renderSuggestions(s, m.suggIdx, m.suggOffset, m.width, m.textarea.Value()))
		}
	}

	// ═══ 确认面板 ═══
	if m.prompting {
		b.WriteString(m.renderPrompt())
	}

	// ═══ 审批面板（子包模块） ═══
	if m.ApproveMgr.Active {
		b.WriteString(m.ApproveMgr.View(m.width))
		b.WriteString("\n")
	}

	// ═══ 输入框（始终显示） ═══
	b.WriteString(StyleInputBox.Width(m.width - 2).Render(m.renderInputLine()))

	// ═══ 快捷栏 ═══
	b.WriteString("\n")
	b.WriteString(m.renderShortcuts())

	return b.String()
}

// ── 启动画面（艺术字居中） ──────────────────────────────────

// ── 输入框行（始终显示输入内容） ──────────────────────────

func (m Model) renderInputLine() string {
	cursor := " "
	if time.Now().UnixMilli()/500%2 == 0 {
		cursor = StyleInputPrompt.Render("▎")
	}
	return fmt.Sprintf("%s %s%s",
		StyleInputPrompt.Render(">"),
		m.textarea.Value(),
		cursor,
	)
}

// ── 对话渲染：从 Conversation Cell 树构建 ──────────────────

func (m Model) renderConversation() string {
	return m.state.Conv.Render(m.width)
}

// ── 状态栏 ─────────────────────────────────────────────────────

func (m Model) renderStatusBar() string {
	pf := string(m.client.ProviderFilter())
	if pf == "" {
		pf = "round-robin"
	}
	plugin := m.agt.Tools().ActivePlugin()
	tokens := tokensFromEngine(m.eng)
	elapsed := time.Since(m.lastStart).Round(time.Second)
	sid := m.eng.SessionID()

	var parts []string
	parts = append(parts, pf)
	if plugin != "" && plugin != "default" {
		parts = append(parts, plugin)
	}
	parts = append(parts, fmt.Sprintf("tok:%s", tokens))
	parts = append(parts, elapsed.String())
	if len(sid) > 8 {
		parts = append(parts, sid[len(sid)-8:])
	}

	right := strings.Join(parts, "  ")
	left := StyleBanner.Render(" ◆ Seele") + StyleMuted.Render(fmt.Sprintf("  %s", m.modelName))
	spacing := max(m.width-lipgloss.Width(left)-lipgloss.Width(right)-4, 1)
	return StyleStatus.Render(left + strings.Repeat(" ", spacing) + right)
}

// ── 任务面板 ───────────────────────────────────────────────────

// ── 选择器渲染（交互式列表） ───────────────────────────────

func (m Model) renderSelector() string {
	if len(m.selItems) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(StyleBanner.Render("  " + m.selTitle))
	b.WriteString("\n\n")
	maxShow := min(len(m.selItems), 20)
	for i := 0; i < maxShow; i++ {
		item := m.selItems[i]
		prefix := "  "
		sty := StyleSuggInactive
		if i == m.selIdx {
			prefix = " ▸ "
			sty = StyleSuggActive
		}
		num := StyleMuted.Render(fmt.Sprintf("%d.", i+1))
		line := StyleTaskItem.Render(fmt.Sprintf("%s%s %s", prefix, num, item.label))
		if item.desc != "" {
			line += "  " + StyleMuted.Render(item.desc)
		}
		b.WriteString(sty.Render(line))
		b.WriteString("\n")
	}
	b.WriteString(StyleMuted.Render("  ↑↓ Enter选择 Esc取消 数字键快捷跳转"))
	return b.String()
}

// ── 快捷栏 ─────────────────────────────────────────────────────

func (m Model) renderShortcuts() string {
	items := []string{
		"Ctrl+L", "/clear", "/model", "/sessions", "/help", "/exit",
	}
	var b strings.Builder
	for i, item := range items {
		if i > 0 {
			b.WriteString("  ")
		}
		b.WriteString(StyleShortcut.Render(item))
	}
	return StyleShortcutBar.Width(m.width).Render(b.String())
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

// ── 工具函数 ─────────────────────────────────────────────────

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
