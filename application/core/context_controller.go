package core

import (
	"fmt"
	"strings"

	"github.com/RedHuang-0622/seelex/seelexctx"
)

const (
	taskContextCheckpointPrefix = "<!-- seelex:context-checkpoint:v1 -->"
	taskContextHistoryTokens    = 12 * 1024
	taskContextToolResultChars  = 1600
)

// compactTaskContext replaces verbose historical tool output with bounded
// evidence and appends a request-private checkpoint for the next ReAct turn.
// It is called by the Engine iteration hook, never while Service.mu is held.
func (service *Service) compactTaskContext(requestID string) error {
	history := service.deps.Engine.History()
	if len(history) == 0 || estimateEngineHistoryTokens(history) < taskContextHistoryTokens && !hasOversizedToolResult(history) {
		return nil
	}

	service.mu.Lock()
	state := service.taskExecution
	if state == nil || state.requestID != requestID || state.status != taskStatusRunning {
		service.mu.Unlock()
		return nil
	}
	if state.contextVersion > 0 && state.compactedEpoch == state.progressEpoch {
		service.mu.Unlock()
		return nil
	}
	state.contextVersion++
	state.compactedEpoch = state.progressEpoch
	checkpoint := fmt.Sprintf("%s\nversion: %d\n%s", taskContextCheckpointPrefix, state.contextVersion, state.contextSummary())
	service.mu.Unlock()

	compacted := make([]EngineMessage, len(history))
	copy(compacted, history)
	for index := range compacted {
		message := &compacted[index]
		if message.Role != "tool" || len(message.Content) <= taskContextToolResultChars {
			continue
		}
		message.Content = "[tool output compacted; durable checkpoint follows]\n" + boundedEvidence(message.Content)
	}
	compacted = append(compacted, EngineMessage{Role: "user", Content: checkpoint})
	if err := service.deps.Engine.ReplaceHistory(service.deps.Engine.SessionID(), compacted); err != nil {
		return fmt.Errorf("compact task context: %w", err)
	}
	return nil
}

func estimateEngineHistoryTokens(history []EngineMessage) int {
	tokens := 0
	for _, message := range history {
		tokens += seelexctx.EstimateTokens(message.Content)
		tokens += seelexctx.EstimateTokens(message.ReasoningContent)
		for _, call := range message.ToolCalls {
			tokens += seelexctx.EstimateTokens(call.Name) + seelexctx.EstimateTokens(call.Arguments)
		}
	}
	return tokens
}

func hasOversizedToolResult(history []EngineMessage) bool {
	for _, message := range history {
		if message.Role == "tool" && len(message.Content) > taskContextToolResultChars {
			return true
		}
	}
	return false
}

// removeTaskContextCheckpoints prevents Application-only control messages from
// being persisted or reconstructed as frontend conversation placeholders.
func (service *Service) removeTaskContextCheckpoints() error {
	history := service.deps.Engine.History()
	filtered := make([]EngineMessage, 0, len(history))
	removed := false
	for _, message := range history {
		if message.Role == "user" && strings.HasPrefix(message.Content, taskContextCheckpointPrefix) {
			removed = true
			continue
		}
		filtered = append(filtered, message)
	}
	if !removed {
		return nil
	}
	if err := service.deps.Engine.ReplaceHistory(service.deps.Engine.SessionID(), filtered); err != nil {
		return fmt.Errorf("remove task context checkpoint: %w", err)
	}
	return nil
}

func isTaskContextCheckpoint(content string) bool {
	return strings.HasPrefix(content, taskContextCheckpointPrefix)
}
