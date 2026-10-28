//go:build gui

package boot

import (
	"fmt"

	"github.com/RedHuang-0622/seelex/application"
	"github.com/RedHuang-0622/seelex/application/assembly"
	assemble "github.com/RedHuang-0622/seelex/backend/engine/assemble"
	"github.com/RedHuang-0622/seelex/gui"
	"github.com/RedHuang-0622/seelex/headless"
	"github.com/RedHuang-0622/seelex/internal/startup"
)

func guiAvailable() bool { return gui.Available() }

// guiDependencies 是桌面宿主的装配表：一行一个端口，右值是拥有该用例的组件指针。
//
// 绑定语句本身就是编译期结构断言（左端字段类型是 gui 自己的 consumer 接口、右端是
// 具体 owner 指针），所以这里不写适配器，也不再需要 `_ gui.X = (*owner)(nil)` 一行。
// 每一行右值都是拥有该用例的 owner，前端因此不需要兼容门面或适配器。
func guiDependencies(components assembly.Components, runtime *assemble.Runtime) gui.Dependencies {
	return gui.Dependencies{
		Accounts:         components.Admin.Account,
		Switches:         components.Policy.Switch,
		RoleSettings:     components.Policy.Role,
		Completion:       components.Conversation.Completion,
		History:          components.Conversation.History,
		ViewSnapshot:     components.Session.Snapshot,
		ViewEvents:       components.Session.View,
		SessionCatalog:   components.Session.Query,
		Draft:            components.Session.DraftEntry,
		Resume:           components.Session.Resume,
		Fork:             components.Session.Fork,
		Delete:           components.Session.Delete,
		Archive:          components.Session.Archive,
		Meta:             components.Session.Meta,
		SessionSubmit:    components.Session.Submit,
		Submission:       components.Conversation.Submission,
		Turn:             components.Runtime.Lifecycle,
		Interaction:      components.Session.Interaction,
		Prompts:          components.Conversation.PromptStack,
		Perf:             components.Admin.Perf,
		MessageEdit:      components.Conversation.EditFork,
		Queue:            components.Conversation.Queue,
		Conversation:     components.Conversation.Unified,
		HistorySearch:    components.Conversation.HistorySearch,
		References:       components.Conversation.Reference,
		WorkspaceCatalog: components.Workspace.Projects,
		WorkspaceActions: components.Workspace.Switch,
		// Composer：图片附件的 owner 在引擎侧（会话媒体分区 + 随图队列），
		// 应用层不认识它，所以这一格由组合根直接从 Runtime 取——不是"绕过端口"，
		// 端口形状仍由 gui 声明，这行赋值就是它的编译期断言。
		Composer:     runtime.ComposerMedia(),
		Schedules:    components.Runtime.Scheduler,
		WorkItems:    components.Execution.WorkItem,
		Subagents:    components.Execution.Subagent,
		RoleSessions: components.Policy.RoleSessions,
		ToolRecords:  components.Execution.ToolRecords,
		SessionSSH:   components.Session.SSH,
	}
}

// startGUI 拥有 GUI 形态的构造与关闭顺序：headless 冒烟控制面（只在
// SEELEX_HEADLESS_PORT 设置时启动）随 GUI 一起起、一起停，窗口生命周期
// 由 gui.Run 持有。这条顺序此前写在 gui/run_wails.go 里，等于让前端包
// 决定装配次序；组合根是唯一允许做这件事的位置。
func startGUI(service *application.Service, runtime *assemble.Runtime, startupWarning, version string) error {
	components := assembly.ComponentsFor(service)
	stopHeadless, err := headless.StartIfRequested(headlessDependencies(components))
	if err != nil {
		return err
	}
	defer stopHeadless()
	return gui.Run(guiDependencies(components, runtime), gui.Options{
		Title: "Seelex", Version: version, StartupWarning: startupWarning,
		AppearancePath: startup.GUIAppearancePath(),
	})
}

// GUI 产物专注桌面形态；TUI 使用不带 gui 标签的独立二进制。
func startTUI(*application.Service) error {
	return fmt.Errorf(`当前二进制未包含 TUI；请使用不带 "gui" 标签构建 TUI 二进制`)
}
