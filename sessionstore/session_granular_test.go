package sessionstore

import (
	"context"
	"encoding/json"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

// newSessionGranularRouter 按后端构造 Router（JSON 默认；SQLite 经
// Configure 切换，验证多后端同契约）。
func newSessionGranularRouter(t *testing.T, backend Backend) *Router {
	t.Helper()
	router := newTestRouter(t)
	if backend != BackendJSON {
		if err := router.Configure(context.Background(), Config{
			Backend: BackendSQLite,
			Path:    filepath.Join(t.TempDir(), "sessions.db"),
		}); err != nil {
			t.Fatal(err)
		}
	}
	return router
}

// TestSessionGranularityPersistence（T2.6）：session:<id> 五片原子读写、
// 项目索引、subagent 与 main 同构落盘。
func TestSessionGranularityPersistence(t *testing.T) {
	for _, backend := range []Backend{BackendJSON, BackendSQLite} {
		t.Run(string(backend), func(t *testing.T) {
			router := newSessionGranularRouter(t, backend)
			store := NewSessionGranularStore(router)
			const projectID = "project-a"
			const mainID = "sess-main"
			router.SetWorkspace(projectID)

			// record 片：会话粒度键原子读写。
			record := Record{
				ID:     mainID,
				Kind:   KindMain,
				Title:  "主会话",
				Status: StatusIdle,
				Binding: Binding{
					WorkspaceID: projectID,
					Kind:        KindMain,
				},
			}
			if err := store.SaveSession(projectID, record); err != nil {
				t.Fatalf("save record: %v", err)
			}
			loaded, ok, err := store.LoadSession(projectID, mainID)
			if err != nil || !ok {
				t.Fatalf("load record ok=%v err=%v", ok, err)
			}
			if loaded.ID != mainID || loaded.Kind != KindMain || loaded.Title != "主会话" ||
				loaded.Status != StatusIdle || loaded.Binding.WorkspaceID != projectID {
				t.Fatalf("loaded record = %+v", loaded)
			}

			// history 片：框架工作历史句柄。
			history := store.History(mainID)
			if err := history.Save(context.Background(), messages(2, "hist")); err != nil {
				t.Fatalf("save history: %v", err)
			}
			loadedHistory, err := history.Load(context.Background())
			if err != nil || len(loadedHistory) != 2 {
				t.Fatalf("load history len=%d err=%v", len(loadedHistory), err)
			}

			// transcript 片：事件日志按会话读取。
			if err := router.SaveCommitWorkspace(projectID, mainID, Commit{
				Events: []Event{{Seq: 1, Role: "user", Content: "hi"}, {Seq: 2, Role: "assistant", Content: "answer"}},
			}); err != nil {
				t.Fatalf("save transcript: %v", err)
			}
			transcript, err := store.Transcript(projectID, mainID)
			if err != nil || len(transcript) != 2 {
				t.Fatalf("transcript len=%d err=%v", len(transcript), err)
			}
			if transcript[0].Seq != 1 || transcript[1].Type != "assistant" {
				t.Fatalf("transcript = %+v", transcript)
			}

			// toolresults 片：工具结果归档引用。
			if err := router.SaveCommitWorkspace(projectID, mainID, Commit{
				ToolResults: []ToolResult{{Ref: "r1", Tool: "bash", Size: 3}},
			}); err != nil {
				t.Fatalf("save toolresults: %v", err)
			}
			refs, err := store.ToolResults(projectID, mainID)
			if err != nil || len(refs) != 1 || refs[0].Ref != "r1" || refs[0].Name != "bash" || refs[0].Size != 3 {
				t.Fatalf("toolresults = %+v err=%v", refs, err)
			}

			// context 片：上下文四栈独立通道。
			contextPayload, err := json.Marshal(ContextStack{Plan: []string{"p1"}, Task: []string{"t1"}})
			if err != nil {
				t.Fatal(err)
			}
			if err := router.SaveContextStateWorkspace(projectID, mainID, contextPayload); err != nil {
				t.Fatalf("save context: %v", err)
			}
			stack, err := store.Context(projectID, mainID)
			if err != nil || len(stack.Plan) != 1 || stack.Plan[0] != "p1" || len(stack.Task) != 1 {
				t.Fatalf("context = %+v err=%v", stack, err)
			}

			// subagent 与 main 同构落盘（parent 关联）。
			subID := "sess-sub"
			subRecord := Record{
				ID:       subID,
				Kind:     KindSubagent,
				ParentID: mainID,
				Status:   StatusRunning,
				Binding: Binding{
					WorkspaceID: projectID,
					ParentID:    mainID,
					Kind:        KindSubagent,
				},
			}
			if err := store.SaveSession(projectID, subRecord); err != nil {
				t.Fatalf("save subagent record: %v", err)
			}
			loadedSub, ok, err := store.LoadSession(projectID, subID)
			if err != nil || !ok || loadedSub.Kind != KindSubagent || loadedSub.ParentID != mainID {
				t.Fatalf("load subagent ok=%v err=%v record=%+v", ok, err, loadedSub)
			}
			if err := store.History(subID).Save(context.Background(), messages(1, "sub")); err != nil {
				t.Fatalf("save subagent history: %v", err)
			}

			// 项目索引：project = 会话集合，main + subagent 都出现。
			infos, err := store.SessionsOf(projectID)
			if err != nil {
				t.Fatalf("sessions of project: %v", err)
			}
			byID := make(map[string]SessionInfo, len(infos))
			for _, info := range infos {
				byID[info.ID] = info
			}
			if len(byID) != 2 {
				t.Fatalf("project index = %+v, want 2 sessions", byID)
			}
			if byID[mainID].Kind != KindMain || byID[subID].Kind != KindSubagent || byID[subID].ParentID != mainID {
				t.Fatalf("project index kinds/parent = %+v", byID)
			}
		})
	}
}

// TestPersistenceIdempotent（B6）：会话粒度重复写/恢复幂等，键不漂移。
func TestPersistenceIdempotent(t *testing.T) {
	for _, backend := range []Backend{BackendJSON, BackendSQLite} {
		t.Run(string(backend), func(t *testing.T) {
			router := newSessionGranularRouter(t, backend)
			store := NewSessionGranularStore(router)
			const projectID = "project-b"
			const sessionID = "sess-idem"
			router.SetWorkspace(projectID)

			record := Record{
				ID:        sessionID,
				Kind:      KindMain,
				Title:     "幂等",
				Status:    StatusIdle,
				UpdatedAt: time.Unix(100, 0),
				Binding:   Binding{WorkspaceID: projectID, Kind: KindMain},
			}
			// 同键重复写：内容不变、索引不膨胀。
			for round := 0; round < 3; round++ {
				if err := store.SaveSession(projectID, record); err != nil {
					t.Fatalf("round %d save: %v", round, err)
				}
			}
			loaded, ok, err := store.LoadSession(projectID, sessionID)
			if err != nil || !ok {
				t.Fatalf("load ok=%v err=%v", ok, err)
			}
			if !reflect.DeepEqual(record.ID, loaded.ID) || loaded.Title != "幂等" ||
				loaded.Kind != KindMain || loaded.Binding.WorkspaceID != projectID {
				t.Fatalf("idempotent record drifted: %+v", loaded)
			}

			// 绑定重复写幂等。
			for round := 0; round < 2; round++ {
				if err := store.Bind(projectID, sessionID, Binding{
					WorkspaceID: projectID,
					Kind:        KindMain,
				}); err != nil {
					t.Fatalf("round %d bind: %v", round, err)
				}
			}
			bound, ok, err := store.LoadSession(projectID, sessionID)
			if err != nil || !ok || bound.Binding.WorkspaceID != projectID || bound.Binding.Kind != KindMain {
				t.Fatalf("bound record = %+v ok=%v err=%v", bound, ok, err)
			}

			// context 重复写幂等。
			payload, _ := json.Marshal(ContextStack{Task: []string{"t1"}})
			for round := 0; round < 2; round++ {
				if err := router.SaveContextStateWorkspace(projectID, sessionID, payload); err != nil {
					t.Fatalf("round %d context: %v", round, err)
				}
			}
			stack, err := store.Context(projectID, sessionID)
			if err != nil || len(stack.Task) != 1 || stack.Task[0] != "t1" {
				t.Fatalf("context = %+v err=%v", stack, err)
			}

			// 索引稳定：重复写后项目列表仍只有该会话。
			infos, err := store.SessionsOf(projectID)
			if err != nil || len(infos) != 1 || infos[0].ID != sessionID {
				t.Fatalf("index after idempotent writes = %+v err=%v", infos, err)
			}
		})
	}
}

// TestSessionsOfToleratesProductionRecordSchema（左侧栏历史会话回归）：
// 生产 state.json 是 application model.SessionRecord（title 为 SessionTitle
// 对象，含 conversation/execution 等字段），与薄封装 Record 不同构。目录
// 枚举必须宽容解析，绝不因类型不匹配清空侧栏。
func TestSessionsOfToleratesProductionRecordSchema(t *testing.T) {
	router := newTestRouter(t)
	store := NewSessionGranularStore(router)
	const projectID = "project-prod"
	const sessionID = "sess-prod"

	// 生产形态记录：title 是对象，额外字段不透明。
	payload := []byte(`{
		"version": 3,
		"id": "sess-prod",
		"title": {"value": "生产会话标题", "source": "first_request"},
		"conversation": {"messages": [{"id":"m1","role":"user","content":"hi"}]},
		"execution": {"read_files": []},
		"updated_at": "2026-09-01T00:00:00Z"
	}`)
	// 先写 commit 生成项目索引条目，再写生产形态 record。
	if err := router.SaveCommitWorkspace(projectID, sessionID, Commit{
		ProviderHistory: messages(1, "prod"),
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveRecordRaw(projectID, sessionID, payload); err != nil {
		t.Fatal(err)
	}

	// 目录必须能列出该会话，且标题从对象 value 提取。
	infos, err := store.SessionsOf(projectID)
	if err != nil {
		t.Fatalf("SessionsOf must not fail on production schema: %v", err)
	}
	if len(infos) != 1 || infos[0].ID != sessionID {
		t.Fatalf("catalog = %+v, want sess-prod（生产 schema 不得清空侧栏）", infos)
	}
	if infos[0].Title != "生产会话标题" {
		t.Fatalf("title = %q, want 生产会话标题（对象 value 提取）", infos[0].Title)
	}

	// LoadSession 宽容：不报错，身份/标题可读。
	record, ok, err := store.LoadSession(projectID, sessionID)
	if err != nil || !ok {
		t.Fatalf("LoadSession(production schema) ok=%v err=%v", ok, err)
	}
	if record.ID != sessionID || record.Title != "生产会话标题" {
		t.Fatalf("record = %+v", record)
	}
}
