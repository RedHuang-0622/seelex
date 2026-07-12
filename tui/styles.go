// Package tui 提供 Seelex 的 TUI 视图层
// 使用 Bubble Tea 框架，装配件模式组合各组件
package tui

import "github.com/charmbracelet/lipgloss"

// ── 初号机配色方案 ──────────────────────────────────────────────

var (
	// 角色样式
	StyleBanner    = lipgloss.NewStyle().Foreground(lipgloss.Color("#A78BFA")).Bold(true)
	StyleUser      = lipgloss.NewStyle().Foreground(lipgloss.Color("#C4B5FD")).Bold(true)
	StyleAssistant = lipgloss.NewStyle().Foreground(lipgloss.Color("#34D399")).Bold(true)
	StyleSystem    = lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280")).Italic(true)
	StyleError     = lipgloss.NewStyle().Foreground(lipgloss.Color("#F87171"))
	StyleToolCall  = lipgloss.NewStyle().Foreground(lipgloss.Color("#F59E0B")).Italic(true)  // 金色工具链
	StyleToolResult = lipgloss.NewStyle().Foreground(lipgloss.Color("#9CA3AF"))

	// 界面元素
	StyleStatus    = lipgloss.NewStyle().Foreground(lipgloss.Color("#9CA3AF"))
	StylePrompt    = lipgloss.NewStyle().Foreground(lipgloss.Color("#A78BFA"))
	StyleMuted     = lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280"))
	StyleStream    = lipgloss.NewStyle().Foreground(lipgloss.Color("#E5E7EB"))
	StyleSep       = lipgloss.NewStyle().Foreground(lipgloss.Color("#374151"))

	// 提示面板
	StyleSuggActive   = lipgloss.NewStyle().Background(lipgloss.Color("#7C3AED")).Foreground(lipgloss.Color("#FFFFFF"))
	StyleSuggInactive = lipgloss.NewStyle().Foreground(lipgloss.Color("#D1D5DB"))
	StyleSuggKind     = lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280")).Italic(true)

	// 会话 ID
	StyleSessionID = lipgloss.NewStyle().Foreground(lipgloss.Color("#34D399")).Bold(true)

	// 输入框
	StyleInputBox = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color("#4B5563")).
			Padding(0, 1).
			Foreground(lipgloss.Color("#E5E7EB"))
	StyleInputPrompt = lipgloss.NewStyle().Foreground(lipgloss.Color("#A78BFA")).Bold(true)
)
