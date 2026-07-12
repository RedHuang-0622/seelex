package tui

import "github.com/charmbracelet/lipgloss"

// ── 初号机配色 ──────────────────────────────────────────────────

var (
	// 角色
	StyleBanner    = lipgloss.NewStyle().Foreground(lipgloss.Color("#A78BFA")).Bold(true)
	StyleUser      = lipgloss.NewStyle().Foreground(lipgloss.Color("#C4B5FD")).Bold(true)
	StyleAssistant = lipgloss.NewStyle().Foreground(lipgloss.Color("#34D399")).Bold(true)
	StyleSystem    = lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280")).Italic(true)
	StyleError     = lipgloss.NewStyle().Foreground(lipgloss.Color("#F87171"))

	// 工具链
	StyleToolCall  = lipgloss.NewStyle().Foreground(lipgloss.Color("#F59E0B")).Italic(true)
	StyleToolResult = lipgloss.NewStyle().Foreground(lipgloss.Color("#9CA3AF"))

	// 红绿 diff（代码高亮变更）
	StyleDiffAdd = lipgloss.NewStyle().Foreground(lipgloss.Color("#34D399")) // 绿：新增
	StyleDiffDel = lipgloss.NewStyle().Foreground(lipgloss.Color("#F87171")) // 红：删除

	// 确认/选择
	StyleConfirm     = lipgloss.NewStyle().Foreground(lipgloss.Color("#F59E0B")).Bold(true)
	StyleChoiceActive = lipgloss.NewStyle().
				Background(lipgloss.Color("#7C3AED")).
				Foreground(lipgloss.Color("#FFFFFF"))
	StyleChoiceInactive = lipgloss.NewStyle().Foreground(lipgloss.Color("#D1D5DB"))

	// 提示面板（别名保持 tools_panel.go 兼容）
	StyleSuggActive   = StyleChoiceActive
	StyleSuggInactive = StyleChoiceInactive
	StyleSuggKind     = StyleMuted

	// 界面
	StyleStatus    = lipgloss.NewStyle().Foreground(lipgloss.Color("#9CA3AF"))
	StylePrompt    = lipgloss.NewStyle().Foreground(lipgloss.Color("#A78BFA"))
	StyleMuted     = lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280"))
	StyleStream    = lipgloss.NewStyle().Foreground(lipgloss.Color("#E5E7EB"))
	StyleSep       = lipgloss.NewStyle().Foreground(lipgloss.Color("#374151"))

	// 会话
	StyleSessionID = lipgloss.NewStyle().Foreground(lipgloss.Color("#34D399"))

	// 输入框
	StyleInputBox    = lipgloss.NewStyle().
				Border(lipgloss.NormalBorder()).
				BorderForeground(lipgloss.Color("#4B5563")).
				Padding(0, 1).
				Foreground(lipgloss.Color("#E5E7EB"))
	StyleInputPrompt = lipgloss.NewStyle().Foreground(lipgloss.Color("#A78BFA")).Bold(true)
)
