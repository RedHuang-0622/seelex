// ── 命令系统 ────────────────────────────────────────────────
//
// 策略模式 + 子包 commands 分离。
// 本文件只做两件事：
//   1. RegisterCommands — 装配命令并同步到 sugg 引擎
//   2. executeCommand — 供 handleEnter 调用的入口

package tui

import (
	"fmt"
	"strings"

	"github.com/RedHuang-0622/Seele/engine"

	"github.com/RedHuang-0622/seelex/session"
	"github.com/RedHuang-0622/seelex/skill"
	"github.com/RedHuang-0622/seelex/tui/commands"
	"github.com/RedHuang-0622/seelex/tui/sugg"
)

// RegisterCommands 注册所有命令并将命令列表同步到 sugg 引擎。
func RegisterCommands(
	eng *engine.Engine,
	runtime RuntimeView,
	modelName string,
	sessionMgr *session.Manager,
	skillReg *skill.Registry,
	skillLoader *skill.Loader,
	plugins PluginController,
) {
	commands.RegisterBuiltin(eng, runtime, modelName, sessionMgr, plugins)

	// 将命令名称同步到 sugg 引擎
	var cmdSuggestions []sugg.Suggestion
	for _, c := range commands.All() {
		cmdSuggestions = append(cmdSuggestions, sugg.Suggestion{
			Text: c.Name(), Description: c.Description(), Kind: "command",
		})
	}
	refreshCmdSuggestions = cmdSuggestions
	_ = skillReg
	_ = skillLoader
}

var refreshCmdSuggestions []sugg.Suggestion

// SyncCommandSuggestions 将已注册的命令同步到 sugg 引擎。
func SyncCommandSuggestions(eng *sugg.Engine) {
	if refreshCmdSuggestions != nil {
		eng.SetCommands(refreshCmdSuggestions)
		refreshCmdSuggestions = nil
	}
}

// executeCommand 执行命令字符串（不含 / 前缀），返回显示消息。
func executeCommand(raw string) *messageView {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil
	}
	parts := strings.Fields(trimmed)
	name := strings.ToLower(strings.TrimPrefix(parts[0], "/"))
	args := parts[1:]

	output := commands.Execute(name, args)
	if name == "exit" && output == "" {
		return &messageView{role: "system", content: ""}
	}
	if strings.HasPrefix(output, "未知命令") {
		return &messageView{
			role:    "system",
			content: fmt.Sprintf("未知命令: %s。输入 /help 查看可用命令。", name),
		}
	}
	return &messageView{role: "system", content: output}
}
