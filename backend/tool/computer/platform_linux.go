//go:build linux

package computer

// 本文件把 X11 后端接到 Desktop 抽象面上：实现在 x11_linux.go（连接与输入
// 出口）、screen_linux.go（截图与光标）、input_linux.go（鼠标与键盘）、
// window_linux.go（窗口枚举与激活）。

// x11Desktop 是 Linux 的 Desktop 实现；无状态，零值可用。
type x11Desktop struct{}

// Capabilities 报告 Linux 的桌面能力：能连上 X11（且具备 XTEST）就有一套
// 完整的截图 / 输入 / 窗口能力。
//
// ScrollTargets 恒为 false：Windows 靠 UI Automation 的 ScrollPattern 读
// "滚动位置与视口占比"，那是控件树才有的信息，X11 协议里只有窗口与像素。
// 因此 Linux 上不注册 computer_scroll_targets（见 tools.go 的注册闸门）。
func (x11Desktop) Capabilities() Capabilities {
	if err := x11Probe(); err != nil {
		return Capabilities{}
	}
	return Capabilities{Desktop: true, ScrollTargets: false}
}

// Prepare 建立（并缓存）一次 X11 连接；失败原因原样返回，装配层据此不注册
// 工具族。X11 不需要像 Windows 那样声明 DPI 感知：坐标就是屏幕物理像素。
func (x11Desktop) Prepare() error { return x11Probe() }

// current 是本平台的 Desktop 实现（见 desktop.go 的路由说明）。
var current Desktop = x11Desktop{}
