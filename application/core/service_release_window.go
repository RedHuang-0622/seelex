package core

// service_release_window.go — 「会话驻留窗口」可调面的**宿主适配 + 门面转发**。
//
// 判据与次序（先校验 → 再落盘 → 最后改内存）已在 M5-R6-release-window 随接收者转移归位
// `middle/switch`（plan §2.4 的 `service_release_window.go` → `middle/switch/`）；本文件只剩宿主面：
// 取值校验（limit 域）、落盘口（`Deps.ReleaseWindowStore`）、改进程级生效值（`limit.Apply` 包装）
// 与当前读数。
//
// 口径（历史结论逐字保留）：窗口值住在**进程级 limits**，释放面每次决策现读它（`releaseBudget`），
// 所以写路径只换掉那一格；读路径**不挂进快照**（窗口是进程级设置，快照投影是按会话组装的，
// 塞进去会让"切会话时它跟着动"变成假事实）。

import (
	"context"

	"github.com/RedHuang-0622/seelex/backend/limit"
	"github.com/RedHuang-0622/seelex/middle/switch"
)

// SetSessionReleaseWindow 改「受保护集之外最多保留几个已加载会话」，返回**真正生效**的值
// （前端据此采纳，而不是假定请求值生效）。判据与次序在 `middle/switch`。
//
// 不设运行守卫（与 SetLoopLimit / SetReasoningEffort 不同）：窗口只约束"哪个已静默的会话可以被释放"，
// 受保护集含运行/排队/在飞会话，所以会话在跑时改它既打断不了谁，也放不掉任何在飞的内存。
func (service *Service) SetSessionReleaseWindow(_ context.Context, window int) (int, error) {
	return service.components.policyApplication.SetSessionReleaseWindow(window)
}

// SessionReleaseWindow 返回当前生效的会话驻留窗口（设置弹窗打开时读一次）。
func (service *Service) SessionReleaseWindow() int {
	return service.components.policyApplication.SessionReleaseWindow()
}

// releaseWindowDeps 装配驻留窗口可调面的宿主面。
func (service *Service) releaseWindowDeps() switching.ReleaseWindowDeps {
	deps := switching.ReleaseWindowDeps{
		Validate: limit.ValidateSessionReleaseWindow,
		Apply:    ApplySessionReleaseWindow,
		Current:  func() int { return Limits().ReleaseWindow() },
	}
	if service != nil && service.Deps.ReleaseWindowStore != nil {
		store := service.Deps.ReleaseWindowStore
		deps.Persist = func(window int) error { return store.SaveSessionReleaseWindow(window) }
	}
	return deps
}
