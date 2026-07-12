package tui

import (
	"fmt"
	"strings"

	"github.com/RedHuang-0622/Seele/agent"
)

// ── 提示条目 ────────────────────────────────────────────────────

type suggestion struct {
	text        string // 补全文本（不含 / @ #）
	description string
	kind        string // "command" | "tool" | "skill"
}

// ── 提示引擎 ────────────────────────────────────────────────────

type suggestionEngine struct {
	agt    *agent.Agent
	skills []suggestion
}

func newSuggestionEngine(agt *agent.Agent) *suggestionEngine {
	return &suggestionEngine{agt: agt}
}

func (se *suggestionEngine) SetSkills(skills []suggestion) {
	se.skills = skills
}

// Suggest 返回匹配输入前缀的建议列表
// 输入格式: /prefix  → 命令/skill 补全
func (se *suggestionEngine) Suggest(input string) []suggestion {
	if !strings.HasPrefix(input, "/") {
		return nil
	}
	prefix := strings.TrimPrefix(input, "/")

	// 收集所有可补全项
	var all []suggestion
	// 命令
	for _, c := range AllCommands() {
		all = append(all, suggestion{
			text: c.Name(), description: c.Description(), kind: "command",
		})
	}
	// Skill
	for _, s := range se.skills {
		all = append(all, suggestion{
			text: s.text, description: s.description, kind: s.kind,
		})
	}

	if prefix == "" {
		return all
	}

	lower := strings.ToLower(prefix)
	var filtered []suggestion
	for _, s := range all {
		if strings.HasPrefix(strings.ToLower(s.text), lower) {
			filtered = append(filtered, s)
		}
	}
	return filtered
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
		line := fmt.Sprintf("%s/%s  [%s]", prefix, s.text, s.kind)
		if s.description != "" {
			desc := s.description
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
	return b.String()
}

// Compile guard
var _ = agent.Options{}
