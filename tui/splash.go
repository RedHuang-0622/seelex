// ── 启动画面：渐变色艺术字 ─────────────────────────────────
//
// 首条消息发送前全屏居中显示

package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// ── 渐变色 SEELEX 艺术字 ──────────────────────────────────────

var gradientSeelex = lipgloss.JoinVertical(lipgloss.Left,
	lipgloss.NewStyle().Foreground(lipgloss.Color("#7C3AED")).Render(`███████╗███████╗███████╗██╗     ███████╗██╗  ██╗`),
	lipgloss.NewStyle().Foreground(lipgloss.Color("#8B5CF6")).Render(`██╔════╝██╔════╝██╔════╝██║     ██╔════╝╚██╗██╔╝`),
	lipgloss.NewStyle().Foreground(lipgloss.Color("#A78BFA")).Render(`███████╗█████╗  █████╗  ██║     █████╗   ╚███╔╝ `),
	lipgloss.NewStyle().Foreground(lipgloss.Color("#34D399")).Render(`╚════██║██╔══╝  ██╔══╝  ██║     ██╔══╝   ██╔██╗ `),
	lipgloss.NewStyle().Foreground(lipgloss.Color("#10B981")).Render(`███████║███████╗███████╗███████╗███████╗██╔╝ ██╗`),
	lipgloss.NewStyle().Foreground(lipgloss.Color("#059669")).Render(`╚══════╝╚══════╝╚══════╝╚══════╝╚══════╝╚═╝  ╚═╝`),
)

// ── 启动画面渲染（垂直水平居中） ──────────────────────────

func (m Model) renderSplash() string {
	logo := gradientSeelex
	logoLines := strings.Count(logo, "\n") + 1
	infoLines := 2
	totalLines := logoLines + infoLines + 1
	vPad := (m.height - totalLines) / 2
	if vPad < 1 {
		vPad = 1
	}

	var b strings.Builder
	for i := 0; i < vPad; i++ {
		b.WriteString("\n")
	}
	for _, line := range strings.Split(logo, "\n") {
		padding := (m.width - lipgloss.Width(line)) / 2
		if padding < 0 {
			padding = 0
		}
		b.WriteString(strings.Repeat(" ", padding))
		b.WriteString(line)
		b.WriteString("\n")
	}

	modelLine := StyleMuted.Render(fmt.Sprintf("  %s", m.modelName))
	mp := (m.width - lipgloss.Width(modelLine)) / 2
	if mp < 0 {
		mp = 0
	}
	b.WriteString(strings.Repeat(" ", mp))
	b.WriteString(modelLine)
	b.WriteString("\n\n")

	hint := StyleMuted.Render("  enter to start")
	hp := (m.width - lipgloss.Width(hint)) / 2
	if hp < 0 {
		hp = 0
	}
	b.WriteString(strings.Repeat(" ", hp))
	b.WriteString(hint)

	return b.String()
}
