# application/session/cold

## 生态位

`application/session/cold` 承载冷会话恢复的前半链：持久化三读、history 装配、
Engine/queue 恢复、workspace 绑定与锁内激活。主要调用方是 core 的冷恢复主链。

## 职责与非职责

职责：

- 并行读取 record 与 transcript，按需回退 legacy history；
- 重建 transcript 与 provider history；
- 恢复 Engine 历史、durable queue 与 system prompt；
- 应用 workspace 绑定、epoch、active、tier、title 顺序。

非职责：

- 不拥有 SessionStore、Engine、Runtime 或 View；
- 不实现冷恢复顶层切换与失败回退；
- 不导入 `application/assembly`。

## 文件结构

- `read.go`：冷加载持久化读取。
- `history.go`：transcript / provider history 装配。
- `restore.go`：Engine 恢复与 queue 回填。
- `workspace.go`：项目根和 session binding 规则。
- `activation.go`：锁内激活准备。
- `task.go`：Task/Plan/checkpoint/compaction 恢复。
- `view.go`：record/legacy 可见窗口构造与安装。
- `commit.go`：锁内 pending-tail、ChatState、Plan、workspace 与 revision。
- `publish.go`：锁外 context、Runtime、Snapshot 与 LRU 发布。
- `*_external_test.go`：`package cold_test` 的 stage 行为、失败语义与顺序测试；
  不读取 assembly 私有字段。

## 依赖方向

允许依赖 backend engine 的 Session/Task 类型、`contract`、`contract/protocol`、
`application/session/state`。禁止依赖 `application/assembly`、GUI/TUI 或存储实现。

## 并发与错误

`ReadApplication` 并行读取 record 与 transcript；调用方保证后续 View 写面在正确
锁内。错误包装、legacy fallback、Engine replace 与 queue 恢复顺序保持迁移前语义。

## 测试与验证

```text
go test ./application/session/cold -count=1
go test ./application/assembly -run 'SnapshotOfCold|ColdStartDraft' -count=1
go test ./application/... -count=1
```

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### activation.go

- `func NewActivation(deps ActivationDeps) *ActivationApplication` — NewActivation constructs the cold activation application.
- `func (application *ActivationApplication) PrepareLocked( sessionID string, epoch uint64, snapshot *model.Snapshot, history []contract.EngineMessage, record model.SessionRecord, hasRecord bool, storedTier string, ) ActivationResult` — PrepareLocked applies epoch, active, tier and title decisions.

### activation_external_test.go

- `func TestSessionColdActivationPreparesActiveSession(t *testing.T)`
- `func TestSessionColdActivationKeepsViewWhenEpochOrTargetChanged(t *testing.T)`

### commit.go

- `func NewCommit(deps CommitDeps) *CommitApplication` — NewCommit constructs the cold-commit application.
- `func (application *CommitApplication) CommitLocked( sessionID string, mayActivate bool, workspaceAvailable bool, record model.SessionRecord, unit *sessioncase.SessionUnit, currentWorkspace *model.WorkspaceInfo, workspaceProjection switchworkspace.Projection, pendingTailNotice string, snapshot *model.Snapshot, ) CommitResult` — CommitLocked applies pending tail, chat, plan, workspace and revision.

### commit_external_test.go

- `func TestSessionColdCommitActivePath(t *testing.T)`
- `func TestSessionColdCommitNonActiveDoesNotTouchSharedFacts(t *testing.T)`

### history.go

- `func NewHistory(deps HistoryDeps) *HistoryApplication` — NewHistory constructs the cold-history application.
- `func (application *HistoryApplication) Assemble(sessionID string, loaded ReadResult) (HistoryResult, error)` — Assemble reconstructs transcript and provider history for a stored session.

### history_external_test.go

- `func TestSessionColdHistoryUsesLegacyHistoryWithoutRecord(t *testing.T)`
- `func TestSessionColdHistoryRebuildsTranscriptAndPrefersWireHistory(t *testing.T)`
- `func TestSessionColdHistoryFallsBackToRecordAndResumeHistory(t *testing.T)`
- `func TestSessionColdHistoryUsesFinalResumeFallback(t *testing.T)`
- `func TestSessionColdHistoryWrapsWireError(t *testing.T)`

### publish.go

- `func NewPublish(deps PublishDeps) *PublishApplication` — NewPublish constructs the cold-publish application.
- `func (application *PublishApplication) Publish( sessionID string, location session_runtime.Location, record model.SessionRecord, mayActivate bool, revision uint64, systemPrompt string, ) error` — Publish attaches context and publishes runtime/snapshot state.

### publish_external_test.go

- `func TestSessionColdPublishActivatesRuntimeAndLRU(t *testing.T)`
- `func TestSessionColdPublishNonActiveStillAttachesAndTouches(t *testing.T)`
- `func TestSessionColdPublishStopsOnContextError(t *testing.T)`

### read.go

- `func NewRead(deps ReadDeps) *ReadApplication` — NewRead constructs the cold-read application.
- `func (application *ReadApplication) Read(sessionID string) (ReadResult, error)` — Read loads record and transcript concurrently, then history when needed.

### read_external_test.go

- `func TestSessionColdReadReturnsRecordAndTranscript(t *testing.T)`
- `func TestSessionColdReadFallsBackToLegacyHistory(t *testing.T)`
- `func TestSessionColdReadErrorPrecedence(t *testing.T)`
- `func TestSessionColdReadIgnoresLegacyErrorsWhenRecordExists(t *testing.T)`

### read_workspace_external_test.go

- `func TestReadUsesRecordAndTranscriptAndSkipsLegacyHistory(t *testing.T)`
- `func TestWorkspaceApplicationUnbindsOnlyWithoutWorkspace(t *testing.T)`

### restore.go

- `func NewRestore(deps RestoreDeps) *RestoreApplication` — NewRestore constructs the cold restore application.
- `func (application *RestoreApplication) Restore(sessionID string, location session_runtime.Location, history []contract.EngineMessage) error` — Restore resumes or replaces engine history, recovers the queue and writes the

### restore_external_test.go

- `func TestSessionColdRestoreResumesEngineAndQueue(t *testing.T)`
- `func TestSessionColdRestoreFallsBackToReplaceHistory(t *testing.T)`
- `func TestSessionColdRestoreStopsOnEngineAndQueueErrors(t *testing.T)`

### task.go

- `func NewTask(deps TaskDeps) *TaskApplication` — NewTask constructs the cold-task application.
- `func (application *TaskApplication) RestoreLocked( sessionID string, location session_runtime.Location, record model.SessionRecord, transcript []model.TranscriptEvent, transcriptRenumbered bool, hasRecord bool, )` — RestoreLocked restores task state under the view lock.

### task_external_test.go

- `func TestSessionColdTaskRestoresCompactionsAndRetainedWindow(t *testing.T)`
- `func TestSessionColdTaskLoadsCompactionsFromNewStore(t *testing.T)`
- `func TestSessionColdTaskResetsWithoutRecord(t *testing.T)`

### view.go

- `func NewView(deps ViewDeps) *ViewApplication` — NewView constructs the cold-view application.
- `func (application *ViewApplication) InstallLocked( sessionID string, location session_runtime.Location, record model.SessionRecord, hasRecord bool, visibleHistory []contract.EngineMessage, historyTotal int, total int, offset int, window int, mayActivate bool, snapshot *model.Snapshot, )` — InstallLocked installs record/legacy visible history under the view lock.

### view_external_test.go

- `func TestSessionColdViewInstallsRecordWindowAndActiveFacts(t *testing.T)`
- `func TestSessionColdViewKeepsNewerVisibleConversation(t *testing.T)`
- `func TestSessionColdViewAppendsLegacyAndFixesPaging(t *testing.T)`

### workspace.go

- `func NewWorkspace(deps WorkspaceDeps) *WorkspaceApplication` — NewWorkspace constructs the cold-workspace application.
- `func (application *WorkspaceApplication) Bind(sessionID, workspaceID string, current *model.WorkspaceInfo)` — Bind restores the project root, session binding or unbind state.

### workspace_external_test.go

- `func TestSessionColdWorkspaceBindsExistingWorkspace(t *testing.T)`
- `func TestSessionColdWorkspaceSkipsGlobalRootWhileChatRuns(t *testing.T)`
- `func TestSessionColdWorkspaceUnbindsOnlyTrulyUnboundSession(t *testing.T)`
- `func TestSessionColdWorkspaceDoesNotUnbindMissingWorkspaceMetadata(t *testing.T)`
