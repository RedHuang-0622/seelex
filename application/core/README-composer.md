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
- `func (service *Service) draftResidueProjects(sessionID string) []string` — draftResidueProjects 返回磁盘上仍以 Status=draft 标记该会话的项目键
- `func containsProjectID(keys []string, key string) bool` — containsProjectID 报告项目键集合里是否已含 key（"" = 默认项目也是合法键）。
- `func (service *Service) persistedDraftCandidate() (session_runtime.Location, model.SessionRecord, bool)` — persistedDraftCandidate 选出最近一份持久化的草稿候选：跨项目枚举目录里的
- `func (service *Service) draftTextFor(projectID, sessionID string, record model.SessionRecord) string` — draftTextFor 读回落盘草稿正文：优先 lifecycle 草稿通道（v8 下草稿的唯一载体，
- `func (service *Service) restorePersistedDraft(takeOverView bool) bool` — restorePersistedDraft 在装配期恢复最近一份持久化的**非空**草稿会话：草稿正文
- `func (service *Service) scheduleShellDraftRestore(shellSessionID string)` — scheduleShellDraftRestore 给"宿主在装配前已预置引擎会话"的启动形状补装持久化
- `func (service *Service) shellViewUntouched(shellSessionID string) bool` — shellViewUntouched 报告视图是否仍停在装配器生成的那个空壳会话上、且启动窗口内
- `func (service *Service) claimShellView(shellSessionID string) bool` — claimShellView 在"视图仍是装配器空壳"且"那条引擎会话没有任何用户可见内容"时允许
- `func (service *Service) engineSessionCarriesContent(sessionID string) bool` — engineSessionCarriesContent 报告该引擎会话是否已有用户可见内容（system 提示词
- `func (service *Service) registerDraftSlotLocked(sessionID string)` — registerDraftSlotLocked 在"当前视图就是一份尚未物化的草稿会话"时登记草稿

### composer_draft_restore_test.go

- `func mustDraftServiceWithEngine(t *testing.T, store SessionPort, engine *fakeEngine) *Service`
- `func draftRowOf(snapshot Snapshot, sessionID string) (SessionInfo, bool)`
- `func waitForCondition(t *testing.T, what string, ready func() bool)`
- `func TestComposerDraftRestoresOntoPrecreatedEmptyEngineSession(t *testing.T)` — TestComposerDraftRestoresOntoPrecreatedEmptyEngineSession 钉住判据 1。
- `func TestComposerDraftKeepsViewOfContentfulEngineSession(t *testing.T)` — TestComposerDraftKeepsViewOfContentfulEngineSession 钉住判据 2。
- `func TestComposerDraftRestoreRehydratesTaskLedger(t *testing.T)` — TestComposerDraftRestoreRehydratesTaskLedger 钉住判据 3。
- `func newV8DraftStore() *v8DraftStore`
- `func (store *v8DraftStore) SaveComposerDraftWorkspace(_ string, sessionID, content string) error`
- `func (store *v8DraftStore) LoadComposerDraftWorkspace(_ string, sessionID string) (string, bool, error)`
- `func (store *v8DraftStore) SaveSessionRecordWorkspace(_ string, sessionID string, record model.SessionRecord) error` — SaveSessionRecordWorkspace 退役 record 通道：只登记目录行（v8 的
- `func (store *v8DraftStore) LoadSessionRecordWorkspace(_, sessionID string) (model.SessionRecord, error)` — LoadSessionRecordWorkspace 退役 record 通道：只有派生的骨架，Composer 正文为空。
- `func (store *v8DraftStore) List() []SessionInfo` — List 按 v8 的 derivedRecord 语义派生目录行：草稿身份 = 草稿文件存在性。
- `func TestComposerDraftTextSurvivesRetiredRecordChannel(t *testing.T)` — TestComposerDraftTextSurvivesRetiredRecordChannel 钉住判据 4：v8 下 record 通道

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
- `func mustDraftService(t *testing.T, store SessionPort) *Service`
- `func TestComposerDraftPersistsAndRestoresAcrossRestart(t *testing.T)` — TestComposerDraftPersistsAndRestoresAcrossRestart（G4 先行第 2 片）：草稿
- `func TestComposerDraftSessionRowVisible(t *testing.T)` — TestComposerDraftSessionRowVisible（回归，修复前 RED）：本会话有未发送草稿
- `func TestComposerWorkspaceRebindConvergesOnMaterialize(t *testing.T)` — TestComposerWorkspaceRebindConvergesOnMaterialize（回归，修复前 RED）：草稿

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
