//go:build darwin

package computer

import "fmt"

// 本文件把 macOS（CoreGraphics + Accessibility）实现接到 Desktop 抽象面上：
// 实现分散在 darwin_core.go（框架绑定与 CF 类型适配）、screen_darwin.go
// （截屏与光标）、input_darwin.go（CGEvent 注入）、window_darwin.go
//（窗口列表与 AX 置前）。

// darwinDesktop 是 macOS 的 Desktop 实现；无状态，零值可用。
type darwinDesktop struct{}

// Capabilities 报告 macOS 的桌面能力：CoreGraphics 提供了完整的截图 / 输入 /
// 窗口枚举，因此 Desktop 为 true。
//
// ScrollTargets 恒为 false：Windows 靠 UI Automation 的 ScrollPattern 才能读到
// "滚动位置与视口占比"，macOS 对应的信息在 Accessibility 的 AXScrollArea /
// AXScrollBar 里，本后端暂未实现。按登记闸门，macOS 上不注册
// computer_scroll_targets（见 tools.go 的 Register）。
func (darwinDesktop) Capabilities() Capabilities {
	if err := darwinProbe(); err != nil {
		return Capabilities{}
	}
	return Capabilities{Desktop: true, ScrollTargets: false}
}

// Prepare 建立框架绑定并检查「屏幕录制」授权。
//
// TCC 授权不能静默获得：没授权就没有画面可截，因此这里显式报错让用户去系统
// 设置里打开，而不是注册一族每次调用都失败的摆设（与 Linux 侧连接不上 X11 时
// 的处理同一口径）。10.15 之前没有授权探针，交由截图调用自行报错。
func (darwinDesktop) Prepare() error {
	if err := darwinProbe(); err != nil {
		return err
	}
	if cgPreflightScreenCaptureAccess == nil {
		return nil
	}
	if cgPreflightScreenCaptureAccess() {
		return nil
	}
	if cgRequestScreenCaptureAccess != nil {
		_ = cgRequestScreenCaptureAccess()
	}
	return fmt.Errorf("computer: macOS 未授予「屏幕录制」权限（系统设置 › 隐私与安全性 › 屏幕录制），授权后需重启 Seelex")
}

// current 是本平台的 Desktop 实现（见 desktop.go 的路由说明）。
var current Desktop = darwinDesktop{}
