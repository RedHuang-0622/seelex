package core

import (
	"fmt"
	"strings"
)

const missingHistoryContent = "[Seelex recovery note: the previous message had no text after an interrupted request; its original content is unavailable.]"

const toolCallHistoryContent = "[Seelex recovery note: the assistant issued the recorded tool call(s); the original accompanying text is unavailable.]"

const contextRecoveryPrefix = "<!-- seelex:context-recovery:v1 -->"
const providerRecoveryPrefix = "<!-- seelex:provider-recovery:v1 -->"
const contextRecoveryRequestDelimiter = "\n## Original User Request\n"

// isProviderOnlyHistoryContent identifies repair text that exists solely to
// satisfy providers requiring non-empty message content. It is never user
// authored and must not be rendered as an assistant reply after resume.
func isProviderOnlyHistoryContent(content string) bool {
	return content == missingHistoryContent || content == toolCallHistoryContent
}

// prepareProviderHistory makes every persisted message safe for providers that
// reject an empty `content` field, including assistant messages with tool calls.
// Tool calls are retained; only their absent explanatory text is restored.
func (service *Service) prepareProviderHistory() error {
	history := service.deps.Engine.History()
	prepared, repaired := repairEmptyHistoryContent(history)
	if !repaired {
		return nil
	}
	if err := service.deps.Engine.ReplaceHistory(service.deps.Engine.SessionID(), prepared); err != nil {
		return fmt.Errorf("repair empty provider history content: %w", err)
	}
	return nil
}

func repairEmptyHistoryContent(history []EngineMessage) ([]EngineMessage, bool) {
	prepared := make([]EngineMessage, len(history))
	copy(prepared, history)
	repaired := false
	for index := range prepared {
		message := &prepared[index]
		if strings.TrimSpace(message.Content) != "" {
			continue
		}
		if message.Role == "assistant" && len(message.ToolCalls) > 0 {
			message.Content = toolCallHistoryContent
			message.ContentSet = true
			repaired = true
			continue
		}
		if !message.ContentSet && message.Role == "assistant" && message.ReasoningContent != "" {
			message.Content = missingHistoryContent
			message.ContentSet = true
			repaired = true
			continue
		}
		if message.Role == "system" || message.Role == "user" || message.Role == "assistant" || message.Role == "tool" {
			message.Content = missingHistoryContent
			message.ContentSet = true
			repaired = true
		}
	}
	return prepared, repaired
}

func nonEmptyProviderInput(input string) string {
	if strings.TrimSpace(input) != "" {
		return input
	}
	return "[Seelex recovery note: the submitted request was empty. Ask the user to provide the missing request details.]"
}

// recoverProviderContext keeps a minimal, evidence-first continuation record
// after a provider rejects the accumulated transcript for exceeding its context
// window. It deliberately does not use a guessed token or character limit: the
// provider has already supplied the authoritative signal that the full history
// is unusable. The record stays private to the engine and is restored to the
// original user request after a successful subsequent turn.
func (service *Service) recoverProviderContext(err error, originalRequest string) error {
	if !isProviderContextExhaustion(err) {
		return nil
	}
	_, recoveryErr := service.recoverProviderFailure(err, originalRequest)
	return recoveryErr
}

// recoverProviderFailure replaces an unusable transcript with a bounded,
// private continuation record only after the provider has rejected the request.
// It never retries a timed-out tool turn automatically: a 504 leaves tool-side
// effects uncertain, so the user must explicitly continue from the checkpoint.
func (service *Service) recoverProviderFailure(err error, originalRequest string) (bool, error) {
	failureKind := classifyProviderFailure(err)
	if failureKind == providerFailureNone {
		return false, nil
	}
	prefix, heading, summary := providerRecoveryDetails(failureKind)

	service.mu.Lock()
	checkpoint := ""
	if state := service.taskExecution; state != nil {
		checkpoint = state.contextSummary()
		state.status = taskStatusInterrupted
	}
	requestID := service.snapshot.Chat.RequestID
	service.setTaskStateLocked(requestID, TaskInterrupted, summary)
	service.mu.Unlock()

	recovery := prefix + "\n## " + heading + `
The raw transcript was removed. Continue from the durable task checkpoint below;
use targeted tools to reacquire omitted detail, then deliver a result or state
what information is still needed. Do not assume a timed-out tool call can be
replayed safely.

` + checkpoint + contextRecoveryRequestDelimiter + nonEmptyProviderInput(originalRequest)

	history := service.deps.Engine.History()
	recovered := retainedSystemHistory(history)
	recovered = append(recovered, EngineMessage{Role: "user", Content: recovery, ContentSet: true})
	if err := service.deps.Engine.ReplaceHistory(service.deps.Engine.SessionID(), recovered); err != nil {
		return false, fmt.Errorf("recover provider context: %w", err)
	}
	return true, nil
}

type providerFailureKind string

const (
	providerFailureNone    providerFailureKind = ""
	providerFailureContext providerFailureKind = "context_exhausted"
	providerFailureHistory providerFailureKind = "invalid_history"
	providerFailureTimeout providerFailureKind = "timeout"
	providerFailureServer  providerFailureKind = "server_unavailable"
)

func classifyProviderFailure(err error) providerFailureKind {
	if isProviderContextExhaustion(err) {
		return providerFailureContext
	}
	if err == nil {
		return providerFailureNone
	}
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "chat content is empty") {
		return providerFailureHistory
	}
	if strings.Contains(message, "http 504") || strings.Contains(message, "timeout_error") ||
		strings.Contains(message, "upstream timeout") || strings.Contains(message, "request timeout") {
		return providerFailureTimeout
	}
	if strings.Contains(message, "http 500") || strings.Contains(message, "http 502") ||
		strings.Contains(message, "http 503") || strings.Contains(message, "http 529") ||
		strings.Contains(message, "server_error") || strings.Contains(message, "internal server error") {
		return providerFailureServer
	}
	return providerFailureNone
}

func providerRecoveryDetails(kind providerFailureKind) (prefix, heading, summary string) {
	switch kind {
	case providerFailureContext:
		return contextRecoveryPrefix, "Context Recovery", "The provider context window was exceeded; progress checkpoint saved."
	case providerFailureHistory:
		return providerRecoveryPrefix, "History Recovery", "The provider rejected an invalid conversation record; progress checkpoint saved."
	case providerFailureTimeout:
		return providerRecoveryPrefix, "Recoverable Provider Interruption", "The model service timed out; progress checkpoint saved. Continue to resume safely."
	case providerFailureServer:
		return providerRecoveryPrefix, "Recoverable Provider Interruption", "The model service was temporarily unavailable; progress checkpoint saved. Continue to resume safely."
	default:
		return "", "", ""
	}
}

func isProviderContextExhaustion(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "context window exceeds") ||
		strings.Contains(message, "context length") ||
		strings.Contains(message, "maximum context") ||
		strings.Contains(message, "上下文窗口")
}

func (service *Service) removeProviderContextRecovery() error {
	history := service.deps.Engine.History()
	filtered := make([]EngineMessage, 0, len(history))
	removed := false
	for _, message := range history {
		if message.Role == "user" && (strings.HasPrefix(message.Content, contextRecoveryPrefix) || strings.HasPrefix(message.Content, providerRecoveryPrefix)) {
			_, original, found := strings.Cut(message.Content, contextRecoveryRequestDelimiter)
			if found {
				message.Content = original
				message.ContentSet = true
				filtered = append(filtered, message)
			}
			removed = true
			continue
		}
		filtered = append(filtered, message)
	}
	if !removed {
		return nil
	}
	if err := service.deps.Engine.ReplaceHistory(service.deps.Engine.SessionID(), filtered); err != nil {
		return fmt.Errorf("remove provider context recovery: %w", err)
	}
	return nil
}
