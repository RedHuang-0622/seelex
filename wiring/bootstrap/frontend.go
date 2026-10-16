package bootstrap

import (
	"fmt"
	"io"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/RedHuang-0622/seelex/application"
	"github.com/RedHuang-0622/seelex/application/console"
	"github.com/RedHuang-0622/seelex/gui"
	"github.com/RedHuang-0622/seelex/tui"
	"github.com/RedHuang-0622/seelex/wiring/config"
)

func InitTUI(app *application.Service) tui.Model { return tui.NewModel(app) }

func StartTUI(model tui.Model) error {
	program := tea.NewProgram(model, tea.WithAltScreen())
	if _, err := program.Run(); err != nil {
		return fmt.Errorf("TUI 错误: %w", err)
	}
	return nil
}

func StartFrontend(flags *config.Flags, app *application.Service, backendOutput io.Writer, startupWarning string, version string) error {
	switch flags.FrontendMode {
	case "gui":
		if err := gui.Run(app, gui.Options{
			Title: "Seelex", Version: version, StartupWarning: startupWarning,
			// 外观设置（皮肤 → 窗口材质）的落盘路径：建窗期读它决定窗口要不要走
			// 透明/亚克力（毛玻璃皮肤），换肤期由 Bridge.SetWindowAppearance 写它。
			AppearancePath: config.GUIAppearancePath(),
		}); err != nil {
			return fmt.Errorf("GUI 错误: %w", err)
		}
		return nil
	case "headless":
		// headless 调试入口：无窗口装配同一 Application，经
		// SEELEX_HEADLESS_PORT 回环 RPC + 事件流驱动（gui/headless.go）。
		if err := gui.RunHeadless(app); err != nil {
			return fmt.Errorf("headless 错误: %w", err)
		}
		return nil
	case "backend":
		return console.Start(app, flags.BackendPrompt, flags.BackendTimeout, backendOutput)
	default:
		return StartTUI(InitTUI(app))
	}
}
