package main

import "github.com/charmbracelet/lipgloss"

// ── 全局样式定义 ─────────────────────────────────────────────────────

var (
	// 角色样式
	styleBanner    = lipgloss.NewStyle().Foreground(lipgloss.Color("#7C3AED")).Bold(true)
	styleUser      = lipgloss.NewStyle().Foreground(lipgloss.Color("#7C3AED")).Bold(true)
	styleAssistant = lipgloss.NewStyle().Foreground(lipgloss.Color("#10B981")).Bold(true)
	styleSystem    = lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280")).Italic(true)
	styleError     = lipgloss.NewStyle().Foreground(lipgloss.Color("#EF4444"))

	// 界面元素样式
	styleStatus  = lipgloss.NewStyle().Foreground(lipgloss.Color("#9CA3AF"))
	stylePrompt  = lipgloss.NewStyle().Foreground(lipgloss.Color("#7C3AED"))
	styleMuted   = lipgloss.NewStyle().Foreground(lipgloss.Color("#4B5563"))
	styleStream  = lipgloss.NewStyle().Foreground(lipgloss.Color("#D1D5DB"))
	styleSep     = lipgloss.NewStyle().Foreground(lipgloss.Color("#374151"))

	// 提示面板样式
	styleSuggBox    = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("#7C3AED"))
	styleSuggActive = lipgloss.NewStyle().Background(lipgloss.Color("#7C3AED")).Foreground(lipgloss.Color("#FFFFFF"))
	styleSuggInactive = lipgloss.NewStyle().Foreground(lipgloss.Color("#D1D5DB"))
	styleSuggKind   = lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280")).Italic(true)

	// 会话 ID 样式
	styleSessionID = lipgloss.NewStyle().Foreground(lipgloss.Color("#F59E0B")).Bold(true)
)
