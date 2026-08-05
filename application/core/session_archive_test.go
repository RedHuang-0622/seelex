package core

import (
	"context"
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
	transcript []TranscriptEvent
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
	return append([]TranscriptEvent(nil), sessions.transcript...), nil
}

func (sessions *archiveSessions) LoadToolResultWorkspace(string, string, string) (StoredToolResult, error) {
	return StoredToolResult{}, errors.New("tool result unavailable")
}

func TestSessionArchivePreservesVisibleHistoryPlanAndReadCache(t *testing.T) {
	engine := &fakeEngine{sessionID: "session-a", history: []EngineMessage{{Role: "system", Content: "private prompt", ContentSet: true}}}
	sessions := &archiveSessions{history: engine.History()}
	service := newTestService(t, engine, withTestSessions(sessions))

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
	restored := newTestService(t, restoredEngine, withTestSessions(sessions))
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
	if len(history) != 1 || history[0].Role != "user" || history[0].Content != "Inspect the repository" {
		t.Fatalf("engine history = %#v, want bounded durable conversation context", history)
	}
	if sessions.tailBudget != defaultContextBudget().TargetAfterCompaction || sessions.tailUnits != 4 {
		t.Fatalf("transcript tail request budget=%d units=%d", sessions.tailBudget, sessions.tailUnits)
	}
}

func TestResumeSessionDropsMetadataOnlyCheckpointAndUsesDurableConversation(t *testing.T) {
	const sessionID = "session-empty-checkpoint"
	checkpointMarker := taskContextCheckpointPrefix + `{"version":7,"covers_event_range":{"start":632,"end":632},"updated_at":"2026-08-05T00:00:00Z"}`
	sessions := &archiveSessions{
		record: SessionRecord{
			Version: sessionRecordVersion,
			ID:      sessionID,
			Title:   SessionTitle{Value: "Review session", Source: "user"},
			Conversation: ConversationRecord{Messages: []Message{
				{ID: "message-checkpoint", Role: "user", Content: checkpointMarker},
				{ID: "message-question", Role: "user", Content: "所以你的评价是？"},
				{ID: "message-report", Role: "assistant", Content: "评审报告摘要"},
			}},
			Projection: &TaskContextProjection{
				SchemaVersion: 1, SessionID: sessionID, TaskID: "task-review", Status: taskStatusInterrupted,
				ObjectiveRef: "event:632",
				Checkpoint:   TaskCheckpoint{Version: 7, CoversEventRange: EventRange{Start: 632, End: 632}, UpdatedAt: time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC)},
			},
		},
		transcript: []TranscriptEvent{{Seq: 632, TaskID: "task-review", Role: "user", Content: checkpointMarker, TokenCount: 8}},
	}
	engine := &fakeEngine{sessionID: sessionID}
	service := newTestService(t, engine, withTestSessions(sessions))

	if err := service.ResumeSession(sessionID); err != nil {
		t.Fatal(err)
	}
	snapshot := service.Snapshot()
	for _, message := range snapshot.Conversation {
		if strings.HasPrefix(message.Content, taskContextCheckpointPrefix) {
			t.Fatalf("internal checkpoint leaked into visible conversation: %#v", snapshot.Conversation)
		}
	}
	if len(snapshot.Conversation) != 3 || snapshot.Conversation[1].Content != "所以你的评价是？" || snapshot.Conversation[2].Content != "评审报告摘要" {
		t.Fatalf("restored conversation = %#v", snapshot.Conversation)
	}
	history := engine.History()
	if len(history) != 2 || history[0].Content != "所以你的评价是？" || history[1].Content != "评审报告摘要" {
		t.Fatalf("provider history = %#v, want durable conversation fallback", history)
	}
	service.mu.RLock()
	state := service.taskExecution
	service.mu.RUnlock()
	if state == nil || state.inheritedCheckpoint != nil {
		t.Fatalf("metadata-only checkpoint was restored as task context: %#v", state)
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
	service := newTestService(t, &fakeEngine{sessionID: "session-window"}, withTestSessions(sessions))
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
	service := newTestService(t, &fakeEngine{sessionID: "session-large"})
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
		service := newTestService(t, &fakeEngine{})
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
	service := newTestService(t, engine)
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
	service := newTestService(t, &fakeEngine{}, withTestSessions(sessions))

	if err := service.ResumeSession("session-record-only"); err != nil {
		t.Fatal(err)
	}
	snapshot := service.Snapshot()
	if snapshot.Session.ID != "session-record-only" || snapshot.Session.Name != "Saved title" || len(snapshot.Conversation) != 2 || snapshot.Conversation[1].Content != "Saved response" {
		t.Fatalf("resumed snapshot = %#v", snapshot)
	}
}

func TestResumeSessionContinuationKeepsTranscriptHistory(t *testing.T) {
	const sessionID = "session-transcript"
	sessions := &archiveSessions{
		record: SessionRecord{
			Version: sessionRecordVersion,
			ID:      sessionID,
			Title:   SessionTitle{Value: "Transcript session", Source: "user"},
		},
		transcript: []TranscriptEvent{
			{Seq: 1, TaskID: "task-1", Role: "user", Content: "original question", TokenCount: 4},
			{Seq: 2, TaskID: "task-1", Role: "assistant", Content: "original answer", TokenCount: 4},
		},
	}
	engine := &fakeEngine{sessionID: sessionID}
	service := newTestService(t, engine, withTestSessions(sessions))

	if err := service.ResumeSession(sessionID); err != nil {
		t.Fatal(err)
	}
	if err := service.Submit(context.Background(), "continue"); err != nil {
		t.Fatal(err)
	}
	if err := service.WaitForIdle(context.Background()); err != nil {
		t.Fatal(err)
	}

	engine.mu.Lock()
	history := append([]EngineMessage(nil), engine.historyBeforeChat...)
	engine.mu.Unlock()
	seenQuestion, seenAnswer := false, false
	for _, message := range history {
		if message.Role == "user" && message.Content == "original question" {
			seenQuestion = true
		}
		if message.Role == "assistant" && message.Content == "original answer" {
			seenAnswer = true
		}
	}
	if !seenQuestion || !seenAnswer {
		t.Fatalf("continuation history lost restored transcript: %#v", history)
	}
	for _, message := range history {
		if strings.HasPrefix(message.Content, sessionArchiveResumePrefix) {
			t.Fatalf("continuation fell back to generic resume marker instead of transcript: %#v", history)
		}
	}
}

func TestResumeSessionContinuationKeepsTrailingUnansweredUserInput(t *testing.T) {
	const sessionID = "session-trailing-user"
	sessions := &archiveSessions{
		record: SessionRecord{
			Version: sessionRecordVersion,
			ID:      sessionID,
			Title:   SessionTitle{Value: "Interrupted session", Source: "user"},
		},
		transcript: []TranscriptEvent{{
			Seq: 1, TaskID: "task-1", Role: "user", Content: "evaluate the architecture review", TokenCount: 4,
		}},
	}
	engine := &fakeEngine{sessionID: sessionID}
	service := newTestService(t, engine, withTestSessions(sessions))

	if err := service.ResumeSession(sessionID); err != nil {
		t.Fatal(err)
	}
	if err := service.Submit(context.Background(), "continue"); err != nil {
		t.Fatal(err)
	}
	if err := service.WaitForIdle(context.Background()); err != nil {
		t.Fatal(err)
	}

	engine.mu.Lock()
	history := append([]EngineMessage(nil), engine.historyBeforeChat...)
	engine.mu.Unlock()
	for _, message := range history {
		if message.Role == "user" && message.Content == "evaluate the architecture review" {
			return
		}
	}
	t.Fatalf("continuation history lost trailing user input: %#v", history)
}

func TestProviderRepairNoteNeverBecomesVisibleAssistantText(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
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
