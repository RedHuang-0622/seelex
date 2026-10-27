package bootstrap

import (
	"fmt"
	"io"

	"github.com/RedHuang-0622/seelex/application"
	"github.com/RedHuang-0622/seelex/application/assembly"
	"github.com/RedHuang-0622/seelex/console"
	"github.com/RedHuang-0622/seelex/headless"
	"github.com/RedHuang-0622/seelex/wiring/config"
)

// StartFrontend 按 `-frontend` 选择形态，并把装配表交出的 owner 直接给该形态。
//
// 这里不构造兼容门面：门面（`application.Frontend`，M4 删除）只是把 17 个 owner 的
// 方法缝成一张宽接口，不拥有任何判据。各形态需要的窄端口由 frontend_ports.go 逐行绑定。
func StartFrontend(flags *config.Flags, app *application.Service, backendOutput io.Writer, startupWarning string, version string) error {
	components := assembly.ComponentsFor(app)
	switch flags.FrontendMode {
	case "gui":
		if err := startGUI(app, startupWarning, version); err != nil {
			return fmt.Errorf("GUI 错误: %w", err)
		}
		return nil
	case "headless":
		// headless 调试入口：无窗口装配同一批 owner，经
		// SEELEX_HEADLESS_PORT 回环 RPC + 事件流驱动（headless/）。
		if err := headless.Run(headlessDependencies(components)); err != nil {
			return fmt.Errorf("headless 错误: %w", err)
		}
		return nil
	case "backend":
		return console.Start(consoleDependencies(components), flags.BackendPrompt, flags.BackendTimeout, backendOutput)
	case "tui":
		return startTUI(app)
	default:
		return fmt.Errorf("不支持的前端模式 %q", flags.FrontendMode)
	}
}
