package sessionstore

import (
	"encoding/json"
	"testing"
)

// TestEnsureIndexedMakesRecordOnlySessionEnumerable G：record-only 草稿会话
// 先 EnsureIndexed（空 commit 建 manifest/meta），再写 state 通道后即可被
// SessionsOf 枚举；重复调用幂等（不重复建 commit）。
func TestEnsureIndexedMakesRecordOnlySessionEnumerable(t *testing.T) {
	for _, backend := range []Backend{BackendJSON} {
		t.Run(string(backend), func(t *testing.T) {
			router := newSessionGranularRouter(t, backend)
			store := NewSessionGranularStore(router)
			const projectID = "project-g"
			const sessionID = "sess-draft"

			if err := store.EnsureIndexed(projectID, sessionID); err != nil {
				t.Fatalf("EnsureIndexed: %v", err)
			}
			if err := store.EnsureIndexed(projectID, sessionID); err != nil {
				t.Fatalf("EnsureIndexed (idempotent): %v", err)
			}
			record := Record{
				ID: sessionID, Kind: KindMain, Title: "工作区草稿",
				Status: StatusDraft,
				Binding: Binding{
					WorkspaceID: projectID,
					Kind:        KindMain,
				},
			}
			payload, err := json.Marshal(record)
			if err != nil {
				t.Fatal(err)
			}
			if err := store.SaveRecordRaw(projectID, sessionID, payload); err != nil {
				t.Fatalf("SaveRecordRaw: %v", err)
			}
			infos, err := store.SessionsOf(projectID)
			if err != nil {
				t.Fatalf("SessionsOf: %v", err)
			}
			found := false
			for _, info := range infos {
				if info.ID == sessionID {
					found = true
					wantStatus := StatusDraft
					if backend == BackendJSON {
						// S20：草稿行判据 = input/draft.json 存在；仅写 record
						// 不再产生 draft 状态。
						wantStatus = StatusIdle
					}
					if info.Status != wantStatus {
						t.Fatalf("enumerated status = %q, want draft", info.Status)
					}
				}
			}
			if !found {
				t.Fatalf("record-only draft missing from project enumeration: %+v", infos)
			}
		})
	}
}
