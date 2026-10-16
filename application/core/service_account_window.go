package core

// service_account_window.go — 「每账号上下文窗口」的可调面（前端 设置 → 账号）。
//
// 口径：窗口是压缩预算的基数（预算 = 窗口 − 账号档 max_tokens − 窗口÷安全除数），
// 而"主账号 1M、便宜的子代理账号 200k"是常态——所以它是**逐账号**的设置，不是一刀切。
// 三个来源各有自己的家：界面设的进数据根（宿主的 AccountWindowStore）、全局阈值进
// config/seelex.yaml 的 limits.context_window、账号档那份是遗留兜底；优先级由运行时
// （seelebridge 的 ContextWindowFor）一处判定，本层只校验 + 落盘 + 把生效值报回去。

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/RedHuang-0622/seelex/backend/limit/accountwindow"
	"github.com/RedHuang-0622/seelex/contract"
)

// SetAccountWindow 改一个账号的上下文窗口（设置弹窗的挡位/自定义值）。
//
// window <= 0 = **清掉这个账号的覆盖**（回落到 seelex.yaml 的 limits.context_window），
// 不是"设成 0"。
//
// 顺序是**先写盘再改内存**（与 SetSessionReleaseWindow 同一条纪律）：写盘失败整步不改，
// 于是"重启后还是旧值"与"界面上看到的值"不会分叉。返回值是**真正生效**的窗口——前端
// 采纳它，不自己记"我以为的值"（后端可能归一化，界面显示的必须是生效的那个）。
func (service *Service) SetAccountWindow(_ context.Context, name string, window int) (int, error) {
	if service == nil {
		return 0, errors.New("account window: service is unavailable")
	}
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
		if err := accountwindow.Validate(window); err != nil {
			return 0, err
		}
	}
	port, ok := service.Deps.Runtime.(contract.AccountWindowPort)
	if !ok {
		return 0, errors.New("account window: 当前宿主不支持按账号设窗口（运行面未实现 AccountWindowPort）")
	}
	if store := service.Deps.AccountWindowStore; store != nil {
		if err := store.SaveAccountWindow(name, window); err != nil {
			return 0, fmt.Errorf("account window: %w", err)
		}
	}
	port.SetAccountWindow(name, window)
	return port.AccountWindowFor(name), nil
}
