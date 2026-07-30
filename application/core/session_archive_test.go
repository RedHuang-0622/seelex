package core

import (
	"errors"
	"strings"
	"testing"
	"time"
)

type archiveSessions struct {
	fakeSessions
	history    []EngineMessage
	historyErr error
	record     SessionRecord
	saved      int
}

func (sessions *archiveSessions) SaveCurrent(string) error {
	sessions.saved++
	return nil
}

func (sessions *archiveSessions) LoadHistory(string) ([]EngineMessage, error) {
	return append([]EngineMessage(nil), sessions.history...), sessions.historyErr
}

func (sessions *archiveSessions) SaveSessionRecord(_ string, record SessionRecord) error {
	sessions.record = record
	return nil
}

func (sessions *archiveSessions) LoadSessionRecord(string) (SessionRecord, error) {
	return sessions.record, nil
}

func (sessions *archiveSessions) LoadSessionRecordWorkspace(string, string) (SessionRecord, error) {
	return sessions.record, nil
}

func TestSessionArchivePreservesVisibleHistoryPlanAndReadCache(t *testing.T) {
	engine := &fakeEngine{sessionID: "session-a", history: []EngineMessage{{Role: "system", Content: "private prompt", ContentSet: true}}}
	service := newTestService(engine)
	defer service.Shutdown()
	sessions := &archiveSessions{history: engine.History()}
	service.deps.Sessions = sessions

	service.mu.Lock()
	service.snapshot.Session = SessionState{ID: "session-a", Name: "Keep this title"}
	service.snapshot.Conversation = []Message{{ID: "user-1", Role: "user", Content: "Inspect the repository", CreatedAt: time.Now()}}
	service.snapshot.Runtime.Plan = &PlanState{EntryNodeID: "inspect", Status: PlanPending, Nodes: []PlanNode{{ID: "inspect", Status: NodePending}}}
	service.snapshot.ReadFiles = []ReadFileRef{{Path: "application/core/chat.go", ReadAt: time.Now()}}
	service.snapshot.Task = &TaskState{RequestID: "task-a", Status: TaskInterrupted, Summary: "checkpoint saved"}
	service.snapshot.Chat = ChatState{RequestID: "task-a"}
	service.sessionTitle = SessionTitle{Value: "Keep this title", Source: "first_request"}
	service.planStack = []SessionPlanFrame{{ID: "plan-a", Plan: service.snapshot.Runtime.Plan, Arguments: `{"entry":"inspect","nodes":{"inspect":{"input":"read"}},"edges":{}}`}}
	service.activePlanID = "plan-a"
	service.taskExecution = newTaskExecutionState("task-a", "Inspect the repository", "high")
	service.taskExecution.planArguments = `{"entry":"inspect","nodes":{"inspect":{"input":"read"}},"edges":{}}`
	service.mu.Unlock()

	if err := service.persistCurrentSession("session-a"); err != nil {
		t.Fatal(err)
	}
	if sessions.saved != 1 || sessions.record.Title.Value != "Keep this title" || len(sessions.record.Conversation.Messages) != 1 || len(sessions.record.PlanStack) != 1 || len(sessions.record.Execution.ReadFiles) != 1 {
		t.Fatalf("record = %#v", sessions.record)
	}

	restoredEngine := &fakeEngine{}
	restored := newTestService(restoredEngine)
	defer restored.Shutdown()
	restored.deps.Sessions = sessions
	if err := restored.resumeSession("session-a"); err != nil {
		t.Fatal(err)
	}
	snapshot := restored.Snapshot()
	if snapshot.Session.Name != "Keep this title" || len(snapshot.Conversation) != 2 || snapshot.Conversation[1].Content != "Inspect the repository" {
		t.Fatalf("restored snapshot = %#v", snapshot)
	}
	if snapshot.Runtime.Plan == nil || snapshot.Runtime.Plan.EntryNodeID != "inspect" || len(snapshot.ReadFiles) != 1 {
		t.Fatalf("restored state = %#v", snapshot)
	}
	history := restoredEngine.History()
	if len(history) != 1 || !strings.HasPrefix(history[0].Content, sessionArchiveResumePrefix) {
		t.Fatalf("engine history = %#v, want bounded archive resume context", history)
	}
}

func TestLoadedPlanIsAppendedToSessionPlanStack(t *testing.T) {
	engine := &fakeEngine{sessionID: "session-plan"}
	service := newTestService(engine)
	defer service.Shutdown()
	arguments := `{"entry":"inspect","nodes":{"inspect":{"input":"read"}},"edges":{}}`

	service.handleToolStart("plan_load", "plan-call", arguments)
	service.handleToolComplete("plan_load", "plan-call", `{"status":"loaded"}`, nil, 0)

	service.mu.RLock()
	defer service.mu.RUnlock()
	if service.activePlanID == "" || len(service.planStack) != 1 {
		t.Fatalf("plan stack = %#v, active = %q", service.planStack, service.activePlanID)
	}
	frame := service.planStack[0]
	if frame.ID != service.activePlanID || frame.Arguments != arguments || frame.Plan == nil || frame.Plan.EntryNodeID != "inspect" {
		t.Fatalf("loaded frame = %#v", frame)
	}
}

func TestResumeSessionUsesRecordWhenProviderHistoryIsUnavailable(t *testing.T) {
	sessions := &archiveSessions{
		historyErr: errors.New("history shard unavailable"),
		record: SessionRecord{
			Version: 2,
			ID:      "session-record-only",
			Title:   SessionTitle{Value: "Saved title", Source: "user"},
			Conversation: ConversationRecord{Messages: []Message{{
				ID: "message-1", Role: "assistant", Content: "Saved response",
			}}},
		},
	}
	service := newTestService(&fakeEngine{})
	defer service.Shutdown()
	service.deps.Sessions = sessions

	if err := service.ResumeSession("session-record-only"); err != nil {
		t.Fatal(err)
	}
	snapshot := service.Snapshot()
	if snapshot.Session.ID != "session-record-only" || snapshot.Session.Name != "Saved title" || len(snapshot.Conversation) != 2 || snapshot.Conversation[1].Content != "Saved response" {
		t.Fatalf("resumed snapshot = %#v", snapshot)
	}
}

func TestProviderRepairNoteNeverBecomesVisibleAssistantText(t *testing.T) {
	service := newTestService(&fakeEngine{})
	defer service.Shutdown()
	service.mu.Lock()
	service.appendHistoryLocked([]EngineMessage{{
		Role:    "assistant",
		Content: toolCallHistoryContent,
		ToolCalls: []EngineToolCall{{
			ID: "call-1", Name: "read_file", Arguments: `{"path":"README.md"}`,
		}},
	}})
	service.mu.Unlock()

	conversation := service.Snapshot().Conversation
	if len(conversation) != 1 || conversation[0].Tool == nil || conversation[0].Content != "" {
		t.Fatalf("visible conversation = %#v", conversation)
	}
}
