package account

// window_overlay.go 是**每账号上下文窗口的活体覆盖层**（P21-8「域方法随类型走」：从
// `backend/engine/assemble/runtime_account.go` 把状态与判据搬进账号路由域）。
//
// 它回答一个问题：**这个账号生效的上下文窗口是多少**。三个来源各有自己的家，本文件是
// 唯一一处把它们排成优先级的地方：
//
//	账号级覆盖（界面设过；数据根 runtime-limits.json，落盘在 backend/limit/overlay）
//	  > limits.context_window（config/seelex.yaml 的全局值）
//	    > 账号档自报的 context_window（遗留兼容键）
//
// 为什么需要账号级：窗口是压缩预算的基数，而"主账号 1M、便宜的子代理账号 200k"是常态——
// 一刀切会让子代理账号被按大窗口算预算。覆盖值只影响判据用的窗口，provider 侧仍有自己的硬
// 限制（填大了请求会被拒）。
//
// 状态与判据随类型走：本类型持有互斥量与表，装配层只持一个句柄并转发（`Runtime.SetAccountWindow`
// / `AccountWindows` / `ContextWindowFor`），界面侧（`backend/limit/accountwindow.Setter`）与
// 应用层（`application/core/service_account_window.go`）都只校验 + 落盘 + 读生效值，不各判一次。

import (
	"strings"
	"sync"
)

// WindowDeps 是**判定生效窗口时**需要的两格宿主读数（都是函数值，域包因此不认识
// 配置与账号池的具体类型）。
type WindowDeps struct {
	// Global 是 `limits.context_window`（config/seelex.yaml 的全局值；<= 0 = 没设）。
	Global int
	// AccountFallback 读账号档自报的窗口（遗留兼容键）。nil = 没有这一档（按 0 参与）。
	AccountFallback func(name string) int
}

// WindowOverlay 是"界面按账号设过的上下文窗口"的活体覆盖层（唯一一份状态）。
//
// 读写都用值拷贝：调用方拿到的 map 永远改不动运行时那一份（`Snapshot`）。
type WindowOverlay struct {
	mu      sync.RWMutex
	windows map[string]int
}

// NewWindowOverlay 装配一例：把起点表（数据根覆盖层读出的那份）拷进自己的表。
// 空白名与非正值在**入口**就被挡掉（表里只存"设过的正值"），空表归一为 nil。
func NewWindowOverlay(seed map[string]int) *WindowOverlay {
	overlay := &WindowOverlay{}
	if len(seed) == 0 {
		return overlay
	}
	windows := make(map[string]int, len(seed))
	for name, window := range seed {
		if strings.TrimSpace(name) == "" || window <= 0 {
			continue
		}
		windows[name] = window
	}
	if len(windows) > 0 {
		overlay.windows = windows
	}
	return overlay
}

// Set 改一个账号的窗口覆盖：`window > 0` 写入这一格，`window <= 0` 清掉它（回落到全局值——
// 不是"设成 0"）。空白账号名忽略（没有可改的键）。
//
// 改后**立刻生效**：下一次判据就按新值算，不需要重启——界面上的挡位因此是"按下去就作数"，
// 与 SessionReleaseWindow 同一条口径。
func (overlay *WindowOverlay) Set(name string, window int) {
	if overlay == nil {
		return
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}
	overlay.mu.Lock()
	defer overlay.mu.Unlock()
	if window > 0 {
		if overlay.windows == nil {
			overlay.windows = map[string]int{}
		}
		overlay.windows[name] = window
		return
	}
	delete(overlay.windows, name)
}

// Snapshot 返回一份拷贝（诊断与落盘对账用；调用方改不动运行时的表，空表返回 nil）。
func (overlay *WindowOverlay) Snapshot() map[string]int {
	if overlay == nil {
		return nil
	}
	overlay.mu.RLock()
	defer overlay.mu.RUnlock()
	return copyWindows(overlay.windows)
}

// For 读该账号的界面覆盖（没设过 → 0）。
func (overlay *WindowOverlay) For(name string) int {
	if overlay == nil || strings.TrimSpace(name) == "" {
		return 0
	}
	overlay.mu.RLock()
	defer overlay.mu.RUnlock()
	return overlay.windows[name]
}

// Effective 返回该账号**生效**的上下文窗口：覆盖 > 全局值 > 账号档自报。
//
// 这是本文件顶部那条优先级的**唯一实现处**：三个来源的读数由 deps 给，本方法只做那一跳。
func (overlay *WindowOverlay) Effective(name string, deps WindowDeps) int {
	if window := overlay.For(name); window > 0 {
		return window
	}
	if deps.Global > 0 {
		return deps.Global
	}
	if deps.AccountFallback == nil {
		return 0
	}
	return deps.AccountFallback(name)
}

// copyWindows 复制一份窗口表（装配期与诊断读用；空表返回 nil）。
func copyWindows(source map[string]int) map[string]int {
	if len(source) == 0 {
		return nil
	}
	copied := make(map[string]int, len(source))
	for name, window := range source {
		if strings.TrimSpace(name) == "" || window <= 0 {
			continue
		}
		copied[name] = window
	}
	if len(copied) == 0 {
		return nil
	}
	return copied
}
