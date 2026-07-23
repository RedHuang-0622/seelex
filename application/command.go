package application

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"
)

type Command interface {
	Name() string
	Description() string
	Execute(context.Context, []string) (CommandResult, error)
}
type CommandResult struct {
	Notice      string
	Exit        bool
	Interaction *Interaction
}
type commandFunc struct {
	name        string
	description string
	execute     func(context.Context, []string) (CommandResult, error)
}

func (command commandFunc) Name() string        { return command.name }
func (command commandFunc) Description() string { return command.description }
func (command commandFunc) Execute(ctx context.Context, args []string) (CommandResult, error) {
	return command.execute(ctx, args)
}

type CommandRegistry struct{ commands map[string]Command }

func NewCommandRegistry() *CommandRegistry {
	return &CommandRegistry{commands: make(map[string]Command)}
}
func (registry *CommandRegistry) Register(command Command) error {
	name := strings.ToLower(strings.TrimSpace(command.Name()))
	if name == "" {
		return fmt.Errorf("command name is empty")
	}
	if _, exists := registry.commands[name]; exists {
		return fmt.Errorf("command %q already registered", name)
	}
	registry.commands[name] = command
	return nil
}
func (registry *CommandRegistry) Get(name string) (Command, bool) {
	command, ok := registry.commands[strings.ToLower(name)]
	return command, ok
}
func (registry *CommandRegistry) All() []Command {
	commands := make([]Command, 0, len(registry.commands))
	for _, command := range registry.commands {
		commands = append(commands, command)
	}
	sort.Slice(commands, func(i, j int) bool { return commands[i].Name() < commands[j].Name() })
	return commands
}

func (service *Service) registerBuiltinCommands() {
	register := func(name, description string, execute func(context.Context, []string) (CommandResult, error)) {
		if err := service.commands.Register(commandFunc{name: name, description: description, execute: execute}); err != nil {
			log.Fatalf("register command %q: %v", name, err)
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
		service.deps.Engine.ClearHistory()
		service.resetConversation("已清空")
		return CommandResult{}, nil
	})
	register("model", "显示当前模型和 Provider", func(context.Context, []string) (CommandResult, error) {
		return CommandResult{Notice: fmt.Sprintf("Model: %s  Provider: %s", service.deps.Runtime.Model(), service.deps.Runtime.Provider())}, nil
	})
	register("history", "显示历史消息统计", func(context.Context, []string) (CommandResult, error) {
		history := service.deps.Engine.History()
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
		trace := service.deps.Engine.TraceText()
		if trace == "" {
			trace = "暂无追踪数据"
		}
		return CommandResult{Notice: trace}, nil
	})
	register("new", "新建会话（当前会话自动保存）", func(context.Context, []string) (CommandResult, error) {
		id := service.deps.Engine.SessionID()
		if err := service.deps.Sessions.SaveCurrent(id); err != nil {
			return CommandResult{}, fmt.Errorf("保存会话失败: %w", err)
		}
		newID := service.deps.Engine.StartSession()
		service.deps.Engine.SetSystemPrompt(service.promptStack.Render())
		service.mu.Lock()
		service.snapshot.Session.ID = newID
		service.snapshot.HistoryOffset = 0
		service.snapshot.TotalMessages = 0
		service.snapshot.HasMoreHistory = false
		service.mu.Unlock()
		service.resetConversation(fmt.Sprintf("已新建会话（已保存 %s）", id))
		return CommandResult{}, nil
	})
	register("resume", "恢复历史会话：/resume <session_id>", func(ctx context.Context, args []string) (CommandResult, error) {
		service.mu.RLock()
		capabilities := service.snapshot.Capabilities
		service.mu.RUnlock()
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
	register("sessions", "列出所有持久化会话", func(context.Context, []string) (CommandResult, error) {
		sessions := service.deps.Sessions.List()
		if len(sessions) == 0 {
			return CommandResult{Notice: "暂无持久化会话"}, nil
		}
		var builder strings.Builder
		builder.WriteString("持久化会话:\n")
		for _, session := range sessions {
			fmt.Fprintf(&builder, "  %s  %s  tok:%d\n", session.ID, session.UpdatedAt.Format("01-02 15:04"), session.TokenCount)
		}
		return CommandResult{Notice: builder.String()}, nil
	})
	register("pool", "显示并切换账号池", func(context.Context, []string) (CommandResult, error) {
		return CommandResult{Interaction: service.accountInteraction()}, nil
	})
	register("plugins", "列出可用插件", func(context.Context, []string) (CommandResult, error) {
		plugins := service.deps.Plugins.All()
		if len(plugins) == 0 {
			return CommandResult{Notice: "暂无可用插件"}, nil
		}
		current, _ := service.deps.Plugins.Current()
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
			current, ok := service.deps.Plugins.Current()
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
		service.mu.RLock()
		snap := service.snapshot
		service.mu.RUnlock()
		return CommandResult{Notice: RenderDiag(snap)}, nil
	})
	register("exit", "退出程序", func(context.Context, []string) (CommandResult, error) { return CommandResult{Exit: true}, nil })
	register("effort", "切换 Effort 等级: /effort <lite|medium|high|max>", func(ctx context.Context, args []string) (CommandResult, error) {
		if len(args) == 0 {
			return CommandResult{Notice: "当前 Effort: " + service.effortManager.Current() + "（可用: lite, medium, high, max）"}, nil
		}
		level := strings.ToLower(strings.TrimSpace(args[0]))
		if err := service.effortManager.Apply(level); err != nil {
			return CommandResult{}, err
		}
		service.deps.Engine.SetSystemPrompt(service.promptStack.Render())
		service.mu.Lock()
		revision := service.bumpLocked()
		service.mu.Unlock()
		service.events.Publish(EventSnapshotChanged, revision, "", nil)
		return CommandResult{Notice: "Effort 已切换为: " + level}, nil
	})
}
