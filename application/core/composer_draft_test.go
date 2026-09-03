package core

import (
	"context"
	"sync"
	"testing"

	"github.com/RedHuang-0622/seelex/application/core/session_runtime"
	"github.com/RedHuang-0622/seelex/application/model"
)

// draftRecordStore 模拟生产 SessionPort：目录行 + record 读写，供 composer
// 持久化与重启恢复用例使用。
type draftRecordStore struct {
	mu        sync.Mutex
	records   map[string]model.SessionRecord
	catalog   []SessionInfo
	workspace string
}

func newDraftRecordStore() *draftRecordStore {
	return &draftRecordStore{records: map[string]model.SessionRecord{}}
}

func (store *draftRecordStore) SaveCurrent(string) error                    { return nil }
func (store *draftRecordStore) Delete(id string) error                      { return nil }
func (store *draftRecordStore) SetWorkspace(workspaceID string)             { store.workspace = workspaceID }
func (store *draftRecordStore) Workspace() string                           { return store.workspace }
func (store *draftRecordStore) LoadHistory(string) ([]EngineMessage, error) { return nil, nil }
func (store *draftRecordStore) LoadHistoryRange(string, int, int) ([]EngineMessage, int, error) {
	return nil, 0, nil
}
func (store *draftRecordStore) SaveSessionRecord(sessionID string, record model.SessionRecord) error {
	return store.SaveSessionRecordWorkspace("", sessionID, record)
}
func (store *draftRecordStore) LoadSessionRecord(sessionID string) (model.SessionRecord, error) {
	return store.LoadSessionRecordWorkspace("", sessionID)
}

func (store *draftRecordStore) List() []SessionInfo {
	store.mu.Lock()
	defer store.mu.Unlock()
	out := make([]SessionInfo, 0, len(store.catalog))
	for _, item := range store.catalog {
		item.UpdatedAt = item.UpdatedAt
		out = append(out, item)
	}
	return out
}

func (store *draftRecordStore) LoadSessionRecordWorkspace(_ string, sessionID string) (model.SessionRecord, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	return store.records[sessionID], nil
}

func (store *draftRecordStore) SaveSessionRecordWorkspace(_ string, sessionID string, record model.SessionRecord) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.records[sessionID] = record
	found := false
	for index := range store.catalog {
		if store.catalog[index].ID == sessionID {
			store.catalog[index].Status = SessionStatus(record.Status)
			store.catalog[index].UpdatedAt = record.UpdatedAt
			found = true
			break
		}
	}
	if !found {
		store.catalog = append(store.catalog, SessionInfo{
			ID: sessionID, Name: "新会话", Status: SessionStatus(record.Status), UpdatedAt: record.UpdatedAt,
		})
	}
	return nil
}

func mustDraftService(t *testing.T, store *draftRecordStore) *Service {
	t.Helper()
	service := mustNew(t, Dependencies{
		Engine: &fakeEngine{lazyStart: true}, Runtime: &fakeRuntime{},
		Plugins: &fakePlugins{current: PluginInfo{Name: "default"}},
		Skills:  fakeSkills{}, Sessions: store,
	})
	return service
}

// TestComposerDraftPersistsAndRestoresAcrossRestart（G4 先行第 2 片）：草稿
// 早分配 SID；SaveComposerDraft 落盘（Status=draft + Composer）；重建 Service
// 后装配器恢复同一草稿（同 ID + composer）；物化提交后 composer 清空。
func TestComposerDraftPersistsAndRestoresAcrossRestart(t *testing.T) {
	store := newDraftRecordStore()
	first := mustDraftService(t, store)

	if err := first.SaveComposerDraft("尚未发送的问题"); err != nil {
		t.Fatal(err)
	}
	snapshot := first.Snapshot()
	if !snapshot.Session.Draft || snapshot.Session.ID == "" || snapshot.Session.Composer != "尚未发送的问题" {
		t.Fatalf("draft snapshot after save = %+v", snapshot.Session)
	}
	draftID := snapshot.Session.ID
	store.mu.Lock()
	record := store.records[draftID]
	store.mu.Unlock()
	if record.Status != SessionStatusDraft || record.Composer.Text != "尚未发送的问题" || record.Version != session_runtime.SessionRecordVersion {
		t.Fatalf("persisted draft record = %+v", record)
	}
	first.Shutdown()

	// 重启：装配器恢复持久化的草稿（同一 SID + composer）。
	second := mustDraftService(t, store)
	defer second.Shutdown()
	restored := second.Snapshot()
	if !restored.Session.Draft || restored.Session.ID != draftID || restored.Session.Composer != "尚未发送的问题" {
		t.Fatalf("restored draft snapshot = %+v", restored.Session)
	}

	// 提交物化：复用草稿 SID，composer 清空且 record 不再标记 draft。
	if err := second.Submit(context.Background(), "尚未发送的问题"); err != nil {
		t.Fatal(err)
	}
	if err := second.WaitForIdle(context.Background()); err != nil {
		t.Fatal(err)
	}
	final := second.Snapshot()
	if final.Session.Draft || final.Session.ID != draftID || final.Session.Composer != "" {
		t.Fatalf("materialized snapshot = %+v", final.Session)
	}
	store.mu.Lock()
	finalRecord := store.records[draftID]
	store.mu.Unlock()
	if finalRecord.Status == SessionStatusDraft || finalRecord.Composer.Text != "" {
		t.Fatalf("composer not cleared after materialize: %+v", finalRecord)
	}
}
