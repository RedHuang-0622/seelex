// ── 建议补全面板渲染 ──────────────────────────────────────
//
// 渲染由 tui/sugg.Engine 返回的补全建议列表。
// 留在主包是因为使用共享样式（StyleMuted / StyleSuggActive 等）。

package tui

import (
	"fmt"
	"strings"

	"github.com/RedHuang-0622/seelex/tui/sugg"
)

const suggWindowSize = 8

// renderSuggestions 渲染建议列表。
// input — 用户当前输入，用于推断触发符（/ 或 #）。
func renderSuggestions(suggestions []sugg.Suggestion, selected, offset int, width int, input string) string {
	n := len(suggestions)
	if n == 0 {
		return ""
	}
	trigger := "/"
	if strings.HasPrefix(input, "#") {
		trigger = "#"
	}
	if selected < offset {
		offset = selected
	}
	if selected >= offset+suggWindowSize {
		offset = selected - suggWindowSize + 1
	}
	if offset+suggWindowSize > n {
		offset = n - suggWindowSize
	}
	if offset < 0 {
		offset = 0
	}
	end := offset + suggWindowSize
	if end > n {
		end = n
	}
	var b strings.Builder
	if offset > 0 {
		b.WriteString(StyleMuted.Render(fmt.Sprintf("  ↑ %d more...", offset)))
		b.WriteString("\n")
	}
	for i := offset; i < end; i++ {
		s := suggestions[i]
		prefix := "  "
		nameStyle := StyleSuggInactive
		if i == selected {
			prefix = "▸ "
			nameStyle = StyleSuggActive
		}
		line := fmt.Sprintf("%s%s%s  [%s]", prefix, trigger, s.Text, s.Kind)
		if s.Description != "" {
			desc := s.Description
			maxDesc := width - len(line) - 5
			if maxDesc < 10 {
				maxDesc = 10
			}
			if len(desc) > maxDesc {
				desc = desc[:maxDesc-3] + "..."
			}
			line += "  " + StyleSuggKind.Render(desc)
		}
		b.WriteString(nameStyle.Render(line))
		b.WriteString("\n")
	}
	if end < n {
		b.WriteString(StyleMuted.Render(fmt.Sprintf("  ↓ %d more...", n-end)))
		b.WriteString("\n")
	}
	return b.String()
}
