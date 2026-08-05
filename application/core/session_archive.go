package core

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"time"

	"github.com/RedHuang-0622/seelex/seelexctx"
)

const (
	sessionRecordVersion       = 3
	sessionArchiveVersion      = 1
	sessionArchiveResumePrefix = "<!-- seelex:session-resume:v1 -->"
)

// sessionRecordPort is intentionally optional so legacy test and framework
// session stores keep working. Production sessionPort stores the record under
// its unique workspace/session key beside framework history.
type sessionRecordPort interface {
	SaveSessionRecord(string, SessionRecord) error
	LoadSessionRecord(string) (SessionRecord, error)
	LoadSessionRecordWorkspace(string, string) (SessionRecord, error)
}

type sessionSnapshotPort interface {
	SaveSessionSnapshot(string, []EngineMessage, SessionRecord, []TranscriptEvent, []StoredToolResult) error
}

type sessionTranscriptPort interface {
	LoadTranscriptTailWorkspace(string, string, int, int) ([]TranscriptEvent, error)
	LoadToolResultWorkspace(string, string, string) (StoredToolResult, error)
}

type sessionConversationRangePort interface {
	LoadConversationRangeWorkspace(string, string, int, int) ([]Message, int, error)
}

type persistedPlanRestorer interface {
	RestorePlan(context.Context, string) error
}

func (service *sessionCoordinator) persistCurrentSession(sessionID string) error {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return errors.New("session ID is required")
	}
	service.mu.Lock()
	record := service.sessionRecordLocked(sessionID)
	events := append([]TranscriptEvent(nil), service.transcript...)
	pendingResults := append([]StoredToolResult(nil), service.pendingToolResults...)
	service.mu.Unlock()

	if store, ok := service.deps.Sessions.(sessionRecordPort); ok {
		existing, err := store.LoadSessionRecord(sessionID)
		if err == nil {
			record.Conversation.Messages = mergeConversationMessages(existing.Conversation.Messages, record.Conversation.Messages)
		} else if !errors.Is(err, fs.ErrNotExist) && !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("load existing session record before merge: %w", err)
		}
	}

	if store, ok := service.deps.Sessions.(sessionSnapshotPort); ok {
		if err := store.SaveSessionSnapshot(sessionID, service.deps.Engine.History(), record, events, pendingResults); err != nil {
			return fmt.Errorf("save atomic session snapshot: %w", err)
		}
		service.mu.Lock()
		service.removeCommittedToolResultsLocked(pendingResults)
		service.mu.Unlock()
		return nil
	}

	if err := service.deps.Sessions.SaveCurrent(sessionID); err != nil {
		return err
	}
	store, ok := service.deps.Sessions.(sessionRecordPort)
	if !ok {
		return nil
	}
	if err := store.SaveSessionRecord(sessionID, record); err != nil {
		return fmt.Errorf("save session record: %w", err)
	}
	return nil
}

func mergeConversationMessages(existing, projected []Message) []Message {
	if len(existing) == 0 {
		return recordConversation(SessionRecord{Conversation: ConversationRecord{Messages: projected}})
	}
	merged := recordConversation(SessionRecord{Conversation: ConversationRecord{Messages: existing}})
	indices := make(map[string]int, len(merged))
	for index := range merged {
		if merged[index].ID != "" {
			indices[merged[index].ID] = index
		}
	}
	for _, message := range projected {
		copy := recordConversation(SessionRecord{Conversation: ConversationRecord{Messages: []Message{message}}})[0]
		if copy.ID != "" {
			if index, ok := indices[copy.ID]; ok {
				merged[index] = copy
				continue
			}
			indices[copy.ID] = len(merged)
		}
		merged = append(merged, copy)
	}
	return merged
}

func (service *sessionCoordinator) sessionRecordLocked(sessionID string) SessionRecord {
	now := time.Now()
	service.syncActivePlanFrameLocked(now)
	title := service.sessionTitle
	if title.Value == "" {
		title = SessionTitle{Value: service.snapshot.Session.Name, Source: "first_request", FinalizedAt: now}
	}
	record := SessionRecord{
		Version: sessionRecordVersion, ID: sessionID, Title: title,
		ActivePlanID: service.activePlanID,
		PlanStack:    cloneSessionPlanStack(service.planStack),
		Conversation: ConversationRecord{UpdatedAt: now},
		Execution:    SessionExecutionRecord{ReadFiles: append([]ReadFileRef(nil), service.snapshot.ReadFiles...)},
		Projection:   service.taskProjectionLocked(sessionID),
		Checkpoints:  append([]TaskCheckpoint(nil), service.taskCheckpoints...),
		ToolResults:  append([]ToolResultRef(nil), service.toolResultRefs...),
		UpdatedAt:    now,
	}
	for _, message := range service.snapshot.Conversation {
		if message.Role == "system" {
			continue
		}
		copy := service.archivedConversationMessageLocked(message)
		record.Conversation.Messages = append(record.Conversation.Messages, copy)
	}
	if task := service.snapshot.Task; task != nil {
		copy := *task
		copy.ContextCompactions = append([]ContextCompaction(nil), task.ContextCompactions...)
		record.Execution.Task = &copy
	}
	if state := service.taskExecution; state != nil && state.requestID == service.snapshot.Chat.RequestID {
		record.Execution.Continuation = state.contextSummary()
	}
	return record
}

func (service *sessionCoordinator) archivedConversationMessageLocked(message Message) Message {
	copy := message
	if message.Tool != nil {
		tool := *message.Tool
		copy.Tool = &tool
		if resultRef := service.resultRefsByToolCallID[tool.ID]; resultRef != "" {
			warning := oversizedToolResultWarning(tool.Name, resultRef)
			copy.Tool.Result = warning
			if copy.Role == "tool_result" {
				copy.Content = warning
			}
		}
	}
	if copy.Role == "user" {
		if resultRef := service.userInputResultRefLocked(copy.Content); resultRef != "" {
			copy.Content = contentReferenceWarning(resultRef)
		}
	}
	return copy
}

func (service *sessionCoordinator) userInputResultRefLocked(content string) string {
	digest := "sha256:" + fmt.Sprintf("%x", sha256.Sum256([]byte("user_input\x00"+content)))
	for _, result := range service.toolResultRefs {
		if result.Tool == "user_input" && result.Digest == digest {
			return result.Ref
		}
	}
	return ""
}

func (service *sessionCoordinator) removeCommittedToolResultsLocked(committed []StoredToolResult) {
	if len(committed) == 0 || len(service.pendingToolResults) == 0 {
		return
	}
	refs := make(map[string]struct{}, len(committed))
	for _, result := range committed {
		refs[result.Ref] = struct{}{}
	}
	pending := service.pendingToolResults[:0]
	for _, result := range service.pendingToolResults {
		if _, ok := refs[result.Ref]; !ok {
			pending = append(pending, result)
		}
	}
	service.pendingToolResults = pending
}

func (service *sessionCoordinator) loadSessionRecord(location sessionLocation, sessionID string) (SessionRecord, bool, error) {
	store, ok := service.deps.Sessions.(sessionRecordPort)
	if !ok {
		return SessionRecord{}, false, nil
	}
	record, err := store.LoadSessionRecordWorkspace(location.workspaceID, sessionID)
	if errors.Is(err, fs.ErrNotExist) || errors.Is(err, sql.ErrNoRows) {
		return SessionRecord{}, false, nil
	}
	if err != nil {
		return SessionRecord{}, false, err
	}
	if record.Version == 2 && record.ID == sessionID {
		record.Version = sessionRecordVersion
		return record, true, nil
	}
	if record.Version != sessionRecordVersion || record.ID != sessionID {
		return SessionRecord{}, false, nil
	}
	return record, true, nil
}

func (service *sessionCoordinator) loadSessionTranscript(location sessionLocation, sessionID string) ([]TranscriptEvent, error) {
	store, ok := service.deps.Sessions.(sessionTranscriptPort)
	if !ok {
		return []TranscriptEvent{}, nil
	}
	budget := contextBudgetFor(service.deps.Runtime)
	events, err := store.LoadTranscriptTailWorkspace(location.workspaceID, sessionID, budget.TargetAfterCompaction, 4)
	if err != nil {
		return nil, err
	}
	filtered := make([]TranscriptEvent, 0, len(events))
	for _, event := range events {
		if event.Role == "system" || isTaskContextCheckpoint(event.Content) || isProviderOnlyHistoryContent(event.Content) {
			continue
		}
		filtered = append(filtered, event)
	}
	return filtered, nil
}

func recordResumeHistory(record SessionRecord) []EngineMessage {
	continuation := strings.TrimSpace(record.Execution.Continuation)
	if continuation == "" {
		continuation = "A prior session is available in the user-visible transcript. Treat its details as historical context and ask targeted questions or reread files when more detail is needed."
	}
	return []EngineMessage{{
		Role: "user", ContentSet: true,
		Content: sessionArchiveResumePrefix + "\n" + continuation,
	}}
}

func recordConversation(record SessionRecord) []Message {
	conversation := make([]Message, 0, len(record.Conversation.Messages))
	for _, message := range record.Conversation.Messages {
		if isInternalConversationMessage(message) {
			continue
		}
		copy := message
		if message.Tool != nil {
			tool := *message.Tool
			copy.Tool = &tool
		}
		conversation = append(conversation, copy)
	}
	return conversation
}

func isInternalConversationMessage(message Message) bool {
	if message.Role == "system" {
		return true
	}
	if message.Role != "user" {
		return false
	}
	return isTaskContextCheckpoint(message.Content) ||
		strings.HasPrefix(message.Content, sessionArchiveResumePrefix) ||
		isProviderOnlyHistoryContent(message.Content)
}

// recordConversationResumeHistory is the durable-record fallback for a cold
// load whose Transcript tail is missing, stale, or contains only an internal
// checkpoint marker. It uses the same bounded protocol-unit selector as the
// append-only transcript path, so the model sees the last visible user turn
// rendered by the UI without loading the full archive.
func recordConversationResumeHistory(record SessionRecord, tokenBudget, maxUnits int) []EngineMessage {
	return transcriptTailHistory(recordConversationTranscript(record), tokenBudget, maxUnits)
}

func recordConversationTranscript(record SessionRecord) []TranscriptEvent {
	events := make([]TranscriptEvent, 0, len(record.Conversation.Messages))
	for _, message := range record.Conversation.Messages {
		if isInternalConversationMessage(message) {
			continue
		}
		event := TranscriptEvent{
			Seq:        uint64(len(events) + 1),
			Role:       message.Role,
			Content:    message.Content,
			TokenCount: seelexctx.EstimateTokens(message.Content),
		}
		switch message.Role {
		case "tool":
			if message.Tool != nil && message.Tool.ID != "" {
				event.Role = "assistant"
				event.ToolCalls = []TranscriptToolCall{{ID: message.Tool.ID, Name: message.Tool.Name, Arguments: message.Tool.Arguments}}
			}
		case "tool_result":
			event.Role = "tool"
			if message.Tool != nil {
				event.ToolCallID = message.Tool.ID
				event.Name = message.Tool.Name
			}
		}
		for _, call := range event.ToolCalls {
			event.TokenCount += seelexctx.EstimateTokens(call.Name) + seelexctx.EstimateTokens(call.Arguments)
		}
		events = append(events, event)
	}
	return events
}

func recordConversationTail(record SessionRecord, window int) []Message {
	messages := recordConversation(record)
	if window > 0 && len(messages) > window {
		messages = messages[len(messages)-window:]
	}
	return recordConversation(SessionRecord{Conversation: ConversationRecord{Messages: messages}})
}

func cloneSessionPlanStack(stack []SessionPlanFrame) []SessionPlanFrame {
	cloned := make([]SessionPlanFrame, len(stack))
	for index := range stack {
		cloned[index] = stack[index]
		cloned[index].Plan = cloneRuntimeState(RuntimeState{Plan: stack[index].Plan}).Plan
	}
	return cloned
}

func (service *sessionCoordinator) syncActivePlanFrameLocked(now time.Time) {
	if service.activePlanID == "" || len(service.planStack) == 0 {
		return
	}
	for index := range service.planStack {
		frame := &service.planStack[index]
		if frame.ID != service.activePlanID {
			continue
		}
		frame.Plan = cloneRuntimeState(RuntimeState{Plan: service.snapshot.Runtime.Plan}).Plan
		frame.UpdatedAt = now
		return
	}
}

func (service *sessionCoordinator) pushLoadedPlanLocked(arguments string, now time.Time) {
	arguments = strings.TrimSpace(arguments)
	if arguments == "" || service.snapshot.Runtime.Plan == nil {
		return
	}
	service.planSequence++
	planID := fmt.Sprintf("plan-%d", service.planSequence)
	service.planStack = append(service.planStack, SessionPlanFrame{
		ID:        planID,
		Plan:      cloneRuntimeState(RuntimeState{Plan: service.snapshot.Runtime.Plan}).Plan,
		Arguments: arguments,
		LoadedAt:  now,
		UpdatedAt: now,
	})
	service.activePlanID = planID
}

func (service *sessionCoordinator) recordReadFileLocked(arguments string) {
	var input struct {
		Path string `json:"path"`
	}
	if json.Unmarshal([]byte(arguments), &input) != nil {
		return
	}
	input.Path = strings.TrimSpace(input.Path)
	if input.Path == "" {
		return
	}
	now := time.Now()
	for index := range service.snapshot.ReadFiles {
		if service.snapshot.ReadFiles[index].Path == input.Path {
			service.snapshot.ReadFiles[index].ReadAt = now
			return
		}
	}
	service.snapshot.ReadFiles = append(service.snapshot.ReadFiles, ReadFileRef{Path: input.Path, ReadAt: now})
}
