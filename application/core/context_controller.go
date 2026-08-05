package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/RedHuang-0622/seelex/seelexctx"
)

const taskContextCheckpointPrefix = "<!-- seelex:context-checkpoint:v1 -->"

const planContextPrefix = "<!-- seelex:active-plan:v1 -->"
const toolResultOmittedPrefix = "<seelex-tool-result-omitted>"

var errProviderContextBudgetExceeded = errors.New("provider context exceeds the safe token budget")

// compactTaskContext replaces the entire mutable transcript with one private,
// bounded checkpoint. Keeping every earlier tool-call argument or a series of
// individually truncated tool results still grows without bound, so it is not
// context management. The durable checkpoint records task state; omitted raw
// details must be reacquired with targeted tools.
//
// It is called by the Engine iteration hook, never while Service.mu is held.
func (service *contextCoordinator) compactTaskContext(requestID string) error {
	_, err := service.prepareExecutionContext(requestID, "")
	if err != nil {
		return err
	}
	if err := service.persistCurrentSession(service.deps.Engine.SessionID()); err != nil {
		return fmt.Errorf("persist context checkpoint: %w", err)
	}
	return nil
}

// prepareExecutionContext rebuilds the provider cache from durable task state
// and complete transcript units. It returns a possibly referenced current
// input and refuses to send any request that still exceeds the safe budget.
func (service *contextCoordinator) prepareExecutionContext(requestID, currentInput string) (string, error) {
	if _, err := service.rejectOversizedToolResults(defaultToolResultLimit()); err != nil {
		return "", err
	}
	budget := contextBudgetFor(service.deps.Runtime)
	tools := service.deps.Runtime.VisibleTools(context.Background())
	existing := service.deps.Engine.History()
	service.mu.RLock()
	systemPrompt := service.systemPromptForActiveTaskLocked()
	service.mu.RUnlock()
	service.deps.Engine.SetSystemPrompt(systemPrompt)
	rawTokens := service.tokenCounter.CountRequest(systemPrompt, existing, currentInput, tools)

	runtimeModel := service.deps.Runtime.Model()
	service.mu.Lock()
	state := service.taskExecution
	if state == nil || state.requestID != requestID {
		service.mu.Unlock()
		return currentInput, nil
	}
	currentInput = service.protectOversizedCurrentInputLocked(requestID, currentInput, budget)
	newCheckpoint := rawTokens >= budget.SoftThreshold && state.compactedEpoch != state.progressEpoch
	if newCheckpoint {
		state.contextVersion++
		state.compactedEpoch = state.progressEpoch
	}
	checkpoint := service.buildTaskCheckpointLocked(state)
	checkpoint.Version = state.contextVersion
	planMessage := service.planContextMessageLocked()
	checkpointMessage := checkpointContextMessage(checkpoint, rawTokens >= budget.HardThreshold)
	events := append([]TranscriptEvent(nil), service.transcript...)
	events = excludeCurrentInputEvent(events, requestID, currentInput)
	service.mu.Unlock()

	systems := retainedSystemHistory(service.deps.Engine.History())
	target := budget.Budget
	if rawTokens >= budget.SoftThreshold {
		target = budget.TargetAfterCompaction
	}
	assembled, estimated := service.fitExecutionHistory(systemPrompt, systems, planMessage, checkpointMessage, events, currentInput, tools, target)
	if estimated > budget.Budget {
		checkpointMessage = checkpointContextMessage(checkpoint, true)
		assembled, estimated = service.fitExecutionHistory(systemPrompt, systems, planMessage, checkpointMessage, nil, currentInput, tools, budget.Budget)
	}
	if estimated > budget.Budget {
		return "", wrapError(fmt.Errorf("%w: estimated=%d budget=%d", errProviderContextBudgetExceeded, estimated, budget.Budget), errorCodeContextExceeded)
	}
	if err := service.deps.Engine.ReplaceHistory(service.deps.Engine.SessionID(), assembled); err != nil {
		return "", fmt.Errorf("assemble provider context: %w", err)
	}
	if err := service.prepareProviderHistory(); err != nil {
		return "", err
	}

	service.mu.Lock()
	state = service.taskExecution
	recorded := false
	var revision uint64
	if state != nil && state.requestID == requestID {
		state.tokenAudit = TokenAudit{
			Model: runtimeModel, Counter: service.tokenCounter.Name(),
			Budget: budget.Budget, SoftThreshold: budget.SoftThreshold, HardThreshold: budget.HardThreshold,
			TargetAfterCompaction: budget.TargetAfterCompaction, EstimatedPromptTokens: estimated,
			ActualPromptTokens: state.tokenAudit.ActualPromptTokens, UpdatedAt: time.Now(),
		}
		if newCheckpoint {
			service.rememberCheckpointLocked(checkpoint)
			recorded = service.recordContextCompactionLocked(requestID, ContextCompaction{
				Version: checkpoint.Version, Reason: "context_budget", MessagesBefore: len(existing),
				EstimatedTokens: rawTokens, CompactedAt: time.Now(),
			})
			if recorded {
				revision = service.bumpLocked()
			}
		}
	}
	service.mu.Unlock()
	if recorded {
		service.events.Publish(EventSnapshotChanged, revision, requestID, nil)
	}
	return currentInput, nil
}

func (service *contextCoordinator) fitExecutionHistory(
	systemPrompt string,
	systems []EngineMessage,
	planMessage, checkpointMessage string,
	events []TranscriptEvent,
	currentInput string,
	tools []Tool,
	target int,
) ([]EngineMessage, int) {
	for maxUnits := Limits().ContextMaxUnits; maxUnits >= 0; maxUnits-- { // limits.context_max_units（默认 4）
		history := append([]EngineMessage(nil), systems...)
		if planMessage != "" {
			history = append(history, EngineMessage{Role: "user", Content: planMessage, ContentSet: true})
		}
		if checkpointMessage != "" {
			history = append(history, EngineMessage{Role: "user", Content: checkpointMessage, ContentSet: true})
		}
		if maxUnits > 0 {
			history = append(history, transcriptTailHistory(events, target, maxUnits)...)
		}
		estimated := service.tokenCounter.CountRequest(systemPrompt, history, currentInput, tools)
		if estimated <= target {
			return history, estimated
		}
	}
	history := append([]EngineMessage(nil), systems...)
	if planMessage != "" {
		history = append(history, EngineMessage{Role: "user", Content: planMessage, ContentSet: true})
	}
	if checkpointMessage != "" {
		history = append(history, EngineMessage{Role: "user", Content: checkpointMessage, ContentSet: true})
	}
	return history, service.tokenCounter.CountRequest(systemPrompt, history, currentInput, tools)
}

func (service *contextCoordinator) planContextMessageLocked() string {
	projection := service.activePlanProjectionLocked()
	if projection == nil || projection.Status == string(PlanCompleted) {
		return ""
	}
	payload := map[string]any{
		"plan_ref": projection.CanonicalPlanRef, "plan_id": projection.PlanID,
		"status": projection.Status, "current": projection.CurrentNode,
		"completed": projection.CompletedNodes, "failed": projection.FailedNodes,
		"pending": projection.PendingNodes,
	}
	if frame := activePlanFrame(service.planStack, service.activePlanID); frame != nil {
		payload["current_slice"] = currentPlanSlice(frame.Arguments, projection.CurrentNode)
	}
	encoded, _ := json.Marshal(payload)
	return planContextPrefix + "\n" + string(encoded)
}

func currentPlanSlice(arguments, currentNode string) any {
	var plan struct {
		Nodes map[string]json.RawMessage `json:"nodes"`
		Edges map[string][]string        `json:"edges"`
	}
	if json.Unmarshal([]byte(arguments), &plan) != nil {
		return nil
	}
	nodes := make(map[string]json.RawMessage)
	edges := make(map[string][]string)
	if node, ok := plan.Nodes[currentNode]; ok {
		nodes[currentNode] = node
	}
	for source, targets := range plan.Edges {
		if source == currentNode {
			edges[source] = append([]string(nil), targets...)
			for _, target := range targets {
				if node, ok := plan.Nodes[target]; ok {
					nodes[target] = node
				}
			}
		}
		for _, target := range targets {
			if target == currentNode {
				edges[source] = appendUniqueStrings(edges[source], target)
				if node, ok := plan.Nodes[source]; ok {
					nodes[source] = node
				}
			}
		}
	}
	return map[string]any{"nodes": nodes, "edges": edges}
}

func checkpointContextMessage(checkpoint TaskCheckpoint, minimal bool) string {
	if !hasSubstantiveCheckpoint(checkpoint) {
		return ""
	}
	if minimal {
		checkpoint.Decisions = nil
		checkpoint.ChangedFiles = nil
		checkpoint.Artifacts = nil
		if len(checkpoint.CompletedWork) > 1 {
			checkpoint.CompletedWork = checkpoint.CompletedWork[len(checkpoint.CompletedWork)-1:]
		}
	}
	encoded, _ := json.Marshal(checkpoint)
	return taskContextCheckpointPrefix + "\n" + string(encoded)
}

func excludeCurrentInputEvent(events []TranscriptEvent, requestID, currentInput string) []TranscriptEvent {
	if currentInput == "" || len(events) == 0 {
		return events
	}
	last := events[len(events)-1]
	if last.TaskID == requestID && last.Role == "user" {
		return events[:len(events)-1]
	}
	return events
}

func (service *contextCoordinator) protectOversizedCurrentInputLocked(requestID, currentInput string, budget contextBudget) string {
	if currentInput == "" || service.tokenCounter.CountText(currentInput) <= budget.TargetAfterCompaction/2 {
		return currentInput
	}
	stored := service.storeToolResultLocked("user_input", currentInput)
	warning := contentReferenceWarning(stored.Ref)
	for index := len(service.transcript) - 1; index >= 0; index-- {
		event := &service.transcript[index]
		if event.TaskID == requestID && event.Role == "user" {
			event.Content = warning
			event.ResultRef = stored.Ref
			event.TokenCount = service.countTranscriptEvent(*event)
			break
		}
	}
	return warning
}

func contentReferenceWarning(resultRef string) string {
	return "<seelex-content-reference>\nresult_ref=" + resultRef + "\n" +
		"The user input is stored out of band because it exceeds the single-item context budget. " +
		"Use read_tool_result with pagination or filtering.\n</seelex-content-reference>"
}

func (service *contextCoordinator) rememberCheckpointLocked(checkpoint TaskCheckpoint) {
	for index := range service.taskCheckpoints {
		if service.taskCheckpoints[index].Version == checkpoint.Version && checkpoint.Version != 0 {
			service.taskCheckpoints[index] = checkpoint
			return
		}
	}
	service.taskCheckpoints = append(service.taskCheckpoints, checkpoint)
}

const frameworkToolOutputTruncatedMarker = "\n...[truncated]"

// rejectOversizedToolResults replaces oversized output with an explicit retry
// instruction before the next provider call. It does not expose a head or tail
// preview: an Agent must not reason from a misleading fragment.
func (service *contextCoordinator) rejectOversizedToolResults(maxChars int) (bool, error) {
	history := service.deps.Engine.History()
	service.mu.RLock()
	refs := make(map[string]string, len(service.resultRefsByToolCallID))
	for callID, resultRef := range service.resultRefsByToolCallID {
		refs[callID] = resultRef
	}
	service.mu.RUnlock()
	filtered, changed := rejectToolResultsWithRefs(history, maxChars, refs)
	if !changed {
		return false, nil
	}
	if err := service.deps.Engine.ReplaceHistory(service.deps.Engine.SessionID(), filtered); err != nil {
		return false, fmt.Errorf("reject oversized tool results: %w", err)
	}
	return true, nil
}

func rejectToolResults(history []EngineMessage, maxChars int) ([]EngineMessage, bool) {
	return rejectToolResultsWithRefs(history, maxChars, nil)
}

func rejectToolResultsWithRefs(history []EngineMessage, maxChars int, refs map[string]string) ([]EngineMessage, bool) {
	filtered := append([]EngineMessage(nil), history...)
	changed := false
	for index := range filtered {
		message := &filtered[index]
		if message.Role != "tool" || !isOversizedToolResult(message.Content, maxChars) {
			continue
		}
		message.Content = oversizedToolResultWarning(message.Name, refs[message.ToolCallID])
		message.ContentSet = true
		changed = true
	}
	return filtered, changed
}

func isOversizedToolResult(content string, maxChars int) bool {
	return len(content) > maxChars || strings.HasSuffix(content, frameworkToolOutputTruncatedMarker)
}

func oversizedToolResultWarning(name, resultRef string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "tool"
	}
	var builder strings.Builder
	builder.WriteString(toolResultOmittedPrefix + "\n")
	builder.WriteString("tool=")
	builder.WriteString(name)
	builder.WriteByte('\n')
	if resultRef != "" {
		builder.WriteString("result_ref=")
		builder.WriteString(resultRef)
		builder.WriteByte('\n')
	}
	builder.WriteString("The result exceeded the provider-context item budget; raw content was not included.\n")
	builder.WriteString("Do not infer facts from omitted content. Use read_tool_result with pagination or filtering, or issue a narrower read-only query.\n")
	builder.WriteString("</seelex-tool-result-omitted>")
	return builder.String()
}

func providerSafeToolResult(name, result string, toolErr error) string {
	if toolErr != nil || !isOversizedToolResult(result, seelexctx.DefaultContextConfig().MaxToolResultChars) {
		return result
	}
	return oversizedToolResultWarning(name, "")
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
func (service *contextCoordinator) removeTaskContextCheckpoints() error {
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
