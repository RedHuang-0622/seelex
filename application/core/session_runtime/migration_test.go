package session_runtime

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/RedHuang-0622/Seele/types"
	"github.com/RedHuang-0622/seelex/application/contract"
	"github.com/RedHuang-0622/seelex/application/model"
	"github.com/RedHuang-0622/seelex/sessionstore"
)

// TestWorkspaceScopedDataReadableThroughSessionGranularStore（迁移测试）：
// 旧 workspace 粒度口写入的物理数据，经会话粒度存储同键读取（Router 复合键
// 保留为物理布局；暴露层切到会话粒度，键不漂移）。
func TestWorkspaceScopedDataReadableThroughSessionGranularStore(t *testing.T) {
	root := t.TempDir()
	router, err := sessionstore.NewRouter(filepath.Join(root, "session-storage.json"), root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = router.Close() })

	const projectID = "project-legacy"
	const sessionID = "sess-legacy"
	router.SetWorkspace(projectID)

	// 旧口写入（workspace 粒度显式键）：history / record / transcript /
	// toolresults / context。
	content := "legacy provider content"
	if err := router.SaveWorkspace(projectID, sessionID, []types.Message{
		{Role: "user", Content: &content},
	}); err != nil {
		t.Fatalf("legacy history write: %v", err)
	}
	legacyHistory := []types.Message{
		{Role: "user", Content: &content},
		{Role: "assistant", Content: &content},
	}
	recordPayload, err := json.Marshal(sessionstore.Record{
		ID:     sessionID,
		Kind:   sessionstore.KindMain,
		Title:  "旧会话",
		Status: sessionstore.StatusIdle,
		Binding: sessionstore.Binding{
			WorkspaceID: projectID,
			Kind:        sessionstore.KindMain,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := router.SaveStateWorkspace(projectID, sessionID, recordPayload); err != nil {
		t.Fatalf("legacy record write: %v", err)
	}
	if err := router.SaveCommitWorkspace(projectID, sessionID, sessionstore.Commit{
		ProviderHistory: legacyHistory,
		Events:          []sessionstore.Event{{Seq: 1, Role: "user", Content: "hi"}},
		ToolResults: []sessionstore.ToolResult{
			{Ref: "legacy:1", Tool: "bash", Content: "out", Size: 3},
		},
	}); err != nil {
		t.Fatalf("legacy commit write: %v", err)
	}
	contextPayload, err := json.Marshal(sessionstore.ContextStack{Task: []string{"t1"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := router.SaveContextStateWorkspace(projectID, sessionID, contextPayload); err != nil {
		t.Fatalf("legacy context write: %v", err)
	}

	// 会话粒度读取（同键不漂移）。
	store := sessionstore.NewSessionGranularStore(router)
	record, ok, err := store.LoadSession(projectID, sessionID)
	if err != nil || !ok {
		t.Fatalf("granular load record ok=%v err=%v", ok, err)
	}
	if record.ID != sessionID || record.Title != "旧会话" || record.Binding.WorkspaceID != projectID {
		t.Fatalf("granular record = %+v", record)
	}

	history, err := store.History(sessionID).Load(context.Background())
	if err != nil || len(history) != 2 || history[0].Role != "user" || history[1].Role != "assistant" {
		t.Fatalf("granular history len=%d err=%v", len(history), err)
	}

	transcript, err := store.Transcript(projectID, sessionID)
	if err != nil || len(transcript) != 1 || transcript[0].Seq != 1 {
		t.Fatalf("granular transcript = %+v err=%v", transcript, err)
	}

	refs, err := store.ToolResults(projectID, sessionID)
	if err != nil || len(refs) != 1 || refs[0].Ref != "legacy:1" {
		t.Fatalf("granular toolresults = %+v err=%v", refs, err)
	}

	stack, err := store.Context(projectID, sessionID)
	if err != nil || len(stack.Task) != 1 || stack.Task[0] != "t1" {
		t.Fatalf("granular context = %+v err=%v", stack, err)
	}

	infos, err := store.SessionsOf(projectID)
	if err != nil || len(infos) != 1 || infos[0].ID != sessionID {
		t.Fatalf("granular index = %+v err=%v", infos, err)
	}
}

// granularPortTestSessions 是同时实现 SessionGranularPort（新口）与
// contract.SessionPort（基面）的测试桩：验证 Coordinator 优先走会话粒度。
type granularPortTestSessions struct {
	*forkTestSessions
	history []contract.EngineMessage
}

func (s *granularPortTestSessions) SessionsOf(string) []model.SessionInfo {
	return []model.SessionInfo{{ID: "sess-g", Name: "granular"}}
}

func (s *granularPortTestSessions) LoadHistory(string) ([]contract.EngineMessage, error) {
	return s.history, nil
}

func (s *granularPortTestSessions) LoadHistoryRange(sessionID string, offset, limit int) ([]contract.EngineMessage, int, error) {
	history := s.history
	if offset > len(history) {
		offset = len(history)
	}
	end := offset + limit
	if limit <= 0 || end > len(history) {
		end = len(history)
	}
	return history[offset:end], len(history), nil
}

func (s *granularPortTestSessions) Delete(string) error { return nil }

// TestLoadSessionHistoryPrefersSessionGranularPort 验证 9.3.2 迁移：
// 装配方提供会话粒度端口时，Coordinator 读历史走会话粒度路径，不再走
// workspace 粒度旧口。
func TestLoadSessionHistoryPrefersSessionGranularPort(t *testing.T) {
	sessions := &granularPortTestSessions{
		forkTestSessions: &forkTestSessions{},
		history: []contract.EngineMessage{
			{Role: "user", ContentSet: true, Content: "granular-path"},
		},
	}
	coordinator := newForkTestCoordinator(t, sessions)

	history, err := coordinator.LoadSessionHistory(Location{}, "sess-g")
	if err != nil {
		t.Fatalf("LoadSessionHistory: %v", err)
	}
	if len(history) != 1 || history[0].Content != "granular-path" {
		t.Fatalf("history = %+v, want granular-path", history)
	}
}
