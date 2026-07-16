// Package splash 提供启动画面的渐变色艺术字渲染。
package splash

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var Gradient = lipgloss.JoinVertical(lipgloss.Left,
	lipgloss.NewStyle().Foreground(lipgloss.Color("#7C3AED")).Render(`███████╗███████╗███████╗██╗     ███████╗██╗  ██╗`),
	lipgloss.NewStyle().Foreground(lipgloss.Color("#8B5CF6")).Render(`██╔════╝██╔════╝██╔════╝██║     ██╔════╝╚██╗██╔╝`),
	lipgloss.NewStyle().Foreground(lipgloss.Color("#A78BFA")).Render(`███████╗█████╗  █████╗  ██║     █████╗   ╚███╔╝ `),
	lipgloss.NewStyle().Foreground(lipgloss.Color("#34D399")).Render(`╚════██║██╔══╝  ██╔══╝  ██║     ██╔══╝   ██╔██╗ `),
	lipgloss.NewStyle().Foreground(lipgloss.Color("#10B981")).Render(`███████║███████╗███████╗███████╗███████╗██╔╝ ██╗`),
	lipgloss.NewStyle().Foreground(lipgloss.Color("#059669")).Render(`╚══════╝╚══════╝╚══════╝╚══════╝╚══════╝╚═╝  ╚═╝`),
)

// Render 返回垂直水平居中的启动画面。
func Render(width, height int, modelName string) string {
	logo := Gradient
	logoLines := strings.Count(logo, "\n") + 1
	totalLines := logoLines + 2 + 1
	vPad := (height - totalLines) / 2
	if vPad < 1 {
		vPad = 1
	}

	var b strings.Builder
	b.WriteString(strings.Repeat("\n", vPad))
	for _, line := range strings.Split(logo, "\n") {
		pad := (width - lipgloss.Width(line)) / 2
		if pad < 0 {
			pad = 0
		}
		b.WriteString(strings.Repeat(" ", pad))
		b.WriteString(line)
		b.WriteString("\n")
	}

	modelLine := mutedStyle.Render(fmt.Sprintf("  %s", modelName))
	mp := (width - lipgloss.Width(modelLine)) / 2
	if mp < 0 {
		mp = 0
	}
	b.WriteString(strings.Repeat(" ", mp))
	b.WriteString(modelLine)
	b.WriteString("\n\n")

	hint := mutedStyle.Render("  enter to start")
	hp := (width - lipgloss.Width(hint)) / 2
	if hp < 0 {
		hp = 0
	}
	b.WriteString(strings.Repeat(" ", hp))
	b.WriteString(hint)

	return b.String()
}

var mutedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280"))
