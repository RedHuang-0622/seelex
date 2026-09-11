// Package computer 提供桌面 computer use 原语：屏幕截图、鼠标与键盘输入、
// 窗口枚举与聚焦。
//
// 生态位见同目录 README.md。Windows 实现基于 user32/gdi32 系统调用；
// 非 Windows 平台编译为返回 ErrUnsupported 的桩实现，使仓库维持
// 跨平台 `go build ./...` 可编译。
package computer

import (
	"errors"
	"fmt"
	"image"
	"time"
)

// ErrUnsupported 表示当前平台没有桌面 computer use 实现。
var ErrUnsupported = errors.New("computer: 当前平台不支持桌面 computer use")

// Rect 是屏幕坐标矩形，坐标为虚拟桌面（多显示器并集）坐标系下的物理像素。
type Rect struct {
	X      int
	Y      int
	Width  int
	Height int
}

// Contains 判断虚拟桌面坐标点是否落在矩形内。
func (r Rect) Contains(x, y int) bool {
	return x >= r.X && x < r.X+r.Width && y >= r.Y && y < r.Y+r.Height
}

// Center 返回矩形中心点。
func (r Rect) Center() Point {
	return Point{X: r.X + r.Width/2, Y: r.Y + r.Height/2}
}

// String 便于日志与工具输出。
func (r Rect) String() string {
	return fmt.Sprintf("%dx%d+%d+%d", r.Width, r.Height, r.X, r.Y)
}

// Point 是虚拟桌面坐标系下的一个点。
type Point struct {
	X int
	Y int
}

// Window 是顶层窗口的观测结果。
type Window struct {
	Handle    uintptr
	Title     string
	Rect      Rect
	Visible   bool
	Minimized bool
}

// ClickOptions 描述一次点击动作。
type ClickOptions struct {
	// Button 取 left、right、middle，空值等同 left。
	Button string
	// Clicks 为点击次数，0 视为 1。
	Clicks int
	// Interval 为多次点击之间的间隔，0 使用默认 60ms。
	Interval time.Duration
}

// ScreenshotOptions 描述截图参数。
type ScreenshotOptions struct {
	// Region 为空时截取整个虚拟桌面。
	Region *Rect
	// MaxWidth 大于 0 时按比例缩小返回图像（仅供模型查看），
	// 坐标系不因此改变。
	MaxWidth int
}

// Capture 描述一次截图结果。
type Capture struct {
	// Image 是最终返回的图像（可能已按 MaxWidth 缩放）。
	Image *image.RGBA
	// Region 是实际截取的虚拟桌面区域。
	Region Rect
	// Scale 是 Image 相对 Region 的缩放系数，1 表示未缩放。
	Scale float64
	// Cursor 是截图时刻的光标位置，便于模型定位。
	Cursor Point
}

// DefaultClickInterval 是多次点击的默认间隔。
const DefaultClickInterval = 60 * time.Millisecond

// Sleep 等待指定毫秒数，供 GUI 稳定期使用。
func Sleep(ms int) {
	if ms <= 0 {
		return
	}
	time.Sleep(time.Duration(ms) * time.Millisecond)
}
