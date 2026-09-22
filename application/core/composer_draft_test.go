package core

import (
	"context"
	"sync"
	"testing"
	"time"

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

// TestComposerDraftSessionRowVisible（回归，修复前 RED）：本会话有未发送草稿
// 时，目录行必须标成 draft。目录行的草稿身份只由草稿槽位判定
// （service_snapshot.go：按槽位补行、槽位行恒为 draft，其余行按单元运行态
// 叠加），而"启动即草稿"的早分配 SID 没有槽位——record 已以 Status=draft 落盘、
// 输入框里也有未发送正文，会话树里那一行却被叠成 idle：草稿会话在页面上看不见。
func TestComposerDraftSessionRowVisible(t *testing.T) {
	store := newDraftRecordStore()
	service := mustDraftService(t, store)
	defer service.Shutdown()
	if err := service.SaveComposerDraft("未发送正文"); err != nil {
		t.Fatal(err)
	}
	draftID := service.Snapshot().Session.ID
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := service.WaitCatalogRefresh(ctx); err != nil {
		t.Fatalf("catalog settle: %v", err)
	}
	var row *SessionInfo
	for index := range service.Snapshot().Sessions {
		if service.Snapshot().Sessions[index].ID == draftID {
			item := service.Snapshot().Sessions[index]
			row = &item
		}
	}
	if row == nil {
		t.Fatalf("draft session %q missing from directory rows %+v", draftID, service.Snapshot().Sessions)
	}
	if row.Status != SessionStatusDraft {
		t.Fatalf("draft row status = %q, want draft (row=%+v)", row.Status, *row)
	}
}

// TestComposerWorkspaceRebindConvergesOnMaterialize（回归，修复前 RED）：草稿
// 落盘的项目键会随"草稿态改绑工作区"漂移（前端「在该工作区新建会话」先在草稿上
// BindWorkspace 一次，BeginNewSession 幂等，见 app.js bindWorkspaceAndStart）。
// 物化只清"物化时的项目键"就会把旧键下那份 Status=draft 的 record 留成幽灵：
// 重启后 DraftCandidates 仍把它当草稿候选，restorePersistedDraft 于是把这份
// **已物化**会话当草稿槽恢复回页面（草稿正文与已落盘消息混在一页、既定消息在
// 会话树里反而看不见）。清空必须按残留（权威）状态收敛到所有项目键。
func TestComposerWorkspaceRebindConvergesOnMaterialize(t *testing.T) {
	store := newWorkspaceDraftStore()
	workspace := newFakeWorkspace()
	service := newWorkspaceDraftService(t, store, workspace)
	defer service.Shutdown()
	if _, err := workspace.Create("p1", "C:\\p1", ""); err != nil {
		t.Fatal(err)
	}
	// fakeWorkspace.Create 是单工作区桩（恒 project-1）：第二个工作区直接落桩表。
	workspace.mu.Lock()
	workspace.items["project-2"] = WorkspaceInfo{ID: "project-2", Name: "p2", RootPath: "C:\\p2"}
	workspace.mu.Unlock()
	if err := service.BindWorkspace("project-1"); err != nil {
		t.Fatal(err)
	}
	if err := service.SaveComposerDraft("改绑前写下的未发送正文"); err != nil {
		t.Fatal(err)
	}
	draftID := service.Snapshot().Session.ID
	if err := service.BindWorkspace("project-2"); err != nil {
		t.Fatal(err)
	}
	if err := service.Submit(context.Background(), "改绑前写下的未发送正文"); err != nil {
		t.Fatal(err)
	}
	if err := service.WaitForIdle(context.Background()); err != nil {
		t.Fatal(err)
	}
	store.mu.Lock()
	residue := map[string]model.SessionRecord{}
	for projectID, bySession := range store.records {
		if record, ok := bySession[draftID]; ok && record.Status == SessionStatusDraft {
			residue[projectID] = record
		}
	}
	store.mu.Unlock()
	if len(residue) > 0 {
		t.Fatalf("draft residue after materialize: %+v", residue)
	}
	for _, candidate := range service.components.sessions.DraftCandidates() {
		if candidate.ID == draftID {
			t.Fatalf("materialized session %q still a draft candidate", draftID)
		}
	}
}
