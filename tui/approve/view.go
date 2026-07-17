package approve

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// View 渲染审批面板。width 为终端总宽度，返回渲染后的字符串。
func (m *Manager) View(width int) string {
	if m.State.Resolved {
		return renderDone(m.State)
	}
	return renderActive(m.State, width)
}

func renderActive(s State, termWidth int) string {
	var b strings.Builder

	// 标题行
	ts := TitleStyle(s.Risk)
	icon := "●"
	if s.Risk == "high" {
		icon = "⚠"
	}
	b.WriteString(ts.Render(fmt.Sprintf("  %s %s", icon, s.RiskLabel())))
	b.WriteString("\n\n")

	// 问题内容
	b.WriteString(renderContent(s.Question))
	b.WriteString("\n")

	// 预览行
	if s.Preview != "" && s.ToolName != "" {
		b.WriteString(PrevStyle.Render("  " + s.Preview))
		b.WriteString("\n")
	}

	// 分隔线
	b.WriteString(SepStyle.Render(strings.Repeat("─", panelWidth(s.Timeout)-4)))
	b.WriteString("\n\n")

	// 选项
	for i, opt := range s.Options {
		b.WriteString(renderOption(i, opt, s.Selected, false))
		b.WriteString("\n")
	}

	// 底部提示
	b.WriteString("\n")
	b.WriteString(renderFooter(s))

	return wrapPanel(b.String(), termWidth)
}

func renderDone(s State) string {
	resultText := "已确认"
	if s.Result == "__CANCEL__" || s.Result == "__TIMEOUT__" || s.Result == "deny" {
		resultText = "已取消"
	}
	return BorderStyle.Render(
		fmt.Sprintf("%s  %s — %s",
			OptDone.Render("✓"),
			s.Question,
			HintStyle.Render(resultText),
		),
	)
}

func renderContent(content string) string {
	lines := strings.Split(content, "\n")
	var b strings.Builder
	for _, line := range lines {
		b.WriteString("  ")
		b.WriteString(line)
		b.WriteString("\n")
	}
	return b.String()
}

func renderOption(idx int, opt ChoiceOption, selected int, resolved bool) string {
	num := fmt.Sprintf("%d.", idx+1)
	label := opt.Label
	if label == "" {
		label = opt.Key
	}

	var desc string
	if opt.Description != "" {
		desc = "  " + DescStyle.Render(opt.Description)
	}

	if resolved {
		line := fmt.Sprintf("    %s %s%s", num, label, desc)
		if idx == selected {
			return OptDone.Render("  ✓ " + line)
		}
		return OptInactive.Render(line)
	}

	isActive := idx == selected
	line := fmt.Sprintf("%s %s", num, label)

	switch opt.Style {
	case "danger":
		if isActive {
			return OptActive.Render(fmt.Sprintf("  ▸ %s", line)) + desc
		}
		return OptDanger.Render(fmt.Sprintf("    %s", line)) + desc
	default:
		if isActive {
			return OptActive.Render(fmt.Sprintf("  ▸ %s", line)) + desc
		}
		return OptInactive.Render(fmt.Sprintf("    %s", line)) + desc
	}
}

func renderFooter(s State) string {
	var hint strings.Builder
	hint.WriteString("  ↑↓ 选择  Enter 确认  Esc 取消")

	if s.Timeout > 0 {
		rem := s.Remaining()
		ts := TimerStyle
		if rem < 5*time.Second {
			ts = TimerUrgentStyle
		}
		hint.WriteString("  ")
		hint.WriteString(ts.Render(fmt.Sprintf("剩余 %s", s.FormatRemaining())))

		// 进度条
		ratio := s.TimeoutRatio()
		bw := 10
		filled := int(ratio * float64(bw))
		if filled > bw {
			filled = bw
		}
		hint.WriteString("  ")
		hint.WriteString(ProgTrackStyle.Render("["))
		hint.WriteString(ProgFillStyle.Render(strings.Repeat("█", filled)))
		hint.WriteString(ProgTrackStyle.Render(strings.Repeat("░", bw-filled)))
		hint.WriteString(ProgTrackStyle.Render("]"))
	}

	hint.WriteString("  数字键快捷选择")
	return HintStyle.Render(hint.String())
}

func wrapPanel(content string, termWidth int) string {
	pw := panelWidth(0)
	panel := BorderStyle.Width(pw).Render(content)
	pad := termWidth - lipgloss.Width(panel)
	if pad > 0 {
		return strings.Repeat(" ", pad/2) + panel
	}
	return panel
}

func panelWidth(_ time.Duration) int {
	return 64
}
