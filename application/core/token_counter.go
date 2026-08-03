package core

import (
	"strings"

	"github.com/RedHuang-0622/seelex/seelexctx"
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

// conservativeTokenCounter is used when the active provider does not expose
// its tokenizer. It counts protocol and tool-schema overhead in addition to a
// conservative text estimate; callers may replace it with a model tokenizer
// without changing context assembly.
type conservativeTokenCounter struct{}

func (conservativeTokenCounter) Name() string { return "conservative-v1" }

func (conservativeTokenCounter) CountText(value string) int {
	if value == "" {
		return 0
	}
	return (len([]byte(value)) + 2) / 3
}

func (counter conservativeTokenCounter) CountMessage(message EngineMessage) int {
	tokens := 4 + counter.CountText(message.Role) + counter.CountText(message.Content) + counter.CountText(message.ReasoningContent)
	if message.ToolCallID != "" {
		tokens += 2 + counter.CountText(message.ToolCallID)
	}
	if message.Name != "" {
		tokens += 2 + counter.CountText(message.Name)
	}
	for _, call := range message.ToolCalls {
		tokens += 8 + counter.CountText(call.ID) + counter.CountText(call.Name) + counter.CountText(call.Arguments)
	}
	return tokens
}

func (counter conservativeTokenCounter) CountRequest(systemPrompt string, history []EngineMessage, currentInput string, tools []Tool) int {
	tokens := 3
	if strings.TrimSpace(systemPrompt) != "" {
		tokens += counter.CountMessage(EngineMessage{Role: "system", Content: systemPrompt, ContentSet: true})
	}
	for _, message := range history {
		tokens += counter.CountMessage(message)
	}
	if strings.TrimSpace(currentInput) != "" {
		tokens += counter.CountMessage(EngineMessage{Role: "user", Content: currentInput, ContentSet: true})
	}
	for _, tool := range tools {
		// Runtime exposes the stable name and description, while provider
		// adapters add a function-schema envelope. Reserve enough protocol
		// space for that schema even when its exact tokenizer is unavailable.
		tokens += Limits().ToolTokenOverhead + counter.CountText(tool.Name) + counter.CountText(tool.Description) // limits.tool_token_overhead（默认 64）
	}
	return tokens
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
