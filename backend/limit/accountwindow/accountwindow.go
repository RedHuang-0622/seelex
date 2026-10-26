// Package accountwindow 是「每账号上下文窗口」的唯一判据处：界面挡位表与自定义值的
// 校验边界。窗口本身是压缩预算的基数（预算 = 窗口 − 账号档 max_tokens − 窗口÷安全
// 除数），「主账号 1M、便宜的子代理 200k」是常态，所以它是**逐账号**的设置。
//
// 生态位与边界见 README.md。判据层只依赖标准库。
package accountwindow

import (
	"fmt"

	"github.com/RedHuang-0622/seelex/contract/dto"
)

// MaxWindow 是自定义挡位的**防手滑上界**（1000 万 token）：它不是"模型真的有这么大"，
// 只是拦掉多打一个 0 之类的输入；填得比模型真实窗口大，请求会被 provider 拒。
const MaxWindow = 10_000_000

// Presets 返回界面上「每账号上下文窗口」的挡位（token，升序）。
// 每账号也可以走自定义值（见 Validate）。
func Presets() []int {
	return dto.AccountWindowPresets()
}

// Validate 校验界面设的账号窗口。
//
// 0 不是"合法的窗口"，而是"清掉这个账号的覆盖"（回落到 seelex.yaml 的
// limits.context_window）；那个语义由调用方在进这里之前处置，进到这里必须是正数。
//
// 错误文案里的 `seelexctx:` 前缀是**搬家前的历史包袱**（本包 2026-10-16 从
// `seelexctx/window.go` 归位到 limit 板块）。文案是用户可见面，按"行为零变化"
// 先逐字保留；要改前缀属于产品可见面的决定，另立一项，不在搬迁里顺手改。
func Validate(window int) error {
	if window <= 0 {
		return fmt.Errorf("seelexctx: account context window must be positive, got %d", window)
	}
	if window > MaxWindow {
		return fmt.Errorf("seelexctx: account context window %d exceeds the sanity bound %d",
			window, MaxWindow)
	}
	return nil
}
