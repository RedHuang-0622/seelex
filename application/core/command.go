package core

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/RedHuang-0622/seelex/application/core/input_router"
)

func (service *Service) registerBuiltinCommands() error {
	var registrationErr error
	register := func(name, description string, execute func(context.Context, []string) (CommandResult, error)) {
		if registrationErr != nil {
			return
		}
		if err := service.commands.Register(input_router.NewCommandFunc(name, description, execute)); err != nil {
			registrationErr = fmt.Errorf("register command %q: %w", name, err)
		}
	}
	register("help", "显示帮助信息", func(context.Context, []string) (CommandResult, error) {
		var builder strings.Builder
		builder.WriteString("可用命令:\n")
		for _, command := range service.commands.All() {
			fmt.Fprintf(&builder, "  /%-12s  %s\n", command.Name(), command.Description())
		}
		builder.WriteString("\n提示: /=命令  #=Skill")
		return CommandResult{Notice: builder.String()}, nil
	})
	register("clear", "清空对话历史", func(context.Context, []string) (CommandResult, error) {
		service.Deps.Engine.ClearHistory()
		service.resetConversation("已清空")
		return CommandResult{}, nil
	})
	register("model", "显示当前模型和 Provider", func(context.Context, []string) (CommandResult, error) {
		return CommandResult{Notice: fmt.Sprintf("Model: %s  Provider: %s", service.Deps.Runtime.Model(), service.Deps.Runtime.Provider())}, nil
	})
	register("history", "显示历史消息统计", func(context.Context, []string) (CommandResult, error) {
		history := service.Deps.Engine.History()
		if len(history) == 0 {
			return CommandResult{Notice: "历史为空"}, nil
		}
		roles := make(map[string]int)
		for _, message := range history {
			roles[message.Role]++
		}
		parts := make([]string, 0, len(roles))
		for role, count := range roles {
			parts = append(parts, fmt.Sprintf("%s: %d", role, count))
		}
		sort.Strings(parts)
		return CommandResult{Notice: fmt.Sprintf("共 %d 条 (%s)", len(history), strings.Join(parts, ", "))}, nil
	})
	register("trace", "显示调用追踪树", func(context.Context, []string) (CommandResult, error) {
		trace := service.Deps.Engine.TraceText()
		if trace == "" {
			trace = "暂无追踪数据"
		}
		return CommandResult{Notice: trace}, nil
	})
	register("new", "准备新会话（首次发送时创建）", func(context.Context, []string) (CommandResult, error) {
		return CommandResult{}, service.BeginNewSession()
	})
	register("resume", "恢复历史会话：/resume <session_id>", func(ctx context.Context, args []string) (CommandResult, error) {
		service.Mu.RLock()
		capabilities := service.Core.Snapshot.Capabilities
		service.Mu.RUnlock()
		if !capabilities.SessionResume {
			reason := strings.TrimSpace(capabilities.SessionResumeReason)
			if reason == "" {
				reason = "当前 Engine 不支持历史替换"
			}
			return CommandResult{Notice: "会话恢复暂不可用: " + reason}, nil
		}
		if len(args) == 0 {
			return CommandResult{Interaction: service.sessionInteraction()}, nil
		}
		return CommandResult{}, service.resumeSession(strings.TrimSpace(args[0]))
	})
	register("fork", "从会话分支出新会话：/fork [session_id]（默认当前会话，切到最新完整轮次）", func(ctx context.Context, args []string) (CommandResult, error) {
		service.Mu.RLock()
		currentID := service.Core.Snapshot.Session.ID
		draft := service.Core.Snapshot.Session.Draft
		service.Mu.RUnlock()
		parentID := currentID
		if len(args) > 0 {
			parentID = strings.TrimSpace(args[0])
		}
		if parentID == "" || (parentID == currentID && draft) {
			return CommandResult{Notice: "当前会话尚未持久化，无法 fork（请先发送一条消息）"}, nil
		}
		childID, err := service.ForkSessionLatest(parentID)
		if err != nil {
			return CommandResult{}, err
		}
		return CommandResult{Notice: "已从 " + parentID + " 分支出新会话: " + childID}, nil
	})
	register("sessions", "列出所有持久化会话", func(context.Context, []string) (CommandResult, error) {
		sessions := service.Snapshot().Sessions
		if len(sessions) == 0 {
			return CommandResult{Notice: "暂无持久化会话"}, nil
		}
		var builder strings.Builder
		builder.WriteString("持久化会话:\n")
		for _, session := range sessions {
			fmt.Fprintf(&builder, "  %s  %s  %s  tok:%d\n", session.ID, session.Name, session.UpdatedAt.Format("01-02 15:04"), session.TokenCount)
		}
		return CommandResult{Notice: builder.String()}, nil
	})
	register("pool", "显示并切换账号池", func(context.Context, []string) (CommandResult, error) {
		return CommandResult{Interaction: service.accountInteraction()}, nil
	})
	register("plugins", "列出可用插件", func(context.Context, []string) (CommandResult, error) {
		plugins := service.Deps.Plugins.All()
		if len(plugins) == 0 {
			return CommandResult{Notice: "暂无可用插件"}, nil
		}
		current, _ := service.Deps.Plugins.Current()
		var builder strings.Builder
		builder.WriteString("可用插件:\n")
		for _, plugin := range plugins {
			marker := "  "
			if plugin.Name == current.Name {
				marker = "* "
			}
			fmt.Fprintf(&builder, "%s%-16s %s\n", marker, plugin.Name, plugin.Description)
		}
		return CommandResult{Notice: builder.String()}, nil
	})
	register("plugin", "切换插件：/plugin <name|off>", func(ctx context.Context, args []string) (CommandResult, error) {
		if len(args) == 0 {
			current, ok := service.Deps.Plugins.Current()
			if !ok {
				return CommandResult{Notice: "当前未激活插件"}, nil
			}
			return CommandResult{Notice: "当前插件: " + current.Name}, nil
		}
		name := strings.ToLower(strings.TrimSpace(args[0]))
		if err := service.SwitchPlugin(ctx, name); err != nil {
			return CommandResult{}, err
		}
		if name == "off" || name == "none" {
			return CommandResult{Notice: "已停用插件"}, nil
		}
		return CommandResult{Notice: "已切换插件: " + name}, nil
	})
	register("diag", "系统诊断信息", func(context.Context, []string) (CommandResult, error) {
		service.Mu.RLock()
		snap := service.Core.Snapshot
		service.Mu.RUnlock()
		return CommandResult{Notice: RenderDiag(snap)}, nil
	})
	register("exit", "退出程序", func(context.Context, []string) (CommandResult, error) { return CommandResult{Exit: true}, nil })
	register("effort", "切换 Effort 等级: /effort <lite|medium|high|max>", func(ctx context.Context, args []string) (CommandResult, error) {
		if len(args) == 0 {
			return CommandResult{Notice: "当前 Effort: " + service.effortManager.Current() + "（可用: lite, medium, high, max）"}, nil
		}
		level := strings.ToLower(strings.TrimSpace(args[0]))
		// 与 GUI 共用 SwitchEffort 单一路径：视图会话 running 时拒绝，且
		// system prompt 只写目标（视图）会话引擎（G0b）。
		if err := service.SwitchEffort(ctx, level); err != nil {
			return CommandResult{}, err
		}
		return CommandResult{Notice: "Effort 已切换为: " + level}, nil
	})
	return registrationErr
}
