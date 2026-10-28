# application/session

## 生态位

`application/session` 承载 Session 聚合、状态与生命周期应用用例。Chat 执行循环仍在
Conversation/Backend，但会话持有的 ChatState、Queue、Approval 引用、View 指针和
revision 由本包的 `state` / `viewstate` 子包唯一拥有。

主要调用方：`application/assembly`、`application/conversation` 与 Wiring 装配；
Middle 只提供纯切换、历史和驻留策略。

## 职责

- 提供 Session 聚合状态、Snapshot 投影以及 Meta、SSH、Lifecycle、Archive、
  Delete、Draft、Resume、Fork、Interaction 与 Release。
- 保持 Application 入站行为与 DTO 身份稳定。
- 隔离 Session 用例，避免继续扩大 `application/assembly`。

## 非职责

- 不执行 Agent loop、工具、引擎流或持久化事务。
- 不实现存储、权限、工作区或 Runtime 规则。
- 不反向导入 `application/assembly`。

## 文件结构

- `meta.go`：会话置顶、别名与排序位的读写应用组件。
- `state/`：SessionUnit、Domain actor、View、InputQueue、Runtime slot 与 chat 状态。
- `viewstate/`：权威 Snapshot、消息序号、revision 与事件发布顺序。
- `ssh.go`：SSH 允许环境、固定环境与项目环境表的校验编排。
- `release.go`：LRU/完整释放的二次守卫、flush 与有序回收。
- `residency.go`：`ResidencyPort`（预览、LRU 序、输入索引、驱逐、归档）与纯值 deps。
- `residency/`：Application-owned resident/content 两层 LRU、受保护集、正文卸载
  与自动回读运行时。
- `history/`：Session resume、冷恢复顶层流程、durable 分页与可见窗口安装。
- `fork/`：durable Session fork 准入、深拷贝、持久化、workspace 绑定与 child
  engine 收口。
- `lifecycle.go`：unload 的过渡锁、持久化、释放和视图续接。
- `archive.go`：archive 门控、记录/引擎卸载与后台释放。
- `delete.go`：删除记录、释放内存、解绑工作区与快照发布。
- `draft_submission.go`：未物化草稿的显式提交激活。
- `draft_entry.go`：`/new` 的旧会话保存、清理与 draft shell 安装。
- `draft_materialization.go`：首请求物化、标题、commit 与发布顺序。
- `attach.go`：`AttachApplication`，唯一拥有 hot attach 的锁外准备、锁内会话投影
  与锁外发布顺序；冷恢复和草稿物化继续共享同一把过渡锁及 `ViewMu → Unit` 锁序。
- `resume.go`：hot/restoring/cold 会话选择分支。
- `restore_recovery.go`：冷恢复失败回退与 fresh draft reset。
- `fork_lineage.go`：只读取 `forked_from` 的窄 JSON 投影并安全解码，不依赖 Store 载荷类型。
- `fork.go`：父 ID 校验、per-session 过渡锁、latest cut 与 commit 后 resume。
- `submit.go`：显式 session 的草稿物化、加载/正文判定、恢复与提交顺序。
- `restoring_shell.go`：restoring 会话的 Snapshot 空壳投影。
- `interaction.go`：`InteractionApplication`，统一 approval/session/account
  决议与失败 notice/close 顺序，不拥有 broker、Session 或 Account 状态。
- `interaction_observer.go`：`InteractionObserverApplication`，唯一拥有审批的
  会话归属、视图单槽镜像、待批计数与 open/close 事件顺序。
- `meta_test.go`：组件读取与 nil 语义测试。
- `ssh_test.go`：环境交集、固定环境与路径校验测试。
- `release_test.go`：守卫、flush 和回收顺序测试。
- `operations_test.go`：删除顺序、记录错误短路和工作区投影测试。
- `orchestration_test.go`：草稿提交锁、resume 分支与恢复回退测试。
- `draft_entry_test.go`：草稿入口保存/清理与物化顺序测试。
- `draft_entry_behavior_external_test.go`：草稿入口 closed/draining、幂等和运行中保护。
- `resume_component_external_test.go`：hot/restoring/cold resume 分支与失败恢复。
- `restoring_shell_external_test.go`：restoring shell 的会话事实清理。
- `restore_recovery_external_test.go`：stale epoch、前会话回退和 fresh draft 降级。
- `restore_draft_reset_external_test.go`：恢复失败后的草稿重置与发布顺序。
- `*_external_test.go`：草稿提交、物化与 fork-lineage 的 owner 外部测试。

## 核心实现

`MetaApplication` 通过 `MetaDeps` 接收 metadata port 与只读查询；写入继续调用
`kernel/sessionpolicy.SetMeta`，读取由 `SessionQuery` 注入。

`SSHApplication` 通过 `SSHDeps` 读取 Session SSH 状态和工作区环境表，只拥有允许
环境子集、固定环境不可变和未选择环境缺省值等应用判据。

`ReleaseApplication` 通过 `ReleaseDeps` 接收受保护集、持久化、引擎/正文/任务/
工作区释放动作；包内只固定二次守卫、flush 与回收顺序。

`LifecycleApplication`、`ArchiveApplication`、`DeleteApplication` 分别固定 unload、
archive、delete 的应用顺序；准入与归档判据仍在 middle 层。

`InteractionApplication` 只消费当前视图交互、后台 approval 反查以及
resume/account/notice/close 回调。审批 broker、Session 恢复和账号切换仍由原 owner
执行。

`ForkApplication` 只拥有 transition/resume 编排；fork snapshot 深拷贝、持久化与
workspace 绑定仍由注入的 `ForkLocked` 宿主执行。`SubmitApplication` 只拥有显式
session 的 admission 顺序，队列和 Chat 执行仍由 Conversation/Session state 拥有。

## 数据流

`application/assembly` / `Frontend` → `application/session` 用例 →
`LifecycleApplication` / `ArchiveApplication` / `DeleteApplication` /
`ReleaseApplication` → 注入的窄端口 → metadata、Session、Workspace、Engine、
Runtime 或存储的既有唯一实现。

## 依赖方向

允许依赖 `contract/dto`、`contract/protocol`、本包的 `state` / `viewstate` 与
注入的窄端口。
residency 判据经 `ResidencyPort` 注入，由 `internal/host.NewResidencyPort`
提供 Middle 实现；本包不 import `middle/residency`。
Fork lineage 只消费会话上下文 JSON 中的 `forked_from` 字段，不 import `store`。
禁止依赖 `application/assembly`、backend 具体实现或 Middle 可变会话状态。

## 并发与错误

- 读写函数由宿主注入；组件不缓存 metadata、SSH 状态或会话实体。
- nil 应用读取保持 `switch.ErrMetaUnsupported` 语义。
- 写入后的目录刷新由注入的 `switch.MetaDeps.Wake` 负责。
- SSH 写入失败不得留下部分环境集合；固定环境不得被后续调用改写。
- Release 在任何具体回收动作前重新检查受保护集，确认不过时原样返回 `nil`。
- Release 的慢 flush 与引擎卸载不持有 `application/assembly` 的共享锁；锁只由注入
  回调在各自的短临界区内获取。

## 扩展方式

新增 Session 能力时使用独立 deps 与组件；不得把完整 `*Service` 注入本包，
也不得把 Agent、Chat 或 Session 主状态机搬入本包。

## Review 指南

- 检查写入是否仍由 `kernel/sessionpolicy.SetMeta` 唯一校验。
- 检查读取是否仍来自绑定事实或 SessionQuery，而不是视图猜测。
- 检查 SSH 环境子集与固定环境判据是否只保留一份。
- 检查 Release 的受保护集、flush 顺序与具体释放实现是否仍委托宿主。
- 检查组件是否依赖 core。

## 测试与验证

```text
go test ./application/session/... -count=1
go test ./application/assembly -run 'SessionMeta|SessionSSH|Release' -count=1
go test ./application/... -count=1
go test -race ./application/... -count=1
```

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### archive.go

- `func NewArchive(deps ArchiveDeps) *ArchiveApplication` — NewArchive constructs the archive application.
- `func (application *ArchiveApplication) Archive(sessionID string) error` — Archive archives a session while preserving the middle-layer decision path.

### attach.go

- `func NewAttach(deps AttachDeps) *AttachApplication` — NewAttach constructs the session attach use case.
- `func (application *AttachApplication) Attach(sessionID string, snapshot *model.Snapshot) (uint64, error)` — Attach points the current snapshot at one resident session. The caller does
- `func (application *AttachApplication) commitLocked( sessionID, storedTier string, workspace *model.WorkspaceInfo, hasWorkspace bool, workspaceProjection switchworkspace.Projection, hasWorkspaceProjection bool, snapshot *model.Snapshot, )`

### attach_test.go

- `func (lock *attachTestLock) Lock()`
- `func (lock *attachTestLock) Unlock()`
- `func (port *attachTestPreparation) EnsureContent(string) error`
- `func (port *attachTestPreparation) TargetRunning(string) bool`
- `func (port *attachTestPreparation) Workspace(string) (*model.WorkspaceInfo, bool)`
- `func (port *attachTestPreparation) BindProjectRootIfSafe(string, string) bool`
- `func (port *attachTestPreparation) SetWorkspaceWriteScope(string)`
- `func (port *attachTestPreparation) SetRuntimeWorkspace(string, string)`
- `func (port *attachTestPreparation) SwitchTasks(string)`
- `func (port *attachTestPreparation) RestoreSubagentAnchors(string)`
- `func (port *attachTestPreparation) WorkspaceProjection() (switchworkspace.Projection, bool)`
- `func (port *attachTestPreparation) StoredPermissionTier(string) string`
- `func (port *attachTestCommit) SessionTitle(string) string`
- `func (port *attachTestCommit) SetActive(string)`
- `func (port *attachTestCommit) Unit(string) *sessioncase.SessionUnit`
- `func (port *attachTestCommit) ApplyStoredPermissionTier(string, string)`
- `func (port *attachTestCommit) SetSessionChat(string, model.ChatState)`
- `func (port *attachTestCommit) MirrorActiveView()`
- `func (port *attachTestCommit) VisibleTask(string) *model.TaskState`
- `func (port *attachTestCommit) ActivePlan(string) *model.PlanState`
- `func (port *attachTestCommit) MirrorPlan(string, *model.PlanState)`
- `func (port *attachTestCommit) MirrorApprovals(string)`
- `func (port *attachTestCommit) ApplyWorkspace(*model.WorkspaceInfo, switchworkspace.Projection)`
- `func (port *attachTestCommit) SystemPrompt() string`
- `func (port *attachTestCommit) PromptRoutesBySession() bool`
- `func (port *attachTestCommit) BumpRevision() uint64`
- `func (port *attachTestPublish) WriteSessionPrompt(string)`
- `func (port *attachTestPublish) WriteGlobalPrompt(string)`
- `func (port *attachTestPublish) RefreshRuntime(string)`
- `func (port *attachTestPublish) PublishSnapshot(string, uint64)`
- `func (port *attachTestPublish) PublishRuntimeProjections()`
- `func (port *attachTestPublish) RefreshCatalog()`
- `func (port *attachTestPublish) TouchResident(string)`
- `func (port *attachTestPublish) TouchContent(string)`
- `func TestSessionAttachCommitsOneCurrentSessionProjection(t *testing.T)`
- `func TestSessionAttachSkipsPromptWritesForRunningTarget(t *testing.T)`
- `func joinAttachCalls(calls []string) string`

### delete.go

- `func NewDelete(deps DeleteDeps) *DeleteApplication` — NewDelete constructs the session delete application.
- `func (application *DeleteApplication) Delete(sessionID string) error` — Delete removes a session record and releases all process-local resources.

### draft_entry.go

- `func NewDraftEntry(deps DraftEntryDeps) *DraftEntryApplication` — NewDraftEntry constructs the draft-entry application.
- `func (application *DraftEntryApplication) Begin() error` — Begin switches the current view into a new draft shell.

### draft_entry_behavior_external_test.go

- `func TestSessionDraftEntryRejectsClosedAndDraining(t *testing.T)`
- `func TestSessionDraftEntryIsIdempotentForDraft(t *testing.T)`
- `func TestSessionDraftEntryStopsWhenIdleSessionSaveFails(t *testing.T)`
- `func TestSessionDraftEntryPreservesRunningSessionAndPublishesDraft(t *testing.T)`

### draft_entry_test.go

- `func TestDraftEntryOrdersIdleSessionCleanup(t *testing.T)`
- `func TestDraftEntrySkipsPersistenceForRunningSession(t *testing.T)`
- `func joinCalls(calls []string) string`

### draft_materialization.go

- `func NewDraftMaterialization(deps DraftMaterializationDeps) *DraftMaterializationApplication` — NewDraftMaterialization constructs the draft materialization application.
- `func (application *DraftMaterializationApplication) Materialize(firstQuestion string) error` — Materialize turns the current draft into a real session on first input.

### draft_materialization_external_test.go

- `func TestDraftMaterializationSkipsNonDraft(t *testing.T)`
- `func TestDraftMaterializationRequiresPreassignedID(t *testing.T)`
- `func TestDraftMaterializationStopsWhenWorkspaceBindFails(t *testing.T)`
- `func TestDraftMaterializationOrdersFirstInput(t *testing.T)`

### draft_submission.go

- `func NewDraftSubmission(deps DraftSubmissionDeps) *DraftSubmissionApplication` — NewDraftSubmission constructs the draft-submit application.
- `func (application *DraftSubmissionApplication) Materialize(sessionID, firstInput string) error` — Materialize performs a cheap target check, rechecks ownership under the view

### draft_submission_external_test.go

- `func (locker *recordingLocker) Lock()`
- `func (locker *recordingLocker) Unlock()`
- `func TestDraftSubmissionSkipsLoadedTarget(t *testing.T)`
- `func TestDraftSubmissionRejectsStaleDraftView(t *testing.T)`
- `func TestDraftSubmissionTrimsAfterViewCheck(t *testing.T)`

### fork.go

- `func NewFork(deps ForkDeps) *ForkApplication`
- `func (application *ForkApplication) Fork(parentID string, request protocol.ForkRequest) (string, error)`
- `func (application *ForkApplication) ForkLatest(parentID string) (string, error)`

### fork_lineage.go

- `func NewForkLineage(deps ForkLineageDeps) *ForkLineageApplication` — NewForkLineage constructs the fork-lineage application.
- `func (application *ForkLineageApplication) Load(location session_runtime.Location, sessionID string) *model.SessionForkRef` — Load returns validated fork lineage or nil for missing/corrupt data.

### fork_lineage_external_test.go

- `func TestForkLineageReadsOnlyNarrowPayloadProjection(t *testing.T)`
- `func TestForkLineageReturnsNilWhenPayloadUnavailable(t *testing.T)`
- `func TestForkLineageRejectsInvalidRecords(t *testing.T)`
- `func TestForkLineageDecodesValidReference(t *testing.T)`
- `func mustLineagePayload(t *testing.T, record store.SessionContextRecord) []byte`
- `func mustJSON(t *testing.T, value any) []byte`

### fork_test.go

- `func TestForkApplicationCreatesAndSwitchesSession(t *testing.T)`
- `func TestForkApplicationReturnsChildWhenSwitchFails(t *testing.T)`

### interaction.go

- `func NewInteraction(deps InteractionDeps) *InteractionApplication`
- `func (application *InteractionApplication) Resolve(ctx context.Context, id, optionID string) error`

### interaction_observer.go

- `func NewInteractionObserver(deps InteractionObserverDeps) *InteractionObserverApplication` — NewInteractionObserver constructs the approval interaction observer.
- `func (application *InteractionObserverApplication) Observe( sessionID, requestID string, interaction *model.Interaction, )` — Observe records an opened or closed broker interaction by session.
- `func (application *InteractionObserverApplication) MirrorLocked(sessionID string)` — MirrorLocked projects the first pending approval for one session. The caller
- `func (application *InteractionObserverApplication) unit(sessionID string) ApprovalUnit`

### interaction_observer_test.go

- `func (lock *interactionObserverLock) Lock()`
- `func (lock *interactionObserverLock) Unlock()`
- `func (unit *interactionObserverUnit) AddApproval(id string)`
- `func (unit *interactionObserverUnit) RemoveApproval(id string)`
- `func TestInteractionObserverKeepsBackgroundApprovalOutOfViewSlot(t *testing.T)`
- `func joinObserverCalls(calls []string) string`

### interaction_test.go

- `func TestInteractionApplicationResolvesViewAndBackgroundApprovals(t *testing.T)`
- `func TestInteractionApplicationSessionFailureNotifiesAndCloses(t *testing.T)`

### lifecycle.go

- `func NewLifecycle(deps LifecycleDeps) *LifecycleApplication` — NewLifecycle constructs the session lifecycle application.
- `func (application *LifecycleApplication) Unload(sessionID string) error` — Unload persists a non-view session, releases its memory and renews the view

### meta.go

- `func NewMetaApplication(deps MetaDeps) *MetaApplication` — NewMetaApplication constructs the metadata application.
- `func (application *MetaApplication) SetSessionMeta(sessionID string, meta model.SessionMeta) error` — SetSessionMeta writes display metadata and wakes the catalog through MetaDeps.
- `func (application *MetaApplication) SessionMeta(sessionID string) (model.SessionMeta, error)` — SessionMeta reads display metadata through the injected read port.

### meta_test.go

- `func TestMetaApplicationDelegatesRead(t *testing.T)`
- `func TestNilMetaApplicationPreservesUnsupportedReadSemantics(t *testing.T)`

### operations_test.go

- `func TestDeleteApplicationOrdersRecordReleaseAndPublication(t *testing.T)`
- `func TestDeleteApplicationStopsOnRecordError(t *testing.T)`
- `func TestDeleteApplicationAppliesWorkspaceProjection(t *testing.T)`

### orchestration_test.go

- `func TestResumeApplicationBranches(t *testing.T)`
- `func TestRestoreRecoveryFallsBackToDraft(t *testing.T)`

### release.go

- `func NewReleaseApplication(deps ReleaseDeps) *ReleaseApplication` — NewReleaseApplication constructs the release application.
- `func (application *ReleaseApplication) Release(sessionID string, mode ReleaseMode) error` — Release releases one session's in-memory state in the fixed order required
- `func (application *ReleaseApplication) Reconcile()` — Reconcile runs both release layers after ownership or activity changes.

### release_test.go

- `func TestReleasePreservesOrderingAndIgnoresProtectedIdleSession(t *testing.T)`
- `func TestReleaseFullOrdering(t *testing.T)`
- `func TestReleaseStopsOnPersistError(t *testing.T)`

### residency_boundary_test.go

- `func TestCoreDoesNotImportResidencyPackage(t *testing.T)`
- `func sessionRepositoryRoot(t *testing.T) string`

### restore_draft_reset_external_test.go

- `func TestSessionRestoreDraftResetPublishesDraftShell(t *testing.T)`

### restore_recovery.go

- `func NewRestoreRecovery(deps RestoreRecoveryDeps) *RestoreRecoveryApplication` — NewRestoreRecovery constructs the restore recovery application.
- `func (application *RestoreRecoveryApplication) Handle(sessionID, previousID string, epoch uint64, _ error)` — Handle restores the previous session or resets to a draft when possible.
- `func NewRestoreDraftReset(deps RestoreDraftResetDeps) *RestoreDraftResetApplication` — NewRestoreDraftReset constructs the restore draft reset application.
- `func (application *RestoreDraftResetApplication) Reset()` — Reset creates or reuses the draft slot and installs its shell.

### restore_recovery_external_test.go

- `func TestSessionRestoreRecoveryClearsRestoringAndIgnoresStaleView(t *testing.T)`
- `func TestSessionRestoreRecoveryIgnoresSupersededEpoch(t *testing.T)`
- `func TestSessionRestoreRecoveryFallsBackToPreviousSession(t *testing.T)`
- `func TestSessionRestoreRecoveryResetsDraftWhenNoFallbackExists(t *testing.T)`

### restoring_shell.go

- `func NewRestoringShell(deps RestoringShellDeps) *RestoringShellApplication` — NewRestoringShell constructs the restoring-shell application.
- `func (application *RestoringShellApplication) PointLocked( sessionID string, unit *sessioncase.SessionUnit, snapshot *model.Snapshot, ) uint64` — PointLocked points the snapshot at a restoring session shell. The caller

### restoring_shell_external_test.go

- `func TestSessionRestoringShellClearsAllSessionFacts(t *testing.T)`

### resume.go

- `func NewResume(deps ResumeDeps) *ResumeApplication` — NewResume constructs the session resume application.
- `func (application *ResumeApplication) Resume(sessionID string) error` — Resume selects hot, restoring or cold loading for a session.

### resume_component_external_test.go

- `func TestSessionResumeRequiresSessionID(t *testing.T)`
- `func TestSessionResumeRestoringShellKeepsExistingLoad(t *testing.T)`
- `func TestSessionResumeRestoringSettledFallsBackToHotAttach(t *testing.T)`
- `func TestSessionResumeHotTargetMarksIntentBeforeAttach(t *testing.T)`
- `func TestSessionResumeColdTargetRunsBackgroundWhileChatBusy(t *testing.T)`
- `func TestSessionResumeColdFailureRunsRecovery(t *testing.T)`

### ssh.go

- `func NewSSHApplication(deps SSHDeps) *SSHApplication` — NewSSHApplication constructs the session SSH application.
- `func (application *SSHApplication) State(sessionID string) (model.SessionSSHInfo, error)` — State reads the current SSH state for a session.
- `func (application *SSHApplication) ProjectEnvironments(sessionID string) ([]model.WorkspaceEnvironmentInfo, string, error)` — ProjectEnvironments reads the environment table for the session's bound
- `func (application *SSHApplication) SetAllowed(sessionID string, envs []string) (model.SessionSSHInfo, error)` — SetAllowed replaces the session's allowed environment set after checking
- `func (application *SSHApplication) SetEnv(sessionID, projectID, env string) (model.SessionSSHInfo, error)` — SetEnv fixes a session's environment once. Repeating the same value is
- `func (application *SSHApplication) projectEnvironments(sessionID string) ([]model.WorkspaceEnvironmentInfo, string, error)`
- `func (application *SSHApplication) projectEnvironmentsFor(projectID string) ([]model.WorkspaceEnvironmentInfo, string, error)`
- `func (application *SSHApplication) sessions() SessionSSHPort`
- `func (application *SSHApplication) workspace() WorkspacePort`
- `func ValidateAllowedEnvs(known map[string]struct{}, envs []string) ([]string, error)` — ValidateAllowedEnvs checks that a requested environment set is a subset of

### ssh_test.go

- `func (sessions *sshTestSessions) SessionSSHState(string) (model.SessionSSHInfo, error)`
- `func (sessions *sshTestSessions) SetSessionSSHState(_ string, state model.SessionSSHInfo) error`
- `func (workspace *sshTestWorkspace) SessionWorkspace(string) (model.WorkspaceInfo, bool)`
- `func (workspace *sshTestWorkspace) WorkspaceEnvironments(string) ([]model.WorkspaceEnvironmentInfo, string, error)`
- `func TestValidateAllowedEnvsRejectsNamesOutsideProject(t *testing.T)`
- `func TestSetEnvFixesEnvironmentOnce(t *testing.T)`
- `func TestSetAllowed(t *testing.T)`
- `func TestSSHApplicationUnavailable(t *testing.T)`
- `func TestSSHApplicationRejectsMissingEnvironment(t *testing.T)`

### submit.go

- `func NewSubmit(deps SubmitDeps) *SubmitApplication`
- `func (application *SubmitApplication) SubmitToSession(ctx context.Context, sessionID, text string) error`

### submit_test.go

- `func TestSubmitApplicationMaterializesAndRoutesExplicitSession(t *testing.T)`
- `func TestSubmitApplicationStopsOnMaterializeFailure(t *testing.T)`
