package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/RedHuang-0622/seelex/application"
)

// ── Plan 面板图标 ────────────────────────────────────────────

var planNodeIcons = map[application.NodeStatus]string{
	application.NodePending:   "○",
	application.NodeRunning:   "▶",
	application.NodeCompleted: "✓",
	application.NodeFailed:    "✗",
	application.NodeAborted:   "⊘",
	application.NodeSkipped:   "–",
}

var planNodeColors = map[application.NodeStatus]lipgloss.Color{
	application.NodePending:   lipgloss.Color("240"),  // 灰
	application.NodeRunning:   lipgloss.Color("220"),  // 金
	application.NodeCompleted: lipgloss.Color("76"),   // 绿
	application.NodeFailed:    lipgloss.Color("196"),  // 红
	application.NodeAborted:   lipgloss.Color("124"),  // 暗红
	application.NodeSkipped:   lipgloss.Color("242"),  // 浅灰
}

var planStatusIcons = map[application.PlanStatus]string{
	application.PlanPending:   "○",
	application.PlanRunning:   "◉",
	application.PlanCompleted: "●",
	application.PlanFailed:    "●",
	application.PlanAborted:   "●",
}

func planNodeIcon(status application.NodeStatus) string {
	if icon, ok := planNodeIcons[status]; ok {
		return icon
	}
	return "?"
}

func planNodeColor(status application.NodeStatus) lipgloss.Color {
	if c, ok := planNodeColors[status]; ok {
		return c
	}
	return lipgloss.Color("240")
}

func planStatusIcon(status application.PlanStatus) string {
	if icon, ok := planStatusIcons[status]; ok {
		return icon
	}
	return "○"
}

// progressBar 渲染一个水平进度条 [████░░░░]。
func progressBar(fraction float64, width int) string {
	if width < 2 {
		width = 2
	}
	filled := int(fraction * float64(width))
	if filled > width {
		filled = width
	}
	if fraction > 0 && filled == 0 {
		filled = 1
	}
	return "[" + strings.Repeat("█", filled) + strings.Repeat("░", width-filled) + "]"
}

// PlanPanel 根据 effort 渲染 Plan 面板。无活跃 Plan 时返回空字符串。
func PlanPanel(plan *application.PlanState, effort string, termWidth int) string {
	if plan == nil {
		return ""
	}
	switch effort {
	case "lite":
		return renderPlanLite(plan, termWidth)
	case "medium":
		return renderPlanMedium(plan, termWidth)
	case "high":
		return renderPlanHigh(plan, termWidth)
	case "max":
		return renderPlanMax(plan, termWidth)
	default:
		return renderPlanMedium(plan, termWidth)
	}
}

// PlanPanelHeight 返回 plan 面板占用的行数（用于 convHeight 计算）。
func PlanPanelHeight(plan *application.PlanState, effort string) int {
	if plan == nil {
		return 0
	}
	switch effort {
	case "lite":
		return 1
	case "medium":
		h := 3 + len(plan.Nodes)
		if h > 18 {
			h = 18
		}
		return h
	case "high", "max":
		h := 2 + len(plan.Nodes) + 1
		if h > 30 {
			h = 30
		}
		return h
	default:
		return 0
	}
}

// ── Lite ─────────────────────────────────────────────────────
// 单行：◉ plan: step-name  [████░░░░  40%]  5.2s
func renderPlanLite(p *application.PlanState, _ int) string {
	var running string
	for _, n := range p.Nodes {
		if n.Status == application.NodeRunning {
			running = n.Label
			break
		}
	}
	if running == "" && len(p.Nodes) > 0 {
		running = p.Nodes[0].Label
	}
	icon := planStatusIcon(p.Status)
	bar := progressBar(p.Progress, 12)
	pct := int(p.Progress * 100)
	elapsed := p.Elapsed
	if elapsed == "" {
		elapsed = "—"
	}
	style := lipgloss.NewStyle().Foreground(lipgloss.Color("250"))
	return style.Render(fmt.Sprintf(" %s plan: %s  %s  %d%%  %s", icon, running, bar, pct, elapsed))
}

// ── Medium ───────────────────────────────────────────────────
// 标题 + 节点列表（紧凑打点表）
func renderPlanMedium(p *application.PlanState, width int) string {
	done := completedCount(p.Nodes)
	total := len(p.Nodes)
	var b strings.Builder

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("250"))
	icon := planStatusIcon(p.Status)
	b.WriteString(titleStyle.Render(fmt.Sprintf(" %s Plan: %s (%d/%d)", icon, p.Name, done, total)))
	if p.Elapsed != "" {
		b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("242")).Render("  " + p.Elapsed))
	}
	b.WriteString("\n")

	sepWidth := min(width-2, 60)
	sep := lipgloss.NewStyle().Foreground(lipgloss.Color("237"))
	b.WriteString(sep.Render(" " + strings.Repeat("─", sepWidth)))
	b.WriteString("\n")

	for _, n := range p.Nodes {
		icon := planNodeIcon(n.Status)
		color := planNodeColor(n.Status)
		elapsed := n.Elapsed
		if elapsed == "" {
			elapsed = "—"
		}
		nodeStyle := lipgloss.NewStyle().Foreground(color)
		b.WriteString(fmt.Sprintf("  %s %s  %s\n",
			nodeStyle.Render(icon),
			nodeStyle.Render(n.Label),
			lipgloss.NewStyle().Foreground(lipgloss.Color("242")).Render(elapsed)))
	}

	bar := progressBar(p.Progress, min(sepWidth, 40))
	b.WriteString(sep.Render(" " + bar))
	return b.String()
}

// ── High ─────────────────────────────────────────────────────
// 带缩进的节点树 + 状态列
func renderPlanHigh(p *application.PlanState, width int) string {
	done := completedCount(p.Nodes)
	total := len(p.Nodes)
	var b strings.Builder

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("250"))
	icon := planStatusIcon(p.Status)
	b.WriteString(titleStyle.Render(fmt.Sprintf(" %s Plan: %s (%d/%d)", icon, p.Name, done, total)))
	if p.Elapsed != "" {
		b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("242")).Render("  " + p.Elapsed))
	}
	b.WriteString("\n")

	sepWidth := min(width-2, 70)
	sep := lipgloss.NewStyle().Foreground(lipgloss.Color("237"))
	b.WriteString(sep.Render(" " + strings.Repeat("─", sepWidth)))
	b.WriteString("\n")

	for i, n := range p.Nodes {
		indent := ""
		if n.Depth > 0 {
			indent = strings.Repeat("  ", n.Depth)
		}
		icon := planNodeIcon(n.Status)
		color := planNodeColor(n.Status)
		elapsed := n.Elapsed
		if elapsed == "" {
			elapsed = "—"
		}
		nodeStyle := lipgloss.NewStyle().Foreground(color)
		b.WriteString(fmt.Sprintf("  %s%s %s  %s\n",
			indent,
			nodeStyle.Render(icon),
			nodeStyle.Render(n.Label),
			lipgloss.NewStyle().Foreground(lipgloss.Color("242")).Render(elapsed)))

		for _, child := range p.Nodes[i].Children {
			cIndent := "  │  "
			if n.Depth > 0 {
				cIndent = indent + "  │  "
			}
			cIcon := planNodeIcon(child.Status)
			cColor := planNodeColor(child.Status)
			cElapsed := child.Elapsed
			if cElapsed == "" {
				cElapsed = "—"
			}
			cStyle := lipgloss.NewStyle().Foreground(cColor)
			b.WriteString(fmt.Sprintf("  %s%s %s  %s\n",
				cIndent,
				cStyle.Render(cIcon),
				cStyle.Render(child.Label),
				lipgloss.NewStyle().Foreground(lipgloss.Color("242")).Render(cElapsed)))
		}
	}

	bar := progressBar(p.Progress, min(sepWidth, 40))
	b.WriteString(sep.Render(" " + bar))
	return b.String()
}

// ── Max ──────────────────────────────────────────────────────
// 全宽表格风格，带边框、token、重试统计
func renderPlanMax(p *application.PlanState, width int) string {
	done := completedCount(p.Nodes)
	total := len(p.Nodes)
	var b strings.Builder

	border := lipgloss.NewStyle().Foreground(lipgloss.Color("237"))
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("250"))

	b.WriteString(border.Render(fmt.Sprintf(" ╔═ %s Plan: %s ═══════════════════════════╗",
		planStatusIcon(p.Status), p.Name)))
	b.WriteString("\n")
	b.WriteString(titleStyle.Render(fmt.Sprintf(" ║  %d/%d nodes completed", done, total)))
	if p.Elapsed != "" {
		b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("242")).
			Render(fmt.Sprintf("  ─  %s", p.Elapsed)))
	}
	b.WriteString("\n")

	b.WriteString(border.Render(fmt.Sprintf(" ╟─ %s ──────────────────────────────────╢",
		strings.Repeat("─", min(50, width-30)))))
	b.WriteString("\n")
	b.WriteString(fmt.Sprintf(" ║  %-24s %-7s %-8s║\n", "Step", "Status", "Time"))

	for _, n := range p.Nodes {
		icon := planNodeIcon(n.Status)
		color := planNodeColor(n.Status)
		elapsed := n.Elapsed
		if elapsed == "" {
			elapsed = "—"
		}
		nodeStyle := lipgloss.NewStyle().Foreground(color)
		indent := ""
		if n.Depth > 0 {
			indent = strings.Repeat(" ", n.Depth*2)
		}
		b.WriteString(fmt.Sprintf(" ║  %s%s %-20s %-7s %-8s║\n",
			indent,
			nodeStyle.Render(icon),
			nodeStyle.Render(n.Label),
			styleStatusText(string(n.Status)).Render(string(n.Status)),
			elapsed))

		for _, child := range n.Children {
			cIcon := planNodeIcon(child.Status)
			cColor := planNodeColor(child.Status)
			cElapsed := child.Elapsed
			if cElapsed == "" {
				cElapsed = "—"
			}
			cIndent := indent + "    "
			b.WriteString(fmt.Sprintf(" ║  %s│  %s %-16s %-7s %-8s║\n",
				cIndent,
				lipgloss.NewStyle().Foreground(cColor).Render(cIcon),
				lipgloss.NewStyle().Foreground(cColor).Render(child.Label),
				styleStatusText(string(child.Status)).Render(string(child.Status)),
				cElapsed))
		}
	}

	b.WriteString(border.Render(fmt.Sprintf(" ╚═%s╝", strings.Repeat("═", min(55, width-10)))))
	return b.String()
}

func styleStatusText(s string) lipgloss.Style {
	switch s {
	case "running":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("220"))
	case "completed":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("76"))
	case "failed":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	default:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	}
}

func completedCount(nodes []application.PlanNode) int {
	n := 0
	for _, node := range nodes {
		if node.Status == application.NodeCompleted || node.Status == application.NodeSkipped {
			n++
		}
	}
	return n
}
