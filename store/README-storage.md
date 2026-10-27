# store/storage（根包分卷）

## 生态位

存储引擎底座：JSON v8 布局、模块 head 发布、fork 子树与历史迁移

覆盖：`json_layout*.go`、`module_heads*.go`、`fork_store*.go`、`router_storage*.go`；未归属文件由覆盖自检拦下。

M2 起，会话路径、guide、module head 信封/checksum 与按会话模块锁身份的唯一实现位于
[`internal/layout/README.md`](internal/layout/README.md)。本卷的 `module_heads.go`
只保留 head 领域重建策略与根 `storeEngine` 适配；README 下方旧索引中若仍列出已删除的
路径/lock helper，以 `internal/layout` 索引为准。

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### fork_store.go

- `func (store *storeEngine) registerSubagent(key Key, operationID string, info subagentInfo) error` — registerSubagent 把子代理登记进父会话第四栈（§2.4/§8.2、S18）：一次派发
- `func (store *storeEngine) updateSubagentStatus(key Key, subagentID, status string) error` — updateSubagentStatus 更新子代理条目状态；批内全完成（单条目即整批）后整批
- `func subagentItemID(subagentID string) string`
- `func (store *storeEngine) readSubagents(key Key) ([]subagentInfo, error)` — readSubagents 读取父会话子代理清单（active + 已归档，按 subagent 栈条目）。
- `func forkCommitID(childKey Key) string` — forkCommitID / subagentCommitID 将「一次逻辑操作」映射成幂等凭据（§2.0 规则 4、
- `func subagentCommitID(subagentID string) string`
- `func (store *storeEngine) forkSession(parentKey, childKey Key, fromSeq uint64) error` — forkSession 从父会话 fromSeq 深拷贝独立子会话（起点 ≥ watermark）。
- `func lastMessageID(rows []Event) string`
- `func (store *storeEngine) sessionExists(key Key) bool`
- `func (store *storeEngine) subagentRoot(parentKey Key, subagentID string) string` — subagentRoot 返回子代理子树根（主会话目录下 subagent_<hash>/）。
- `func (store *storeEngine) newSubagent(parentKey Key, subagentID string, fromSeq uint64) (*storeEngine, Key, error)` — newSubagent 创建子代理现场（同构子树 + 无独立 blob）。
- `func (store *storeEngine) deleteSession(key Key) error` — deleteSession 删除整个会话目录（Reset/归档语义；显式键路径）。目录删除后

### json_layout.go

- `func newJSONRepositoryWithLayout(root string, settings storeroot.Settings) (*jsonRepository, error)`
- `func (repository *jsonRepository) active(key Key) bool`
- `func (repository *jsonRepository) writeCommitLayout(key Key, commit Commit) error` — writeCommitLayout 是 WriteCommit 的 分支：
- `func (store *storeEngine) publishToolResultRefs(key Key, refs []string) error` — publishToolResultRefs 在全部结果文件写完后原子发布 refs 索引（S13：不再
- `func (store *storeEngine) readToolResultRefs(key Key) ([]string, error)` — readToolResultRefs 读取 refs 索引（缺失 = nil + fs.ErrNotExist；索引丢失时
- `func (repository *jsonRepository) writeCheckpointLayout(key Key, payload []byte) error`
- `func (repository *jsonRepository) readCheckpointLayout(key Key) ([]byte, error)`
- `func (repository *jsonRepository) writeSystemPromptLayout(key Key, snapshot systemPromptSnapshot) error`
- `func (repository *jsonRepository) readSystemPromptLayout(key Key) (systemPromptSnapshot, error)`
- `func (repository *jsonRepository) appendGoalAuditLayout(key Key, entry GoalAuditEntry) error` — appendGoalAuditLayout 把 goal 审计条目写进 EVENT 通道（S19：GoalAudit 权威
- `func (repository *jsonRepository) readGoalAuditLayout(key Key) ([]GoalAuditEntry, error)` — readGoalAuditLayout 读取 EVENT 通道中的 goal.* 审计条目。
- `func (repository *jsonRepository) sessionMeta(key Key) (frameworkStorage.SessionMeta, bool)` — sessionMeta 返回该会话的枚举 meta（message head.Meta；S20 会话枚举面）。
- `func (repository *jsonRepository) hasDraft(key Key) bool` — hasDraft 报告会话是否存在未发送草稿（§2.5.4 草稿行判据，S20）。
- `func (repository *jsonRepository) writeSessionDisplayMetaLayout(projectID string, payload []byte) error` — writeSessionDisplayMetaLayout / readSessionDisplayMetaLayout 是项目级会话
- `func (repository *jsonRepository) readSessionDisplayMetaLayout(projectID string) ([]byte, error)`
- `func (repository *jsonRepository) derivedRecordPayload(key Key) ([]byte, error)` — derivedRecordPayload 按 §2.5.4 派生 SessionRecord 形状的 JSON（version/id/
- `func derivedConversationMessages(rows []Event) []ConversationMessage` — derivedConversationMessages 把 message 事件行派生为 conversation 消息，
- `func derivedToolCallMessageID(rowID string, index int) string` — derivedToolCallMessageID 给同一行派生出的调用消息分配稳定且唯一的 ID。
- `func (repository *jsonRepository) assembleWireWorkspace(key Key, budget, k int) ([]types.Message, bool, error)` — assembleWireWorkspace 对 会话执行 wire 装配（frame 摘要 + tail + 最近 K
- `func wireToTypesMessages(wire []wireMessage) []types.Message` — wireToTypesMessages 把 R2 wire 消息映射为 provider types.Message。
- `func (repository *jsonRepository) commitCompactFrameWorkspace(key Key, frame CompactFrame) (bool, error)` — commitCompactFrameWorkspace 把运行期 compact 帧桥接进 compact 通道
- `func (repository *jsonRepository) resolveCompactRange(key Key, frame CompactFrame) (uint64, uint64, bool)` — resolveCompactRange 把 ChatQueue 单元索引 [frame.From, frame.To] 映射为
- `func (repository *jsonRepository) retentionAdvisoryWorkspace(key Key) (RetentionAdvisory, error)` — retentionAdvisoryWorkspace 返回 会话 retention 水位建议（compact 帧数
- `func rawBytesEstimate(head messageHead, alert uint64) uint64`
- `func (repository *jsonRepository) lruDeleteWorkspace(key Key, upToSeq uint64, confirmed bool) (bool, error)` — lruDeleteWorkspace 用户确认后的 LRU 前缀删除（会话）。
- `func (repository *jsonRepository) lifecycleRecoverWorkspace(key Key) (int, bool, error)` — lifecycleRecoverWorkspace 重启恢复 lifecycle 队列（发送未确认项回
- `func messagesToEventRows(messages []types.Message) []Event` — messagesToEventRows 把 provider history（无事件通道的兼容写）转成事件行。
- `func strOrEmpty(value *string) string`
- `func rowsToProviderMessages(rows []Event) []types.Message` — rowsToProviderMessages 只把完整协议单元行映射为 provider messages
- `func (repository *jsonRepository) readAllLayoutMessages(key Key) ([]types.Message, error)` — readAllLayoutMessages 全量读由 message 事件行派生（S11：history.json
- `func (repository *jsonRepository) readRangeLayout(key Key, offset, limit int) ([]types.Message, int, error)`
- `func (repository *jsonRepository) readEventTailLayout(key Key, tokenBudget, maxUnits int) ([]Event, error)`
- `func (repository *jsonRepository) currentGenerationLayout(key Key) (string, error)`
- `func readMetaFromDir(sessionRoot string) (frameworkStorage.SessionMeta, bool)` — readMetaFromDir 读取 会话目录枚举 meta（message head.Meta；显式目录
- `func (repository *jsonRepository) listMeta(projectID string) []frameworkStorage.SessionMeta` — listMeta 返回项目内 会话的枚举 meta。

### module_heads.go

- `func newStoreEngine(root string, settings storeroot.Settings) *storeEngine` — 构造 JSON 会话引擎并持有 layout 机制实例。
- `func (store *storeEngine) locks(key Key) *sessionModuleLocks` — 返回会话短临界区缓存；模块锁已下沉 layout。
- `func (store *storeEngine) mu(key Key, mod storageModule) *sync.Mutex` — 校验模块登记并委托 layout 的稳定锁身份。
- `func moduleForStackKind(kind StackKind) storageModule` / `moduleStackKindOf(module storageModule) StackKind` — 栈 kind 与 module ID 的唯一映射。
- `func validStorageModule(mod storageModule) bool` — 显式拒绝未登记模块，禁止静默锁别名。
- `func isLayoutSessionDir(sessionRoot string) bool` — 委托 layout 判定 JSON v8 guide。
- `func (store *storeEngine) sessionRoot/metadataDir/modulePath/guidePath(key Key) string` — 根适配器；物理派生唯一实现位于 `store/internal/layout`。
- `func layoutScope(key Key) storelayout.Scope` — 把根 Key 映射为无业务布局 Scope。
- `func (store *storeEngine) ensureLayoutGuide(key Key) (layoutGuide, error)` — 委托 layout 幂等发布 guide。
- `func headChecksum(head moduleHeadFile) (string, error)` / `publishModuleHead` / `readModuleHeadFile*` — 根测试与兼容入口委托 layout head 原语。
- `func (store *storeEngine) repairModuleHead*` — 根领域层按 message/event/compact/stack 数据文件重建 head；layout 不解释 payload。
- `func (store *storeEngine) rebuildMessageHeadFromData/rebuildEventHeadFromData/rebuildCompactHeadFromData/rebuildStackKindHeadFromData` — 各领域 head 重建策略。
- `func decodeHeadPayload[T any](head moduleHeadFile) (T, error)` — 通用 payload 解码。
- `func (store *storeEngine) commitModuleHead/commitContentModule/deleteModule` — 根模块生命周期适配入口。
- `func modulePayloadCommitID(mod storageModule, payload any) string` — 按 payload 内容派生确定性提交凭据。
- `func readModuleHeadPayload/readOptionalModuleHead/readModuleHeadPayloadLocked` — 通用 head payload 读面。
### router_storage.go

- `func NewRouterStorage(router *Router, sessionID string) *RouterStorage` — NewRouterStorage 构造 Router 冷存储适配器（绑定会话）。
- `func (s *RouterStorage) Append(ctx context.Context, messages []types.Message) error` — Append 把事件行追加进 message 通道（② 后端写路径）。v8 JSON 布局以
- `func (s *RouterStorage) ReadRange(ctx context.Context, offset, limit int) ([]types.Message, int, error)` — ReadRange 按 [offset, offset+limit) 读消息，返回区间与总数
- `func (s *RouterStorage) Count() int` — Count 返回已落库消息总数。

### router_storage_test.go

- `func TestRouterStorageLifecycle(t *testing.T)` — TestRouterStorageLifecycle 验证 RouterStorage 满足 lifecycle.Storage：
- `func strPtr(value string) *string`
