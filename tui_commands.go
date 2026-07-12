package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/RedHuang-0622/Seele/agent/core/api"
	"github.com/RedHuang-0622/Seele/engine"

	"github.com/RedHuang-0622/seelex/session"
)

// Command 命令接口（策略模式）
type Command interface {
	Name() string
	Description() string
	Execute(args []string) string
}

// ── 命令注册表（策略容器）────────────────────────────────────────

var globalCommands = make(map[string]Command)

func registerCommand(cmd Command) {
	globalCommands[cmd.Name()] = cmd
}

func getCommand(name string) (Command, bool) {
	cmd, ok := globalCommands[name]
	return cmd, ok
}

func allCommands() []Command {
	cmds := make([]Command, 0, len(globalCommands))
	for _, c := range globalCommands {
		cmds = append(cmds, c)
	}
	sort.Slice(cmds, func(i, j int) bool {
		return cmds[i].Name() < cmds[j].Name()
	})
	return cmds
}

// ── 具体命令 ────────────────────────────────────────────────────

// HelpCommand — /help
type HelpCommand struct{}

func (c HelpCommand) Name() string       { return "help" }
func (c HelpCommand) Description() string { return "显示帮助信息" }
func (c HelpCommand) Execute(args []string) string {
	var b strings.Builder
	b.WriteString("可用命令:\n")
	for _, cmd := range allCommands() {
		b.WriteString(fmt.Sprintf("  /%-12s  %s\n", cmd.Name(), cmd.Description()))
	}
	b.WriteString("\n提示: 输入 @ 查看可用工具, 输入 / 查看命令列表")
	return b.String()
}

// ClearCommand — /clear
type ClearCommand struct{ eng *engine.Engine }

func (c ClearCommand) Name() string       { return "clear" }
func (c ClearCommand) Description() string { return "清空对话历史" }
func (c ClearCommand) Execute(args []string) string {
	c.eng.ClearHistory()
	return "已清空"
}

// ModelCommand — /model
type ModelCommand struct {
	modelName string
	client    *api.ChatClient
}

func (c ModelCommand) Name() string       { return "model" }
func (c ModelCommand) Description() string { return "显示当前模型和 Provider" }
func (c ModelCommand) Execute(args []string) string {
	pf := c.client.ProviderFilter()
	return fmt.Sprintf("Model: %s  Provider: %s", c.modelName, pf)
}

// HistoryCommand — /history
type HistoryCommand struct{ eng *engine.Engine }

func (c HistoryCommand) Name() string       { return "history" }
func (c HistoryCommand) Description() string { return "显示历史消息统计" }
func (c HistoryCommand) Execute(args []string) string {
	hist := c.eng.History()
	if len(hist) == 0 {
		return "历史为空"
	}
	rc := make(map[string]int)
	for _, h := range hist {
		rc[h.Role]++
	}
	var ps []string
	for r, cnt := range rc {
		ps = append(ps, fmt.Sprintf("%s: %d", r, cnt))
	}
	sort.Strings(ps)
	return fmt.Sprintf("共 %d 条 (%s)", len(hist), strings.Join(ps, ", "))
}

// TraceCommand — /trace
type TraceCommand struct{ eng *engine.Engine }

func (c TraceCommand) Name() string       { return "trace" }
func (c TraceCommand) Description() string { return "显示调用追踪树" }
func (c TraceCommand) Execute(args []string) string {
	tree := c.eng.ExportTrace()
	if tree == nil || tree.Root == nil {
		return "暂无追踪数据"
	}
	return tree.String()
}

// NewSessionCommand — /new（新建会话，持久化当前会话后清空）
type NewSessionCommand struct {
	eng        *engine.Engine
	sessionMgr *session.Manager
}

func (c NewSessionCommand) Name() string       { return "new" }
func (c NewSessionCommand) Description() string { return "新建会话（当前会话自动保存）" }
func (c NewSessionCommand) Execute(args []string) string {
	if err := c.sessionMgr.SaveCurrent(c.eng.SessionID()); err != nil {
		return fmt.Sprintf("保存会话失败: %v", err)
	}
	c.eng.ClearHistory()
	return fmt.Sprintf("已新建会话（已保存 %s）", c.eng.SessionID())
}

// ResumeCommand — /resume <id>
type ResumeCommand struct {
	eng        *engine.Engine
	sessionMgr *session.Manager
}

func (c ResumeCommand) Name() string       { return "resume" }
func (c ResumeCommand) Description() string { return "恢复历史会话：/resume <session_id>" }
func (c ResumeCommand) Execute(args []string) string {
	if len(args) == 0 {
		return "用法: /resume <session_id>（用 /sessions 查看）"
	}
	sid := strings.TrimSpace(args[0])
	if err := c.sessionMgr.Resume(sid); err != nil {
		return fmt.Sprintf("恢复失败: %v", err)
	}
	return fmt.Sprintf("已恢复会话 %s", sid)
}

// SessionsCommand — /sessions
type SessionsCommand struct {
	sessionMgr *session.Manager
}

func (c SessionsCommand) Name() string       { return "sessions" }
func (c SessionsCommand) Description() string { return "列出所有持久化会话" }
func (c SessionsCommand) Execute(args []string) string {
	metas := c.sessionMgr.List()
	if len(metas) == 0 {
		return "暂无持久化会话"
	}
	var b strings.Builder
	b.WriteString("持久化会话:\n")
	for _, m := range metas {
		b.WriteString(fmt.Sprintf("  %s  %s  tok:%d\n",
			m.SessionID, m.UpdatedAt.Format("01-02 15:04"), m.TokenCount))
	}
	return b.String()
}

// PoolCommand — /pool
type PoolCommand struct{ client *api.ChatClient }

func (c PoolCommand) Name() string       { return "pool" }
func (c PoolCommand) Description() string { return "显示账号池信息" }
func (c PoolCommand) Execute(args []string) string {
	pool := c.client.AccountPool()
	if pool == nil {
		return "无账号池"
	}
	var b strings.Builder
	b.WriteString("账号列表:\n")
	for _, a := range pool.All() {
		status := "✓"
		if a.Disabled {
			status = "✗"
		}
		b.WriteString(fmt.Sprintf("  %s [%s] %s %s\n", status, a.Provider, a.Name, a.Model))
	}
	return b.String()
}

// ExitCommand — /exit
type ExitCommand struct{}

func (c ExitCommand) Name() string       { return "exit" }
func (c ExitCommand) Description() string { return "退出程序" }
func (c ExitCommand) Execute(args []string) string {
	return "__EXIT__"
}

// ── 命令初始化（工厂方法）────────────────────────────────────────

func initCommands(
	eng *engine.Engine,
	client *api.ChatClient,
	modelName string,
	sessionMgr *session.Manager,
) {
	registerCommand(HelpCommand{})
	registerCommand(ClearCommand{eng: eng})
	registerCommand(ModelCommand{modelName: modelName, client: client})
	registerCommand(HistoryCommand{eng: eng})
	registerCommand(TraceCommand{eng: eng})
	registerCommand(NewSessionCommand{eng: eng, sessionMgr: sessionMgr})
	registerCommand(ResumeCommand{eng: eng, sessionMgr: sessionMgr})
	registerCommand(SessionsCommand{sessionMgr: sessionMgr})
	registerCommand(PoolCommand{client: client})
	registerCommand(ExitCommand{})
}

// ── 命令路由 ────────────────────────────────────────────────────

func executeCommand(raw string) *messageView {
	parts := strings.Fields(raw)
	if len(parts) == 0 {
		return nil
	}
	cmdName := strings.TrimPrefix(strings.ToLower(parts[0]), "/")
	args := parts[1:]

	if cmd, ok := getCommand(cmdName); ok {
		result := cmd.Execute(args)
		if result == "__EXIT__" {
			// 外层会处理退出
			return &messageView{role: "system", content: ""}
		}
		return &messageView{role: "system", content: result}
	}
	return &messageView{role: "system", content: fmt.Sprintf("未知命令: /%s（输入 /help）", cmdName)}
}

// ── 命令补全提示 ────────────────────────────────────────────

func commandSuggestions(prefix string) []suggestion {
	if prefix == "" {
		all := allCommands()
		suggs := make([]suggestion, len(all))
		for i, c := range all {
			suggs[i] = suggestion{
				text:        "/" + c.Name(),
				description: c.Description(),
				kind:        "command",
			}
		}
		return suggs
	}

	lower := strings.ToLower(prefix)
	var suggs []suggestion
	for _, c := range allCommands() {
		n := "/" + c.Name()
		if strings.HasPrefix(strings.ToLower(n), lower) {
			suggs = append(suggs, suggestion{
				text:        n,
				description: c.Description(),
				kind:        "command",
			})
		}
	}
	return suggs
}
