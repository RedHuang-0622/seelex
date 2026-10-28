package boot

import (
	"fmt"
	"io"

	"github.com/RedHuang-0622/seelex/application"
	"github.com/RedHuang-0622/seelex/application/assembly"
	assemble "github.com/RedHuang-0622/seelex/backend/engine/assemble"
	"github.com/RedHuang-0622/seelex/console"
	"github.com/RedHuang-0622/seelex/headless"
	"github.com/RedHuang-0622/seelex/internal/startup"
)

// StartFrontend 按 `-frontend` 选择形态，并把装配表交出的 owner 直接给该形态。
//
// 这里不构造统一前端门面。各形态需要的窄端口由 frontend_ports.go 逐行绑定到
// owner，前端包只持有自己声明的最小依赖。
//
// runtime 是引擎宿主的唯一指针：GUI 的图片附件面（输入框拖拽/粘贴的图直接送进
// 会话媒体分区）只存在于引擎里，应用层不认识它，所以由组合根把它交下去——这也是
// 唯一不走 Components 的一格（理由见 gui/dependencies.go 的 ComposerMedia）。
func StartFrontend(flags *startup.Flags, app *application.Service, runtime *assemble.Runtime, backendOutput io.Writer, startupWarning string, version string) error {
	components := assembly.ComponentsFor(app)
	switch flags.FrontendMode {
	case "gui":
		if err := startGUI(app, runtime, startupWarning, version); err != nil {
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
