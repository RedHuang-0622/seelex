package bootstrap

import (
	"fmt"
	"io"

	"github.com/RedHuang-0622/seelex/application"
	"github.com/RedHuang-0622/seelex/console"
	"github.com/RedHuang-0622/seelex/headless"
	wiringroot "github.com/RedHuang-0622/seelex/wiring"
	"github.com/RedHuang-0622/seelex/wiring/config"
)

func StartFrontend(flags *config.Flags, app *application.Service, backendOutput io.Writer, startupWarning string, version string) error {
	frontend := wiringroot.NewFrontend(app)
	switch flags.FrontendMode {
	case "gui":
		if err := startGUI(frontend, startupWarning, version); err != nil {
			return fmt.Errorf("GUI 错误: %w", err)
		}
		return nil
	case "headless":
		// headless 调试入口：无窗口装配同一 Application，经
		// SEELEX_HEADLESS_PORT 回环 RPC + 事件流驱动（headless/）。
		if err := headless.Run(frontend); err != nil {
			return fmt.Errorf("headless 错误: %w", err)
		}
		return nil
	case "backend":
		return console.Start(app, flags.BackendPrompt, flags.BackendTimeout, backendOutput)
	case "tui":
		return startTUI(frontend)
	default:
		return fmt.Errorf("不支持的前端模式 %q", flags.FrontendMode)
	}
}
