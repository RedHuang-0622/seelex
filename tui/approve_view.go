// ── 审批组件视图渲染 ──────────────────────────────────────
//
// 渲染效果（调研文档 §5.4）：
//
//	╭──────────────────────────────────────────╮
//	│  ⚠ 高危操作                              │
//	│                                          │
//	│  即将执行：deploy-production              │
//	│                                          │
//	│  ▸ 1. 允许执行   放行此操作               │
//	│    2. 始终允许   记住此选择               │
//	│    3. 拒绝       禁止执行                 │
//	│                                          │
//	│  ↑↓ 选择 Enter确认 Esc取消  剩余 28s     │
//	╰──────────────────────────────────────────╯

package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// ── 主渲染入口 ───────────────────────────────────────────────

// renderApprove 渲染完整的审批面板（卡片式）。
func (m Model) renderApprove() string {
	a := m.approve
	if a.resolved {
		return m.renderApproveDone()
	}

	var b strings.Builder

	// ─ 标题行（风险等级颜色） ─
	titleStyle := approveTitleStyle(a.risk)
	riskIcon := "●"
	if a.risk == "high" {
		riskIcon = "⚠"
	}
	title := fmt.Sprintf("  %s %s", riskIcon, a.riskLabel())
	b.WriteString(titleStyle.Render(title))
	b.WriteString("\n\n")

	// ─ 问题内容 ─
	b.WriteString(renderApproveContent(a.question, a.width()))
	b.WriteString("\n")

	// ─ 预览行（Permission Gate 场景） ─
	if a.preview != "" && a.toolName != "" {
		b.WriteString(StyleApprovePreview.Render("  " + a.preview))
		b.WriteString("\n")
	}

	// ─ 分隔线 ─
	b.WriteString(StyleApproveSep.Render(strings.Repeat("─", a.width()-4)))
	b.WriteString("\n\n")

	// ─ 选项列表 ─
	for i, opt := range a.options {
		b.WriteString(renderApproveOption(i, opt, a.selected, a.resolved))
		b.WriteString("\n")
	}

	// ─ 底部提示行 ─
	b.WriteString("\n")
	b.WriteString(renderApproveFooter(a))

	return wrapApprovePanel(b.String(), a.width())
}

// renderApproveOption 渲染单个选项。
func renderApproveOption(idx int, opt approveOpt, selected int, resolved bool) string {
	prefix := "  "
	num := fmt.Sprintf("%d.", idx+1)

	var label, desc string
	if opt.Label != "" {
		label = opt.Label
	} else {
		label = opt.Key
	}
	if opt.Description != "" {
		desc = "  " + StyleApproveDesc.Render(opt.Description)
	}

	line := fmt.Sprintf("%s %s %s%s", prefix, num, label, desc)

	if resolved {
		if idx == selected {
			return StyleApproveOptDone.Render("  ✓ " + line)
		}
		return StyleApproveOptInactive.Render(line)
	}

	isActive := idx == selected

	switch opt.Style {
	case "danger":
		if isActive {
			return StyleApproveOptActive.Render(fmt.Sprintf("  ▸ %s %s", num, label)) + desc
		}
		return StyleApproveOptDanger.Render(fmt.Sprintf("    %s %s", num, label)) + desc
	case "primary", "warning":
		if isActive {
			return StyleApproveOptActive.Render(fmt.Sprintf("  ▸ %s %s", num, label)) + desc
		}
		return StyleApproveOptInactive.Render(fmt.Sprintf("    %s %s", num, label)) + desc
	default:
		if isActive {
			return StyleApproveOptActive.Render(fmt.Sprintf("  ▸ %s %s", num, label)) + desc
		}
		return StyleApproveOptInactive.Render(fmt.Sprintf("    %s %s", num, label)) + desc
	}
}

// renderApproveContent 渲染问题内容（支持多行）。
func renderApproveContent(content string, width int) string {
	lines := strings.Split(content, "\n")
	var b strings.Builder
	for _, line := range lines {
		b.WriteString("  ")
		b.WriteString(line)
		b.WriteString("\n")
	}
	return b.String()
}

// renderApproveFooter 渲染底部提示行（含超时倒计时）。
func renderApproveFooter(a approveState) string {
	var hint strings.Builder
	hint.WriteString("  ↑↓ 选择  Enter 确认  Esc 取消")

	if a.timeout > 0 {
		rem := a.remaining()
		timerStyle := StyleApproveTimer
		if rem < 5*time.Second {
			timerStyle = StyleApproveTimerUrgent
		}
		hint.WriteString("  ")
		hint.WriteString(timerStyle.Render(fmt.Sprintf("剩余 %s", a.formatRemaining())))

		// 进度条
		ratio := a.timeoutRatio()
		barWidth := 10
		filled := int(ratio * float64(barWidth))
		if filled > barWidth {
			filled = barWidth
		}
		hint.WriteString("  ")
		hint.WriteString(StyleApproveProgress.Render("["))
		hint.WriteString(StyleApproveProgressFill.Render(strings.Repeat("█", filled)))
		hint.WriteString(StyleApproveProgress.Render(strings.Repeat("░", barWidth-filled)))
		hint.WriteString(StyleApproveProgress.Render("]"))
	}

	hint.WriteString("  数字键快捷选择")
	return StyleApproveHint.Render(hint.String())
}

// renderApproveDone 渲染已解决的审批面板。
func (m Model) renderApproveDone() string {
	a := m.approve
	var b strings.Builder

	resultText := "已确认"
	if a.result == "__CANCEL__" || a.result == "__TIMEOUT__" || a.result == "deny" {
		resultText = "已取消"
	}

	b.WriteString(StyleApproveBorder.Render(
		fmt.Sprintf("%s  %s — %s",
			StyleApproveOptDone.Render("✓"),
			a.question,
			StyleMuted.Render(resultText),
		),
	))
	return b.String()
}

// wrapApprovePanel 将审批内容放入圆角边框面板中。
func wrapApprovePanel(content string, width int) string {
	panelWidth := width - 4
	if panelWidth > 64 {
		panelWidth = 64
	}
	if panelWidth < 40 {
		panelWidth = 40
	}

	// 使用 StyleApproveBorder 包裹内容
	panel := StyleApproveBorder.
		Width(panelWidth).
		Render(content)

	// 居中显示
	padLeft := max(0, (width-lipgloss.Width(panel))/2)
	if padLeft > 0 {
		return strings.Repeat(" ", padLeft) + panel
	}
	return panel
}

// approveWidth 获取审批面板可用宽度。
func (a *approveState) width() int {
	return 60 // 默认宽度，实际渲染时由 wrapApprovePanel 调整
}
