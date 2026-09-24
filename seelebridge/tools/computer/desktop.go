package computer

import "time"

// ── 原语抽象面 ──────────────────────────────────────────────
//
// computer use 全部平台相关的行为都收在 Desktop 这一个接口里：工具层
// （tools*.go）、MCP 面（mcp/）与 Seelex 装配层都只依赖它，不关心底下是
// Win32、X11 还是别的栈。
//
// 路由是**编译期**完成的，不做运行时探测。这不是偷懒，是硬约束：Windows
// 实现直连 user32/gdi32（`syscall.NewLazyDLL` 只定义在 syscall 的
// dll_windows.go 里），把它编到别的平台连编译都过不去。因此每个平台在
// 自己的 `platform_<goos>.go` 里给一个 `current`，由 build tag 决定哪一个
// 进二进制——这也与仓库"不支持平台编译为桩"的既有做法一致，并保住
// CGO_ENABLED=0 的四平台交叉编译。
//
// 加一个平台 = 新写一个实现 Desktop 的文件 + 一个 Current()；工具层、权限
// 规则、媒体通路一行都不用改。

// Capabilities 声明一个实现能回答哪些问题。工具层据此决定注册哪些工具：
// 宁可不暴露，也不挂一串必然失败的摆设。
type Capabilities struct {
	// Desktop 表示当前进程有可操作的桌面：能截图、能注入输入、能枚举窗口。
	Desktop bool
	// ScrollTargets 表示实现能只读回答"哪些面板可以用滚轮操作、滚到哪一段、
	// 视口还有多少内容"。Windows 用 UI Automation 的 ScrollPattern（见
	// uia_windows.go）；X11 协议里没有这个信息，因此 Linux 为 false，对应
	// 的 computer_scroll_targets 工具在 Linux 上不注册。
	ScrollTargets bool
}

// Desktop 是桌面 computer use 的平台实现面：13 个操作原语 + 2 个可滚动面板
// 观测 + 能力声明。坐标语义统一为虚拟桌面（多显示器并集）的物理像素。
type Desktop interface {
	// Capabilities 报告本实现支持哪些能力（工具注册的闸门）。
	Capabilities() Capabilities
	// Prepare 做一次性进程级准备，返回错误表示桌面能力不可用（装配层据此
	// 不注册工具族）。Windows 在此声明 Per-Monitor V2 DPI 感知；macOS 应在
	// 此检查屏幕录制与辅助功能授权（TCC 不能静默获得，必须报错让用户去
	// 系统设置里打开）。
	Prepare() error

	// 观测
	VirtualScreen() (Rect, error)
	CaptureShot(ScreenshotOptions) (Capture, error)
	SavePNG(path string, opts ScreenshotOptions) (Capture, error)
	CursorPosition() (Point, error)
	ListWindows() ([]Window, error)
	ForegroundWindow() (Window, error)

	// 输入注入
	MoveMouse(Point) error
	Click(Point, ClickOptions) error
	Drag(from, to Point, duration time.Duration) error
	Scroll(point Point, delta int) error
	TypeText(text string) error
	PressKeys(combo string, times int) error
	FocusWindow(match string) (Window, error)

	// 可滚动面板观测；Capabilities().ScrollTargets 为 false 时必须返回
	// ErrUnsupported，而不是给一个编出来的答案。
	ListScrollTargets(ScrollTargetOptions) ([]ScrollTarget, error)
	ScrollStateAtPoint(Point, time.Duration) (ScrollTarget, bool, error)
}
