//go:build gui

package bootstrap

import (
	"fmt"

	"github.com/RedHuang-0622/seelex/application"
	"github.com/RedHuang-0622/seelex/application/assembly"
	"github.com/RedHuang-0622/seelex/gui"
	"github.com/RedHuang-0622/seelex/headless"
	wiringroot "github.com/RedHuang-0622/seelex/wiring"
	"github.com/RedHuang-0622/seelex/wiring/config"
)

func guiAvailable() bool { return gui.Available() }

// guiDependencies 是桌面宿主的装配表：一行一个端口，右值是拥有该用例的组件指针。
//
// 绑定语句本身就是编译期结构断言（左端字段类型是 gui 自己的 consumer 接口、右端是
// 具体 owner 指针），所以这里不写适配器，也不再需要 `_ gui.X = (*owner)(nil)` 一行。
// 每一行右值都与兼容门面（`application/compat`，M4 删除）转发的字段逐字相同：门面
// 每个方法都转发到同一个 owner 的同名方法，迁移前后判据落在同一处。
//
// `App` 是过渡字段（宽门面，M3 分片迁移后由 S7 删除）；它只承载尚未迁移的片，
// 新能力一律进按 owner 切分的端口，禁止再往宽门面上加方法。
func guiDependencies(components assembly.Components, app *application.Frontend) gui.Dependencies {
	return gui.Dependencies{
		App:          app,
		Accounts:     components.Admin.Account,
		Switches:     components.Policy.Switch,
		RoleSettings: components.Policy.Role,
		Completion:   components.Conversation.Completion,
		History:      components.Conversation.History,
		ViewSnapshot: components.Session.Snapshot,
		ViewEvents:   components.Session.View,
	}
}

// startGUI 拥有 GUI 形态的构造与关闭顺序：headless 冒烟控制面（只在
// SEELEX_HEADLESS_PORT 设置时启动）随 GUI 一起起、一起停，窗口生命周期
// 由 gui.Run 持有。这条顺序此前写在 gui/run_wails.go 里，等于让前端包
// 决定装配次序；组合根是唯一允许做这件事的位置。
func startGUI(service *application.Service, startupWarning, version string) error {
	components := assembly.ComponentsFor(service)
	stopHeadless, err := headless.StartIfRequested(headlessDependencies(components))
	if err != nil {
		return err
	}
	defer stopHeadless()
	return gui.Run(guiDependencies(components, wiringroot.NewFrontend(service)), gui.Options{
		Title: "Seelex", Version: version, StartupWarning: startupWarning,
		AppearancePath: config.GUIAppearancePath(),
	})
}

// GUI 产物专注桌面形态；TUI 使用不带 gui 标签的独立二进制。
func startTUI(*application.Service) error {
	return fmt.Errorf(`当前二进制未包含 TUI；请使用不带 "gui" 标签构建 TUI 二进制`)
}
