package accountwindow

import (
	"errors"
	"fmt"
	"strings"

	"github.com/RedHuang-0622/seelex/contract"
)

// Setter 是「每账号上下文窗口」的**可调面**（前端 设置 → 账号）：校验 + 先写盘再改内存 +
// 把生效值报回去。口径见本包 README：窗口是压缩预算的基数，逐账号可调；三个来源
// （界面设置 / `seelex.yaml` 的 limits.context_window / 账号档兜底）的优先级由运行时
// 一处判定，本类型只做边界校验与落盘。
//
// 依赖全部是契约端口（结构满足，无需适配层）：
//   - Runtime 为 nil 表示宿主运行面未实现 AccountWindowPort（调用即报"不支持"）；
//   - Store 为 nil 表示不落盘（只改内存）。
type Setter struct {
	Runtime contract.AccountWindowPort
	Store   contract.AccountWindowStore
}

// Set 改一个账号的上下文窗口（设置弹窗的挡位/自定义值）。
//
// window <= 0 = **清掉这个账号的覆盖**（回落到 seelex.yaml 的 limits.context_window），
// 不是"设成 0"。
//
// 顺序是**先写盘再改内存**（与 SetSessionReleaseWindow 同一条纪律）：写盘失败整步不改，
// 于是"重启后还是旧值"与"界面上看到的值"不会分叉。返回值是**真正生效**的窗口——前端
// 采纳它，不自己记"我以为的值"（后端可能归一化，界面显示的必须是生效的那个）。
func (s Setter) Set(name string, window int) (int, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return 0, errors.New("account window: account name is required")
	}
	// 负数不是"清除"，而是调用方 bug（0 才是清除）：静默当成清除会把用户设过的挡位
	// 悄悄抹掉，所以在边界就拒。
	if window < 0 {
		return 0, fmt.Errorf("account window: window must be >= 0 (0 = 清除该账号的覆盖), got %d", window)
	}
	if window > 0 {
		if err := Validate(window); err != nil {
			return 0, err
		}
	}
	if s.Runtime == nil {
		return 0, errors.New("account window: 当前宿主不支持按账号设窗口（运行面未实现 AccountWindowPort）")
	}
	if s.Store != nil {
		if err := s.Store.SaveAccountWindow(name, window); err != nil {
			return 0, fmt.Errorf("account window: %w", err)
		}
	}
	s.Runtime.SetAccountWindow(name, window)
	return s.Runtime.AccountWindowFor(name), nil
}
