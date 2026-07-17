package main

import (
	"context"
	"strings"

	"github.com/RedHuang-0622/Seele/engine"

	"github.com/RedHuang-0622/seelex/application"
	"github.com/RedHuang-0622/seelex/plugin"
	"github.com/RedHuang-0622/seelex/seelebridge"
	"github.com/RedHuang-0622/seelex/session"
	"github.com/RedHuang-0622/seelex/skill"
)

type enginePort struct{ engine *engine.Engine }

func (port enginePort) ChatStream(ctx context.Context, input string, onChunk func(string)) (string, error) {
	return port.engine.ChatStream(ctx, input, onChunk)
}
func (port enginePort) ClearHistory()                 { port.engine.ClearHistory() }
func (port enginePort) SessionID() string             { return port.engine.SessionID() }
func (port enginePort) SetSystemPrompt(prompt string) { port.engine.SetSystemPrompt(prompt) }
func (port enginePort) TraceText() string {
	tree := port.engine.ExportTrace()
	if tree == nil || tree.Root == nil {
		return ""
	}
	return tree.String()
}
func (port enginePort) TokenCount() string {
	tree := port.engine.ExportTrace()
	if tree == nil || tree.Root == nil {
		return "0"
	}
	for _, child := range tree.Root.Children {
		if child.Kind == seelebridge.SpanLLMCall {
			if tokens, ok := child.Attrs["total_tokens"]; ok {
				return tokens
			}
		}
	}
	return "0"
}
func (port enginePort) History() []application.EngineMessage {
	return adaptMessages(port.engine.History())
}

type runtimePort struct{ runtime *seelebridge.Runtime }

func (port runtimePort) Model() string                  { return port.runtime.Model() }
func (port runtimePort) Provider() string               { return port.runtime.Provider() }
func (port runtimePort) ActivePlugin() string           { return port.runtime.ActivePlugin() }
func (port runtimePort) SelectAccount(name string) bool { return port.runtime.SelectAccount(name) }
func (port runtimePort) VisibleTools(ctx context.Context) []application.Tool {
	tools := port.runtime.VisibleTools(ctx)
	result := make([]application.Tool, 0, len(tools))
	for _, tool := range tools {
		result = append(result, application.Tool{Name: tool.Name, Description: tool.Description})
	}
	return result
}
func (port runtimePort) Accounts() []application.AccountInfo {
	accounts := port.runtime.Accounts()
	result := make([]application.AccountInfo, 0, len(accounts))
	for _, account := range accounts {
		result = append(result, application.AccountInfo{Name: account.Name, Provider: account.Provider, Model: account.Model, Disabled: account.Disabled})
	}
	return result
}

type pluginPort struct{ manager *plugin.Manager }

func (port pluginPort) Activate(ctx context.Context, name string) error {
	return port.manager.Activate(ctx, name)
}
func (port pluginPort) Deactivate(ctx context.Context) error { return port.manager.Deactivate(ctx) }
func (port pluginPort) Current() (application.PluginInfo, bool) {
	current, ok := port.manager.Current()
	if !ok {
		return application.PluginInfo{}, false
	}
	return adaptPlugin(current), true
}
func (port pluginPort) All() []application.PluginInfo {
	plugins := port.manager.All()
	result := make([]application.PluginInfo, 0, len(plugins))
	for _, item := range plugins {
		result = append(result, adaptPlugin(item))
	}
	return result
}

type skillPort struct{ registry *skill.Registry }

func (port skillPort) Get(name string) (application.SkillInfo, bool) {
	item, ok := port.registry.Get(name)
	if !ok {
		return application.SkillInfo{}, false
	}
	return adaptSkill(item), true
}
func (port skillPort) All() []application.SkillInfo {
	skills := port.registry.All()
	result := make([]application.SkillInfo, 0, len(skills))
	for _, item := range skills {
		result = append(result, adaptSkill(item))
	}
	return result
}

type sessionPort struct{ manager *session.Manager }

func (port sessionPort) SaveCurrent(id string) error { return port.manager.SaveCurrent(id) }
func (port sessionPort) Resume(id string) error      { return port.manager.Resume(id) }
func (port sessionPort) LoadHistory(id string) ([]application.EngineMessage, error) {
	messages, err := port.manager.LoadHistory(id)
	if err != nil {
		return nil, err
	}
	return adaptMessages(messages), nil
}
func (port sessionPort) List() []application.SessionInfo {
	sessions := port.manager.List()
	result := make([]application.SessionInfo, 0, len(sessions))
	for _, item := range sessions {
		result = append(result, application.SessionInfo{ID: item.SessionID, UpdatedAt: item.UpdatedAt, TokenCount: item.TokenCount})
	}
	return result
}

func adaptPlugin(item plugin.Plugin) application.PluginInfo {
	return application.PluginInfo{Name: item.Name, Description: item.Description, Prompt: item.Prompt}
}
func adaptSkill(item skill.Skill) application.SkillInfo {
	return application.SkillInfo{Name: item.Name, Description: item.Description, Prompt: item.Prompt}
}
func adaptMessages(messages []seelebridge.Message) []application.EngineMessage {
	result := make([]application.EngineMessage, 0, len(messages))
	for _, message := range messages {
		adapted := application.EngineMessage{Role: message.Role, Name: message.Name}
		if message.Content != nil {
			adapted.Content = *message.Content
		}
		adapted.ToolCalls = make([]application.EngineToolCall, 0, len(message.ToolCalls))
		for _, call := range message.ToolCalls {
			adapted.ToolCalls = append(adapted.ToolCalls, application.EngineToolCall{ID: call.ID, Name: call.Function.Name, Arguments: call.Function.Arguments})
		}
		result = append(result, adapted)
	}
	return result
}

func approvalOption(choice string) application.InteractionOption {
	options := map[string]application.InteractionOption{
		"execute": {ID: "execute", Label: "执行", Description: "按计划执行", Style: "primary"},
		"skip":    {ID: "skip", Label: "跳过", Description: "跳过当前节点", Style: "secondary"},
		"abort":   {ID: "abort", Label: "终止", Description: "终止工作流", Style: "danger"},
		"confirm": {ID: "confirm", Label: "确认", Description: "确认并继续", Style: "primary"},
		"retry":   {ID: "retry", Label: "重试", Description: "重新执行", Style: "warning"},
	}
	if option, ok := options[choice]; ok {
		return option
	}
	return application.InteractionOption{ID: choice, Label: choice}
}

func approvalAccepted(optionID string) bool {
	switch strings.ToLower(strings.TrimSpace(optionID)) {
	case "", "__cancel__", "__timeout__", "no", "deny", "reject", "refuse", "cancel", "abort", "skip", "false", "否", "拒绝", "取消", "终止", "跳过":
		return false
	default:
		return true
	}
}
