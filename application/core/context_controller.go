package core

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/RedHuang-0622/seelex/seelexctx"
)

const (
	taskContextCheckpointPrefix = "<!-- seelex:context-checkpoint:v1 -->"
)

// compactTaskContext replaces the entire mutable transcript with one private,
// bounded checkpoint. Keeping every earlier tool-call argument or a series of
// individually truncated tool results still grows without bound, so it is not
// context management. The durable checkpoint records task state; omitted raw
// details must be reacquired with targeted tools.
//
// It is called by the Engine iteration hook, never while Service.mu is held.
func (service *Service) compactTaskContext(requestID string) error {
	policy := seelexctx.DefaultContextConfig()
	// A large result is not itself evidence that the complete conversation has
	// become unsafe. Keep a bounded head-and-tail preview in provider history;
	// the full user-visible result remains in the session record and can be
	// inspected or re-read with a targeted tool call. Only total context pressure
	// may replace the transcript with the durable checkpoint.
	if _, err := service.truncateOversizedToolResults(policy.MaxToolResultChars); err != nil {
		return err
	}
	history := service.deps.Engine.History()
	estimatedTokens := estimateEngineHistoryTokens(history)
	if len(history) == 0 || estimatedTokens < policy.CompressThreshold {
		return nil
	}

	service.mu.Lock()
	state := service.taskExecution
	if state == nil || state.requestID != requestID || state.status != taskStatusRunning {
		service.mu.Unlock()
		return nil
	}
	state.contextVersion++
	state.compactedEpoch = state.progressEpoch
	version := state.contextVersion
	checkpoint := fmt.Sprintf("%s\nversion: %d\n%s", taskContextCheckpointPrefix, version, state.contextSummary())
	service.mu.Unlock()

	compacted := taskContextRecoveryHistory(history, checkpoint)
	if err := service.deps.Engine.ReplaceHistory(service.deps.Engine.SessionID(), compacted); err != nil {
		return fmt.Errorf("compact task context: %w", err)
	}

	compaction := ContextCompaction{
		Version: version, Reason: "context_budget", MessagesBefore: len(history),
		EstimatedTokens: estimatedTokens, CompactedAt: time.Now(),
	}
	service.mu.Lock()
	recorded := service.recordContextCompactionLocked(requestID, compaction)
	var revision uint64
	if recorded {
		revision = service.bumpLocked()
	}
	service.mu.Unlock()
	if recorded {
		service.events.Publish(EventSnapshotChanged, revision, requestID, nil)
	}
	return nil
}

const toolOutputPreviewMarker = "\n...[tool output shortened in provider context; use a targeted tool call to re-read omitted detail]...\n"

// truncateOversizedToolResults reduces one noisy tool result without erasing
// the active conversation. It deliberately preserves the tool-call protocol
// fields and both ends of the output: command listings and failures frequently
// put their useful context at opposite ends.
func (service *Service) truncateOversizedToolResults(maxChars int) (bool, error) {
	history := service.deps.Engine.History()
	truncated, changed := truncateToolResults(history, maxChars)
	if !changed {
		return false, nil
	}
	if err := service.deps.Engine.ReplaceHistory(service.deps.Engine.SessionID(), truncated); err != nil {
		return false, fmt.Errorf("truncate tool results: %w", err)
	}
	return true, nil
}

func truncateToolResults(history []EngineMessage, maxChars int) ([]EngineMessage, bool) {
	if maxChars <= len(toolOutputPreviewMarker) {
		return history, false
	}
	truncated := append([]EngineMessage(nil), history...)
	changed := false
	for index := range truncated {
		message := &truncated[index]
		if message.Role != "tool" || len(message.Content) <= maxChars {
			continue
		}
		message.Content = toolOutputPreview(message.Content, maxChars)
		message.ContentSet = true
		changed = true
	}
	return truncated, changed
}

func toolOutputPreview(content string, maxChars int) string {
	if len(content) <= maxChars {
		return content
	}
	if maxChars <= len(toolOutputPreviewMarker) {
		return utf8Prefix(content, maxChars)
	}
	bodyChars := maxChars - len(toolOutputPreviewMarker)
	headChars := bodyChars / 2
	tailChars := bodyChars - headChars
	return utf8Prefix(content, headChars) + toolOutputPreviewMarker + utf8Suffix(content, tailChars)
}

func utf8Prefix(value string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	end := 0
	for index := range value {
		if index > maxBytes {
			break
		}
		end = index
	}
	if len(value) <= maxBytes {
		return value
	}
	return value[:end]
}

func utf8Suffix(value string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	start := len(value)
	for start > 0 {
		_, size := utf8.DecodeLastRuneInString(value[:start])
		if len(value[start-size:]) > maxBytes {
			break
		}
		start -= size
	}
	return value[start:]
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

// taskContextRecoveryHistory keeps system instructions and replaces all
// mutable user/assistant/tool protocol records with a checkpoint. Dropping a
// partial assistant/tool pair avoids an invalid tool protocol on the next
// provider request and ensures repeated compactions do not accumulate notes.
func taskContextRecoveryHistory(history []EngineMessage, checkpoint string) []EngineMessage {
	compacted := retainedSystemHistory(history)
	return append(compacted, EngineMessage{Role: "user", Content: checkpoint, ContentSet: true})
}

// retainedSystemHistory preserves one product instruction. Framework-side
// summaries are also system messages; retaining every one lets a faulty or
// repeated history replacement multiply the prompt rather than compact it.
func retainedSystemHistory(history []EngineMessage) []EngineMessage {
	for _, message := range history {
		if message.Role == "system" {
			return []EngineMessage{message}
		}
	}
	return nil
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
