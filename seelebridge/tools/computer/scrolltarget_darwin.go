//go:build darwin

package computer

import "time"

// macOS 暂无可滚动面板观测：Capabilities().ScrollTargets 为 false，因此这两个
// 原语按约定返回 ErrUnsupported（而不是给一个编出来的答案），对应的
// computer_scroll_targets 工具在 macOS 上不注册（见 tools.go 的注册闸门）。
//
// 要补齐时：用 Accessibility 枚举 AXScrollArea 子树并读 AXVerticalScrollBar 的
// AXValue（0..1）与 AXSize，填进 ScrollAxis——纯函数的筛选/排序/点命中
//（scrolltarget.go）可直接复用，平台实现只需补这一层读属性。

// ListScrollTargets 返回 ErrUnsupported。
func (darwinDesktop) ListScrollTargets(ScrollTargetOptions) ([]ScrollTarget, error) {
	return nil, ErrUnsupported
}

// ScrollStateAtPoint 返回 ErrUnsupported。
func (darwinDesktop) ScrollStateAtPoint(Point, time.Duration) (ScrollTarget, bool, error) {
	return ScrollTarget{}, false, ErrUnsupported
}
