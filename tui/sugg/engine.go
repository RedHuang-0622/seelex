// Package sugg 提供提示补全引擎。
package sugg

import (
	"context"
	"strings"

	"github.com/RedHuang-0622/seelex/seelebridge"
)

type ToolSource interface {
	VisibleTools(ctx context.Context) []seelebridge.Tool
}

// Suggestion 表示一个补全条目。
type Suggestion struct {
	Text        string
	Description string
	Kind        string // "command" | "tool" | "skill"
}

// Engine 是提示补全引擎，由主 Model 持有。
type Engine struct {
	source ToolSource
	skills []Suggestion
	tools  []Suggestion
	cmds   []Suggestion
}

// NewEngine 创建提示补全引擎。
func NewEngine(source ToolSource) *Engine {
	return &Engine{source: source}
}

// SetSkills 设置 Skill 补全列表。
func (e *Engine) SetSkills(skills []Suggestion) {
	e.skills = skills
}

// SetTools 设置工具补全列表。
func (e *Engine) SetTools(tools []Suggestion) {
	e.tools = tools
}

// SetCommands 设置命令补全列表（由主包在注册命令后调用）。
func (e *Engine) SetCommands(cmds []Suggestion) {
	e.cmds = cmds
}

// RefreshTools 从 Agent 重新加载工具列表。
func (e *Engine) RefreshTools() {
	if e.source == nil {
		return
	}
	tools := e.source.VisibleTools(context.Background())
	e.tools = make([]Suggestion, 0, len(tools))
	for _, t := range tools {
		e.tools = append(e.tools, Suggestion{
			Text: t.Name, Description: t.Description, Kind: "tool",
		})
	}
}

// Suggest 返回匹配输入前缀的建议列表。
// /prefix → command + tool + skill 混合；#prefix → skill。
func (e *Engine) Suggest(input string) []Suggestion {
	switch {
	case strings.HasPrefix(input, "/"):
		return e.suggestCommand(strings.TrimPrefix(input, "/"))
	case strings.HasPrefix(input, "#"):
		return e.suggestSkill(strings.TrimPrefix(input, "#"))
	}
	return nil
}

func (e *Engine) suggestCommand(prefix string) []Suggestion {
	all := make([]Suggestion, 0, len(e.cmds)+len(e.skills)+len(e.tools))
	all = append(all, e.cmds...)
	all = append(all, e.skills...)
	all = append(all, e.tools...)
	if prefix == "" {
		return all
	}
	lower := strings.ToLower(prefix)
	var filtered []Suggestion
	for _, s := range all {
		if strings.HasPrefix(strings.ToLower(s.Text), lower) {
			filtered = append(filtered, s)
		}
	}
	return filtered
}

func (e *Engine) suggestSkill(prefix string) []Suggestion {
	if prefix == "" {
		res := make([]Suggestion, len(e.skills))
		copy(res, e.skills)
		return res
	}
	lower := strings.ToLower(prefix)
	var filtered []Suggestion
	for _, s := range e.skills {
		if strings.HasPrefix(strings.ToLower(s.Text), lower) {
			filtered = append(filtered, s)
		}
	}
	return filtered
}
