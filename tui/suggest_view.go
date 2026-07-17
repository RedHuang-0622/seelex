package tui

import (
	"fmt"
	"strings"

	"github.com/RedHuang-0622/seelex/application"
)

const suggWindowSize = 8

func renderSuggestions(suggestions []application.Suggestion, selected, offset, width int, input string) string {
	count := len(suggestions)
	if count == 0 {
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
	if offset+suggWindowSize > count {
		offset = count - suggWindowSize
	}
	if offset < 0 {
		offset = 0
	}
	end := min(offset+suggWindowSize, count)
	var builder strings.Builder
	if offset > 0 {
		builder.WriteString(StyleMuted.Render(fmt.Sprintf("  ↑ %d more...", offset)))
		builder.WriteString("\n")
	}
	for index := offset; index < end; index++ {
		suggestion := suggestions[index]
		prefix, style := "  ", StyleSuggInactive
		if index == selected {
			prefix, style = "▸ ", StyleSuggActive
		}
		line := fmt.Sprintf("%s%s%s  [%s]", prefix, trigger, suggestion.Text, suggestion.Kind)
		if suggestion.Description != "" {
			description := suggestion.Description
			maxDescription := max(width-len(line)-5, 10)
			if len(description) > maxDescription {
				description = description[:maxDescription-3] + "..."
			}
			line += "  " + StyleSuggKind.Render(description)
		}
		builder.WriteString(style.Render(line))
		builder.WriteString("\n")
	}
	if end < count {
		builder.WriteString(StyleMuted.Render(fmt.Sprintf("  ↓ %d more...", count-end)))
		builder.WriteString("\n")
	}
	return builder.String()
}
