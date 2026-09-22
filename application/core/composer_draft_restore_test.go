package core

import (
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
	"github.com/RedHuang-0622/seelex/application/core/session_runtime"
	"github.com/RedHuang-0622/seelex/application/model"
)

// 装配期草稿恢复的四条判据（回归，修复前 RED）：
//
//  1. 引擎**已被宿主预置成一条空会话/空壳**时，持久化的非空草稿仍要装回视图：
//     装配器此前只按 `Engine.SessionID() == ""` 认定冷启动，这条路径直接跳过
//     草稿恢复（devlog 2026-09-22-draft-page-wiring-and-smoke.md §3.4 的"未如约"）。
//     该补装放在目录收敛后的后台判断里（装配期不读会话目录，
//     见 TestSnapshotDoesNotReadBlockedSessionCatalog）。
//  2. 引擎带着**有内容**的历史时，那是用户正在看的会话——草稿不许抢视图，只登记
//     草稿槽位（目录行恒 draft）+ 把落盘正文回读进该会话单元。
//  3. 恢复出来的视图会话自己的 task 台账（工作表格内容随 record 落盘）要装回
//     运行时注册表，工作表格投影里因此看得见这一系列条目。
//  4. 草稿正文活在与消息通道分离的 lifecycle 草稿通道里（v8 已退役 state/record
//     通道，record 里的 Composer 会被丢弃）：正文必须能从该通道读回、物化后必须
//     真的清掉（草稿行判据 = 这份草稿文件的存在性）。
const draftRestoreText = "重启后要看见的未发送正文"

func mustDraftServiceWithEngine(t *testing.T, store SessionPort, engine *fakeEngine) *Service {
	t.Helper()
	return mustNew(t, Dependencies{
		Engine: engine, Runtime: &fakeRuntime{},
		Plugins: &fakePlugins{current: PluginInfo{Name: "default"}},
		Skills:  fakeSkills{}, Sessions: store,
	})
}

func draftRowOf(snapshot Snapshot, sessionID string) (SessionInfo, bool) {
	for _, row := range snapshot.Sessions {
		if row.ID == sessionID {
			return row, true
		}
	}
	return SessionInfo{}, false
}

func waitForCondition(t *testing.T, what string, ready func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if ready() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// TestComposerDraftRestoresOntoPrecreatedEmptyEngineSession 钉住判据 1。
func TestComposerDraftRestoresOntoPrecreatedEmptyEngineSession(t *testing.T) {
	store := newDraftRecordStore()
	first := mustDraftService(t, store)
	if err := first.SaveComposerDraft(draftRestoreText); err != nil {
		t.Fatal(err)
	}
	draftID := first.Snapshot().Session.ID
	if draftID == "" {
		t.Fatal("draft session ID must be pre-assigned")
	}
	first.Shutdown()

	// 重启：宿主在装配前把引擎预置成一条空会话（没有任何内容）——视图仍是空壳，
	// 持久化的非空草稿要在目录收敛后补装回视图（draft + composer 都能看见）。
	second := mustDraftServiceWithEngine(t, store, &fakeEngine{sessionID: "sess_host_precreated"})
	defer second.Shutdown()

	waitForCondition(t, "draft takeover of the precreated shell view", func() bool {
		return second.Snapshot().Session.Draft && second.Snapshot().Session.ID == draftID
	})
	restored := second.Snapshot()
	if restored.Session.Composer != draftRestoreText {
		t.Fatalf("restored draft composer = %q, want %q", restored.Session.Composer, draftRestoreText)
	}
	row, ok := draftRowOf(restored, draftID)
	if !ok {
		t.Fatalf("restored draft %q missing from directory rows %+v", draftID, restored.Sessions)
	}
	if row.Status != SessionStatusDraft {
		t.Fatalf("restored draft row status = %q, want draft", row.Status)
	}
}

// TestComposerDraftKeepsViewOfContentfulEngineSession 钉住判据 2。
func TestComposerDraftKeepsViewOfContentfulEngineSession(t *testing.T) {
	store := newDraftRecordStore()
	first := mustDraftService(t, store)
	if err := first.SaveComposerDraft(draftRestoreText); err != nil {
		t.Fatal(err)
	}
	draftID := first.Snapshot().Session.ID
	first.Shutdown()

	const hostSessionID = "sess_host_resumed"
	host := &fakeEngine{
		sessionID: hostSessionID,
		history:   []EngineMessage{{Role: "user", Content: "宿主恢复的既定历史"}},
	}
	second := mustDraftServiceWithEngine(t, store, host)
	defer second.Shutdown()

	waitForCondition(t, "retained draft slot on the contentful host session", func() bool {
		unit := second.sessions.Unit(draftID)
		return unit != nil && unit.ComposerText() == draftRestoreText
	})
	view := second.Snapshot()
	if view.Session.Draft || view.Session.ID != hostSessionID {
		t.Fatalf("contentful engine session must keep the view: %+v", view.Session)
	}
	row, ok := draftRowOf(view, draftID)
	if !ok {
		t.Fatalf("retained draft %q missing from directory rows %+v", draftID, view.Sessions)
	}
	if row.Status != SessionStatusDraft {
		t.Fatalf("retained draft row status = %q, want draft", row.Status)
	}
}

// TestComposerDraftRestoreRehydratesTaskLedger 钉住判据 3。
func TestComposerDraftRestoreRehydratesTaskLedger(t *testing.T) {
	store := newDraftRecordStore()
	first := mustDraftService(t, store)
	if err := first.SaveComposerDraft(draftRestoreText); err != nil {
		t.Fatal(err)
	}
	draftID := first.Snapshot().Session.ID
	// 会话 record 自带 task 台账（工作表格内容随会话落盘）；恢复后要装回运行时。
	store.mu.Lock()
	record := store.records[draftID]
	record.Tasks = []dto.TaskRecord{{
		ID: "todo:0", Kind: "todo", Task: "恢复后仍要看得见的工作条目", Status: dto.TaskPending,
	}}
	store.records[draftID] = record
	store.mu.Unlock()
	first.Shutdown()

	second := mustDraftServiceWithEngine(t, store, &fakeEngine{sessionID: "sess_host_precreated"})
	defer second.Shutdown()

	waitForCondition(t, "restored task ledger on the drafted session", func() bool {
		records := second.Deps.Runtime.TaskSnapshotFor(draftID)
		return len(records) == 1 && records[0].ID == "todo:0"
	})
	found := false
	for _, row := range second.Snapshot().Runtime.WorkTable {
		if row.ID == "todo:0" {
			found = true
		}
	}
	if !found {
		t.Fatalf("work table after draft restore = %+v（任务台账没进工作表格投影）", second.Snapshot().Runtime.WorkTable)
	}
}

// v8DraftStore 模拟 v8 存储：state/record 通道已退役——record 里没有 Composer
// 正文，目录行的 draft 身份来自 lifecycle 草稿文件（sessionstore §2.5.4），草稿
// 正文只活在草稿通道里。
type v8DraftStore struct {
	*draftRecordStore
	drafts map[string]string
}

func newV8DraftStore() *v8DraftStore {
	return &v8DraftStore{draftRecordStore: newDraftRecordStore(), drafts: map[string]string{}}
}

func (store *v8DraftStore) SaveComposerDraftWorkspace(_ string, sessionID, content string) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	if content == "" {
		delete(store.drafts, sessionID)
		return nil
	}
	store.drafts[sessionID] = content
	return nil
}

func (store *v8DraftStore) LoadComposerDraftWorkspace(_ string, sessionID string) (string, bool, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	text, ok := store.drafts[sessionID]
	return text, ok, nil
}

// SaveSessionRecordWorkspace 退役 record 通道：只登记目录行（v8 的
// EnsureIndexed 语义），不保留 record 内容。
func (store *v8DraftStore) SaveSessionRecordWorkspace(_ string, sessionID string, record model.SessionRecord) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	for index := range store.catalog {
		if store.catalog[index].ID == sessionID {
			store.catalog[index].UpdatedAt = record.UpdatedAt
			return nil
		}
	}
	store.catalog = append(store.catalog, SessionInfo{ID: sessionID, Name: "新会话", UpdatedAt: record.UpdatedAt})
	return nil
}

// LoadSessionRecordWorkspace 退役 record 通道：只有派生的骨架，Composer 正文为空。
func (store *v8DraftStore) LoadSessionRecordWorkspace(_, sessionID string) (model.SessionRecord, error) {
	return model.SessionRecord{Version: session_runtime.SessionRecordVersion, ID: sessionID}, nil
}

// List 按 v8 的 derivedRecord 语义派生目录行：草稿身份 = 草稿文件存在性。
func (store *v8DraftStore) List() []SessionInfo {
	store.mu.Lock()
	defer store.mu.Unlock()
	rows := make([]SessionInfo, 0, len(store.catalog))
	for _, item := range store.catalog {
		row := item
		if store.drafts[item.ID] != "" {
			row.Status = SessionStatusDraft
		} else {
			row.Status = SessionStatusIdle
		}
		rows = append(rows, row)
	}
	return rows
}

// TestComposerDraftTextSurvivesRetiredRecordChannel 钉住判据 4：v8 下 record 通道
// 已退役（Composer 正文被丢弃），草稿正文只有落在 lifecycle 草稿通道里才跨重启
// 可恢复；物化后草稿通道必须真的清空（否则目录里留一条幽灵草稿行）。
func TestComposerDraftTextSurvivesRetiredRecordChannel(t *testing.T) {
	store := newV8DraftStore()
	first := mustDraftService(t, store)
	if err := first.SaveComposerDraft(draftRestoreText); err != nil {
		t.Fatal(err)
	}
	draftID := first.Snapshot().Session.ID
	store.mu.Lock()
	channelText, channelOK := store.drafts[draftID]
	store.mu.Unlock()
	if !channelOK || channelText != draftRestoreText {
		t.Fatalf("draft text missing from the lifecycle draft channel: %q ok=%v", channelText, channelOK)
	}
	first.Shutdown()

	// 重启（冷启动形状：引擎无 bundle）：草稿正文从 lifecycle 草稿通道读回。
	second := mustDraftServiceWithEngine(t, store, &fakeEngine{lazyStart: true})
	restored := second.Snapshot()
	if !restored.Session.Draft || restored.Session.ID != draftID || restored.Session.Composer != draftRestoreText {
		t.Fatalf("restored draft over retired record channel = %+v", restored.Session)
	}

	// 物化：草稿通道清空 + 目录行不再标 draft。
	if err := second.Submit(t.Context(), draftRestoreText); err != nil {
		t.Fatal(err)
	}
	if err := second.WaitForIdle(t.Context()); err != nil {
		t.Fatal(err)
	}
	store.mu.Lock()
	_, leftover := store.drafts[draftID]
	store.mu.Unlock()
	if leftover {
		t.Fatalf("draft channel not cleared after materialize: %+v", store.drafts)
	}
	for _, row := range second.Snapshot().Sessions {
		if row.ID == draftID && row.Status == SessionStatusDraft {
			t.Fatalf("materialized session still shows a draft row: %+v", row)
		}
	}
	second.Shutdown()
}
