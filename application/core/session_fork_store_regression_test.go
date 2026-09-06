package core

// 真实 Router + SessionGranularStore 的 fork 回归：父会话以与生产一致的
// 提交（Commit: state+events）落盘，随后 ForkSessionLatest 走真实存储
// 适配读写（LoadRecordRaw/EventRange/SaveCommit），断言子会话在恢复后
// SnapshotOf 可见截至切点的正文。曾复现：子 record 落盘含 2 条消息，但
// SnapshotOf(child) 恒 total=0。

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/RedHuang-0622/seelex/application/contract"
	"github.com/RedHuang-0622/seelex/application/model"
	"github.com/RedHuang-0622/seelex/sessionstore"
)

// routerForkSessions 是 Dependencies.Sessions 的测试双件：满足 base
// SessionPort，并提供 fork/resume 需要的 record/snapshot/transcript/
// granular 可选端口，全部直连真实 Router（与 internal/adapters.SessionPort
// 同语义，但不依赖 seelebridge.Runtime 的 context 挂接）。
type routerForkSessions struct {
	router    *sessionstore.Router
	granular  *sessionstore.SessionGranularStore
	workspace string
}

func newRouterForkSessions(t *testing.T) *routerForkSessions {
	t.Helper()
	root := t.TempDir()
	router, err := sessionstore.NewRouter(filepath.Join(root, "session-storage.json"), root)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	t.Cleanup(func() { _ = router.Close() })
	return &routerForkSessions{router: router, granular: sessionstore.NewSessionGranularStore(router)}
}

func (s *routerForkSessions) saveParentFixture(t *testing.T, record model.SessionRecord,
	events []sessionstore.Event, contextPayload []byte) {
	t.Helper()
	payload, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.granular.SaveCommit("", record.ID, sessionstore.Commit{
		Events: events,
		State:  payload,
	}); err != nil {
		t.Fatalf("save parent fixture: %v", err)
	}
	if err := s.granular.SaveContext("", record.ID, contextPayload); err != nil {
		t.Fatalf("save parent context: %v", err)
	}
}

// ─── base SessionPort ────────────────────────────────────────────────

func (s *routerForkSessions) SaveCurrent(string) error { return nil }
func (s *routerForkSessions) SetWorkspace(projectID string) {
	s.workspace = projectID
}
func (s *routerForkSessions) Workspace() string { return s.workspace }

func (s *routerForkSessions) List() []model.SessionInfo {
	return s.SessionsOf(s.workspace)
}

func (s *routerForkSessions) LoadHistory(string) ([]EngineMessage, error) {
	return nil, nil
}

func (s *routerForkSessions) LoadHistoryRange(string, int, int) ([]EngineMessage, int, error) {
	return nil, 0, nil
}

func (s *routerForkSessions) Delete(sessionID string) error {
	return s.granular.Delete(s.granular.ResolveProjectForSession(sessionID), sessionID)
}

// ─── SessionGranularPort ──────────────────────────────────────────────

func (s *routerForkSessions) SessionsOf(projectID string) []model.SessionInfo {
	infos, err := s.granular.SessionsOf(projectID)
	if err != nil {
		return nil
	}
	result := make([]model.SessionInfo, 0, len(infos))
	for _, info := range infos {
		result = append(result, model.SessionInfo{
			ID:         info.ID,
			Name:       info.Title,
			UpdatedAt:  info.UpdatedAt,
			TokenCount: info.TokenCount,
		})
	}
	return result
}

// ─── SessionRecordPort / SessionSnapshotPort / SessionForkPort ───────

func (s *routerForkSessions) SaveSessionRecord(sessionID string, record model.SessionRecord) error {
	return s.SaveSessionRecordWorkspace("", sessionID, record)
}

func (s *routerForkSessions) SaveSessionRecordWorkspace(projectID, sessionID string, record model.SessionRecord) error {
	payload, err := json.Marshal(record)
	if err != nil {
		return err
	}
	return s.granular.SaveRecordRaw(projectID, sessionID, payload)
}

func (s *routerForkSessions) LoadSessionRecord(sessionID string) (model.SessionRecord, error) {
	return s.LoadSessionRecordWorkspace("", sessionID)
}

func (s *routerForkSessions) LoadSessionRecordWorkspace(projectID, sessionID string) (model.SessionRecord, error) {
	payload, err := s.granular.LoadRecordRaw(projectID, sessionID)
	if err != nil {
		return model.SessionRecord{}, err
	}
	var record model.SessionRecord
	if err := json.Unmarshal(payload, &record); err != nil {
		return model.SessionRecord{}, err
	}
	if record.ID != sessionID {
		return model.SessionRecord{}, os.ErrNotExist
	}
	return record, nil
}

func (s *routerForkSessions) SaveSessionSnapshot(
	sessionID string, history []contract.EngineMessage,
	record model.SessionRecord, events []model.TranscriptEvent,
	results []model.StoredToolResult,
) error {
	return s.SaveSessionSnapshotWorkspace("", sessionID, history, record, events, results)
}

func (s *routerForkSessions) SaveSessionSnapshotWorkspace(
	projectID, sessionID string, _ []contract.EngineMessage,
	record model.SessionRecord, events []model.TranscriptEvent,
	results []model.StoredToolResult,
) error {
	payload, err := json.Marshal(record)
	if err != nil {
		return err
	}
	return s.granular.SaveCommit(projectID, sessionID, sessionstore.Commit{
		Events:      transcriptEventsToStore(events),
		State:       payload,
		ToolResults: storedToolResultsToStore(results),
	})
}

func (s *routerForkSessions) LoadEventRangeWorkspace(projectID, sessionID string, fromSeq, toSeq uint64) ([]sessionstore.Event, error) {
	return s.granular.EventRange(projectID, sessionID, fromSeq, toSeq)
}

func (s *routerForkSessions) LoadToolResultsWorkspace(projectID, sessionID string) ([]sessionstore.ToolResult, error) {
	return s.granular.ListToolResults(projectID, sessionID)
}

func (s *routerForkSessions) LoadContextStateWorkspace(projectID, sessionID string) ([]byte, error) {
	return s.granular.LoadContextRaw(projectID, sessionID)
}

func (s *routerForkSessions) SaveContextStateWorkspace(projectID, sessionID string, payload []byte) error {
	return s.granular.SaveContext(projectID, sessionID, payload)
}

func (s *routerForkSessions) CurrentGenerationWorkspace(projectID, sessionID string) (string, error) {
	return s.granular.CurrentGeneration(projectID, sessionID)
}

func (s *routerForkSessions) LoadTranscriptTailWorkspace(projectID, sessionID string, _, _ int) ([]model.TranscriptEvent, error) {
	events, err := s.granular.TranscriptTail(projectID, sessionID, 1<<20, 4)
	if err != nil {
		return nil, err
	}
	return storeEventsToTranscript(events), nil
}

func (s *routerForkSessions) LoadToolResultWorkspace(projectID, sessionID, ref string) (model.StoredToolResult, error) {
	result, err := s.granular.ToolResult(projectID, sessionID, ref)
	if err != nil {
		return model.StoredToolResult{}, err
	}
	return model.StoredToolResult{ToolResultRef: model.ToolResultRef{Ref: result.Ref}}, nil
}

// ─── 转换 ──────────────────────────────────────────────────────────────

func transcriptEventsToStore(events []model.TranscriptEvent) []sessionstore.Event {
	stored := make([]sessionstore.Event, len(events))
	for index, event := range events {
		calls := make([]sessionstore.EventToolCall, len(event.ToolCalls))
		for callIndex, call := range event.ToolCalls {
			calls[callIndex] = sessionstore.EventToolCall{ID: call.ID, Name: call.Name, Arguments: call.Arguments}
		}
		stored[index] = sessionstore.Event{
			Seq: event.Seq, TaskID: event.TaskID, MessageID: event.MessageID, Role: event.Role,
			ReasoningContent: event.ReasoningContent, Content: event.Content,
			ToolCallID: event.ToolCallID, Name: event.Name, ToolCalls: calls,
			ResultRef: event.ResultRef, TokenCount: event.TokenCount, CreatedAt: event.CreatedAt,
		}
	}
	return stored
}

func storeEventsToTranscript(events []sessionstore.Event) []model.TranscriptEvent {
	adapted := make([]model.TranscriptEvent, len(events))
	for index, event := range events {
		calls := make([]model.TranscriptToolCall, len(event.ToolCalls))
		for callIndex, call := range event.ToolCalls {
			calls[callIndex] = model.TranscriptToolCall{ID: call.ID, Name: call.Name, Arguments: call.Arguments}
		}
		adapted[index] = model.TranscriptEvent{
			Seq: event.Seq, TaskID: event.TaskID, MessageID: event.MessageID, Role: event.Role,
			ReasoningContent: event.ReasoningContent, Content: event.Content,
			ToolCallID: event.ToolCallID, Name: event.Name, ToolCalls: calls,
			ResultRef: event.ResultRef, TokenCount: event.TokenCount, CreatedAt: event.CreatedAt,
		}
	}
	return adapted
}

func storedToolResultsToStore(results []model.StoredToolResult) []sessionstore.ToolResult {
	stored := make([]sessionstore.ToolResult, len(results))
	for index, result := range results {
		stored[index] = sessionstore.ToolResult{
			Ref:        result.ToolResultRef.Ref,
			Tool:       result.ToolResultRef.Tool,
			Size:       result.ToolResultRef.Size,
			TokenCount: result.ToolResultRef.TokenCount,
			Content:    result.Content,
		}
	}
	return stored
}

// 编译期断言：routerForkSessions 满足 base SessionPort。
var _ contract.SessionPort = (*routerForkSessions)(nil)

func TestForkChildVisibleWithRealStore(t *testing.T) {
	parent := model.SessionRecord{
		Version: 3,
		ID:      "parent",
		Title:   model.SessionTitle{Value: "Parent", Source: "first_request"},
		Conversation: model.ConversationRecord{Messages: []model.Message{
			{ID: "m1", Role: "user", Content: "hi"},
			{ID: "m2", Role: "assistant", Content: "hello"},
		}},
	}
	events := []sessionstore.Event{
		{Seq: 1, TaskID: "chat-1", Role: "user", Content: "hi", MessageID: "m1"},
		{Seq: 2, TaskID: "chat-1", Role: "assistant", Content: "hello", MessageID: "m2"},
	}
	sessions := newRouterForkSessions(t)
	sessions.saveParentFixture(t, parent, events, []byte(`{"schema_version":1}`))

	engine := newMultiSessionEngine()
	service := newTestService(t, engine,
		withTestSessions(sessions),
	)

	childID, err := service.ForkSessionLatest("parent")
	if err != nil {
		t.Fatalf("ForkSessionLatest: %v", err)
	}
	if got := service.Snapshot().Session.ID; got != childID {
		t.Fatalf("active after fork = %q, want %q", got, childID)
	}
	snapshot, err := service.SnapshotOf(childID)
	if err != nil {
		t.Fatalf("SnapshotOf(child): %v", err)
	}
	if !snapshot.Resident || snapshot.TotalMessages != 2 {
		t.Fatalf("child snapshot = resident:%v total:%d, want resident:true total:2", snapshot.Resident, snapshot.TotalMessages)
	}
	if last := lastMessageOf(snapshot.Conversation); last == nil || last.Role != "assistant" || last.Content != "hello" {
		t.Fatalf("child 最后消息 = %#v, want assistant hello", last)
	}
}
