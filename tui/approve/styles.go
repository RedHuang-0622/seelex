package approve

import "github.com/charmbracelet/lipgloss"

// ── 面板容器 ─────────────────────────────────────────────────────

var (
	BorderStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#7C3AED")).
			Padding(1, 2).
			Width(60)

	LowTitleStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#34D399")).
			Bold(true)

	MidTitleStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#F59E0B")).
			Bold(true)

	HighTitleStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#F87171")).
			Bold(true)
)

// ── 选项 ─────────────────────────────────────────────────────────

var (
	OptActive = lipgloss.NewStyle().
			Background(lipgloss.Color("#7C3AED")).
			Foreground(lipgloss.Color("#FFFFFF")).
			Padding(0, 1)

	OptInactive = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#D1D5DB"))

	OptDanger = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#F87171"))

	OptDone = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#34D399")).
		Bold(true)
)

// ── 描述 / 提示 ─────────────────────────────────────────────────

var (
	DescStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280")).Italic(true)
	HintStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280"))
	SepStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#4B5563"))
	PrevStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#9CA3AF")).Padding(0, 2)
)

// ── 超时进度条 ─────────────────────────────────────────────────

var (
	TimerStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("#F59E0B"))
	TimerUrgentStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#F87171")).Bold(true)
	ProgTrackStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#374151"))
	ProgFillStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#F59E0B"))
)

// TitleStyle 根据风险等级返回标题样式。
func TitleStyle(risk string) lipgloss.Style {
	switch risk {
	case "high":
		return HighTitleStyle
	case "medium":
		return MidTitleStyle
	default:
		return LowTitleStyle
	}
}
