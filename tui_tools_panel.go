package main

import (
	"fmt"
	"strings"

	"github.com/RedHuang-0622/Seele/agent"
)

// ── 提示条目 ────────────────────────────────────────────────────

// suggestion 表示一个提示建议条目（命令/工具/Skill）
type suggestion struct {
	text        string
	description string
	kind        string // "command" | "tool" | "skill"
}

// ── 提示引擎 ────────────────────────────────────────────────────

// suggestionEngine 管理提示列表的生成和过滤
type suggestionEngine struct {
	agt    *agent.Agent
	skills []suggestion // 预加载的 skill 列表
	tools  []suggestion // 缓存的工具列表
}

func newSuggestionEngine(agt *agent.Agent) *suggestionEngine {
	return &suggestionEngine{agt: agt}
}

// RefreshTools 刷新工具列表缓存
func (se *suggestionEngine) RefreshTools() {
	tools := se.agt.VisibleTools(nil)
	se.tools = make([]suggestion, 0, len(tools))
	for _, t := range tools {
		se.tools = append(se.tools, suggestion{
			text:        "@" + t.Function.Name,
			description: t.Function.Description,
			kind:        "tool",
		})
	}
}

// SetSkills 设置 skill 列表
func (se *suggestionEngine) SetSkills(skills []suggestion) {
	se.skills = skills
}

// Suggest 根据前缀返回匹配的提示列表
func (se *suggestionEngine) Suggest(prefix string) []suggestion {
	if prefix == "" {
		return nil
	}

	var result []suggestion

	switch {
	case strings.HasPrefix(prefix, "/"):
		// 命令提示
		result = commandSuggestions(strings.TrimPrefix(prefix, "/"))

	case strings.HasPrefix(prefix, "@"):
		// 工具提示
		query := strings.ToLower(strings.TrimPrefix(prefix, "@"))
		for _, t := range se.tools {
			name := strings.TrimPrefix(t.text, "@")
			if query == "" || strings.HasPrefix(strings.ToLower(name), query) {
				result = append(result, t)
			}
		}

	case strings.HasPrefix(prefix, "#"):
		// Skill 提示
		query := strings.ToLower(strings.TrimPrefix(prefix, "#"))
		for _, s := range se.skills {
			name := strings.TrimPrefix(s.text, "#")
			if query == "" || strings.HasPrefix(strings.ToLower(name), query) {
				result = append(result, s)
			}
		}
	}

	return result
}

// ── 提示面板渲染 ──────────────────────────────────────────────

// renderSuggestions 渲染提示面板（显示在输入框上方）
func renderSuggestions(suggestions []suggestion, selected int, width int) string {
	if len(suggestions) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("\n")

	maxShow := 8
	if len(suggestions) < maxShow {
		maxShow = len(suggestions)
	}

	for i := 0; i < maxShow; i++ {
		s := suggestions[i]
		prefix := "  "
		nameStyle := styleSuggInactive
		if i == selected {
			prefix = "▸ "
			nameStyle = styleSuggActive
		}

		kindTag := fmt.Sprintf("[%s]", s.kind)
		line := fmt.Sprintf("%s%s %s %s", prefix, nameStyle, kindTag, s.text)
		if s.description != "" {
			desc := s.description
			if len(desc) > width-len(line)-5 {
				desc = desc[:width-len(line)-8] + "..."
			}
			line += "  " + desc
		}
		b.WriteString(line)
		b.WriteString("\n")
	}

	if len(suggestions) > maxShow {
		b.WriteString(styleMuted.Render(fmt.Sprintf("  ...还有 %d 个", len(suggestions)-maxShow)))
		b.WriteString("\n")
	}

	return b.String()
}

// ── 工具名补全 ───────────────────────────────────────────────

// toolSuggestions 返回匹配前缀的工具列表
func toolSuggestions(agt *agent.Agent, prefix string) []suggestion {
	tools := agt.VisibleTools(nil)
	var result []suggestion
	for _, t := range tools {
		if prefix == "" || strings.HasPrefix(strings.ToLower(t.Function.Name), strings.ToLower(prefix)) {
			result = append(result, suggestion{
				text:        "@" + t.Function.Name,
				description: t.Function.Description,
				kind:        "tool",
			})
		}
	}
	return result
}
