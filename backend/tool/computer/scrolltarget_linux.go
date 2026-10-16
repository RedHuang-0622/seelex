//go:build linux

package computer

import "time"

// 可滚动面板观测在 X11 上没有等价能力：Windows 靠 UI Automation 的
// ScrollPattern 读"滚动位置 / 视口占比"，那是控件树才有的信息，X11 协议里
// 只有窗口与像素。
//
// 因此这里显式返回 ErrUnsupported，并且把能力声明里的 ScrollTargets 置为
// false —— computer_scroll_targets 在 Linux 上根本不注册（见 tools.go 的
// 注册闸门），而不是挂一个每次调用都必然失败的摆设。
//
// computer_scroll 本身照常可用：它只需要"在某个坐标滚一格"；落点回读取不到时
// 只会少一块观测信息（tools_scroll.go 里对 scrollTargets 的错误是降级处理）。

// ListScrollTargets 在 X11 上返回 ErrUnsupported。
func (x11Desktop) ListScrollTargets(ScrollTargetOptions) ([]ScrollTarget, error) {
	return nil, ErrUnsupported
}

// ScrollStateAtPoint 在 X11 上返回 ErrUnsupported。
func (x11Desktop) ScrollStateAtPoint(Point, time.Duration) (ScrollTarget, bool, error) {
	return ScrollTarget{}, false, ErrUnsupported
}
