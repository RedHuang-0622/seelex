package core

import (
	cc "github.com/RedHuang-0622/seelex/application/core/context_control"
	"github.com/RedHuang-0622/seelex/application/core/task_context"
	"github.com/RedHuang-0622/seelex/seelexctx"
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
//	floor  = limits.context_retain_floor_percent × 预算（保护区下限；0 = 未配置）
//
// 读尾发生在请求装配之前，没有"本次要携带的上下文"可估 → all_context 取账号
// 上下文窗口（会话能携带的上限）：未达上限的会话不会被占比窗口误裁（读尾是
// 重建既有历史，不是新做压缩决策），达到上限的会话则与压缩侧保留同一窗口。
// 返回 0 = 无法判定（调用方保持不设限/旧口径）。
func RetainedReadTailBudget(runtime any) int {
	budget := task_context.ContextBudgetFor(runtime)
	return CurrentWindowConfig().RetainedContextTokensWithFloor(budget.Window, budget.Window, RetainFloorTokens(budget))
}

// RetainFloorTokens 返回当前配置下保护区下限的**比例部分**（token 数）：
// limits.context_retain_floor_percent × 预算；未配置（0）或预算不可用时为 0
// —— 此时下限只剩「至少 1 个完整协议单元」这条兜底（见 seelexctx.RetainFloorTokens）。
func RetainFloorTokens(budget task_context.ContextBudget) int {
	return seelexctx.RetainFloorTokens(Limits().ContextRetainFloorPercent, budget.Budget)
}

// ValidateRetainWindow 校验 window 段与 limits 段的组合是否自洽（启动期调用）：
// 保护区下限（比例 × 预算）不得高于保留上限 window.retain_tokens —— 见
// seelexctx.WindowConfig.ValidateRetainWindow（非法组合必须报错，不许静默取小）。
//
// windowTokens 取账号声明的上下文窗口，预算按 task_context 的生产公式推导
// （窗口 − 输出预留 − 窗口/安全除数），与压缩判据共用同一份口径；
// windowTokens <= 0（账号未声明窗口）时只校验比例本身（比例 × 预算无从计算，
// 该维留给运行期把下限夹进上限）。
func ValidateRetainWindow(config WindowConfig, limits seelexctx.Limits, windowTokens int) error {
	budget := 0
	if windowTokens > 0 {
		budget = task_context.ContextBudgetFor(retainWindowLimits{
			window: windowTokens, output: limits.OutputReserveTokens,
		}).Budget
	}
	return config.ValidateRetainWindow(seelexctx.RetainFloorTokens(limits.ContextRetainFloorPercent, budget))
}

// retainWindowLimits 是启动期预算推导的最小 provider（ContextBudgetFor 的输入
// 只要求窗口与输出预留两个数；避免在 core 里复制一份预算公式）。
type retainWindowLimits struct {
	window int
	output int
}

func (r retainWindowLimits) ContextWindow() int   { return r.window }
func (r retainWindowLimits) MaxOutputTokens() int { return r.output }
