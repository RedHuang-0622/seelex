package core

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/application/core/context_runtime"
	"github.com/RedHuang-0622/seelex/application/core/session_runtime"
	"github.com/RedHuang-0622/seelex/application/core/task_context"
	"github.com/RedHuang-0622/seelex/application/core/view_state"
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

func (sessions *archiveSessions) SaveSessionRecordWorkspace(_ string, _ string, record SessionRecord) error {
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

// TestEnrichTranscriptMessageIDsPairsEventsToMessages 验证 event-to-message
// 关联：tool call/result 按 CallID 配对，user/assistant 按角色+内容配对，
// 不按数组位置推导；无法稳定配对的保持空。
// contextAwareSessions 是 sessionContextPort 的测试桩：记录 attach/detach
// 调用，可注入 attach 失败。
type contextAwareSessions struct {
	archiveSessions
	mu        sync.Mutex
	attached  []string // workspaceID:sessionID
	detached  int
	attachErr error
}

func (sessions *contextAwareSessions) AttachSessionContext(workspaceID, sessionID string) error {
	sessions.mu.Lock()
	defer sessions.mu.Unlock()
	sessions.attached = append(sessions.attached, workspaceID+":"+sessionID)
	return sessions.attachErr
}

func (sessions *contextAwareSessions) DetachSessionContext() {
	sessions.mu.Lock()
	defer sessions.mu.Unlock()
	sessions.detached++
}

// TestResumeSessionAttachesSessionContext 验证恢复会话后 context 模块被
// 挂接（AttachSessionContext 携带 workspace/session 定位），下一轮 prompt
// 组装前四栈已就绪。
func TestResumeSessionAttachesSessionContext(t *testing.T) {
	engine := &fakeEngine{sessionID: "session-a", history: []EngineMessage{{Role: "system", Content: "private prompt", ContentSet: true}}}
	sessions := &contextAwareSessions{archiveSessions: archiveSessions{history: engine.History()}}
	service := newTestService(t, engine, withTestSessions(sessions))

	service.ViewMu.Lock()
	service.Core.Snapshot.Session = SessionState{ID: "session-a", Name: "Keep this title"}
	service.Core.Snapshot.Conversation = []Message{{ID: "user-1", Role: "user", Content: "Inspect the repository", CreatedAt: time.Now()}}
	service.components.sessions.SetSessionTitleLocked("session-a", SessionTitle{Value: "Keep this title", Source: "first_request"})
	service.ViewMu.Unlock()

	if err := service.components.sessions.PersistCurrentSession(session_runtime.Location{Meta: SessionInfo{ID: "session-a"}}, "session-a"); err != nil {
		t.Fatal(err)
	}
	restored := newTestService(t, &fakeEngine{}, withTestSessions(sessions))
	if err := restored.resumeSession("session-a"); err != nil {
		t.Fatal(err)
	}
	sessions.mu.Lock()
	defer sessions.mu.Unlock()
	if len(sessions.attached) != 1 || !strings.HasSuffix(sessions.attached[0], ":session-a") {
		t.Fatalf("attach calls = %v, want one attach for session-a", sessions.attached)
	}
}

// TestResumeSessionFailsWhenContextCorrupt 验证损坏/不可用的 context 模块
// 使恢复显式失败（不静默降级成内存栈；模块化方案降级矩阵）。
func TestResumeSessionFailsWhenContextCorrupt(t *testing.T) {
	engine := &fakeEngine{sessionID: "session-a", history: []EngineMessage{{Role: "system", Content: "private prompt", ContentSet: true}}}
	sessions := &contextAwareSessions{archiveSessions: archiveSessions{history: engine.History()}}
	sessions.attachErr = errors.New("context store unavailable")
	service := newTestService(t, engine, withTestSessions(sessions))

	service.ViewMu.Lock()
	service.Core.Snapshot.Session = SessionState{ID: "session-a", Name: "Keep this title"}
	service.Core.Snapshot.Conversation = []Message{{ID: "user-1", Role: "user", Content: "Inspect the repository", CreatedAt: time.Now()}}
	service.components.sessions.SetSessionTitleLocked("session-a", SessionTitle{Value: "Keep this title", Source: "first_request"})
	service.ViewMu.Unlock()
	if err := service.components.sessions.PersistCurrentSession(session_runtime.Location{Meta: SessionInfo{ID: "session-a"}}, "session-a"); err != nil {
		t.Fatal(err)
	}
	restored := newTestService(t, &fakeEngine{}, withTestSessions(sessions))
	if err := restored.resumeSession("session-a"); err == nil || !strings.Contains(err.Error(), "attach session context") {
		t.Fatalf("resume err = %v, want attach session context failure", err)
	}
}

// TestBeginNewSessionDetachesSessionContext 验证进入 draft/新建会话时解绑
// context 模块，防止四栈串到下一个会话。
func TestBeginNewSessionDetachesSessionContext(t *testing.T) {
	engine := &fakeEngine{sessionID: "session-a", history: []EngineMessage{{Role: "system", Content: "private prompt", ContentSet: true}}}
	sessions := &contextAwareSessions{archiveSessions: archiveSessions{history: engine.History()}}
	service := newTestService(t, engine, withTestSessions(sessions))

	service.ViewMu.Lock()
	service.Core.Snapshot.Session = SessionState{ID: "session-a", Name: "old session"}
	service.Core.Snapshot.Conversation = []Message{{ID: "user-1", Role: "user", Content: "previous turn", CreatedAt: time.Now()}}
	service.ViewMu.Unlock()
	if err := service.BeginNewSession(); err != nil {
		t.Fatal(err)
	}
	sessions.mu.Lock()
	defer sessions.mu.Unlock()
	if sessions.detached != 1 {
		t.Fatalf("detach calls = %d, want 1", sessions.detached)
	}
}

func TestSessionArchivePreservesVisibleHistoryPlanAndReadCache(t *testing.T) {
	engine := &fakeEngine{sessionID: "session-a", history: []EngineMessage{{Role: "system", Content: "private prompt", ContentSet: true}}}
	sessions := &archiveSessions{history: engine.History()}
	service := newTestService(t, engine, withTestSessions(sessions))

	service.ViewMu.Lock()
	service.Core.Snapshot.Session = SessionState{ID: "session-a", Name: "Keep this title"}
	service.Core.Snapshot.Conversation = []Message{{ID: "user-1", Role: "user", Content: "Inspect the repository", CreatedAt: time.Now()}}
	service.Core.Snapshot.Runtime.Plan = &PlanState{EntryNodeID: "inspect", Status: PlanPending, Nodes: []PlanNode{{ID: "inspect", Status: NodePending}}}
	service.Core.Snapshot.ReadFiles = []ReadFileRef{{Path: "application/core/chat.go", ReadAt: time.Now()}}
	service.Core.Snapshot.Task = &TaskState{RequestID: "task-a", Status: TaskInterrupted, Summary: "checkpoint saved"}
	service.Core.Snapshot.Chat = ChatState{RequestID: "task-a"}
	service.components.sessions.SetSessionTitleLocked("session-a", SessionTitle{Value: "Keep this title", Source: "first_request"})
	service.components.tasks.SetPlanStateLocked([]SessionPlanFrame{{ID: "plan-a", Plan: service.Core.Snapshot.Runtime.Plan, Arguments: `{"entry":"inspect","nodes":{"inspect":{"input":"read"}},"edges":{}}`}}, "plan-a")
	service.components.tasks.BeginTask("task-a", "Inspect the repository", "high", nil, TaskCheckpoint{})
	service.components.tasks.CurrentTaskExecution().Status = task_context.StatusInterrupted
	service.components.tasks.CurrentTaskExecution().Checkpoint("inspect", "inspect source", string(NodeCompleted), "found call path", "")
	service.components.tasks.CurrentTaskExecution().PlanArguments = `{"entry":"inspect","nodes":{"inspect":{"input":"read"}},"edges":{}}`
	service.components.tasks.ActivateTaskSkillsLocked(service.components.tasks.CurrentTaskExecution(), []PromptLayer{{Kind: "skill", Name: "review", Text: "review prompt"}})
	// 技能正文以 internal 事件落 transcript；真实可见轮次跟随其后（transcript
	// 非空即由事件重建可见对话——技能事件本身被 isInternalContent 过滤）。
	service.components.tasks.AppendTranscriptEventLocked(TranscriptEvent{TaskID: "task-a", Role: "user", Content: "Inspect the repository"})
	service.ViewMu.Unlock()

	if err := service.components.sessions.PersistCurrentSession(session_runtime.Location{Meta: SessionInfo{ID: "session-a"}}, "session-a"); err != nil {
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
	restored.ViewMu.RLock()
	continuation := restored.components.tasks.CurrentTaskExecution()
	restoredPrompt := restored.components.prompts.SystemPromptForActiveTaskLocked()
	restored.ViewMu.RUnlock()
	skillCarried := continuation != nil && len(continuation.TrustedSkillLayers) == 1 &&
		strings.Contains(continuation.TrustedSkillLayers[0].Text, "review prompt")
	if continuation == nil || continuation.Status != task_context.StatusInterrupted || continuation.InheritedCheckpoint == nil ||
		len(continuation.InheritedCheckpoint.CompletedWork) != 1 || !skillCarried {
		t.Fatalf("restored projection = %#v prompt=%q", continuation, restoredPrompt)
	}
	// 恢复的任务继续携带技能层（TrustedSkillLayers），但 system 只含稳定
	// base+目录：技能正文不在 system，也不在持久化 transcript（internal 事件
	// 落盘前被过滤），恢复时由 ensureActiveSkillEventsLocked 补 append 回内存
	// transcript，下一次装配即携带。
	if strings.Contains(restoredPrompt, "## Trusted Active Skill") || strings.Contains(restoredPrompt, "review prompt") {
		t.Fatalf("restored system must not embed skill body: %q", restoredPrompt)
	}
	history := restoredEngine.History()
	if len(history) != 1 || history[0].Role != "user" || history[0].Content != "Inspect the repository" {
		t.Fatalf("engine history = %#v, want bounded durable conversation context", history)
	}
	if sessions.tailBudget != task_context.DefaultContextBudget().TargetAfterCompaction || sessions.tailUnits != 4 {
		t.Fatalf("transcript tail request budget=%d units=%d", sessions.tailBudget, sessions.tailUnits)
	}
}

func TestResumeSessionDropsMetadataOnlyCheckpointAndUsesDurableConversation(t *testing.T) {
	const sessionID = "session-empty-checkpoint"
	checkpointMarker := context_runtime.TaskContextCheckpointPrefix + `{"version":7,"covers_event_range":{"start":632,"end":632},"updated_at":"2026-08-05T00:00:00Z"}`
	sessions := &archiveSessions{
		record: SessionRecord{
			Version: session_runtime.SessionRecordVersion,
			ID:      sessionID,
			Title:   SessionTitle{Value: "Review session", Source: "user"},
			Conversation: ConversationRecord{Messages: []Message{
				{ID: "message-checkpoint", Role: "user", Content: checkpointMarker},
				{ID: "message-question", Role: "user", Content: "所以你的评价是？"},
				{ID: "message-report", Role: "assistant", Content: "评审报告摘要"},
			}},
			Projection: &TaskContextProjection{
				SchemaVersion: 1, SessionID: sessionID, TaskID: "task-review", Status: task_context.StatusInterrupted,
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
		if strings.HasPrefix(message.Content, context_runtime.TaskContextCheckpointPrefix) {
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
	if err := service.Submit(context.Background(), "继续"); err != nil {
		t.Fatal(err)
	}
	if err := service.WaitForIdle(context.Background()); err != nil {
		t.Fatal(err)
	}
	engine.mu.Lock()
	prepared := append([]EngineMessage(nil), engine.historyBeforeChat...)
	engine.mu.Unlock()
	if !service.components.sessions.HistoryContainsUser(prepared, "所以你的评价是？") || !historyContainsAssistant(prepared, "评审报告摘要") {
		t.Fatalf("next-turn provider history lost durable conversation: %#v", prepared)
	}
	for _, message := range prepared {
		if strings.HasPrefix(message.Content, context_runtime.TaskContextCheckpointPrefix) {
			t.Fatalf("empty checkpoint leaked into next-turn provider history: %#v", prepared)
		}
	}
	service.ViewMu.RLock()
	state := service.components.tasks.CurrentTaskExecution()
	service.ViewMu.RUnlock()
	if state == nil || state.InheritedCheckpoint != nil {
		t.Fatalf("metadata-only checkpoint was restored as task context: %#v", state)
	}
}

func TestResumeLongContextReasksOpeningQuestionFromCheckpoint(t *testing.T) {
	const sessionID = "session-long-context"
	conversation := []Message{
		{Role: "user", Content: "你好，我是hzr"},
		{Role: "assistant", Content: "你好 hzr"},
	}
	for index := 0; index < 12; index++ {
		conversation = append(conversation,
			Message{Role: "user", Content: strings.Repeat("这是长上下文中的审查事实，不能覆盖开头身份信息。", 320)},
			Message{Role: "assistant", Content: strings.Repeat("已记录该轮审查事实和证据。", 320)},
		)
	}
	conversation = append(conversation, Message{Role: "user", Content: "我的名字是什么？"})
	sessions := &archiveSessions{
		record: SessionRecord{
			Version:      session_runtime.SessionRecordVersion,
			ID:           sessionID,
			Conversation: ConversationRecord{Messages: conversation},
			Projection: &TaskContextProjection{
				SchemaVersion: 1, SessionID: sessionID, TaskID: "task-long", Status: task_context.StatusInterrupted,
				Checkpoint: TaskCheckpoint{
					Version:       8,
					CompletedWork: []string{"user_name=hzr"},
					UpdatedAt:     time.Now(),
				},
			},
		},
		transcript: []TranscriptEvent{{Seq: 632, TaskID: "task-long", Role: "user", Content: context_runtime.TaskContextCheckpointPrefix + `{"version":8}`, TokenCount: 2}},
	}
	engine := &fakeEngine{sessionID: sessionID}
	service := newTestService(t, engine, withTestSessions(sessions))
	if err := service.ResumeSession(sessionID); err != nil {
		t.Fatal(err)
	}
	if err := service.Submit(context.Background(), "请回答开头的简单问题"); err != nil {
		t.Fatal(err)
	}
	if err := service.WaitForIdle(context.Background()); err != nil {
		t.Fatal(err)
	}

	engine.mu.Lock()
	prepared := append([]EngineMessage(nil), engine.historyBeforeChat...)
	engine.mu.Unlock()
	if !service.components.sessions.HistoryContainsUser(prepared, "我的名字是什么？") {
		t.Fatalf("long-context smoke lost opening question: %#v", prepared)
	}
	for _, message := range prepared {
		if context_runtime.IsTaskContextCheckpoint(message.Content) {
			t.Fatalf("long-context normal path must not inject checkpoint message: %#v", prepared)
		}
	}
	// 持久化的身份 checkpoint 仍保留在任务投影（恢复/续接数据面），只是不再
	// 进入 LLM 上下文。
	service.ViewMu.RLock()
	projection := service.components.tasks.TaskProjectionLocked(sessionID)
	service.ViewMu.RUnlock()
	if projection == nil || !strings.Contains(strings.Join(projection.Checkpoint.CompletedWork, "\n"), "user_name=hzr") {
		t.Fatalf("long-context smoke lost durable identity checkpoint: %#v", prepared)
	}
}

func historyContainsAssistant(history []EngineMessage, content string) bool {
	for _, message := range history {
		if message.Role == "assistant" && message.Content == content {
			return true
		}
	}
	return false
}

func TestBoundConversationTailKeepsOnlyConfiguredVariableHeightWindow(t *testing.T) {
	messages := []Message{
		{ID: "system-1", Role: "system"},
		{ID: "message-1", Role: "user"},
		{ID: "message-2", Role: "assistant"},
		{ID: "system-2", Role: "system"},
		{ID: "message-3", Role: "assistant"},
	}
	bounded := view_state.BoundConversationTail(messages, 2)
	got := make([]string, 0, len(bounded))
	for _, message := range bounded {
		got = append(got, message.ID)
	}
	want := []string{"system-1", "message-2", "system-2", "message-3"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("bounded conversation IDs = %v, want %v", got, want)
	}
}

func TestPersistSessionRecordRebuildsConversationFromTranscript(t *testing.T) {
	sessions := &archiveSessions{record: SessionRecord{
		Version: session_runtime.SessionRecordVersion,
		ID:      "session-window",
		Conversation: ConversationRecord{Messages: []Message{
			{ID: "message-1", Role: "user", Content: "old question"},
			{ID: "message-2", Role: "assistant", Content: "stale answer"},
		}},
	}}
	service := newTestService(t, &fakeEngine{sessionID: "session-window"}, withTestSessions(sessions))
	service.ViewMu.Lock()
	service.Core.Snapshot.Session = SessionState{ID: "session-window"}
	// transcript 是全量权威事件源：record 由其全量重建（阶段 0 语义，
	// 取代旧的"磁盘 record + 窗口投影增量合并"路径）。
	service.components.tasks.AppendTranscriptEventLocked(TranscriptEvent{Role: "user", Content: "old question"})
	service.components.tasks.AppendTranscriptEventLocked(TranscriptEvent{Role: "assistant", Content: "updated answer"})
	service.components.tasks.AppendTranscriptEventLocked(TranscriptEvent{Role: "user", Content: "new question"})
	service.ViewMu.Unlock()

	if err := service.components.sessions.PersistCurrentSession(session_runtime.Location{Meta: SessionInfo{ID: "session-window"}}, "session-window"); err != nil {
		t.Fatal(err)
	}
	got := sessions.record.Conversation.Messages
	if len(got) != 3 || got[0].Content != "old question" || got[1].Content != "updated answer" || got[2].Content != "new question" {
		t.Fatalf("rebuilt durable conversation = %#v", got)
	}
}

func TestSessionRecordStoresLargeContentByReference(t *testing.T) {
	service := newTestService(t, &fakeEngine{sessionID: "session-large"})
	defer service.Shutdown()
	raw := strings.Repeat("raw-secret-output", task_context.DefaultToolResultLimit())
	service.ViewMu.Lock()
	service.Core.Snapshot.Session = SessionState{ID: "session-large"}
	service.Core.Snapshot.Chat = ChatState{RequestID: "task-large"}
	service.components.tasks.BeginTask("task-large", "inspect", "high", nil, TaskCheckpoint{})
	stored := service.components.tasks.StoreToolResultLocked("bash", raw)
	service.components.tasks.SetResultRefByCallIDLocked("call-large", stored.Ref)
	service.Core.Snapshot.Conversation = []Message{
		{Role: "tool", Tool: &ToolCall{ID: "call-large", Name: "bash", Result: raw}},
		{Role: "tool_result", Content: raw, Tool: &ToolCall{ID: "call-large", Name: "bash", Result: raw}},
	}
	record := service.components.sessions.SessionRecordLocked("session-large", nil)
	service.ViewMu.Unlock()

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
		service.ViewMu.Lock()
		service.Core.Snapshot.Task = &TaskState{Status: status}
		service.ViewMu.Unlock()
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

	service.handleToolStart(context.Background(), "plan_load", "plan-call", arguments)
	service.handleToolComplete("plan_load", "plan-call", `{"status":"loaded"}`, nil, 0)

	service.ViewMu.RLock()
	defer service.ViewMu.RUnlock()
	if service.components.tasks.ActivePlanID() == "" || len(service.components.tasks.PlanStack()) != 1 {
		t.Fatalf("plan stack = %#v, active = %q", service.components.tasks.PlanStack(), service.components.tasks.ActivePlanID())
	}
	frame := service.components.tasks.PlanStack()[0]
	if frame.ID != service.components.tasks.ActivePlanID() || frame.Arguments != arguments || frame.Plan == nil || frame.Plan.EntryNodeID != "inspect" {
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
			Version: session_runtime.SessionRecordVersion,
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
		if strings.HasPrefix(message.Content, session_runtime.SessionArchiveResumePrefix) {
			t.Fatalf("continuation fell back to generic resume marker instead of transcript: %#v", history)
		}
	}
}

// TestResumeSessionContinuationKeepsToolStepsWithoutEmptyAssistantEvents 是
// 长会话恢复顺序的回归：durable 可见会话里「助手思考步骤 → 工具调用 → 工具
// 结果 → 助手正文」必须原样进入重建的恢复历史；只有推理没有正文的助手步骤
// 不能变成空 assistant 事件（那正是恢复后「工具挤成一坨、正文掉队」的现场）。
func TestResumeSessionContinuationKeepsToolStepsWithoutEmptyAssistantEvents(t *testing.T) {
	const sessionID = "session-tool-steps"
	sessions := &archiveSessions{
		record: SessionRecord{
			Version: session_runtime.SessionRecordVersion,
			ID:      sessionID,
			Title:   SessionTitle{Value: "Tool step session", Source: "user"},
			Conversation: ConversationRecord{Messages: []Message{
				{ID: "message-1", Role: "user", Content: "original question"},
				{ID: "seq-2", Role: "assistant", ReasoningContent: "先读文件"},
				{ID: "seq-2#tool-1", Role: "tool", Tool: &ToolCall{
					ID: "call-1", Name: "read", Arguments: `{"path":"a.go"}`, Status: "success",
				}},
				{ID: "message-3", Role: "tool_result", Content: "package a", Tool: &ToolCall{
					ID: "call-1", Name: "read", Result: "package a", Status: "success",
				}},
				{ID: "message-4", Role: "assistant", Content: "original answer"},
			}},
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
	callIndex, outputIndex, answerIndex := -1, -1, -1
	for index, message := range history {
		if len(message.ToolCalls) > 0 {
			callIndex = index
			if call := message.ToolCalls[0]; call.ID != "call-1" || call.Arguments != `{"path":"a.go"}` {
				t.Fatalf("restored tool call = %#v", call)
			}
		}
		if message.Role == "tool" && strings.Contains(message.Content, "package a") {
			outputIndex = index
		}
		if message.Role == "assistant" && message.Content == "original answer" {
			answerIndex = index
		}
		if message.Content == context_runtime.MissingHistoryContent {
			t.Fatalf("restored history grew an empty assistant step: %#v", history)
		}
	}
	// 工具链轮紧跟 user 轮（index 1）：中间不能多出一轮「只有思考、没有正文」
	// 的助手步骤——那正是恢复历史里凭空长出来的空轮次。
	if callIndex != 1 || outputIndex != 2 || answerIndex != 3 {
		t.Fatalf("restored tool step order = call:%d output:%d answer:%d (%#v)", callIndex, outputIndex, answerIndex, history)
	}
}

func TestResumeSessionContinuationKeepsTrailingUnansweredUserInput(t *testing.T) {
	const sessionID = "session-trailing-user"
	sessions := &archiveSessions{
		record: SessionRecord{
			Version: session_runtime.SessionRecordVersion,
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
	service.ViewMu.Lock()
	service.appendHistoryLocked([]EngineMessage{{
		Role:    "assistant",
		Content: context_runtime.ToolCallHistoryContent,
		ToolCalls: []EngineToolCall{{
			ID: "call-1", Name: "read_file", Arguments: `{"path":"README.md"}`,
		}},
	}})
	service.ViewMu.Unlock()

	conversation := service.Snapshot().Conversation
	if len(conversation) != 1 || conversation[0].Tool == nil || conversation[0].Content != "" {
		t.Fatalf("visible conversation = %#v", conversation)
	}
}
