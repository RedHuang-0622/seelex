# core/composer（根包分卷）

## 生态位

输入草稿合成器的持久化与工作区绑定（重启后恢复）

覆盖：`composer*.go`；未归属文件由覆盖自检拦下。

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### composer_draft.go

- `func (service *Service) SaveComposerDraft(text string) error` — SaveComposerDraft 保存当前视图草稿会话的未发送输入（仅 draft 会话允许；
- `func (service *Service) persistComposerDraft(sessionID, text string) error` — persistComposerDraft 把草稿会话 record 落盘（Status=draft + Composer）。
- `func (service *Service) draftWorkspaceID(sessionID string) string` — draftWorkspaceID 返回当前草稿会话的绑定项目（draft 槽/视图携带；非草稿或
- `func (service *Service) persistComposerDraftIn(projectID, sessionID, text string) error` — persistComposerDraftIn 把草稿 record 写到显式项目（"" = 默认项目）。
- `func (service *Service) clearComposerDraft(sessionID, projectID string)` — clearComposerDraft 在草稿物化成功后清空 composer（内存 + 落盘）。
- `func (service *Service) restorePersistedDraft()` — restorePersistedDraft 在冷启动（引擎未建 bundle）时恢复最近一个持久化的

### composer_draft_test.go

- `func newDraftRecordStore() *draftRecordStore`
- `func (store *draftRecordStore) SaveCurrent(string) error`
- `func (store *draftRecordStore) Delete(id string) error`
- `func (store *draftRecordStore) SetWorkspace(workspaceID string)`
- `func (store *draftRecordStore) Workspace() string`
- `func (store *draftRecordStore) LoadHistory(string) ([]EngineMessage, error)`
- `func (store *draftRecordStore) LoadHistoryRange(string, int, int) ([]EngineMessage, int, error)`
- `func (store *draftRecordStore) SaveSessionRecord(sessionID string, record model.SessionRecord) error`
- `func (store *draftRecordStore) LoadSessionRecord(sessionID string) (model.SessionRecord, error)`
- `func (store *draftRecordStore) List() []SessionInfo`
- `func (store *draftRecordStore) LoadSessionRecordWorkspace(_ string, sessionID string) (model.SessionRecord, error)`
- `func (store *draftRecordStore) SaveSessionRecordWorkspace(_ string, sessionID string, record model.SessionRecord) error`
- `func mustDraftService(t *testing.T, store *draftRecordStore) *Service`
- `func TestComposerDraftPersistsAndRestoresAcrossRestart(t *testing.T)` — TestComposerDraftPersistsAndRestoresAcrossRestart（G4 先行第 2 片）：草稿

### composer_workspace_draft_test.go

- `func newWorkspaceDraftStore() *workspaceDraftStore`
- `func (store *workspaceDraftStore) SaveCurrent(string) error`
- `func (store *workspaceDraftStore) Delete(string) error`
- `func (store *workspaceDraftStore) SetWorkspace(workspaceID string)`
- `func (store *workspaceDraftStore) Workspace() string`
- `func (store *workspaceDraftStore) LoadHistory(string) ([]EngineMessage, error)`
- `func (store *workspaceDraftStore) LoadHistoryRange(string, int, int) ([]EngineMessage, int, error)`
- `func (store *workspaceDraftStore) List() []SessionInfo`
- `func (store *workspaceDraftStore) SessionsOf(projectID string) []SessionInfo`
- `func (store *workspaceDraftStore) SaveSessionRecord(sessionID string, record model.SessionRecord) error`
- `func (store *workspaceDraftStore) LoadSessionRecord(sessionID string) (model.SessionRecord, error)`
- `func (store *workspaceDraftStore) SaveSessionRecordWorkspace(projectID, sessionID string, record model.SessionRecord) error`
- `func (store *workspaceDraftStore) LoadSessionRecordWorkspace(projectID, sessionID string) (model.SessionRecord, error)`
- `func (store *workspaceDraftStore) EnsureSessionIndexed(string, string) error`
- `func (store *workspaceDraftStore) recordAt(projectID, sessionID string) (model.SessionRecord, bool)`
- `func newWorkspaceDraftService(t *testing.T, store *workspaceDraftStore, workspace *fakeWorkspace) *Service`
- `func TestComposerWorkspaceDraftBindingPersistsAcrossRestart(t *testing.T)` — TestComposerWorkspaceDraftBindingPersistsAcrossRestart G：工作区草稿在
