package task_context

import (
	"math"
	"strings"
	"sync"

	"github.com/RedHuang-0622/seelex/application/contract"
	"github.com/RedHuang-0622/seelex/application/core/internal/limits"
	"github.com/RedHuang-0622/seelex/application/model"
	"github.com/RedHuang-0622/seelex/seelexctx"
	"github.com/RedHuang-0622/seelex/seelexctx/tokens"
)

// ContextBudget 是 provider 上下文 token 预算（窗口/输出预留/安全预留/压缩
// 目标等）。
type ContextBudget struct {
	Window                int
	OutputReserve         int
	SafetyReserve         int
	Budget                int
	SoftThreshold         int
	HardThreshold         int
	TargetAfterCompaction int
	// SingleItemInputLimit 是**单条**输入（当前输入/单个事件）的超限外置阈值：
	// 超过它就把正文归档为 result_ref，只把引用告示交给模型（见
	// coordinator.protectOversizedCurrentInputLocked）。取值 = 预算 ×
	// context_single_item_percent（默认 50%）。0 = 未设置（手写字面量/老装配）：
	// 此时不外置，保持旧行为。
	SingleItemInputLimit int
}

// RequestTokenCounter 是上下文装配的 token 计数契约（可被模型 tokenizer
// 替换而不改装配）。
type RequestTokenCounter interface {
	Name() string
	CountText(string) int
	CountMessage(contract.EngineMessage) int
	CountRequest(string, []contract.EngineMessage, string, []model.Tool) int
}

type contextLimitProvider interface {
	ContextWindow() int
	MaxOutputTokens() int
}

// CalibratedTokenCounter 是生产默认计数器：
//   - 基础估算用 seelexctx/tokens 的脚本感知公式，并叠加 protocol/
//     tool-schema 开销；
//   - 每次 LLM 调用返回真实 usage 后，Observe 用 EMA 修正因子校正后续估算，
//     使事前估算向 provider 真实计数收敛（保守方向 clamp）。
type CalibratedTokenCounter struct {
	mu       sync.Mutex
	factor   float64 // 修正因子，初始 1.0，clamp [minCalibrationFactor, maxCalibrationFactor]
	observed int
}

const (
	minCalibrationFactor = 0.5
	maxCalibrationFactor = 2.5
	calibrationEMAAlpha  = 0.3
)

// NewCalibratedTokenCounter 构造生产默认计数器。
func NewCalibratedTokenCounter() *CalibratedTokenCounter {
	return &CalibratedTokenCounter{factor: 1}
}

// Name 返回计数器标识。
func (c *CalibratedTokenCounter) Name() string { return "calibrated-script-v1" }

// CountText 估算文本 token 数。
func (c *CalibratedTokenCounter) CountText(value string) int {
	if value == "" {
		return 0
	}
	return c.apply(tokens.Count(value))
}

// CountMessage 估算单条消息 token 数。
func (c *CalibratedTokenCounter) CountMessage(message contract.EngineMessage) int {
	count := 4 + c.CountText(message.Role) + c.CountText(message.Content) + c.CountText(message.ReasoningContent)
	if message.ToolCallID != "" {
		count += 2 + c.CountText(message.ToolCallID)
	}
	if message.Name != "" {
		count += 2 + c.CountText(message.Name)
	}
	for _, call := range message.ToolCalls {
		count += 8 + c.CountText(call.ID) + c.CountText(call.Name) + c.CountText(call.Arguments)
	}
	return count
}

// CountRequest 估算一次完整请求的 token 数（system + history + input +
// tool schema 开销）。
func (c *CalibratedTokenCounter) CountRequest(systemPrompt string, history []contract.EngineMessage, currentInput string, tools []model.Tool) int {
	count := 3
	if strings.TrimSpace(systemPrompt) != "" {
		count += c.CountMessage(contract.EngineMessage{Role: "system", Content: systemPrompt, ContentSet: true})
	}
	for _, message := range history {
		count += c.CountMessage(message)
	}
	if strings.TrimSpace(currentInput) != "" {
		count += c.CountMessage(contract.EngineMessage{Role: "user", Content: currentInput, ContentSet: true})
	}
	for _, tool := range tools {
		// Runtime exposes the stable name and description, while provider
		// adapters add a function-schema envelope. Reserve enough protocol
		// space for that schema even when its exact tokenizer is unavailable.
		count += limits.Get().ToolTokenOverhead + c.CountText(tool.Name) + c.CountText(tool.Description) // limits.tool_token_overhead（默认 64）
	}
	return count
}

// apply 对基础估算施加修正因子（向上取整；0 保持 0）。
func (c *CalibratedTokenCounter) apply(base int) int {
	if base <= 0 {
		return 0
	}
	c.mu.Lock()
	factor := c.factor
	c.mu.Unlock()
	return int(math.Ceil(float64(base) * factor))
}

// Observe 用一次 LLM 调用的真实 usage 校准因子（EMA；clamp 保守区间）。
func (c *CalibratedTokenCounter) Observe(estimated, actual int) {
	if estimated <= 0 || actual <= 0 {
		return
	}
	ratio := float64(actual) / float64(estimated)
	c.mu.Lock()
	c.factor = c.factor*(1-calibrationEMAAlpha) + ratio*calibrationEMAAlpha
	if c.factor < minCalibrationFactor {
		c.factor = minCalibrationFactor
	}
	if c.factor > maxCalibrationFactor {
		c.factor = maxCalibrationFactor
	}
	c.observed++
	c.mu.Unlock()
}

// DefaultContextBudget 返回基于 seelexctx 默认窗口的预算。
func DefaultContextBudget() ContextBudget {
	window := seelexctx.DefaultContextConfig().MaxTokens
	outputReserve := window / 8
	if outputReserve < limits.Get().OutputReserveTokens { // limits.output_reserve_tokens（默认 512）
		outputReserve = limits.Get().OutputReserveTokens
	}
	return newContextBudget(window, outputReserve)
}

// ContextBudgetFor 返回给定 Runtime 的上下文预算（未实现 contextLimitProvider
// 或配置非法时回退默认）。
func ContextBudgetFor(runtime any) ContextBudget {
	provider, ok := runtime.(contextLimitProvider)
	if !ok {
		return DefaultContextBudget()
	}
	window := provider.ContextWindow()
	outputReserve := provider.MaxOutputTokens()
	if window <= 0 || outputReserve <= 0 || outputReserve+window/8 >= window {
		return DefaultContextBudget()
	}
	return newContextBudget(window, outputReserve)
}

// newContextBudget 由上下文窗口与输出预留推导压缩预算。所有比例都是**配置项**
// （config/seelex.yaml limits 段：context_safety_reserve_divisor /
// context_soft_percent / context_hard_percent / context_target_percent /
// context_single_item_percent），不在这里硬编码：调参不该改代码，而魔法数字一旦
// 散在代码里，报表口径与判据口径就会各说各话（读者看到"超了硬线"却什么都没发生）。
// 默认值只有一处来源：seelexctx.DefaultLimits()（配置默认档）。本函数只做
// "调用方手写字面量/老装配"的防御性兜底，不另立一套数字。
func newContextBudget(window, outputReserve int) ContextBudget {
	if window <= 0 {
		return ContextBudget{}
	}
	settings := limits.Get()
	defaults := seelexctx.DefaultLimits()
	divisor := settings.ContextSafetyReserveDivisor
	if divisor <= 0 {
		divisor = defaults.ContextSafetyReserveDivisor
	}
	safetyReserve := window / divisor
	budget := window - outputReserve - safetyReserve
	return ContextBudget{
		Window: window, OutputReserve: outputReserve, SafetyReserve: safetyReserve,
		Budget: budget,
		// 2026-09-30 起自动压缩只剩一条线（取消软线提前量，见 context_runtime 的
		// 压缩判据）：这里按**硬线**比例产出 SoftThreshold，报告面/进度面因此与判据
		// 同源（软 == 硬 = 唯一那条线），不再出现"报表说已过软线、系统却没折"的
		// 口径分裂。limits.context_soft_percent 自此不再被消费（保留键兼容旧配置）。
		SoftThreshold:         percentOf(budget, settings.ContextHardPercent, defaults.ContextHardPercent),
		HardThreshold:         percentOf(budget, settings.ContextHardPercent, defaults.ContextHardPercent),
		TargetAfterCompaction: percentOf(budget, settings.ContextTargetPercent, defaults.ContextTargetPercent),
		SingleItemInputLimit:  percentOf(budget, settings.ContextSingleItemPercent, defaults.ContextSingleItemPercent),
	}
}

// percentOf 取预算的百分比；配置缺失/非法（<=0 或 >100）时回退默认比例，
// 保证写错配置也不会把预算算成 0（那会让每一轮都触发压缩）。
func percentOf(value, percent, fallback int) int {
	if percent <= 0 || percent > 100 {
		percent = fallback
	}
	return value * percent / 100
}
