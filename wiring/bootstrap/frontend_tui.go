//go:build !gui

package bootstrap

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/RedHuang-0622/seelex/application"
	"github.com/RedHuang-0622/seelex/tui"
)

func guiAvailable() bool { return false }

func startGUI(*application.Service, string, string) error {
	return fmt.Errorf(`当前二进制未包含 GUI；请使用 go run -tags "gui,desktop,production" . -frontend gui`)
}

func InitTUI(app *application.Service) tui.Model { return tui.NewModel(app) }

func startTUI(app *application.Service) error {
	return StartTUI(InitTUI(app))
}

func StartTUI(model tui.Model) error {
	program := tea.NewProgram(model, tea.WithAltScreen())
	if _, err := program.Run(); err != nil {
		return fmt.Errorf("TUI 错误: %w", err)
	}
	return nil
}
