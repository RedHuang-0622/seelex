package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/RedHuang-0622/Seele/agent"
)

// ── 提示条目 ────────────────────────────────────────────────────

type suggestion struct {
	text        string // 补全文本（不含触发符）
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

func (se *suggestionEngine) SetSkills(skills []suggestion) {
	se.skills = skills
}

func (se *suggestionEngine) SetTools(tools []suggestion) {
	se.tools = tools
}

func (se *suggestionEngine) RefreshTools() {
	if se.agt == nil {
		return
	}
	tools := se.agt.VisibleTools(context.Background())
	se.tools = make([]suggestion, 0, len(tools))
	for _, t := range tools {
		se.tools = append(se.tools, suggestion{
			text: t.Function.Name, description: t.Function.Description, kind: "tool",
		})
	}
}

// Suggest 返回匹配输入前缀的建议列表
// /prefix → 命令 + Tool + Skill
// #prefix → Skill
func (se *suggestionEngine) Suggest(input string) []suggestion {
	switch {
	case strings.HasPrefix(input, "/"):
		return se.suggestCommand(strings.TrimPrefix(input, "/"))
	case strings.HasPrefix(input, "#"):
		return se.suggestSkill(strings.TrimPrefix(input, "#"))
	}
	return nil
}

// suggestCommand — 命令 + Tool + Skill 混合补全
func (se *suggestionEngine) suggestCommand(prefix string) []suggestion {
	var all []suggestion
	for _, c := range AllCommands() {
		all = append(all, suggestion{
			text: c.Name(), description: c.Description(), kind: "command",
		})
	}
	for _, s := range se.skills {
		all = append(all, suggestion{
			text: s.text, description: s.description, kind: s.kind,
		})
	}
	for _, t := range se.tools {
		all = append(all, suggestion{
			text: t.text, description: t.description, kind: t.kind,
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

// suggestSkill — # 触发：仅 Skill 补全
func (se *suggestionEngine) suggestSkill(prefix string) []suggestion {
	if prefix == "" {
		// 返回全部 skill
		res := make([]suggestion, len(se.skills))
		copy(res, se.skills)
		return res
	}
	lower := strings.ToLower(prefix)
	var filtered []suggestion
	for _, s := range se.skills {
		if strings.HasPrefix(strings.ToLower(s.text), lower) {
			filtered = append(filtered, s)
		}
	}
	return filtered
}

// ── 提示面板渲染（支持滑动窗口 + 自适应触发符） ────────────

const suggWindowSize = 8

// renderSuggestions 渲染建议列表
// input — 用户当前输入，用于推断触发符（/ 或 #）
func renderSuggestions(suggestions []suggestion, selected, offset int, width int, input string) string {
	n := len(suggestions)
	if n == 0 {
		return ""
	}
	// 推断触发符
	trigger := "/"
	if strings.HasPrefix(input, "#") {
		trigger = "#"
	}

	// 修正 offset，确保选中的在可见范围内
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
		line := fmt.Sprintf("%s%s%s  [%s]", prefix, trigger, s.text, s.kind)
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

	if end < n {
		b.WriteString(StyleMuted.Render(fmt.Sprintf("  ↓ %d more...", n-end)))
		b.WriteString("\n")
	}

	return b.String()
}

// ── 命令补全（导出供其他模块使用） ────────────────────────────

func commandSuggestions(prefix string) []suggestion {
	if prefix == "" {
		all := AllCommands()
		s := make([]suggestion, len(all))
		for i, c := range all {
			s[i] = suggestion{text: c.Name(), description: c.Description(), kind: "command"}
		}
		return s
	}
	lower := strings.ToLower(prefix)
	var res []suggestion
	for _, c := range AllCommands() {
		n := c.Name()
		if strings.HasPrefix(strings.ToLower(n), lower) {
			res = append(res, suggestion{text: n, description: c.Description(), kind: "command"})
		}
	}
	return res
}

// Compile guard
var _ = agent.Options{}
