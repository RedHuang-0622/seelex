// Package input_router owns the input dispatch and command registry types.
// 路由与注册表不持有任何 Service 状态；装配根注入闭包路由，命令注册由
// core 根包的 registerBuiltinCommands 完成。
package input_router

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/RedHuang-0622/seelex/application/model"
)

// Command 是内置命令的注册契约（实现：CommandFunc 或测试桩）。
type Command interface {
	Name() string
	Description() string
	Execute(context.Context, []string) (CommandResult, error)
}

// CommandResult 是命令执行的返回面（Notice/Exit/Interaction）。
type CommandResult struct {
	Notice      string
	Exit        bool
	Interaction *model.Interaction
}

// CommandFunc 是命令的闭包实现（装配根注入执行体）。
type CommandFunc struct {
	name        string
	description string
	execute     func(context.Context, []string) (CommandResult, error)
}

// NewCommandFunc 构造命令闭包实现。
func NewCommandFunc(name, description string, execute func(context.Context, []string) (CommandResult, error)) CommandFunc {
	return CommandFunc{name: name, description: description, execute: execute}
}

func (command CommandFunc) Name() string        { return command.name }
func (command CommandFunc) Description() string { return command.description }
func (command CommandFunc) Execute(ctx context.Context, args []string) (CommandResult, error) {
	return command.execute(ctx, args)
}

// CommandRegistry 是命令名 → 实现的注册表（线程安全由调用方保证）。
type CommandRegistry struct{ commands map[string]Command }

// NewCommandRegistry 构造空注册表。
func NewCommandRegistry() *CommandRegistry {
	return &CommandRegistry{commands: make(map[string]Command)}
}

// Register 注册命令（大小写不敏感；重名报错）。
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

// Get 按名查找命令。
func (registry *CommandRegistry) Get(name string) (Command, bool) {
	command, ok := registry.commands[strings.ToLower(name)]
	return command, ok
}

// All 返回按名排序的全部命令。
func (registry *CommandRegistry) All() []Command {
	commands := make([]Command, 0, len(registry.commands))
	for _, command := range registry.commands {
		commands = append(commands, command)
	}
	sort.Slice(commands, func(i, j int) bool { return commands[i].Name() < commands[j].Name() })
	return commands
}
