package core

import (
	"math"
	"strings"
	"sync"

	"github.com/RedHuang-0622/seelex/seelexctx"
	"github.com/RedHuang-0622/seelex/seelexctx/tokens"
)

type contextBudget struct {
	Window                int
	OutputReserve         int
	SafetyReserve         int
	Budget                int
	SoftThreshold         int
	HardThreshold         int
	TargetAfterCompaction int
}

type requestTokenCounter interface {
	Name() string
	CountText(string) int
	CountMessage(EngineMessage) int
	CountRequest(string, []EngineMessage, string, []Tool) int
}

type contextLimitProvider interface {
	ContextWindow() int
	MaxOutputTokens() int
}

// calibratedTokenCounter 是生产默认计数器：
//   - 基础估算用 seelexctx/tokens 的脚本感知公式（替代 len/3），并叠加
//     protocol/tool-schema 开销；
//   - 每次 LLM 调用返回真实 usage 后，Observe 用 EMA 修正因子校正后续估算，
//     使事前估算向 provider 真实计数收敛（保守方向 clamp），
//     避免“估算说够、实际爆顶”。
//
// 实现可被模型 tokenizer 替换而不改上下文装配（requestTokenCounter 契约）。
type calibratedTokenCounter struct {
	mu       sync.Mutex
	factor   float64 // 修正因子，初始 1.0，clamp [minCalibrationFactor, maxCalibrationFactor]
	observed int
}

const (
	minCalibrationFactor = 0.5
	maxCalibrationFactor = 2.5
	calibrationEMAAlpha  = 0.3
)

func newCalibratedTokenCounter() *calibratedTokenCounter {
	return &calibratedTokenCounter{factor: 1}
}

func (c *calibratedTokenCounter) Name() string { return "calibrated-script-v1" }

func (c *calibratedTokenCounter) CountText(value string) int {
	if value == "" {
		return 0
	}
	return c.apply(tokens.Count(value))
}

func (c *calibratedTokenCounter) CountMessage(message EngineMessage) int {
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

func (c *calibratedTokenCounter) CountRequest(systemPrompt string, history []EngineMessage, currentInput string, tools []Tool) int {
	count := 3
	if strings.TrimSpace(systemPrompt) != "" {
		count += c.CountMessage(EngineMessage{Role: "system", Content: systemPrompt, ContentSet: true})
	}
	for _, message := range history {
		count += c.CountMessage(message)
	}
	if strings.TrimSpace(currentInput) != "" {
		count += c.CountMessage(EngineMessage{Role: "user", Content: currentInput, ContentSet: true})
	}
	for _, tool := range tools {
		// Runtime exposes the stable name and description, while provider
		// adapters add a function-schema envelope. Reserve enough protocol
		// space for that schema even when its exact tokenizer is unavailable.
		count += Limits().ToolTokenOverhead + c.CountText(tool.Name) + c.CountText(tool.Description) // limits.tool_token_overhead（默认 64）
	}
	return count
}

// apply 对基础估算施加修正因子（向上取整；0 保持 0）。
func (c *calibratedTokenCounter) apply(base int) int {
	if base <= 0 {
		return 0
	}
	c.mu.Lock()
	factor := c.factor
	c.mu.Unlock()
	return int(math.Ceil(float64(base) * factor))
}

// Observe 用一次 LLM 调用的真实 usage 校准：
// factor = (1-α)·factor + α·(actual/estimated)，结果 clamp 到
// [minCalibrationFactor, maxCalibrationFactor]；estimated/actual 非正时忽略
// （如工具型响应 usage 为 0）。
func (c *calibratedTokenCounter) Observe(estimated, actual int) {
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

func defaultContextBudget() contextBudget {
	window := seelexctx.DefaultContextConfig().MaxTokens
	outputReserve := window / 8
	if outputReserve < Limits().OutputReserveTokens { // limits.output_reserve_tokens（默认 512）
		outputReserve = Limits().OutputReserveTokens
	}
	return newContextBudget(window, outputReserve)
}

func contextBudgetFor(runtime any) contextBudget {
	limits, ok := runtime.(contextLimitProvider)
	if !ok {
		return defaultContextBudget()
	}
	window := limits.ContextWindow()
	outputReserve := limits.MaxOutputTokens()
	if window <= 0 || outputReserve <= 0 || outputReserve+window/8 >= window {
		return defaultContextBudget()
	}
	return newContextBudget(window, outputReserve)
}

func newContextBudget(window, outputReserve int) contextBudget {
	safetyReserve := window / 8
	budget := window - outputReserve - safetyReserve
	return contextBudget{
		Window: window, OutputReserve: outputReserve, SafetyReserve: safetyReserve,
		Budget: budget, SoftThreshold: budget * 75 / 100, HardThreshold: budget * 90 / 100,
		TargetAfterCompaction: budget * 60 / 100,
	}
}
