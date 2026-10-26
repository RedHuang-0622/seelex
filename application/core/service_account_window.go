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
)

// SetAccountWindow 改一个账号的上下文窗口（设置弹窗的挡位/自定义值）。
//
// 判据与落盘顺序（先写盘再改内存）都在 `backend/limit/accountwindow.Setter`——本方法只解析
// 宿主端口并转发（M5-R-limita 的接收者转移），错误语义与迁移前逐字一致。
func (service *Service) SetAccountWindow(_ context.Context, name string, window int) (int, error) {
	return service.adminApplicationComponent().SetAccountWindow(context.Background(), name, window)
}
