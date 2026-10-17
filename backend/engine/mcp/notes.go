package mcp

import (
	"strings"

	tools "github.com/RedHuang-0622/Seele/tools"
)

// providerSource 是注记装饰器需要的 provider 表面：框架 MCP provider 满足它，
// 测试里的假实现也能满足。装饰器只改模型看到的那段文字，不碰 handler。
type providerSource interface {
	ProviderName() string
	ServerNames() []string
	Tools() []tools.ToolEntry
}

// notesProvider 在框架 MCP provider 之上，把配置里的 tool_notes 追加到对应
// server 每个工具的描述末尾。
//
// 为什么需要这一层：工具描述**由 MCP server 自己给出**（框架 provider.fetchTools
// 原样透传 mt.Description），而「在本机实测出来的调用纪律」属于宿主——哪种入参
// 形态能拿回结果、失败长什么样，只有宿主知道。既不改 MCP server，也不改框架
// 合约，只在注册进 registry 之前把那段文字补上，模型在第一次调用之前就看得见。
//
// 名字规则与框架一致（frameworkmcp.Provider.Tools）：多 server 时 entry 名为
// `server__tool`，单 server 时不带前缀。
type notesProvider struct {
	inner providerSource
	notes map[string][]string
}

func (p *notesProvider) ProviderName() string { return p.inner.ProviderName() }

func (p *notesProvider) Tools() []tools.ToolEntry {
	entries := p.inner.Tools()
	if len(p.notes) == 0 || len(entries) == 0 {
		return entries
	}
	attached := p.inner.ServerNames()
	if len(attached) == 0 {
		return entries
	}
	for i := range entries {
		notes := p.notesFor(entries[i].Definition.Function.Name, attached)
		if len(notes) == 0 {
			continue
		}
		entries[i].Definition.Function.Description = appendNotes(
			entries[i].Definition.Function.Description, notes)
	}
	return entries
}

// notesFor 按 entry 名找回它所属 server 的注记。
//
// 单 server 时 entry 名不带前缀，直接按键取；多 server 时按 `server__` 前缀取，
// 取最长命中者（server 名互为前缀时也不会取错）。
func (p *notesProvider) notesFor(entryName string, attached []string) []string {
	if len(attached) == 1 {
		return p.notes[attached[0]]
	}
	longest := ""
	for _, server := range attached {
		if len(server) > len(longest) && strings.HasPrefix(entryName, server+"__") {
			longest = server
		}
	}
	return p.notes[longest]
}

// appendNotes 把注记折在描述末尾（空段分隔），空白注记跳过。
func appendNotes(description string, notes []string) string {
	var builder strings.Builder
	builder.WriteString(strings.TrimSpace(description))
	for _, note := range notes {
		note = strings.TrimSpace(note)
		if note == "" {
			continue
		}
		if builder.Len() > 0 {
			builder.WriteString("\n\n")
		}
		builder.WriteString(note)
	}
	return builder.String()
}
