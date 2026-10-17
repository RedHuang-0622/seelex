package computer

import "time"

// 本文件是原语层的门面：包级函数一律转发给当前平台（编译期选定）的
// Desktop 实现。外部调用方——mcp/、seelebridge 装配层、真机冒烟测试——继续
// 用包级函数，换平台对它们完全透明。

// Current 返回当前平台的 Desktop 实现。路由在编译期由 build tag 决定
// （见 desktop.go 的说明），不做运行时探测。
func Current() Desktop { return current }

// Prepare 见 Desktop.Prepare。
func Prepare() error { return current.Prepare() }

// Supported 报告当前平台是否有可用的桌面 computer use。装配层据此决定是否
// 注册整个工具族（宁可不给，也不挂一串必然失败的摆设）。
func Supported() bool { return current.Capabilities().Desktop }

// ScrollTargetsSupported 报告当前实现能否只读回答可滚动面板的状态。
func ScrollTargetsSupported() bool { return current.Capabilities().ScrollTargets }

// VirtualScreen 返回虚拟桌面（多显示器并集）矩形。
func VirtualScreen() (Rect, error) { return current.VirtualScreen() }

// CaptureShot 抓屏并按选项缩放。
func CaptureShot(opts ScreenshotOptions) (Capture, error) { return current.CaptureShot(opts) }

// SavePNG 抓屏并写入 PNG 文件。
func SavePNG(path string, opts ScreenshotOptions) (Capture, error) {
	return current.SavePNG(path, opts)
}

// CursorPosition 返回当前光标位置（虚拟桌面坐标）。
func CursorPosition() (Point, error) { return current.CursorPosition() }

// MoveMouse 把光标移动到指定坐标。
func MoveMouse(p Point) error { return current.MoveMouse(p) }

// Click 移动光标到指定坐标并点击。
func Click(p Point, opts ClickOptions) error { return current.Click(p, opts) }

// Drag 按住左键从 from 拖到 to 再释放。
func Drag(from, to Point, duration time.Duration) error { return current.Drag(from, to, duration) }

// Scroll 在指定坐标滚动滚轮，delta 为 WHEEL_DELTA(120) 的倍数，正数向上。
func Scroll(p Point, delta int) error { return current.Scroll(p, delta) }

// TypeText 以字面文本方式注入输入（支持 CJK 与 emoji）。
func TypeText(text string) error { return current.TypeText(text) }

// PressKeys 按下组合键（形如 ctrl+shift+t）。
func PressKeys(combo string, times int) error { return current.PressKeys(combo, times) }

// ListWindows 枚举顶层窗口。
func ListWindows() ([]Window, error) { return current.ListWindows() }

// ForegroundWindow 返回当前前台窗口。
func ForegroundWindow() (Window, error) { return current.ForegroundWindow() }

// FocusWindow 按标题子串查找窗口并置前。
func FocusWindow(match string) (Window, error) { return current.FocusWindow(match) }

// ListScrollTargets 枚举窗口里可用滚轮操作的面板。
func ListScrollTargets(opts ScrollTargetOptions) ([]ScrollTarget, error) {
	return current.ListScrollTargets(opts)
}

// ScrollStateAtPoint 回读落点处的可滚动面板状态。
func ScrollStateAtPoint(p Point, timeout time.Duration) (ScrollTarget, bool, error) {
	return current.ScrollStateAtPoint(p, timeout)
}
