package core

import (
	cc "github.com/RedHuang-0622/seelex/application/core/context_control"
	"github.com/RedHuang-0622/seelex/application/core/task_context"
)

// ApplyWindowConfig 应用 seele.yaml window 段（零值字段自动补默认）；
// 门面导出，实现位于 context_control。压缩保留窗口决策（context_runtime）
// 与框架侧 WindowPolicy 读同一份配置，避免第二套硬编码窗口数字。
func ApplyWindowConfig(config WindowConfig) {
	cc.Apply(config)
}

// CurrentWindowConfig 返回当前生效的 window 段（含默认值）。
func CurrentWindowConfig() WindowConfig {
	return cc.Current()
}

// RetainedReadTailBudget 返回**读尾**（冷恢复装载 transcript/history 尾部）的
// 保留前缀 token 预算，与请求装配侧同一实现、同一份 window 配置：
//
//	token1 = window.retain_tokens（未配置 → 账号上下文窗口）
//	token2 = window.ratio × all_context（占比窗口）
//
// 读尾发生在请求装配之前，没有"本次要携带的上下文"可估 → all_context 取账号
// 上下文窗口（会话能携带的上限）：未达上限的会话不会被占比窗口误裁（读尾是
// 重建既有历史，不是新做压缩决策），达到上限的会话则与压缩侧保留同一窗口。
// 返回 0 = 无法判定（调用方保持不设限/旧口径）。
func RetainedReadTailBudget(runtime any) int {
	budget := task_context.ContextBudgetFor(runtime)
	return CurrentWindowConfig().RetainedContextTokens(budget.Window, budget.Window)
}
