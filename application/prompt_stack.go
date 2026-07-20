// Package application provides the Seelex application service layer.
package application

import (
	"strings"
)

// PromptLayer 表示 system prompt 的一个分层。
type PromptLayer struct {
	Kind string // "base" | "effort" | "skill"
	Name string // plugin name / effort level / skill name
	Text string // prompt 内容
}

// PromptStack 实现多层 system prompt 栈。
// 层序从底到顶: base → effort → skill_1 → skill_2 → ...
// Render() 用分隔符拼接所有层。
type PromptStack struct {
	layers []PromptLayer
}

// NewPromptStack 创建空栈。
func NewPromptStack() *PromptStack {
	return &PromptStack{layers: make([]PromptLayer, 0, 8)}
}

// Push 压入一层。同名 layer（kind+name 相同）会被覆盖更新。
func (ps *PromptStack) Push(kind, name, text string) {
	ps.remove(kind, name)
	ps.layers = append(ps.layers, PromptLayer{Kind: kind, Name: name, Text: text})
}

// Pop 按 name 删除一层。返回是否找到并删除。
func (ps *PromptStack) Pop(name string) bool {
	for i, l := range ps.layers {
		if l.Name == name {
			ps.layers = append(ps.layers[:i], ps.layers[i+1:]...)
			return true
		}
	}
	return false
}

// PopKind 删除最后一个指定 kind 的层，返回其 name。
// 如果该 kind 不存在，返回空字符串。
func (ps *PromptStack) PopKind(kind string) string {
	for i := len(ps.layers) - 1; i >= 0; i-- {
		if ps.layers[i].Kind == kind {
			name := ps.layers[i].Name
			ps.layers = append(ps.layers[:i], ps.layers[i+1:]...)
			return name
		}
	}
	return ""
}

// ClearKind 删除所有指定 kind 的层。
func (ps *PromptStack) ClearKind(kind string) {
	filtered := make([]PromptLayer, 0, len(ps.layers))
	for _, l := range ps.layers {
		if l.Kind != kind {
			filtered = append(filtered, l)
		}
	}
	ps.layers = filtered
}

// Reset 清空所有层，设置 base 层。
func (ps *PromptStack) Reset(baseText string) {
	ps.layers = make([]PromptLayer, 0, 8)
	if baseText != "" {
		ps.layers = append(ps.layers, PromptLayer{Kind: "base", Name: "base", Text: baseText})
	}
}

// Render 将所有层用分隔符拼接为完整 system prompt。
func (ps *PromptStack) Render() string {
	if len(ps.layers) == 0 {
		return ""
	}
	parts := make([]string, 0, len(ps.layers))
	for _, l := range ps.layers {
		text := strings.TrimSpace(l.Text)
		if text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\n\n---\n\n")
}

// Has 检查是否存在指定 kind 的层。
func (ps *PromptStack) Has(kind string) bool {
	for _, l := range ps.layers {
		if l.Kind == kind {
			return true
		}
	}
	return false
}

// Layers 返回当前所有层的副本（用于 TUI 显示）。
func (ps *PromptStack) Layers() []PromptLayer {
	cp := make([]PromptLayer, len(ps.layers))
	copy(cp, ps.layers)
	return cp
}

// Count 返回层数。
func (ps *PromptStack) Count() int {
	return len(ps.layers)
}

func (ps *PromptStack) remove(kind, name string) {
	for i, l := range ps.layers {
		if l.Kind == kind && l.Name == name {
			ps.layers = append(ps.layers[:i], ps.layers[i+1:]...)
			return
		}
	}
}

// Describe 返回人类可读的栈摘要（用于状态栏等）。
func (ps *PromptStack) Describe() string {
	var skills []string
	var effort string
	for _, l := range ps.layers {
		switch l.Kind {
		case "skill":
			skills = append(skills, l.Name)
		case "effort":
			effort = l.Name
		}
	}
	parts := make([]string, 0, 3)
	if effort != "" {
		parts = append(parts, "E:"+effort)
	}
	if len(skills) > 0 {
		parts = append(parts, strings.Join(skills, "|"))
	}
	if len(parts) == 0 {
		return "base"
	}
	return strings.Join(parts, "  ")
}

