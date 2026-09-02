package sessionstore

import (
	"fmt"
	"sync"
	"testing"
)

// TestSessionMetaStoreRoundTrip 验证项目级展示元数据 blob：读写、清除、项目隔离，
// 以及最关键的一条 —— 承载 blob 的伪会话键不得出现在会话目录里（不造幽灵会话）。
func TestSessionMetaStoreRoundTrip(t *testing.T) {
	for _, backend := range []Backend{BackendJSON, BackendSQLite} {
		t.Run(string(backend), func(t *testing.T) {
			router := newSessionGranularRouter(t, backend)
			store := NewSessionGranularStore(router)
			meta := NewSessionMetaStore(router)

			if err := store.SaveSession("project-meta", Record{ID: "sess-a", Kind: KindMain, Status: StatusIdle}); err != nil {
				t.Fatalf("save sess-a: %v", err)
			}
			if err := store.SaveSession("", Record{ID: "sess-b", Kind: KindMain, Status: StatusIdle}); err != nil {
				t.Fatalf("save sess-b: %v", err)
			}

			if _, err := meta.Set("project-meta", "sess-a", SessionDisplayMeta{Pinned: true, Alias: "线上排查", SortOrder: 3}); err != nil {
				t.Fatalf("set meta: %v", err)
			}
			// 默认项目（未关联会话）也要能存：键空间按项目分开。
			if _, err := meta.Set("", "sess-b", SessionDisplayMeta{Pinned: true}); err != nil {
				t.Fatalf("set unassociated meta: %v", err)
			}

			metas, err := meta.Meta("project-meta")
			if err != nil {
				t.Fatalf("meta read: %v", err)
			}
			if got := metas["sess-a"]; !got.Pinned || got.Alias != "线上排查" || got.SortOrder != 3 {
				t.Fatalf("sess-a meta = %+v, want pinned/alias/order persisted", got)
			}
			if _, ok := metas["sess-b"]; ok {
				t.Fatal("project blob leaked another project's session meta")
			}

			// 零值即清除条目，而不是留下一条空记录。
			if _, err := meta.Set("project-meta", "sess-a", SessionDisplayMeta{}); err != nil {
				t.Fatalf("clear meta: %v", err)
			}
			if metas, err = meta.Meta("project-meta"); err != nil || len(metas) != 0 {
				t.Fatalf("meta after clear = %+v err=%v, want empty", metas, err)
			}

			// 目录枚举只看有 manifest 的会话目录：伪键不得变成会话。
			listed, err := store.SessionsOf("project-meta")
			if err != nil {
				t.Fatalf("SessionsOf: %v", err)
			}
			for _, info := range listed {
				if info.ID != "sess-a" {
					t.Fatalf("project directory contains unexpected session %q", info.ID)
				}
			}
			// 删除真实会话不影响 blob（两者键空间不同）。
			if err := store.Delete("project-meta", "sess-a"); err != nil {
				t.Fatalf("delete session: %v", err)
			}
			if _, err := meta.Meta("project-meta"); err != nil {
				t.Fatalf("meta read after delete: %v", err)
			}
		})
	}
}

// TestSessionMetaStoreConcurrentSets 并发写同项目不同会话（-race）：读改写必须
// 串行化，否则后一个写会覆盖前一个的条目。
func TestSessionMetaStoreConcurrentSets(t *testing.T) {
	router := newSessionGranularRouter(t, BackendJSON)
	meta := NewSessionMetaStore(router)
	const writers = 8
	var group sync.WaitGroup
	for index := 0; index < writers; index++ {
		group.Add(1)
		go func(index int) {
			defer group.Done()
			sessionID := fmt.Sprintf("sess-%d", index)
			if _, err := meta.Set("project-concurrent", sessionID, SessionDisplayMeta{SortOrder: index + 1}); err != nil {
				t.Errorf("set %s: %v", sessionID, err)
			}
		}(index)
	}
	group.Wait()

	metas, err := meta.Meta("project-concurrent")
	if err != nil {
		t.Fatalf("meta read: %v", err)
	}
	if len(metas) != writers {
		t.Fatalf("stored entries = %d, want %d（并发读改写丢失更新）", len(metas), writers)
	}
}
