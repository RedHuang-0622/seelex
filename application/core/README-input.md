# core/input（根包分卷）

## 生态位

输入分派与路由兼容测试

覆盖：`input*.go` + 显式名单（见生成器 `ROOT_GROUPS`）；未归属文件由覆盖自检拦下。

## 文件引用的输入口径（2026-10-12 J-1b）

`@文件` / composer 的 `+` 加号 / 右栏工作树拖拽**三个入口 = 同一条工作区相对路径引用**，
**引用不是内容、不是字节**：提交后正文里就是路径文本，接到它的 agent 自己用
`read_file` / `grep` 去读（权限走既有只读组规则）。因此：

- **`@` 不是输入前缀**（`completion.go` 的 `sigils` 表只有 `/` `#` `$`，见文件头前缀契约
  注释里 J-1b 的"为什么"）：`@src/app.js 看看这里` 是**普通对话**，走
  `SubmitToSession` + 显式视图会话 ID，不是路由分支；
- 候选与渲染全在前端（`gui/frontend/dist/file-reference.js`）：候选来自已加载的工作区树
  （`Bridge.WorkspaceTree`，不逐键读盘），渲染是同一枚 chip 的两种宿主（composer 草稿区 /
  对话正文）；三入口共用 `file-drop.js` 的 `normalizeDropPath`（绝对路径 / `..` / 超长一律拒绝）；
- **不新增 `reader` 角色、不改 `roles`、不搬内容字节、不加能力声明**：一轮问的"多模态是否
  需要新增 role"的最终答案是"不变"，记录见
  `docs/2026-10-12-frontend-consistency-and-fixes/README.md` §3 J-1b；
- 这条口径必须让 agent 知道（否则出现"给了路径却没去读"的空转），提示词落在
  `internal/promptassets/assets/system/instructions.md`。

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### durable_queue_wire_test.go

- `func (s *queueRecordingSessions) QueueEnqueueWorkspace(_, _, _, content string) error`
- `func (s *queueRecordingSessions) QueueMarkConsumedWorkspace(_, _, turnID string) error`
- `func (s *queueRecordingSessions) QueueConfirmConsumedWorkspace(_, _, turnID string) error`
- `func (s *queueRecordingSessions) QueueFailConsumedWorkspace(_, _, turnID string) error`
- `func (s *queueRecordingSessions) QueueRecoverItemsWorkspace(_, _ string) (store.QueueRecoveryReport, bool, error)`
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
