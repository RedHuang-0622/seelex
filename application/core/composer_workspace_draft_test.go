package core

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/RedHuang-0622/seelex/application/model"
)

// workspaceDraftStore 是工作区草稿 binding 落盘的测试桩：per-project record
// + 目录行 + 项目索引确保口。SaveCurrent/历史为 no-op（本用例只走 composer
// record 通道与目录枚举）。
type workspaceDraftStore struct {
	mu        sync.Mutex
	records   map[string]map[string]model.SessionRecord // projectID → sessionID → record
	workspace string
}

func newWorkspaceDraftStore() *workspaceDraftStore {
	return &workspaceDraftStore{records: map[string]map[string]model.SessionRecord{}}
}

func (store *workspaceDraftStore) SaveCurrent(string) error                    { return nil }
func (store *workspaceDraftStore) Delete(string) error                         { return nil }
func (store *workspaceDraftStore) SetWorkspace(workspaceID string)             { store.workspace = workspaceID }
func (store *workspaceDraftStore) Workspace() string                           { return store.workspace }
func (store *workspaceDraftStore) LoadHistory(string) ([]EngineMessage, error) { return nil, nil }
func (store *workspaceDraftStore) LoadHistoryRange(string, int, int) ([]EngineMessage, int, error) {
	return nil, 0, nil
}
func (store *workspaceDraftStore) List() []SessionInfo {
	store.mu.Lock()
	defer store.mu.Unlock()
	var out []SessionInfo
	for projectID, bySession := range store.records {
		for sessionID, record := range bySession {
			out = append(out, SessionInfo{
				ID: sessionID, UpdatedAt: record.UpdatedAt, Status: SessionStatus(record.Status),
			})
			_ = projectID
		}
	}
	return out
}
func (store *workspaceDraftStore) SessionsOf(projectID string) []SessionInfo {
	store.mu.Lock()
	defer store.mu.Unlock()
	var out []SessionInfo
	for sessionID, record := range store.records[projectID] {
		out = append(out, SessionInfo{
			ID: sessionID, UpdatedAt: record.UpdatedAt, Status: SessionStatus(record.Status),
		})
	}
	return out
}

func (store *workspaceDraftStore) SaveSessionRecord(sessionID string, record model.SessionRecord) error {
	return store.SaveSessionRecordWorkspace("", sessionID, record)
}
func (store *workspaceDraftStore) LoadSessionRecord(sessionID string) (model.SessionRecord, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	for _, bySession := range store.records {
		if record, ok := bySession[sessionID]; ok {
			return record, nil
		}
	}
	return model.SessionRecord{}, errors.New("record not found")
}
func (store *workspaceDraftStore) SaveSessionRecordWorkspace(projectID, sessionID string, record model.SessionRecord) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.records[projectID] == nil {
		store.records[projectID] = map[string]model.SessionRecord{}
	}
	store.records[projectID][sessionID] = record
	return nil
}
func (store *workspaceDraftStore) LoadSessionRecordWorkspace(projectID, sessionID string) (model.SessionRecord, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	record, ok := store.records[projectID][sessionID]
	if !ok {
		return model.SessionRecord{}, errors.New("record not found")
	}
	return record, nil
}
func (store *workspaceDraftStore) EnsureSessionIndexed(string, string) error { return nil }

func (store *workspaceDraftStore) recordAt(projectID, sessionID string) (model.SessionRecord, bool) {
	store.mu.Lock()
	defer store.mu.Unlock()
	record, ok := store.records[projectID][sessionID]
	return record, ok
}

func newWorkspaceDraftService(t *testing.T, store *workspaceDraftStore, workspace *fakeWorkspace) *Service {
	t.Helper()
	service := mustNew(t, Dependencies{
		Engine:    &fakeEngine{lazyStart: true},
		Runtime:   &fakeRuntime{},
		Plugins:   &fakePlugins{current: PluginInfo{Name: "default"}},
		Skills:    fakeSkills{},
		Sessions:  store,
		Workspace: workspace,
	})
	return service
}

// TestComposerWorkspaceDraftBindingPersistsAcrossRestart G：工作区草稿在
// BindWorkspace 后按绑定项目写 record（不再落默认项目）；冷启动装配器跨项目
// 找回同一 SID + composer + 项目绑定；物化复用同一 SID 与项目。
func TestComposerWorkspaceDraftBindingPersistsAcrossRestart(t *testing.T) {
	store := newWorkspaceDraftStore()
	workspace := newFakeWorkspace()
	first := newWorkspaceDraftService(t, store, workspace)
	if _, err := workspace.Create("proj", "C:\\proj", ""); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if err := first.BindWorkspace("project-1"); err != nil {
		t.Fatalf("BindWorkspace: %v", err)
	}
	if err := first.SaveComposerDraft("绑定工作区的未发送问题"); err != nil {
		t.Fatal(err)
	}
	draftID := first.Snapshot().Session.ID
	if draftID == "" {
		t.Fatal("draft session has no id")
	}
	record, ok := store.recordAt("project-1", draftID)
	if !ok || record.Status != SessionStatusDraft || record.Composer.Text != "绑定工作区的未发送问题" {
		t.Fatalf("bound draft record = %+v ok=%v, want project-1 record with composer", record, ok)
	}
	if _, leaked := store.recordAt("", draftID); leaked {
		t.Fatal("bound draft leaked into default project")
	}
	first.Shutdown()

	// 重启：同一存储 + 同一 workspace 仓库。
	second := newWorkspaceDraftService(t, store, workspace)
	defer second.Shutdown()
	restored := second.Snapshot()
	if !restored.Session.Draft || restored.Session.ID != draftID || restored.Session.Composer != "绑定工作区的未发送问题" {
		t.Fatalf("restored bound draft = %+v", restored.Session)
	}
	if restored.CurrentWorkspace == nil || restored.CurrentWorkspace.ID != "project-1" {
		t.Fatalf("restored bound draft lost workspace: %+v", restored.CurrentWorkspace)
	}
	second.ViewMu.RLock()
	draftWorkspaceID := ""
	if second.draft != nil && second.draft.Workspace != nil {
		draftWorkspaceID = second.draft.Workspace.ID
	}
	second.ViewMu.RUnlock()
	if draftWorkspaceID != "project-1" {
		t.Fatalf("restored draft slot workspace = %q, want project-1", draftWorkspaceID)
	}

	// 物化：同一 SID，项目归属不变，composer 清空。
	if err := second.Submit(context.Background(), "绑定工作区的未发送问题"); err != nil {
		t.Fatal(err)
	}
	if err := second.WaitForIdle(context.Background()); err != nil {
		t.Fatal(err)
	}
	final := second.Snapshot()
	if final.Session.Draft || final.Session.ID != draftID || final.Session.Composer != "" {
		t.Fatalf("materialized bound draft = %+v", final.Session)
	}
	if final.CurrentWorkspace == nil || final.CurrentWorkspace.ID != "project-1" {
		t.Fatalf("materialized session lost workspace: %+v", final.CurrentWorkspace)
	}
	finalRecord, ok := store.recordAt("project-1", draftID)
	if !ok || finalRecord.Status == SessionStatusDraft || finalRecord.Composer.Text != "" {
		t.Fatalf("final bound record = %+v ok=%v, want cleared composer", finalRecord, ok)
	}
}
