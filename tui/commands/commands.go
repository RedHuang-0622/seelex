// Package commands 提供命令策略模式框架和内置命令实现。
//
// 主包在启动时调用 commands.Register() 注册命令，
// 然后本包通过 Get() / All() / Execute() 提供服务。
package commands

import "sort"

// ── 命令策略接口 ────────────────────────────────────────────────

type Command interface {
	Name() string
	Description() string
	Execute(args []string) string
}

var registry = make(map[string]Command)

// Register 注册一个命令。
func Register(cmd Command) {
	registry[cmd.Name()] = cmd
}

// Get 按名称查找命令。
func Get(name string) (Command, bool) {
	cmd, ok := registry[name]
	return cmd, ok
}

// All 返回所有已注册命令（按名称排序）。
func All() []Command {
	cmds := make([]Command, 0, len(registry))
	for _, c := range registry {
		cmds = append(cmds, c)
	}
	sort.Slice(cmds, func(i, j int) bool {
		return cmds[i].Name() < cmds[j].Name()
	})
	return cmds
}

// Execute 执行指定命令，返回输出字符串。
func Execute(name string, args []string) string {
	cmd, ok := registry[name]
	if !ok {
		return "未知命令: " + name
	}
	return cmd.Execute(args)
}
