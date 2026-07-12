package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/RedHuang-0622/Seele/agent"
)

// ── 提示条目 ────────────────────────────────────────────────────

type suggestion struct {
	text        string
	description string
	kind        string // "command" | "tool" | "skill"
}

// ── 提示引擎 ────────────────────────────────────────────────────

type suggestionEngine struct {
	agt    *agent.Agent
	skills []suggestion
	tools  []suggestion
}

func newSuggestionEngine(agt *agent.Agent) *suggestionEngine {
	return &suggestionEngine{agt: agt}
}

func (se *suggestionEngine) RefreshTools() {
	tools := se.agt.VisibleTools(context.TODO())
	se.tools = make([]suggestion, 0, len(tools))
	for _, t := range tools {
		se.tools = append(se.tools, suggestion{
			text:        "@" + t.Function.Name,
			description: t.Function.Description,
			kind:        "tool",
		})
	}
}

func (se *suggestionEngine) SetSkills(skills []suggestion) {
	se.skills = skills
}

func (se *suggestionEngine) Suggest(prefix string) []suggestion {
	if prefix == "" {
		return nil
	}
	var result []suggestion
	switch {
	case strings.HasPrefix(prefix, "/"):
		result = commandSuggestions(strings.TrimPrefix(prefix, "/"))
	case strings.HasPrefix(prefix, "@"):
		q := strings.ToLower(strings.TrimPrefix(prefix, "@"))
		for _, t := range se.tools {
			n := strings.TrimPrefix(t.text, "@")
			if q == "" || strings.HasPrefix(strings.ToLower(n), q) {
				result = append(result, t)
			}
		}
	case strings.HasPrefix(prefix, "#"):
		q := strings.ToLower(strings.TrimPrefix(prefix, "#"))
		for _, s := range se.skills {
			n := strings.TrimPrefix(s.text, "#")
			if q == "" || strings.HasPrefix(strings.ToLower(n), q) {
				result = append(result, s)
			}
		}
	}
	return result
}

// ── 提示面板渲染 ──────────────────────────────────────────────

func renderSuggestions(suggestions []suggestion, selected int, width int) string {
	if len(suggestions) == 0 {
		return ""
	}
	var b strings.Builder
	maxShow := min(len(suggestions), 8)
	for i := 0; i < maxShow; i++ {
		s := suggestions[i]
		prefix := "  "
		nameStyle := StyleSuggInactive
		if i == selected {
			prefix = "▸ "
			nameStyle = StyleSuggActive
		}
		line := fmt.Sprintf("%s%s [%s]", prefix, s.text, s.kind)
		if s.description != "" {
			desc := s.description
			maxDesc := width - len(line) - 5
			if maxDesc < 10 {
				maxDesc = 10
			}
			if len(desc) > maxDesc {
				desc = desc[:maxDesc-3] + "..."
			}
			line += fmt.Sprintf("  %s", StyleSuggKind.Render(desc))
		}
		b.WriteString(nameStyle.Render(line))
		b.WriteString("\n")
	}
	return b.String()
}
