// ── 审批组件样式 ──────────────────────────────────────────
//
// 风险等级配色方案（调研文档 §5.4 "approve_styles.go"）

package tui

import "github.com/charmbracelet/lipgloss"

// ── 审批面板容器 ──────────────────────────────────────────────

var (
	// StyleApproveBorder 审批面板边框
	StyleApproveBorder = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("#7C3AED")).
				Padding(1, 2).
				Width(60)

	// StyleApproveLow 低风险标题
	StyleApproveLow = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#34D399")).
			Bold(true)

	// StyleApproveMedium 中风险标题
	StyleApproveMedium = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#F59E0B")).
				Bold(true)

	// StyleApproveHigh 高风险标题
	StyleApproveHigh = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#F87171")).
				Bold(true)
)

// ── 选项样式 ──────────────────────────────────────────────────

var (
	// StyleApproveOptActive 选中的选项（紫色高亮）
	StyleApproveOptActive = lipgloss.NewStyle().
				Background(lipgloss.Color("#7C3AED")).
				Foreground(lipgloss.Color("#FFFFFF")).
				Padding(0, 1)

	// StyleApproveOptInactive 未选中的选项
	StyleApproveOptInactive = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#D1D5DB"))

	// StyleApproveOptDanger 危险选项（红色）
	StyleApproveOptDanger = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#F87171"))

	// StyleApproveOptDone 已确认（绿色）
	StyleApproveOptDone = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#34D399")).
				Bold(true)
)

// ── 描述文字 ──────────────────────────────────────────────────

var (
	// StyleApproveDesc 选项描述
	StyleApproveDesc = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#6B7280")).
				Italic(true)

	// StyleApprovePreview 预览内容
	StyleApprovePreview = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#9CA3AF")).
				Padding(0, 2)

	// StyleApproveHint 底部提示文字
	StyleApproveHint = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#6B7280"))
)

// ── 超时 ──────────────────────────────────────────────────────

var (
	// StyleApproveTimer 超时计时器
	StyleApproveTimer = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#F59E0B"))

	// StyleApproveTimerUrgent 紧急超时（< 5s）
	StyleApproveTimerUrgent = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#F87171")).
				Bold(true)

	// StyleApproveProgress 超时进度条轨道
	StyleApproveProgress = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#374151"))

	// StyleApproveProgressFill 超时进度条填充
	StyleApproveProgressFill = lipgloss.NewStyle().
					Foreground(lipgloss.Color("#F59E0B"))
)

// ── 分隔线 ────────────────────────────────────────────────────

var (
	StyleApproveSep = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#4B5563"))
)

// ── 风险颜色映射 ─────────────────────────────────────────

func approveTitleStyle(risk string) lipgloss.Style {
	switch risk {
	case "high":
		return StyleApproveHigh
	case "medium":
		return StyleApproveMedium
	default:
		return StyleApproveLow
	}
}
