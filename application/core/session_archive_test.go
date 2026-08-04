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
	tailBudget int
	tailUnits  int
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

func (sessions *archiveSessions) LoadTranscriptTailWorkspace(_, _ string, tokenBudget, maxUnits int) ([]TranscriptEvent, error) {
	sessions.tailBudget = tokenBudget
	sessions.tailUnits = maxUnits
	return nil, nil
}

func (sessions *archiveSessions) LoadToolResultWorkspace(string, string, string) (StoredToolResult, error) {
	return StoredToolResult{}, errors.New("tool result unavailable")
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
	service.taskExecution.status = taskStatusInterrupted
	service.taskExecution.checkpoint("inspect", "inspect source", string(NodeCompleted), "found call path", "")
	service.taskExecution.planArguments = `{"entry":"inspect","nodes":{"inspect":{"input":"read"}},"edges":{}}`
	service.components.tasks.activateTaskSkillsLocked(service.taskExecution, []PromptLayer{{Kind: "skill", Name: "review", Text: "review prompt"}})
	service.mu.Unlock()

	if err := service.components.sessions.persistCurrentSession("session-a"); err != nil {
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
	restored.mu.RLock()
	continuation := restored.taskExecution
	restoredPrompt := restored.components.prompts.systemPromptForActiveTaskLocked()
	restored.mu.RUnlock()
	if continuation == nil || continuation.status != taskStatusInterrupted || continuation.inheritedCheckpoint == nil ||
		len(continuation.inheritedCheckpoint.CompletedWork) != 1 || !strings.Contains(restoredPrompt, "review prompt") {
		t.Fatalf("restored projection = %#v prompt=%q", continuation, restoredPrompt)
	}
	history := restoredEngine.History()
	if len(history) != 1 || !strings.HasPrefix(history[0].Content, sessionArchiveResumePrefix) {
		t.Fatalf("engine history = %#v, want bounded archive resume context", history)
	}
	if sessions.tailBudget != defaultContextBudget().TargetAfterCompaction || sessions.tailUnits != 4 {
		t.Fatalf("transcript tail request budget=%d units=%d", sessions.tailBudget, sessions.tailUnits)
	}
}

func TestBoundConversationTailKeepsOnlyConfiguredVariableHeightWindow(t *testing.T) {
	messages := []Message{
		{ID: "system-1", Role: "system"},
		{ID: "message-1", Role: "user"},
		{ID: "message-2", Role: "assistant"},
		{ID: "system-2", Role: "system"},
		{ID: "message-3", Role: "assistant"},
	}
	bounded := boundConversationTail(messages, 2)
	got := make([]string, 0, len(bounded))
	for _, message := range bounded {
		got = append(got, message.ID)
	}
	want := []string{"system-1", "message-2", "system-2", "message-3"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("bounded conversation IDs = %v, want %v", got, want)
	}
}

func TestPersistSessionRecordMergesBoundedProjectionWithFullHistory(t *testing.T) {
	sessions := &archiveSessions{record: SessionRecord{
		Version: sessionRecordVersion,
		ID:      "session-window",
		Conversation: ConversationRecord{Messages: []Message{
			{ID: "message-1", Role: "user", Content: "old question"},
			{ID: "message-2", Role: "assistant", Content: "stale answer"},
		}},
	}}
	service := newTestService(&fakeEngine{sessionID: "session-window"})
	defer service.Shutdown()
	service.deps.Sessions = sessions
	service.mu.Lock()
	service.snapshot.Session = SessionState{ID: "session-window"}
	service.snapshot.Conversation = []Message{
		{ID: "message-2", Role: "assistant", Content: "updated answer"},
		{ID: "message-3", Role: "user", Content: "new question"},
	}
	service.mu.Unlock()

	if err := service.components.sessions.persistCurrentSession("session-window"); err != nil {
		t.Fatal(err)
	}
	got := sessions.record.Conversation.Messages
	if len(got) != 3 || got[0].ID != "message-1" || got[1].Content != "updated answer" || got[2].ID != "message-3" {
		t.Fatalf("merged durable conversation = %#v", got)
	}
}

func TestSessionRecordStoresLargeContentByReference(t *testing.T) {
	service := newTestService(&fakeEngine{sessionID: "session-large"})
	defer service.Shutdown()
	raw := strings.Repeat("raw-secret-output", defaultToolResultLimit())
	service.mu.Lock()
	service.snapshot.Session = SessionState{ID: "session-large"}
	service.snapshot.Chat = ChatState{RequestID: "task-large"}
	service.taskExecution = newTaskExecutionState("task-large", "inspect", "high")
	stored := service.components.tasks.storeToolResultLocked("bash", raw)
	service.resultRefsByToolCallID["call-large"] = stored.Ref
	service.snapshot.Conversation = []Message{
		{Role: "tool", Tool: &ToolCall{ID: "call-large", Name: "bash", Result: raw}},
		{Role: "tool_result", Content: raw, Tool: &ToolCall{ID: "call-large", Name: "bash", Result: raw}},
	}
	record := service.components.sessions.sessionRecordLocked("session-large")
	service.mu.Unlock()

	for _, message := range record.Conversation.Messages {
		if strings.Contains(message.Content, "raw-secret-output") || (message.Tool != nil && strings.Contains(message.Tool.Result, "raw-secret-output")) {
			t.Fatalf("raw result leaked into SessionRecord: %#v", message)
		}
		if message.Tool != nil && !strings.Contains(message.Tool.Result, stored.Ref) {
			t.Fatalf("result reference missing from archived tool: %#v", message.Tool)
		}
	}
	page, err := service.ReadToolResultHandler(t.Context(), `{"result_ref":"`+stored.Ref+`","offset":0,"limit":32}`)
	if err != nil || !strings.Contains(page, "raw-secret-output") {
		t.Fatalf("read_tool_result page=%q err=%v", page, err)
	}
}

func TestCompletedTaskClearsTaskScopedSkillsBeforeNextRequest(t *testing.T) {
	for _, status := range []TaskStatus{TaskCompleted, TaskFailed} {
		service := newTestService(&fakeEngine{})
		service.promptStack.Push("skill", "review", "review prompt")
		service.mu.Lock()
		service.snapshot.Task = &TaskState{Status: status}
		service.mu.Unlock()
		service.prepareCompletedTaskBoundary()
		if skills := selectedSkillLayers(service.promptStack.Layers()); len(skills) != 0 {
			service.Shutdown()
			t.Fatalf("terminal task %s retained skills: %#v", status, skills)
		}
		service.Shutdown()
	}
}

func TestToolResultPaginationMakesProgressAcrossUTF8Boundaries(t *testing.T) {
	result := StoredToolResult{ToolResultRef: ToolResultRef{Ref: "tr-unicode", Tool: "read"}, Content: "中文"}
	page, err := encodeToolResultPage(result, 0, 1, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(page, `"content":"中"`) || !strings.Contains(page, `"next_offset":3`) {
		t.Fatalf("unicode page = %s", page)
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
