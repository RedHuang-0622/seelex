# core/input（根包分卷）

## 生态位

输入分派与路由兼容测试

覆盖：`input*.go` + 显式名单（见生成器 `ROOT_GROUPS`）；未归属文件由覆盖自检拦下。

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### durable_queue_wire_test.go

- `func (s *queueRecordingSessions) QueueEnqueueWorkspace(_, _, _, content string) error`
- `func (s *queueRecordingSessions) QueueMarkConsumedWorkspace(_, _, turnID string) error`
- `func (s *queueRecordingSessions) QueueConfirmConsumedWorkspace(_, _, turnID string) error`
- `func (s *queueRecordingSessions) QueueFailConsumedWorkspace(_, _, turnID string) error`
- `func (s *queueRecordingSessions) QueueRecoverItemsWorkspace(_, _ string) (sessionstore.QueueRecoveryReport, bool, error)`
- `func (s *queueRecordingSessions) queueCalls() (enqueued, marked, confirmed, failed []string)`
- `func (*snapshotFailingSessions) SaveSessionSnapshot( string, []contract.EngineMessage, model.SessionRecord, []model.TranscriptEvent, []model.StoredToolResult, ) error`
- `func (*snapshotFailingSessions) SaveSessionSnapshotWorkspace( string, string, []contract.EngineMessage, model.SessionRecord, []model.TranscriptEvent, []model.StoredToolResult, ) error`
- `func waitQueueCalls(t *testing.T, sessions interface { queueCalls() (enqueued, marked, confirmed, failed []string) }, ready func(enqueued, marked, confirmed, failed []string) bool)`
- `func newBlockingEngine() *sessionBackedBlockingEngine`
- `func TestDurableQueueMirrorsQueuedInputAndConfirmsTurn(t *testing.T)` — TestDurableQueueMirrorsQueuedInputAndConfirmsTurn 运行中提交 → 镜像落盘；
- `func TestDurableQueueReturnsConsumedContentToDraftOnPersistFailure(t *testing.T)` — TestDurableQueueReturnsConsumedContentToDraftOnPersistFailure 快照落盘失败
- `func TestDurableQueueBackfillVisibleAndDrainedOnNextSubmit(t *testing.T)` — TestDurableQueueBackfillVisibleAndDrainedOnNextSubmit 冷加载恢复的输入

### input.go

- `func (service *Service) submitCommand(ctx context.Context, input string) error`
- `func (service *Service) submitSkill(ctx context.Context, name string, args []string, input string) error`
- `func (service *Service) submitPluginSwitch(ctx context.Context, name string) error` — submitPluginSwitch 是 `#` 前缀的落点：切换/停用插件。
- `func (service *Service) endSkill() error`
- `func (service *Service) activateSkillAndSubmit(ctx context.Context, skill SkillInfo, args []string, input string) error`
- `func (service *Service) prepareCompletedTaskBoundary()`
- `func (service *Service) applySkill(skill SkillInfo)`

### input_router_compat_test.go

- `func TestNewAssemblesInputRouter(t *testing.T)`

### input_unknown_command_notice_test.go

- `func TestUnknownCommandNoticeSeparatesToolsFromCommands(t *testing.T)` — 未知命令 / 帮助文案的服务发现一致性。
- `func TestUnknownCommandSubmitKeepsNoticeNotError(t *testing.T)` — TestUnknownCommandSubmitKeepsNoticeNotError：未知命令仍然不是错误（照常回
- `func TestHelpStatesPanelListsOnlyExecutableEntries(t *testing.T)` — TestHelpStatesPanelListsOnlyExecutableEntries：`/help` 的口径必须与建议面板一致——
