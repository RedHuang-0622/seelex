//go:build gui

package bootstrap

import (
	"fmt"

	"github.com/RedHuang-0622/seelex/application"
	"github.com/RedHuang-0622/seelex/gui"
	"github.com/RedHuang-0622/seelex/wiring/config"
)

func guiAvailable() bool { return gui.Available() }

func startGUI(app *application.Frontend, startupWarning, version string) error {
	return gui.Run(app, gui.Options{
		Title: "Seelex", Version: version, StartupWarning: startupWarning,
		AppearancePath: config.GUIAppearancePath(),
	})
}

// GUI 产物专注桌面形态；TUI 使用不带 gui 标签的独立二进制。
func startTUI(*application.Frontend) error {
	return fmt.Errorf(`当前二进制未包含 TUI；请使用不带 "gui" 标签构建 TUI 二进制`)
}
