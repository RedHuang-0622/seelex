//go:build !gui

package boot

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/RedHuang-0622/seelex/application"
	"github.com/RedHuang-0622/seelex/application/assembly"
	assemble "github.com/RedHuang-0622/seelex/backend/engine/assemble"
	"github.com/RedHuang-0622/seelex/tui"
)

func guiAvailable() bool { return false }

func startGUI(*application.Service, *assemble.Runtime, string, string) error {
	return fmt.Errorf(`当前二进制未包含 GUI；请使用 go run -tags "gui,desktop,production" . -frontend gui`)
}

// tuiDependencies 是 TUI 的装配表：一行一个端口，右值是拥有该用例的组件指针。
//
// 绑定语句本身就是编译期结构断言（左端字段类型是 consumer 接口、右端是具体
// owner 指针），所以这里不写适配器，也不再需要 `_ tui.X = (*owner)(nil)` 一行。
// 每个端口直接对应拥有该用例的 owner，不存在中间转发层。
func tuiDependencies(components assembly.Components) tui.Dependencies {
	return tui.Dependencies{
		Snapshot:    components.Session.Snapshot,
		Events:      components.Session.View,
		Submission:  components.Conversation.Submission,
		Completion:  components.Conversation.Completion,
		Interaction: components.Session.Interaction,
		Lifecycle:   components.Runtime.Lifecycle,
		Effort:      components.Policy.Switch,
		History:     components.Conversation.History,
		Queue:       components.Conversation.Queue,
		ToolRecord:  components.Execution.ToolRecords,
	}
}

func InitTUI(service *application.Service) tui.Model {
	return tui.NewModel(tuiDependencies(assembly.ComponentsFor(service)))
}

func startTUI(service *application.Service) error {
	return StartTUI(InitTUI(service))
}

func StartTUI(model tui.Model) error {
	program := tea.NewProgram(model, tea.WithAltScreen())
	if _, err := program.Run(); err != nil {
		return fmt.Errorf("TUI 错误: %w", err)
	}
	return nil
}
