//go:build !windows && !linux && !darwin

package computer

import "time"

// 本文件是"没有桌面实现"的平台（*bsd 等尚未接入的 GOOS）的占位实现：
// 每个原语都显式返回 ErrUnsupported，Capabilities 全为 false，因此装配层
// 不会注册任何 computer_* 工具——仓库宁可让模型看不到这族工具，也不挂一串
// 必然失败的摆设。
//
// 已接入的平台各有自己的 platform_<goos>.go：windows（user32/gdi32 + UIA）、
// linux（纯 Go 的 xgb/X11 + XTEST）、darwin（纯 Go dlopen 的 CoreGraphics +
// CGEvent + Accessibility）。要再接一个平台：新增 `platform_<goos>.go` +
// 对应的 screen/input/window 实现，并把本文件的 build tag 再排除掉该 GOOS；
// 清单与边界见 README 的"新增平台"一节。

type unsupportedDesktop struct{}

// Capabilities 报告本平台没有任何桌面能力。
func (unsupportedDesktop) Capabilities() Capabilities { return Capabilities{} }

// Prepare 无需准备。
func (unsupportedDesktop) Prepare() error { return nil }

// VirtualScreen 返回 ErrUnsupported。
func (unsupportedDesktop) VirtualScreen() (Rect, error) { return Rect{}, ErrUnsupported }

// CaptureShot 返回 ErrUnsupported。
func (unsupportedDesktop) CaptureShot(ScreenshotOptions) (Capture, error) {
	return Capture{}, ErrUnsupported
}

// SavePNG 返回 ErrUnsupported。
func (unsupportedDesktop) SavePNG(string, ScreenshotOptions) (Capture, error) {
	return Capture{}, ErrUnsupported
}

// CursorPosition 返回 ErrUnsupported。
func (unsupportedDesktop) CursorPosition() (Point, error) { return Point{}, ErrUnsupported }

// MoveMouse 返回 ErrUnsupported。
func (unsupportedDesktop) MoveMouse(Point) error { return ErrUnsupported }

// Click 返回 ErrUnsupported。
func (unsupportedDesktop) Click(Point, ClickOptions) error { return ErrUnsupported }

// Drag 返回 ErrUnsupported。
func (unsupportedDesktop) Drag(Point, Point, time.Duration) error { return ErrUnsupported }

// Scroll 返回 ErrUnsupported。
func (unsupportedDesktop) Scroll(Point, int) error { return ErrUnsupported }

// TypeText 返回 ErrUnsupported。
func (unsupportedDesktop) TypeText(string) error { return ErrUnsupported }

// PressKeys 返回 ErrUnsupported。
func (unsupportedDesktop) PressKeys(string, int) error { return ErrUnsupported }

// ListWindows 返回 ErrUnsupported。
func (unsupportedDesktop) ListWindows() ([]Window, error) { return nil, ErrUnsupported }

// ForegroundWindow 返回 ErrUnsupported。
func (unsupportedDesktop) ForegroundWindow() (Window, error) { return Window{}, ErrUnsupported }

// FocusWindow 返回 ErrUnsupported。
func (unsupportedDesktop) FocusWindow(string) (Window, error) { return Window{}, ErrUnsupported }

// ListScrollTargets 返回 ErrUnsupported（控件树是 Windows/macOS 才有的栈，
// X11 与其它平台不做假实现）。
func (unsupportedDesktop) ListScrollTargets(ScrollTargetOptions) ([]ScrollTarget, error) {
	return nil, ErrUnsupported
}

// ScrollStateAtPoint 返回 ErrUnsupported。
func (unsupportedDesktop) ScrollStateAtPoint(Point, time.Duration) (ScrollTarget, bool, error) {
	return ScrollTarget{}, false, ErrUnsupported
}

// current 是本平台的 Desktop 实现。
var current Desktop = unsupportedDesktop{}
