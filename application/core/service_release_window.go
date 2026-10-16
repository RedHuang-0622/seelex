package core

// service_release_window.go — 「会话驻留窗口」的可调面（前端 设置 → 会话驻留）。
//
// 口径：窗口值住在**进程级 limits**（limit.Limits.SessionReleaseWindow），由
// backend/limit 的原子快照持有；释放面**每次决策现读**它
// （session_release.go 的 releaseBudget → Limits().ReleaseWindow()）。所以这里的写
// 路径只换掉那一格：下一次释放就用新值，不必重启、不必遍历释放调用点、不必重建任何
// LRU 结构。
//
// 为什么读路径不挂进快照：窗口是**进程级**设置，而快照的 Runtime 投影是**按会话**
// 组装的（字段归属见 model.SessionRuntime）。把进程级读数塞进会话投影，会让"切会话
// 时它跟着动"变成一个假事实。设置弹窗是"打开时读一次"的面，与 SessionStorageConfig
// 同一姿势（另拉一次读数，不进快照）。

import (
	"context"
	"fmt"

	"github.com/RedHuang-0622/seelex/backend/limit"
)

// SetSessionReleaseWindow 改「受保护集之外最多保留几个已加载会话」，返回**真正生效**
// 的值（前端据此采纳，而不是假定请求值生效）。
//
// 次序是刻意的：先校验 → 再落盘 → 最后改内存。
//   - 校验在最前：非法值一个字节都不改（不写盘、不动内存）。
//   - 落盘在改内存之前：写盘失败 ⇒ 内存不动 ⇒ 返回值与"重启后的值"一致，前端回滚到
//     最后一份权威值是诚实的。
//   - 宿主没装配落盘口（Deps.ReleaseWindowStore == nil）⇒ 只本次运行有效，不算失败。
//
// 不设运行守卫（与 SetLoopLimit / SetReasoningEffort 不同）：窗口只约束"哪个**已
// 静默**的会话可以被释放"——受保护集含运行/排队/在飞会话（见 protectedSessionLocked），
// 所以会话在跑时改它既打断不了谁，也放不掉任何在飞的内存。
func (service *Service) SetSessionReleaseWindow(_ context.Context, window int) (int, error) {
	if err := limit.ValidateSessionReleaseWindow(window); err != nil {
		return 0, err
	}
	if service != nil && service.Deps.ReleaseWindowStore != nil {
		if err := service.Deps.ReleaseWindowStore.SaveSessionReleaseWindow(window); err != nil {
			return 0, fmt.Errorf("保存会话驻留窗口失败: %w", err)
		}
	}
	applied, err := ApplySessionReleaseWindow(window)
	if err != nil {
		return 0, err
	}
	return applied, nil
}

// SessionReleaseWindow 返回当前生效的会话驻留窗口（设置弹窗打开时读一次）。
//
// 它读的是**进程级**权威（不是按会话的投影），所以任何一面改了窗口都会在这里看到；
// 反过来说，前端不该把它缓存成"我以为的值"——弹窗每次打开都重新读一次。
func (service *Service) SessionReleaseWindow() int {
	return Limits().ReleaseWindow()
}
