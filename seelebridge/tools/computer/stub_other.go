//go:build !windows

package computer

import "time"

// EnableDPIAwareness 在非 Windows 平台为空实现。
func EnableDPIAwareness() {}

// VirtualScreen 在非 Windows 平台返回 ErrUnsupported。
func VirtualScreen() (Rect, error) { return Rect{}, ErrUnsupported }

// Capture 在非 Windows 平台返回 ErrUnsupported。
func CaptureShot(opts ScreenshotOptions) (Capture, error) { return Capture{}, ErrUnsupported }

// SavePNG 在非 Windows 平台返回 ErrUnsupported。
func SavePNG(path string, opts ScreenshotOptions) (Capture, error) {
	return Capture{}, ErrUnsupported
}

// CursorPosition 在非 Windows 平台返回 ErrUnsupported。
func CursorPosition() (Point, error) { return Point{}, ErrUnsupported }

// MoveMouse 在非 Windows 平台返回 ErrUnsupported。
func MoveMouse(p Point) error { return ErrUnsupported }

// Click 在非 Windows 平台返回 ErrUnsupported。
func Click(p Point, opts ClickOptions) error { return ErrUnsupported }

// Drag 在非 Windows 平台返回 ErrUnsupported。
func Drag(from, to Point, duration time.Duration) error { return ErrUnsupported }

// Scroll 在非 Windows 平台返回 ErrUnsupported。
func Scroll(p Point, delta int) error { return ErrUnsupported }

// TypeText 在非 Windows 平台返回 ErrUnsupported。
func TypeText(text string) error { return ErrUnsupported }

// PressKeys 在非 Windows 平台返回 ErrUnsupported。
func PressKeys(combo string, times int) error { return ErrUnsupported }

// ListWindows 在非 Windows 平台返回 ErrUnsupported。
func ListWindows() ([]Window, error) { return nil, ErrUnsupported }

// ForegroundWindow 在非 Windows 平台返回 ErrUnsupported。
func ForegroundWindow() (Window, error) { return Window{}, ErrUnsupported }

// FocusWindow 在非 Windows 平台返回 ErrUnsupported。
func FocusWindow(match string) (Window, error) { return Window{}, ErrUnsupported }
