package commands

import (
	"fmt"
	"sort"
	"strings"

	"github.com/RedHuang-0622/Seele/agent/core/api"
	"github.com/RedHuang-0622/Seele/engine"

	"github.com/RedHuang-0622/seelex/session"
)

// ── 命令工厂 ─────────────────────────────────────────────────

// RegisterBuiltin 注册所有内置命令。
func RegisterBuiltin(
	eng *engine.Engine,
	client *api.ChatClient,
	modelName string,
	sessionMgr *session.Manager,
) {
	Register(&helpCmd{})
	Register(&clearCmd{eng: eng})
	Register(&modelCmd{modelName: modelName, client: client})
	Register(&historyCmd{eng: eng})
	Register(&traceCmd{eng: eng})
	Register(&newSessionCmd{eng: eng, sessionMgr: sessionMgr})
	Register(&resumeCmd{eng: eng, sessionMgr: sessionMgr})
	Register(&sessionsCmd{sessionMgr: sessionMgr})
	Register(&poolCmd{client: client})
	Register(&exitCmd{})
}

// ── 具体命令 ─────────────────────────────────────────────────

type helpCmd struct{}

func (helpCmd) Name() string        { return "help" }
func (helpCmd) Description() string { return "显示帮助信息" }
func (helpCmd) Execute([]string) string {
	var b strings.Builder
	b.WriteString("可用命令:\n")
	for _, cmd := range All() {
		b.WriteString(fmt.Sprintf("  /%-12s  %s\n", cmd.Name(), cmd.Description()))
	}
	b.WriteString("\n提示: @=工具  /=命令  #=Skill")
	return b.String()
}

type clearCmd struct{ eng *engine.Engine }

func (c clearCmd) Name() string        { return "clear" }
func (c clearCmd) Description() string  { return "清空对话历史" }
func (c clearCmd) Execute([]string) string {
	c.eng.ClearHistory()
	return "已清空"
}

type modelCmd struct {
	modelName string
	client    *api.ChatClient
}

func (c modelCmd) Name() string        { return "model" }
func (c modelCmd) Description() string  { return "显示当前模型和 Provider" }
func (c modelCmd) Execute([]string) string {
	return fmt.Sprintf("Model: %s  Provider: %s", c.modelName, c.client.ProviderFilter())
}

type historyCmd struct{ eng *engine.Engine }

func (c historyCmd) Name() string        { return "history" }
func (c historyCmd) Description() string  { return "显示历史消息统计" }
func (c historyCmd) Execute([]string) string {
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

type traceCmd struct{ eng *engine.Engine }

func (c traceCmd) Name() string        { return "trace" }
func (c traceCmd) Description() string  { return "显示调用追踪树" }
func (c traceCmd) Execute([]string) string {
	tree := c.eng.ExportTrace()
	if tree == nil || tree.Root == nil {
		return "暂无追踪数据"
	}
	return tree.String()
}

type newSessionCmd struct {
	eng        *engine.Engine
	sessionMgr *session.Manager
}

func (c newSessionCmd) Name() string        { return "new" }
func (c newSessionCmd) Description() string  { return "新建会话（当前会话自动保存）" }
func (c newSessionCmd) Execute([]string) string {
	if err := c.sessionMgr.SaveCurrent(c.eng.SessionID()); err != nil {
		return fmt.Sprintf("保存会话失败: %v", err)
	}
	c.eng.ClearHistory()
	return fmt.Sprintf("已新建会话（已保存 %s）", c.eng.SessionID())
}

type resumeCmd struct {
	eng        *engine.Engine
	sessionMgr *session.Manager
}

func (c resumeCmd) Name() string        { return "resume" }
func (c resumeCmd) Description() string  { return "恢复历史会话：/resume <session_id>" }
func (c resumeCmd) Execute(args []string) string {
	if len(args) == 0 {
		return "用法: /resume <session_id>（用 /sessions 查看）"
	}
	if err := c.sessionMgr.Resume(strings.TrimSpace(args[0])); err != nil {
		return fmt.Sprintf("恢复失败: %v", err)
	}
	return fmt.Sprintf("已恢复会话 %s", args[0])
}

type sessionsCmd struct{ sessionMgr *session.Manager }

func (c sessionsCmd) Name() string        { return "sessions" }
func (c sessionsCmd) Description() string  { return "列出所有持久化会话" }
func (c sessionsCmd) Execute([]string) string {
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

type poolCmd struct{ client *api.ChatClient }

func (c poolCmd) Name() string        { return "pool" }
func (c poolCmd) Description() string  { return "显示账号池信息" }
func (c poolCmd) Execute([]string) string {
	pool := c.client.AccountPool()
	if pool == nil {
		return "无账号池"
	}
	var b strings.Builder
	b.WriteString("账号列表:\n")
	for _, a := range pool.All() {
		b.WriteString(fmt.Sprintf("  %s  %s  %s\n", a.Name, a.Provider, a.Model))
	}
	return b.String()
}

type exitCmd struct{}

func (exitCmd) Name() string        { return "exit" }
func (exitCmd) Description() string  { return "退出程序" }
func (exitCmd) Execute([]string) string { return "" }
