//go:build windows

package computer

// 本文件把 Win32 实现接到 Desktop 抽象面上：实现分散在
// screen_windows.go（GDI 截图 + DPI）、input_windows.go（SendInput）、
// window_windows.go（EnumWindows 等）、uia_windows.go（UI Automation）。

// win32Desktop 是 Windows 的 Desktop 实现；无状态，零值可用。
type win32Desktop struct{}

// Capabilities 报告 Windows 的桌面能力：原生截图/输入/窗口枚举，加上
// UI Automation 的可滚动面板观测（ScrollPattern）。
func (win32Desktop) Capabilities() Capabilities {
	return Capabilities{Desktop: true, ScrollTargets: true}
}

// current 是本平台的 Desktop 实现（见 desktop.go 的路由说明）。
var current Desktop = win32Desktop{}
