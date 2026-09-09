package session_runtime

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/application/contract"
	"github.com/RedHuang-0622/seelex/application/contract/dto"
	"github.com/RedHuang-0622/seelex/application/core/internal/state"
	"github.com/RedHuang-0622/seelex/application/model"
	"github.com/RedHuang-0622/seelex/sessionstore"
)

// forkTestSessions 是 PrepareFork 的持久化面桩：持有父会话数据面，并记录
// 子会话快照落盘参数。
type forkTestSessions struct {
	parentRecord model.SessionRecord
	events       []sessionstore.Event
	context      []byte
	results      []sessionstore.ToolResult
	generation   string
	workspaceID  string

	savedChild   string
	savedRecord  model.SessionRecord
	savedEvents  []model.TranscriptEvent
	savedContext []byte
	savedResults []model.StoredToolResult
}

func (s *forkTestSessions) SaveCurrent(string) error                             { return nil }
func (s *forkTestSessions) Delete(string) error                                  { return nil }
func (s *forkTestSessions) List() []model.SessionInfo                            { return nil }
func (s *forkTestSessions) LoadHistory(string) ([]contract.EngineMessage, error) { return nil, nil }
func (s *forkTestSessions) LoadHistoryRange(string, int, int) ([]contract.EngineMessage, int, error) {
	return nil, 0, nil
}
func (s *forkTestSessions) SetWorkspace(string) {}
func (s *forkTestSessions) Workspace() string   { return s.workspaceID }

func (s *forkTestSessions) SaveSessionRecord(string, model.SessionRecord) error { return nil }
func (s *forkTestSessions) SaveSessionRecordWorkspace(string, string, model.SessionRecord) error {
	return nil
}
func (s *forkTestSessions) LoadSessionRecord(string) (model.SessionRecord, error) {
	return s.parentRecord, nil
}
func (s *forkTestSessions) LoadSessionRecordWorkspace(projectID, sessionID string) (model.SessionRecord, error) {
	if sessionID == s.savedChild {
		return s.savedRecord, nil
	}
	return s.parentRecord, nil
}

func (s *forkTestSessions) LoadEventRangeWorkspace(projectID, sessionID string, fromSeq, toSeq uint64) ([]sessionstore.Event, error) {
	return s.events, nil
}
func (s *forkTestSessions) LoadToolResultsWorkspace(projectID, sessionID string) ([]sessionstore.ToolResult, error) {
	return s.results, nil
}
func (s *forkTestSessions) SaveSessionSnapshotWorkspace(projectID, sessionID string, history []contract.EngineMessage, record model.SessionRecord, events []model.TranscriptEvent, results []model.StoredToolResult) error {
	s.savedChild = sessionID
	s.savedRecord = record
	s.savedEvents = events
	s.savedResults = results
	return nil
}
func (s *forkTestSessions) LoadContextStateWorkspace(projectID, sessionID string) ([]byte, error) {
	return s.context, nil
}
func (s *forkTestSessions) SaveContextStateWorkspace(projectID, sessionID string, payload []byte) error {
	s.savedContext = payload
	return nil
}
func (s *forkTestSessions) CurrentGenerationWorkspace(projectID, sessionID string) (string, error) {
	return s.generation, nil
}

func newForkTestCoordinator(t *testing.T, sessions contract.SessionPort) *Coordinator {
	t.Helper()
	core := state.New(contract.Dependencies{Sessions: sessions})
	return NewCoordinator(Deps{
		Core: core,
		IsInternalContent: func(content string) bool {
			return strings.HasPrefix(content, "<!-- seelex:") || strings.HasPrefix(content, "<provider>")
		},
		DisplayUserInput: func(input string) string { return input },
	})
}

func forkTestFixture() (*forkTestSessions, time.Time) {
	t1 := time.Unix(1, 0)
	t2 := time.Unix(2, 0)
	t3 := time.Unix(3, 0)
	t4 := time.Unix(4, 0)
	t5 := time.Unix(5, 0)
	events := []sessionstore.Event{
		{Seq: 1, TaskID: "chat-1", Role: "user", Content: "inspect", MessageID: "m1", CreatedAt: t1},
		{Seq: 2, TaskID: "chat-1", Role: "assistant", ToolCalls: []sessionstore.EventToolCall{{ID: "a", Name: "read"}}, MessageID: "m2", CreatedAt: t2},
		{Seq: 3, TaskID: "chat-1", Role: "tool", ToolCallID: "a", Name: "read", ResultRef: "result:read", MessageID: "m3", CreatedAt: t3},
		{Seq: 4, TaskID: "chat-2", Role: "user", Content: "next", MessageID: "m4", CreatedAt: t4},
		{Seq: 5, TaskID: "chat-2", Role: "assistant", Content: "answer", MessageID: "m5", CreatedAt: t5},
	}
	messages := []model.Message{
		{ID: "m1", Role: "user", Content: "inspect", CreatedAt: t1},
		{ID: "m2", Role: "assistant", Content: "read", CreatedAt: t2},
		{ID: "m3", Role: "tool_result", Content: "found", CreatedAt: t3},
		{ID: "m4", Role: "user", Content: "next", CreatedAt: t4},
		{ID: "m5", Role: "assistant", Content: "answer", CreatedAt: t5},
	}
	record := model.SessionRecord{
		Version: SessionRecordVersion,
		ID:      "parent",
		Title:   model.SessionTitle{Value: "Parent title", Source: "first_request", FinalizedAt: t1},
		PlanStack: []model.SessionPlanFrame{
			{ID: "plan-1", LoadedAt: t1},
			{ID: "plan-2", LoadedAt: t4},
		},
		ActivePlanID: "plan-1",
		Tasks: []dto.TaskRecord{
			{ID: "task-1", Kind: "plan", CreatedAt: t1},
			{ID: "subagent:1", Kind: "subagent", CreatedAt: t1},
			{ID: "todo:1", Kind: "todo", CreatedAt: t1},
			{ID: "task-2", Kind: "task", CreatedAt: t4},
			{ID: "todo:2", Kind: "todo", CreatedAt: t4},
		},
		Conversation: model.ConversationRecord{Messages: messages, UpdatedAt: t5},
		Checkpoints: []model.TaskCheckpoint{{
			CoversEventRange: model.EventRange{Start: 1, End: 10},
			ToolResultRefs:   []string{"result:checkpoint"},
		}},
		Projection: &model.TaskContextProjection{
			SchemaVersion: model.TaskContextProjectionSchemaVersion,
			SessionID:     "parent",
			Checkpoint: model.TaskCheckpoint{
				CoversEventRange: model.EventRange{Start: 1, End: 10},
				ToolResultRefs:   []string{"result:checkpoint"},
			},
		},
		Execution: model.SessionExecutionRecord{
			ReadFiles: []model.ReadFileRef{
				{Path: "a.txt", ReadAt: t1},
				{Path: "b.txt", ReadAt: t4},
			},
			Task: &model.TaskState{
				Status:    model.TaskCompleted,
				UpdatedAt: t4,
				ContextCompactions: []model.ContextCompaction{{
					Version: 1, Reason: "window", MessagesBefore: 5, EstimatedTokens: 10, CompactedAt: t2,
				}},
			},
			Continuation: "latest request summary",
		},
		ToolResults: []model.ToolResultRef{
			{Ref: "result:read", Tool: "read"},
			{Ref: "result:checkpoint", Tool: "checkpoint"},
			{Ref: sessionstore.CompressedTurnRefPrefix + "seg-cross", Tool: "compact_frame"},
			{Ref: "result:later", Tool: "later"},
		},
		UpdatedAt: t5,
	}
	contextRecord := sessionstore.SessionContextRecord{
		SchemaVersion: sessionstore.SessionContextSchemaVersion,
		SystemPrompt:  "system prompt",
		PlanStack: []sessionstore.PlanFrame{
			{PlanID: "plan-1", Title: "p1", Status: "active", EnteredAt: t1},
			{PlanID: "plan-2", Title: "p2", Status: "active", EnteredAt: t4},
		},
		TaskStack: []sessionstore.TaskFrame{
			{TaskID: "task-1", Objective: "one", Status: "active", EnteredAt: t1},
			{TaskID: "task-2", Objective: "two", Status: "active", EnteredAt: t4},
		},
		SkillStack: []sessionstore.SkillFrame{
			{SkillID: "s1", Name: "one", ActivatedAt: t1},
			{SkillID: "s2", Name: "two", ActivatedAt: t4},
		},
		GoalStack: []sessionstore.GoalFrame{
			{GoalID: "goal-1", Title: "父目标", Status: "active", EnteredAt: t1},
			{GoalID: "goal-2", Title: "fork 后新目标", Status: "active", EnteredAt: t4},
		},
		GoalAudit: []sessionstore.GoalAuditEntry{
			{Seq: 1, Kind: "goal.begin", GoalID: "goal-1", Title: "父目标", Status: "active", At: t1.Unix()},
			{Seq: 2, Kind: "goal.begin", GoalID: "goal-2", Title: "fork 后新目标", Status: "active", At: t4.Unix()},
		},
		CompactStack: []sessionstore.CompactFrame{
			{
				SegmentID: "seg-cross", From: 0, To: 2, EventFrom: 1, EventTo: 5,
				MessageFrom: "m1", MessageTo: "m5",
				Evidence: []sessionstore.EvidenceRef{{Ref: "result:evidence"}},
				Summary:  "summary up to five",
			},
			{
				SegmentID: "seg-later", EventFrom: 4, EventTo: 5,
				MessageFrom: "m4", MessageTo: "m5", Summary: "summary later",
			},
		},
	}
	contextPayload, _ := json.Marshal(contextRecord)
	sessions := &forkTestSessions{
		parentRecord: record,
		events:       events,
		context:      contextPayload,
		results: []sessionstore.ToolResult{
			{Ref: "result:read", Tool: "read", Content: "found", Digest: "d1", Size: 5, TokenCount: 1, CreatedAt: t3},
			{Ref: sessionstore.CompressedTurnRefPrefix + "seg-cross", Tool: "compact_frame", Content: "archived", Digest: "d2", Size: 8, TokenCount: 2, CreatedAt: t2},
			{Ref: "result:later", Tool: "later", Content: "post fork", Digest: "d3", Size: 9, TokenCount: 3, CreatedAt: t5},
		},
		generation:  "generation-parent-1",
		workspaceID: "ws-1",
	}
	return sessions, t3
}

func TestPrepareForkTruncatesToRequestBoundary(t *testing.T) {
	sessions, cutTime := forkTestFixture()
	coordinator := newForkTestCoordinator(t, sessions)
	location := Location{WorkspaceID: "ws-1"}

	forkContext, err := coordinator.PrepareFork(location, "child", "parent", model.ForkRequest{RequestID: "chat-1"})
	if err != nil {
		t.Fatal(err)
	}
	record := forkContext.Record
	if record.ID != "child" || record.Version != SessionRecordVersion {
		t.Fatalf("child record identity = %#v", record)
	}
	lineage := record.ForkedFrom
	if lineage == nil {
		t.Fatal("forked_from lineage is missing")
	}
	if lineage.ParentSessionID != "parent" || lineage.ParentWorkspaceID != "ws-1" || lineage.ParentGeneration != "generation-parent-1" {
		t.Fatalf("lineage = %#v", lineage)
	}
	point := lineage.ForkPoint
	if point.EventSeq != 3 || point.EventCount != 3 || point.Round != 1 || point.RequestID != "chat-1" || point.MessageID != "m3" || point.MessageCount != 3 {
		t.Fatalf("fork point = %#v", point)
	}
	if len(forkContext.Events) != 3 || forkContext.Events[2].Seq != 3 {
		t.Fatalf("fork events = %#v", forkContext.Events)
	}
	if len(record.Conversation.Messages) != 3 {
		t.Fatalf("conversation messages = %#v", record.Conversation.Messages)
	}
	for _, message := range record.Conversation.Messages {
		if message.CreatedAt.After(cutTime) {
			t.Fatalf("post-cut message leaked: %#v", message)
		}
	}
	if record.Checkpoints[0].CoversEventRange.End != 3 {
		t.Fatalf("checkpoint end = %d, want 3", record.Checkpoints[0].CoversEventRange.End)
	}
	if record.Projection.Checkpoint.CoversEventRange.End != 3 {
		t.Fatalf("projection checkpoint end = %d, want 3", record.Projection.Checkpoint.CoversEventRange.End)
	}
	if len(record.PlanStack) != 1 || record.PlanStack[0].ID != "plan-1" || record.ActivePlanID != "plan-1" {
		t.Fatalf("plan stack = %#v active=%q", record.PlanStack, record.ActivePlanID)
	}
	if len(record.Tasks) != 2 || record.Tasks[0].ID != "task-1" || record.Tasks[0].Kind != "plan" || record.Tasks[1].ID != "subagent:1" || record.Tasks[1].Kind != "subagent" {
		t.Fatalf("tasks = %#v（子会话不得继承父 todolist）", record.Tasks)
	}
	for _, task := range record.Tasks {
		if task.Kind == "todo" {
			t.Fatalf("child session must not inherit parent todolist: %#v", task)
		}
	}
	if len(record.Execution.ReadFiles) != 1 || record.Execution.ReadFiles[0].Path != "a.txt" {
		t.Fatalf("read files = %#v", record.Execution.ReadFiles)
	}
	if record.Execution.Task == nil || len(record.Execution.Task.ContextCompactions) != 1 || !record.Execution.Task.UpdatedAt.IsZero() {
		t.Fatalf("execution task = %#v (结论晚于切断点应只保留压缩历史)", record.Execution.Task)
	}
	if record.Execution.Continuation != "" {
		t.Fatalf("continuation must not be inherited: %q", record.Execution.Continuation)
	}
	wantRefs := []string{"result:read", "result:checkpoint", sessionstore.CompressedTurnRefPrefix + "seg-cross"}
	gotRefs := make(map[string]bool, len(record.ToolResults))
	for _, ref := range record.ToolResults {
		gotRefs[ref.Ref] = true
	}
	for _, ref := range wantRefs {
		if !gotRefs[ref] {
			t.Fatalf("reachable ref %q missing from registry: %#v", ref, record.ToolResults)
		}
	}
	if gotRefs["result:later"] {
		t.Fatalf("unreachable post-cut ref leaked into registry: %#v", record.ToolResults)
	}

	// 全量 tool-results 物理复制（含父通道中不可达/后 fork 条目）。
	if len(forkContext.ToolResults) != 3 {
		t.Fatalf("fork tool results = %d, want full channel copy (3)", len(forkContext.ToolResults))
	}
	// context blob 不再承载三栈：plan/task 权威 = §2.4 栈通道（子会话由
	// ForkStacks 按 message 锚重建），blob 内不得留影子副本；skill 记录仍按
	// fork 时刻过滤，压缩帧整帧继承 + 重写。
	var contextRecord sessionstore.SessionContextRecord
	if err := json.Unmarshal(forkContext.Context, &contextRecord); err != nil {
		t.Fatal(err)
	}
	if len(contextRecord.PlanStack) != 0 || len(contextRecord.TaskStack) != 0 {
		t.Fatalf("stacks must not travel in the context blob: plan=%#v task=%#v",
			contextRecord.PlanStack, contextRecord.TaskStack)
	}
	if len(contextRecord.SkillStack) != 1 || contextRecord.SkillStack[0].SkillID != "s1" {
		t.Fatalf("context skill stack = %#v", contextRecord.SkillStack)
	}
	if len(contextRecord.GoalStack) != 0 {
		t.Fatalf("fork 会话不得继承父 goal 栈（D4）: %#v", contextRecord.GoalStack)
	}
	if len(contextRecord.GoalAudit) != 0 {
		t.Fatalf("fork 会话不得继承父 goal 审计账本: %#v", contextRecord.GoalAudit)
	}
	if contextRecord.SchemaVersion != sessionstore.SessionContextSchemaVersion {
		t.Fatalf("fork context schema = %d, want %d", contextRecord.SchemaVersion, sessionstore.SessionContextSchemaVersion)
	}
	if len(contextRecord.CompactStack) != 1 {
		t.Fatalf("compact stack = %#v, want only the inherited crossing frame", contextRecord.CompactStack)
	}
	frame := contextRecord.CompactStack[0]
	if frame.SegmentID != "seg-cross" || frame.EventTo != 3 || frame.MessageTo != "m3" || frame.EventFrom != 1 {
		t.Fatalf("rewritten frame = %#v", frame)
	}
	if len(frame.Evidence) != 1 || frame.Evidence[0].Ref != "result:evidence" {
		t.Fatalf("frame evidence lost: %#v", frame.Evidence)
	}
}

func TestPrepareForkRejectsInvalidCutPoints(t *testing.T) {
	sessions, _ := forkTestFixture()
	coordinator := newForkTestCoordinator(t, sessions)
	location := Location{WorkspaceID: "ws-1"}

	if _, err := coordinator.PrepareFork(location, "child", "parent", model.ForkRequest{EventSeq: 2}); err == nil || !strings.Contains(err.Error(), "paragraph boundary") {
		t.Fatalf("interior cut error = %v, want paragraph boundary rejection", err)
	}
	if _, err := coordinator.PrepareFork(location, "child", "parent", model.ForkRequest{RequestID: "chat-missing"}); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("unknown request error = %v", err)
	}
	if _, err := coordinator.PrepareFork(location, "child", "parent", model.ForkRequest{RequestID: "chat-1", EventSeq: 3}); err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("both selectors error = %v", err)
	}
}

func TestPrepareForkAtStartProducesEmptyChild(t *testing.T) {
	sessions, _ := forkTestFixture()
	coordinator := newForkTestCoordinator(t, sessions)
	location := Location{WorkspaceID: "ws-1"}
	forkContext, err := coordinator.PrepareFork(location, "child", "parent", model.ForkRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(forkContext.Events) != 0 || len(forkContext.Record.Conversation.Messages) != 0 {
		t.Fatalf("empty fork inherited content: events=%d messages=%d", len(forkContext.Events), len(forkContext.Record.Conversation.Messages))
	}
	if forkContext.Record.ForkedFrom == nil || forkContext.Record.ForkedFrom.ForkPoint.EventSeq != 0 {
		t.Fatalf("fork start lineage = %#v", forkContext.Record.ForkedFrom)
	}
	var contextRecord sessionstore.SessionContextRecord
	if err := json.Unmarshal(forkContext.Context, &contextRecord); err != nil {
		t.Fatal(err)
	}
	if len(contextRecord.CompactStack) != 0 {
		t.Fatalf("empty fork must drop all compact frames: %#v", contextRecord.CompactStack)
	}
}

func TestForkTaskRecordsFiltersTodolist(t *testing.T) {
	cut := time.Unix(2, 0)
	tasks := []dto.TaskRecord{
		{ID: "todo:0", Kind: "todo", CreatedAt: time.Unix(1, 0)},
		{ID: "task-1", Kind: "task", CreatedAt: time.Unix(1, 0)},
		{ID: "plan:1", Kind: "plan", CreatedAt: time.Unix(1, 0)},
		{ID: "subagent:1", Kind: "subagent", CreatedAt: time.Unix(1, 0)},
		{ID: "legacy", CreatedAt: time.Unix(1, 0)},
		{ID: "todo:1", Kind: "todo", CreatedAt: time.Unix(3, 0)},
		{ID: "task-2", Kind: "task", CreatedAt: time.Unix(3, 0)},
	}
	got := forkTaskRecordsByTime(tasks, cut)
	want := []string{"task-1", "plan:1", "subagent:1", "legacy"}
	if len(got) != len(want) {
		t.Fatalf("fork tasks = %d, want %d: %#v", len(got), len(want), got)
	}
	for index, id := range want {
		if got[index].ID != id {
			t.Fatalf("fork tasks[%d] = %q, want %q（顺序保持）", index, got[index].ID, id)
		}
	}
	for _, task := range got {
		if task.Kind == "todo" {
			t.Fatalf("todolist item leaked into child: %#v", task)
		}
	}
}
