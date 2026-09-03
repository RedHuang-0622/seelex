package sessionstore

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

// TestArchiveStatusRewriteKeepsFivePieces 钉住 C2 的存储契约：归档只重写
// record 状态通道（Status=archived），history/transcript/tool-results/
// context 四片与项目索引原样保留，重开（按 ID 读回）不丢任何数据；枚举仍
// 返回归档行（常规目录的过滤在应用层分格枚举处，不在存储层）。
func TestArchiveStatusRewriteKeepsFivePieces(t *testing.T) {
	for _, backend := range []Backend{BackendJSON, BackendSQLite} {
		t.Run(string(backend), func(t *testing.T) {
			router := newSessionGranularRouter(t, backend)
			store := NewSessionGranularStore(router)
			ctx := context.Background()
			const projectID = "project-a"
			const sessionID = "sess-archive"

			record := Record{
				ID:     sessionID,
				Kind:   KindMain,
				Title:  "归档前标题",
				Status: StatusIdle,
				Binding: Binding{
					WorkspaceID: projectID,
					Kind:        KindMain,
				},
				UpdatedAt: time.Unix(1, 0),
			}
			if err := store.SaveSession(projectID, record); err != nil {
				t.Fatalf("save record: %v", err)
			}

			if err := router.SaveCommitWorkspace(projectID, sessionID, Commit{
				Events: []Event{
					{Seq: 1, Role: "user", Content: "hi"},
					{Seq: 2, Role: "assistant", Content: "answer"},
				},
				ProviderHistory: messages(2, "hist"),
				ToolResults:     []ToolResult{{Ref: "r1", Tool: "bash", Size: 3}},
			}); err != nil {
				t.Fatalf("save snapshot commit: %v", err)
			}
			contextPayload := []byte(`{"plan":["plan-a"]}`)
			if err := store.SaveContext(projectID, sessionID, contextPayload); err != nil {
				t.Fatalf("save context: %v", err)
			}

			// 归档 = 只重写 record 通道状态（薄封装 Record 与生产
			// model.SessionRecord 同通道宽容解析）。
			archived := record
			archived.Status = StatusArchived
			archived.UpdatedAt = time.Now()
			payload, err := json.Marshal(archived)
			if err != nil {
				t.Fatalf("marshal archived record: %v", err)
			}
			if err := store.SaveRecordRaw(projectID, sessionID, payload); err != nil {
				t.Fatalf("archive record rewrite: %v", err)
			}

			// 枚举仍含归档行（过滤在应用层），状态为 archived。
			infos, err := store.SessionsOf(projectID)
			if err != nil {
				t.Fatalf("SessionsOf: %v", err)
			}
			found := false
			for _, info := range infos {
				if info.ID == sessionID {
					found = true
					if info.Status != StatusArchived {
						t.Fatalf("enumerated status = %q, want archived", info.Status)
					}
				}
			}
			if !found {
				t.Fatal("archived session missing from project enumeration")
			}

			// 重开五片读回。
			loaded, ok, err := store.LoadSession(projectID, sessionID)
			if err != nil || !ok {
				t.Fatalf("reopen record ok=%v err=%v", ok, err)
			}
			if loaded.Status != StatusArchived || loaded.Title != "归档前标题" {
				t.Fatalf("reopened record = %+v", loaded)
			}
			loadedHistory, err := store.HistoryForProject(projectID, sessionID).Load(ctx)
			if err != nil || len(loadedHistory) != 2 {
				t.Fatalf("reopen history len=%d err=%v", len(loadedHistory), err)
			}
			transcript, err := store.Transcript(projectID, sessionID)
			if err != nil || len(transcript) != 2 {
				t.Fatalf("reopen transcript len=%d err=%v", len(transcript), err)
			}
			results, err := store.ToolResults(projectID, sessionID)
			if err != nil || len(results) != 1 || results[0].Ref != "r1" {
				t.Fatalf("reopen toolresults = %+v err=%v", results, err)
			}
			stack, err := store.Context(projectID, sessionID)
			if err != nil || len(stack.Plan) != 1 || stack.Plan[0] != "plan-a" {
				t.Fatalf("reopen context = %+v err=%v", stack, err)
			}
		})
	}
}
