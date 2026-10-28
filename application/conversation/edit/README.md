# application/conversation/edit

## 生态位

`application/conversation/edit` 承载“编辑用户消息并 fork 出 child 会话”的组件层。
`application/assembly` 只在组合根构造 Registry、Validator、TargetResolver、
Rollback 与 Process，并把最终 `EditApplication` 作为
`Components.Conversation.EditFork` 暴露给前端适配器。组件负责 admission、
target 解析、child 原子编排、rollback 和消息可编辑性。

## 职责

- operation TTL / 同 ID 幂等；
- current/running/queue/revision/window/compression 准入；
- target user event 与完整段落 fork cut；
- child fork → resume → submit → rollback 顺序；
- 可见用户消息 EventSeq 与单条禁用原因。

## 非职责

- 不拥有 Session/Fork 存储、Engine 或 View；
- 不修改 wire 字段；
- 不导入 `application/assembly`。

## 文件结构

- `registry.go`：operation 幂等账与 TTL。
- `application.go`：顶层输入校验、operation waiter 与 process 委托。
- `validator.go`：父会话准入。
- `resolver.go`：目标事件与 fork cut。
- `process.go`：child 原子编排。
- `rollback.go`：child 清理与 parent 恢复错误合成。
- `annotation.go`：消息可编辑性投影。

## 测试与验证

```text
go test ./application/conversation/edit -count=1
go test ./application/assembly -run 'EditUserMessageFork' -count=1
go test ./application/... -count=1
```

`owner_behavior_external_test.go` 使用 `package edit_test`，从外部验证 Registry、
Validator、Resolver、Process、Rollback 和 Annotation；assembly 只保留跨组件编辑并
fork 的集成用例。

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### annotation.go

- `func NewAnnotation(deps AnnotationDeps) *AnnotationApplication` — NewAnnotation constructs the annotation application.
- `func (application *AnnotationApplication) Annotate(sessionID string, messages []model.Message, running bool) []model.Message` — Annotate backfills EventSeq and the single disabled reason.

### application.go

- `func NewEditApplication(deps EditApplicationDeps) *EditApplication` — NewEditApplication constructs the edit/fork application.
- `func (application *EditApplication) EditUserMessageAndFork( ctx context.Context, parentID string, request model.EditUserMessageForkRequest, ) (model.EditUserMessageForkResult, error)` — EditUserMessageAndFork atomically forks a child and submits edited text.

### owner_behavior_external_test.go

- `func (locker *editTestLocker) Lock()`
- `func (locker *editTestLocker) Unlock()`
- `func TestOperationRegistryDeduplicatesMatchingRequest(t *testing.T)`
- `func TestOperationRegistryRejectsReusedIDWithDifferentRequest(t *testing.T)`
- `func TestOperationRegistryExpiresSuccessfulOperations(t *testing.T)`
- `func TestOperationRegistryDropsFailedOperation(t *testing.T)`
- `func TestOperationRegistryWaiterSeesCompletedResult(t *testing.T)`
- `func TestValidatorRejectsNonCurrentParent(t *testing.T)`
- `func TestValidatorRejectsBusyAndQueuedParent(t *testing.T)`
- `func TestValidatorRejectsRevisionWindowAndCompression(t *testing.T)`
- `func TestValidatorAcceptsEditableCurrentWindow(t *testing.T)`
- `func TestTargetResolverRejectsMissingStore(t *testing.T)`
- `func TestTargetResolverComputesRevisionAndParagraphCut(t *testing.T)`
- `func TestTargetResolverRejectsIneligibleTargets(t *testing.T)`
- `func TestTargetResolverWrapsLoadFailure(t *testing.T)`
- `func TestRollbackReturnsCauseWhenBothActionsSucceed(t *testing.T)`
- `func TestRollbackCombinesChildAndParentFailures(t *testing.T)`
- `func TestProcessStopsBeforeValidateWhenResolveFails(t *testing.T)`
- `func TestProcessStopsBeforeForkWhenValidateFails(t *testing.T)`
- `func TestProcessForksUnderTransitionAndStartsChild(t *testing.T)`
- `func TestProcessRollsBackWhenChildResumeFails(t *testing.T)`
- `func TestProcessRollsBackWhenSubmitFails(t *testing.T)`
- `func TestAnnotationBackfillsSeqAndMarksEditable(t *testing.T)`
- `func TestAnnotationExplainsDisabledReasons(t *testing.T)`

### process.go

- `func NewProcess(deps ProcessDeps) *ProcessApplication` — NewProcess constructs the edit/fork process.
- `func (application *ProcessApplication) Run(ctx context.Context, parentID string, request protocol.EditUserMessageForkRequest) (protocol.EditUserMessageForkResult, error)` — Run resolves, validates, forks, resumes, submits and rolls back on failure.

### registry.go

- `func (operation *Operation) Done() <-chan struct` — Done closes when the operation completes.
- `func (operation *Operation) Outcome() (model.EditUserMessageForkResult, error)` — Outcome returns the completed result and error. Callers must first wait for
- `func (operation *Operation) CompletedAt() time.Time` — CompletedAt returns the successful completion time.
- `func NewOperationRegistry(ttl time.Duration, now func() time.Time) *OperationRegistry` — NewOperationRegistry constructs the idempotency registry.
- `func (registry *OperationRegistry) Begin(parentID string, request model.EditUserMessageForkRequest) (*Operation, bool, error)` — Begin creates or reuses an operation.
- `func (registry *OperationRegistry) Finish(operation *Operation, result model.EditUserMessageForkResult, err error)` — Finish publishes one operation outcome.

### resolver.go

- `func NewTargetResolver(deps TargetResolverDeps) *TargetResolver` — NewTargetResolver constructs the target resolver.
- `func (resolver *TargetResolver) Resolve(parentID string, targetSeq uint64) (protocol.TranscriptEvent, uint64, uint64, error)` — Resolve returns target, fork cut and parent revision.

### rollback.go

- `func NewRollback(deps RollbackDeps) *RollbackApplication` — NewRollback constructs the rollback application.
- `func (application *RollbackApplication) Rollback(parentID, childID string, cause error) error` — Rollback removes child and restores parent while preserving cause.

### validator.go

- `func NewValidator(deps ValidatorDeps) *Validator` — NewValidator constructs the edit validator.
- `func (validator *Validator) Validate(parentID string, parentRevision, expectedRevision, targetSeq uint64) error` — Validate checks current-session, running, queue, revision, window and floor.
