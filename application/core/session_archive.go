package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"time"
)

const (
	sessionRecordVersion       = 2
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

type persistedPlanRestorer interface {
	RestorePlan(context.Context, string) error
}

func (service *Service) persistCurrentSession(sessionID string) error {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return errors.New("session ID is required")
	}
	service.mu.RLock()
	record := service.sessionRecordLocked(sessionID)
	service.mu.RUnlock()

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

func (service *Service) sessionRecordLocked(sessionID string) SessionRecord {
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
		UpdatedAt:    now,
	}
	for _, message := range service.snapshot.Conversation {
		if message.Role == "system" {
			continue
		}
		copy := message
		if message.Tool != nil {
			tool := *message.Tool
			copy.Tool = &tool
		}
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

func (service *Service) loadSessionRecord(location sessionLocation, sessionID string) (SessionRecord, bool, error) {
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
	if record.Version != sessionRecordVersion || record.ID != sessionID {
		return SessionRecord{}, false, nil
	}
	return record, true, nil
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
		copy := message
		if message.Tool != nil {
			tool := *message.Tool
			copy.Tool = &tool
		}
		conversation = append(conversation, copy)
	}
	return conversation
}

func cloneSessionPlanStack(stack []SessionPlanFrame) []SessionPlanFrame {
	cloned := make([]SessionPlanFrame, len(stack))
	for index := range stack {
		cloned[index] = stack[index]
		cloned[index].Plan = cloneRuntimeState(RuntimeState{Plan: stack[index].Plan}).Plan
	}
	return cloned
}

func (service *Service) syncActivePlanFrameLocked(now time.Time) {
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

func (service *Service) pushLoadedPlanLocked(arguments string, now time.Time) {
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

func (service *Service) recordReadFileLocked(arguments string) {
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
