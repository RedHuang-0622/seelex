package core

import (
	"context"
	"errors"
	"testing"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
	"github.com/RedHuang-0622/seelex/application/model"
	"github.com/RedHuang-0622/seelex/session"
	"github.com/RedHuang-0622/seelex/sessionstore"
)

// forkServiceSessions 是 Service.ForkSession 的会话桩：持有父会话数据面并
// 记录子会话落盘参数；同时实现 resume 需要的 SessionRecordPort。
type forkServiceSessions struct {
	fakeSessions
	parent     SessionRecord
	events     []sessionstore.Event
	context    []byte
	results    []sessionstore.ToolResult
	generation string

	savedChild   string
	savedRecord  SessionRecord
	savedEvents  []model.TranscriptEvent
	savedResults []model.StoredToolResult
}

func (s *forkServiceSessions) LoadSessionRecord(string) (SessionRecord, error) {
	return s.parent, nil
}

func (s *forkServiceSessions) LoadSessionRecordWorkspace(_, sessionID string) (SessionRecord, error) {
	if sessionID == s.savedChild {
		return s.savedRecord, nil
	}
	return s.parent, nil
}

func (s *forkServiceSessions) SaveSessionRecord(string, SessionRecord) error { return nil }
func (s *forkServiceSessions) SaveSessionRecordWorkspace(string, string, SessionRecord) error {
	return nil
}

func (s *forkServiceSessions) LoadEventRangeWorkspace(projectID, sessionID string, fromSeq, toSeq uint64) ([]sessionstore.Event, error) {
	return s.events, nil
}

func (s *forkServiceSessions) LoadToolResultsWorkspace(projectID, sessionID string) ([]sessionstore.ToolResult, error) {
	return s.results, nil
}

func (s *forkServiceSessions) SaveSessionSnapshotWorkspace(projectID, sessionID string, history []EngineMessage, record SessionRecord, events []model.TranscriptEvent, results []model.StoredToolResult) error {
	s.savedChild = sessionID
	s.savedRecord = record
	s.savedEvents = events
	s.savedResults = results
	return nil
}

func (s *forkServiceSessions) LoadContextStateWorkspace(projectID, sessionID string) ([]byte, error) {
	return s.context, nil
}

func (s *forkServiceSessions) SaveContextStateWorkspace(projectID, sessionID string, payload []byte) error {
	return nil
}

func (s *forkServiceSessions) CurrentGenerationWorkspace(projectID, sessionID string) (string, error) {
	return s.generation, nil
}

func TestForkSessionCreatesAndSwitchesToChild(t *testing.T) {
	parent := SessionRecord{
		Version: 3,
		ID:      "parent",
		Title:   SessionTitle{Value: "Parent", Source: "first_request"},
		Conversation: ConversationRecord{Messages: []Message{
			{ID: "m1", Role: "user", Content: "hi"},
			{ID: "m2", Role: "assistant", Content: "hello"},
		}},
		Tasks: []dto.TaskRecord{
			{ID: "plan:1", Kind: "plan"},
			{ID: "todo:0", Kind: "todo"},
		},
	}
	sessions := &forkServiceSessions{
		parent: parent,
		events: []sessionstore.Event{
			{Seq: 1, TaskID: "chat-1", Role: "user", Content: "hi", MessageID: "m1"},
			{Seq: 2, TaskID: "chat-1", Role: "assistant", Content: "hello", MessageID: "m2"},
		},
		context:    []byte(`{"schema_version":1}`),
		generation: "generation-parent",
		results:    []sessionstore.ToolResult{{Ref: "result:1", Tool: "bash", Content: "out", Digest: "d", Size: 3, TokenCount: 1}},
	}
	service := newTestService(t, &fakeEngine{}, withTestSessions(sessions))

	childID, err := service.ForkSession("parent", ForkRequest{RequestID: "chat-1"})
	if err != nil {
		t.Fatal(err)
	}
	if childID != "session-new" || sessions.savedChild != "session-new" {
		t.Fatalf("child id = %q, saved under %q (want session-new)", childID, sessions.savedChild)
	}
	lineage := sessions.savedRecord.ForkedFrom
	if lineage == nil || lineage.ParentSessionID != "parent" || lineage.ParentGeneration != "generation-parent" {
		t.Fatalf("saved child lineage = %#v", lineage)
	}
	if len(sessions.savedEvents) != 2 || sessions.savedEvents[1].Seq != 2 {
		t.Fatalf("saved child events = %#v", sessions.savedEvents)
	}
	// tool-results 全量物理复制进子会话提交。
	if len(sessions.savedResults) != 1 || sessions.savedResults[0].Ref != "result:1" {
		t.Fatalf("saved child tool results = %#v", sessions.savedResults)
	}
	// 子会话 task 注册表保留 plan 等非 todo 条目，父 todolist 不继承。
	if len(sessions.savedRecord.Tasks) != 1 || sessions.savedRecord.Tasks[0].ID != "plan:1" {
		t.Fatalf("saved child tasks = %#v, want only plan:1（todolist 全新）", sessions.savedRecord.Tasks)
	}
	// fork 后自动切换到子会话继续。
	if got := service.Snapshot().Session.ID; got != "session-new" {
		t.Fatalf("active session after fork = %q, want session-new", got)
	}
}

func TestForkSessionLatestResolvesNewestParagraph(t *testing.T) {
	parent := SessionRecord{
		Version: 3,
		ID:      "parent",
		Title:   SessionTitle{Value: "Parent", Source: "first_request"},
		Conversation: ConversationRecord{Messages: []Message{
			{ID: "m1", Role: "user", Content: "hi"},
			{ID: "m2", Role: "assistant", Content: "hello"},
			{ID: "m3", Role: "user", Content: "again"},
			{ID: "m4", Role: "assistant", Content: "answer"},
		}},
	}
	sessions := &forkServiceSessions{
		parent: parent,
		events: []sessionstore.Event{
			{Seq: 1, TaskID: "chat-1", Role: "user", Content: "hi", MessageID: "m1"},
			{Seq: 2, TaskID: "chat-1", Role: "assistant", Content: "hello", MessageID: "m2"},
			{Seq: 3, TaskID: "chat-2", Role: "user", Content: "again", MessageID: "m3"},
			{Seq: 4, TaskID: "chat-2", Role: "assistant", Content: "answer", MessageID: "m4"},
		},
		context:    []byte(`{"schema_version":1}`),
		generation: "generation-parent",
	}
	service := newTestService(t, &fakeEngine{}, withTestSessions(sessions))

	childID, err := service.ForkSessionLatest("parent")
	if err != nil {
		t.Fatal(err)
	}
	if childID != "session-new" {
		t.Fatalf("child id = %q", childID)
	}
	if len(sessions.savedEvents) != 4 || sessions.savedEvents[3].Seq != 4 {
		t.Fatalf("latest fork inherited events = %#v, want newest complete round (seq 4)", sessions.savedEvents)
	}
	if sessions.savedRecord.ForkedFrom == nil || sessions.savedRecord.ForkedFrom.ForkPoint.EventSeq != 4 {
		t.Fatalf("fork point = %#v, want EventSeq 4", sessions.savedRecord.ForkedFrom)
	}
}

func TestForkCommandForksCurrentSession(t *testing.T) {
	parent := SessionRecord{
		Version: 3,
		ID:      "parent",
		Title:   SessionTitle{Value: "Parent", Source: "first_request"},
	}
	sessions := &forkServiceSessions{
		parent: parent,
		events: []sessionstore.Event{
			{Seq: 1, TaskID: "chat-1", Role: "user", Content: "hi", MessageID: "m1"},
			{Seq: 2, TaskID: "chat-1", Role: "assistant", Content: "hello", MessageID: "m2"},
		},
		context:    []byte(`{"schema_version":1}`),
		generation: "generation-parent",
	}
	service := newTestService(t, &fakeEngine{}, withTestSessions(sessions))
	service.Mu.Lock()
	service.Core.Snapshot.Session = SessionState{ID: "parent"}
	service.Mu.Unlock()

	if err := service.submitCommand(context.Background(), "/fork"); err != nil {
		t.Fatal(err)
	}
	if sessions.savedChild != "session-new" {
		t.Fatalf("/fork did not fork the current session: saved under %q", sessions.savedChild)
	}
	if got := service.Snapshot().Session.ID; got != "session-new" {
		t.Fatalf("active session after /fork = %q, want session-new", got)
	}
}

// TestForkSessionDeepCopyIsolation（T2.7）：fork 子会话 record 与父数据面
// 引用不相交——改子不影响父（Conversation/Tasks/PlanStack）。
func TestForkSessionDeepCopyIsolation(t *testing.T) {
	parent := SessionRecord{
		Version: 3,
		ID:      "parent",
		Title:   SessionTitle{Value: "Parent", Source: "first_request"},
		Conversation: ConversationRecord{Messages: []Message{
			{ID: "m1", Role: "user", Content: "hi"},
			{ID: "m2", Role: "assistant", Content: "let me read"},
			{ID: "m3", Role: "tool_result", Content: "out", Tool: &ToolCall{ID: "t1", Name: "read", Arguments: "{}"}},
		}},
		Tasks: []dto.TaskRecord{
			{ID: "plan:1", Kind: "plan"},
		},
		PlanStack: []model.SessionPlanFrame{
			{ID: "plan-1"},
		},
	}
	sessions := &forkServiceSessions{
		parent: parent,
		events: []sessionstore.Event{
			{Seq: 1, TaskID: "chat-1", Role: "user", Content: "hi", MessageID: "m1"},
			{Seq: 2, TaskID: "chat-1", Role: "assistant", ToolCalls: []sessionstore.EventToolCall{{ID: "t1", Name: "read", Arguments: "{}"}}, MessageID: "m2"},
			{Seq: 3, TaskID: "chat-1", Role: "tool", ToolCallID: "t1", Name: "read", Content: "out", MessageID: "m3"},
		},
		context:    []byte(`{"schema_version":1}`),
		generation: "generation-parent",
	}
	service := newTestService(t, &fakeEngine{}, withTestSessions(sessions))

	if _, err := service.ForkSession("parent", ForkRequest{RequestID: "chat-1"}); err != nil {
		t.Fatal(err)
	}
	// 修改子会话 record：追加消息 + 改 tool 参数 + 改 plan。
	sessions.savedRecord.Conversation.Messages = append(sessions.savedRecord.Conversation.Messages,
		Message{ID: "c-1", Role: "assistant", Content: "child"})
	mutatedTool := false
	for index := range sessions.savedRecord.Conversation.Messages {
		if sessions.savedRecord.Conversation.Messages[index].Tool != nil {
			sessions.savedRecord.Conversation.Messages[index].Tool.Arguments = "mutated"
			mutatedTool = true
			break
		}
	}
	if !mutatedTool {
		t.Fatal("child record has no tool message to mutate (fixture shape mismatch)")
	}
	if len(sessions.savedRecord.PlanStack) > 0 {
		sessions.savedRecord.PlanStack[0].ID = "plan-child"
	}
	sessions.savedRecord.Tasks = append(sessions.savedRecord.Tasks, dto.TaskRecord{ID: "child-task", Kind: "todo"})

	// 父数据面不受影响。
	if len(parent.Conversation.Messages) != 3 {
		t.Fatalf("parent conversation mutated: %+v", parent.Conversation.Messages)
	}
	for _, message := range parent.Conversation.Messages {
		if message.Tool != nil && message.Tool.Arguments == "mutated" {
			t.Fatal("parent tool arguments mutated by child")
		}
	}
	if parent.PlanStack[0].ID != "plan-1" {
		t.Fatalf("parent plan stack mutated: %+v", parent.PlanStack)
	}
	if len(parent.Tasks) != 1 {
		t.Fatalf("parent tasks mutated: %+v", parent.Tasks)
	}
}

// TestForkSessionRejectsRunningParent（UC5）：父会话运行中拒绝 fork。
func TestForkSessionRejectsRunningParent(t *testing.T) {
	sessions := &forkServiceSessions{
		parent:     SessionRecord{Version: 3, ID: "parent", Title: SessionTitle{Value: "Parent"}},
		context:    []byte(`{}`),
		generation: "generation-parent",
	}
	service := newTestService(t, &fakeEngine{}, withTestSessions(sessions))

	// 标记父会话运行中。
	service.Mu.Lock()
	service.Core.Snapshot.Session = SessionState{ID: "parent"}
	unit := service.sessions.Unit("parent")
	if unit == nil {
		unit, _ = session.NewSessionUnit("parent")
		service.sessions.Register(unit)
	}
	unit.SetChatState(ChatState{Running: true, RequestID: "req-parent"}, nil)
	service.Mu.Unlock()

	if _, err := service.ForkSession("parent", ForkRequest{}); !errors.Is(err, ErrChatRunning) {
		t.Fatalf("ForkSession(running parent) = %v, want ErrChatRunning", err)
	}
	if sessions.savedChild != "" {
		t.Fatalf("fork wrote child %q while parent running", sessions.savedChild)
	}
}
