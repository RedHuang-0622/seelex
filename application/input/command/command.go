// Package command owns the non-mutating builtin command registration slice.
package command

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/contract/dto"
	protocol "github.com/RedHuang-0622/seelex/contract/protocol"
	"github.com/RedHuang-0622/seelex/middle/protocol/model"
)

// Registry is the command registry capability needed by builtins.
type Registry interface {
	Register(contract.Command) error
	All() []contract.Command
}

// Deps supplies read-only host values and the clear action.
type Deps struct {
	Model        func() (modelName, provider string)
	History      func() []contract.EngineMessage
	Trace        func() string
	Sessions     func() []model.SessionInfo
	Diag         func() string
	Clear        func()
	SigilCommand string
	SigilPlugin  string
	SigilSkill   string
	Mutation     MutationDeps
}

// MutationDeps supplies the state-changing command actions without exposing Service.
type MutationDeps struct {
	Compact            func(context.Context) (notice string, err error)
	BeginNewSession    func() error
	Capabilities       func() (supported bool, reason string)
	SessionInteraction func() *protocol.Interaction
	Resume             func(string) error
	CurrentSession     func() (id string, draft bool)
	Fork               func(string) (string, error)
	AccountInteraction func() *protocol.Interaction
	Plugins            func() ([]model.PluginInfo, model.PluginInfo, bool)
	SwitchPlugin       func(context.Context, string) error
	Effort             func() string
	SwitchEffort       func(context.Context, string) error
	Permission         func() string
	PermissionTiers    func() []dto.PermissionTierInfo
	SetPermission      func(string) (string, error)
}

// Application registers the builtin read side of the command surface.
type Application struct {
	registry Registry
	deps     Deps
}

// New constructs a builtin command application.
func New(registry Registry, deps Deps) *Application {
	return &Application{registry: registry, deps: deps}
}

// Register installs help, clear, model, history, trace, sessions, diag and exit.
func (application *Application) Register() error {
	if application == nil || application.registry == nil {
		return fmt.Errorf("command registry is nil")
	}
	return application.register(application.commands())
}

// RegisterMutations installs compact, session, plugin, effort and permission commands.
func (application *Application) RegisterMutations() error {
	if application == nil || application.registry == nil {
		return fmt.Errorf("command registry is nil")
	}
	return application.register(application.mutationCommands())
}

func (application *Application) register(commands []contract.Command) error {
	for _, command := range commands {
		if err := application.registry.Register(command); err != nil {
			return fmt.Errorf("register command %q: %w", command.Name(), err)
		}
	}
	return nil
}

func (application *Application) commands() []contract.Command {
	return []contract.Command{
		commandFunc{
			name: "help", description: "显示帮助信息",
			execute: func(context.Context, []string) (contract.CommandResult, error) {
				var builder strings.Builder
				builder.WriteString("可用命令:\n")
				for _, command := range application.registry.All() {
					fmt.Fprintf(&builder, "  /%-12s  %s\n", command.Name(), command.Description())
				}
				builder.WriteString(fmt.Sprintf("\n提示: %s=命令  %s=切换插件  %s=召回 Skill",
					application.deps.SigilCommand, application.deps.SigilPlugin, application.deps.SigilSkill))
				builder.WriteString(fmt.Sprintf(
					"\n说明: %s 面板只列可直接提交的入口（命令与 Skill；Skill 也可用 %s<name>）。"+
						"工具由模型调用、不在此列出：需要 %s名字 显式调用时，先把该能力注册成命令。",
					application.deps.SigilCommand, application.deps.SigilSkill, application.deps.SigilCommand))
				return contract.CommandResult{Notice: builder.String()}, nil
			},
		},
		commandFunc{
			name: "clear", description: "清空对话历史",
			execute: func(context.Context, []string) (contract.CommandResult, error) {
				if application.deps.Clear != nil {
					application.deps.Clear()
				}
				return contract.CommandResult{}, nil
			},
		},
		commandFunc{
			name: "model", description: "显示当前模型和 Provider",
			execute: func(context.Context, []string) (contract.CommandResult, error) {
				name, provider := "", ""
				if application.deps.Model != nil {
					name, provider = application.deps.Model()
				}
				return contract.CommandResult{Notice: fmt.Sprintf("Model: %s  Provider: %s", name, provider)}, nil
			},
		},
		commandFunc{
			name: "history", description: "显示历史消息统计",
			execute: func(context.Context, []string) (contract.CommandResult, error) {
				var history []contract.EngineMessage
				if application.deps.History != nil {
					history = application.deps.History()
				}
				if len(history) == 0 {
					return contract.CommandResult{Notice: "历史为空"}, nil
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
				return contract.CommandResult{Notice: fmt.Sprintf("共 %d 条 (%s)", len(history), strings.Join(parts, ", "))}, nil
			},
		},
		commandFunc{
			name: "trace", description: "显示调用追踪树",
			execute: func(context.Context, []string) (contract.CommandResult, error) {
				trace := ""
				if application.deps.Trace != nil {
					trace = application.deps.Trace()
				}
				if trace == "" {
					trace = "暂无追踪数据"
				}
				return contract.CommandResult{Notice: trace}, nil
			},
		},
		commandFunc{
			name: "sessions", description: "列出所有持久化会话",
			execute: func(context.Context, []string) (contract.CommandResult, error) {
				var sessions []model.SessionInfo
				if application.deps.Sessions != nil {
					sessions = application.deps.Sessions()
				}
				if len(sessions) == 0 {
					return contract.CommandResult{Notice: "暂无持久化会话"}, nil
				}
				var builder strings.Builder
				builder.WriteString("持久化会话:\n")
				for _, session := range sessions {
					fmt.Fprintf(&builder, "  %s  %s  %s  tok:%d\n", session.ID, session.Name, session.UpdatedAt.Format("01-02 15:04"), session.TokenCount)
				}
				return contract.CommandResult{Notice: builder.String()}, nil
			},
		},
		commandFunc{
			name: "diag", description: "系统诊断信息",
			execute: func(context.Context, []string) (contract.CommandResult, error) {
				diag := ""
				if application.deps.Diag != nil {
					diag = application.deps.Diag()
				}
				return contract.CommandResult{Notice: diag}, nil
			},
		},
		commandFunc{
			name: "exit", description: "退出程序",
			execute: func(context.Context, []string) (contract.CommandResult, error) {
				return contract.CommandResult{Exit: true}, nil
			},
		},
	}
}

func (application *Application) mutationCommands() []contract.Command {
	deps := application.deps.Mutation
	return []contract.Command{
		commandFunc{
			name: "compact", description: "压缩当前上下文（压缩为有界 checkpoint，原始轮次仍可回读）",
			execute: func(ctx context.Context, _ []string) (contract.CommandResult, error) {
				if deps.Compact == nil {
					return contract.CommandResult{}, nil
				}
				notice, err := deps.Compact(ctx)
				return contract.CommandResult{Notice: notice}, err
			},
		},
		commandFunc{
			name: "new", description: "准备新会话（首次发送时创建）",
			execute: func(context.Context, []string) (contract.CommandResult, error) {
				if deps.BeginNewSession == nil {
					return contract.CommandResult{}, nil
				}
				return contract.CommandResult{}, deps.BeginNewSession()
			},
		},
		commandFunc{
			name: "resume", description: "恢复历史会话：/resume <session_id>",
			execute: func(_ context.Context, args []string) (contract.CommandResult, error) {
				if deps.Capabilities != nil {
					supported, reason := deps.Capabilities()
					if !supported {
						if strings.TrimSpace(reason) == "" {
							reason = "当前 Engine 不支持历史替换"
						}
						return contract.CommandResult{Notice: "会话恢复暂不可用: " + reason}, nil
					}
				}
				if len(args) == 0 {
					var interaction *protocol.Interaction
					if deps.SessionInteraction != nil {
						interaction = deps.SessionInteraction()
					}
					return contract.CommandResult{Interaction: interaction}, nil
				}
				if deps.Resume == nil {
					return contract.CommandResult{}, nil
				}
				return contract.CommandResult{}, deps.Resume(strings.TrimSpace(args[0]))
			},
		},
		commandFunc{
			name: "fork", description: "从会话分支出新会话：/fork [session_id]（默认当前会话，切到最新完整轮次）",
			execute: func(_ context.Context, args []string) (contract.CommandResult, error) {
				currentID, draft := "", false
				if deps.CurrentSession != nil {
					currentID, draft = deps.CurrentSession()
				}
				parentID := currentID
				if len(args) > 0 {
					parentID = strings.TrimSpace(args[0])
				}
				if parentID == "" || (parentID == currentID && draft) {
					return contract.CommandResult{Notice: "当前会话尚未持久化，无法 fork（请先发送一条消息）"}, nil
				}
				if deps.Fork == nil {
					return contract.CommandResult{}, nil
				}
				childID, err := deps.Fork(parentID)
				if err != nil {
					return contract.CommandResult{}, err
				}
				return contract.CommandResult{Notice: "已从 " + parentID + " 分支出新会话: " + childID}, nil
			},
		},
		commandFunc{
			name: "pool", description: "显示并切换账号池",
			execute: func(context.Context, []string) (contract.CommandResult, error) {
				var interaction *protocol.Interaction
				if deps.AccountInteraction != nil {
					interaction = deps.AccountInteraction()
				}
				return contract.CommandResult{Interaction: interaction}, nil
			},
		},
		commandFunc{
			name: "plugins", description: "列出可用插件",
			execute: func(context.Context, []string) (contract.CommandResult, error) {
				var plugins []model.PluginInfo
				current, currentOK := model.PluginInfo{}, false
				if deps.Plugins != nil {
					plugins, current, currentOK = deps.Plugins()
				}
				if len(plugins) == 0 {
					return contract.CommandResult{Notice: "暂无可用插件"}, nil
				}
				var builder strings.Builder
				builder.WriteString("可用插件:\n")
				for _, plugin := range plugins {
					marker := "  "
					if currentOK && plugin.Name == current.Name {
						marker = "* "
					}
					fmt.Fprintf(&builder, "%s%-16s %s\n", marker, plugin.Name, plugin.Description)
				}
				return contract.CommandResult{Notice: builder.String()}, nil
			},
		},
		commandFunc{
			name: "plugin", description: "切换插件：/plugin <name|off>",
			execute: func(ctx context.Context, args []string) (contract.CommandResult, error) {
				if len(args) == 0 {
					current, ok := model.PluginInfo{}, false
					if deps.Plugins != nil {
						_, current, ok = deps.Plugins()
					}
					if !ok {
						return contract.CommandResult{Notice: "当前未激活插件"}, nil
					}
					return contract.CommandResult{Notice: "当前插件: " + current.Name}, nil
				}
				name := strings.ToLower(strings.TrimSpace(args[0]))
				if deps.SwitchPlugin == nil {
					return contract.CommandResult{}, nil
				}
				if err := deps.SwitchPlugin(ctx, name); err != nil {
					return contract.CommandResult{}, err
				}
				if name == "off" || name == "none" {
					return contract.CommandResult{Notice: "已停用插件"}, nil
				}
				return contract.CommandResult{Notice: "已切换插件: " + name}, nil
			},
		},
		commandFunc{
			name: "effort", description: "切换 Effort 等级: /effort <lite|medium|high|max>",
			execute: func(ctx context.Context, args []string) (contract.CommandResult, error) {
				if len(args) == 0 {
					current := ""
					if deps.Effort != nil {
						current = deps.Effort()
					}
					return contract.CommandResult{Notice: "当前 Effort: " + current + "（可用: lite, medium, high, max）"}, nil
				}
				level := strings.ToLower(strings.TrimSpace(args[0]))
				if deps.SwitchEffort == nil {
					return contract.CommandResult{}, nil
				}
				if err := deps.SwitchEffort(ctx, level); err != nil {
					return contract.CommandResult{}, err
				}
				return contract.CommandResult{Notice: "Effort 已切换为: " + level}, nil
			},
		},
		commandFunc{
			name: "permission", description: "查看/切换本会话权限档位: /permission <manual|edit|auto|full>",
			execute: func(_ context.Context, args []string) (contract.CommandResult, error) {
				if len(args) == 0 {
					current := ""
					if deps.Permission != nil {
						current = deps.Permission()
					}
					var builder strings.Builder
					builder.WriteString("当前档位（本会话）: " + current + "\n")
					if deps.PermissionTiers != nil {
						for _, tier := range deps.PermissionTiers() {
							marker := "  "
							if tier.ID == current {
								marker = "* "
							}
							fmt.Fprintf(&builder, "%s%-8s %s\n", marker, tier.ID, tier.Description)
						}
					}
					return contract.CommandResult{Notice: builder.String()}, nil
				}
				if deps.SetPermission == nil {
					return contract.CommandResult{}, nil
				}
				effective, err := deps.SetPermission(args[0])
				if err != nil {
					return contract.CommandResult{}, err
				}
				return contract.CommandResult{Notice: "本会话权限档位已切换为: " + effective}, nil
			},
		},
	}
}

type commandFunc struct {
	name        string
	description string
	execute     func(context.Context, []string) (contract.CommandResult, error)
}

func (command commandFunc) Name() string        { return command.name }
func (command commandFunc) Description() string { return command.description }
func (command commandFunc) Execute(ctx context.Context, args []string) (contract.CommandResult, error) {
	return command.execute(ctx, args)
}
