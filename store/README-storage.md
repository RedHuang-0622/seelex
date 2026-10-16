# store/storage（根包分卷）

## 生态位

存储引擎底座：JSON v8 布局、模块 head 发布、fork 子树与历史迁移

覆盖：`json_layout*.go`、`module_heads*.go`、`fork_store*.go`、`router_storage*.go`；未归属文件由覆盖自检拦下。

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

- `func newStoreEngine(root string, settings storeroot.Settings) *storeEngine` — newStoreEngine 构造 会话存储布局引擎。root 不存在时延后到首个会话提交再创建
- `func (store *storeEngine) locks(key Key) *sessionModuleLocks` — locks 返回（必要时创建）该会话的模块锁与短临界区状态集合。
- `func (store *storeEngine) mu(key Key, mod storageModule) *sync.Mutex` — mu 返回指定会话 + 模块的锁（按会话分片；注册表短临界区，不参与 IO）。
- `func moduleForStackKind(kind StackKind) storageModule` — moduleForStackKind 返回栈 kind 的模块 head 枚举（S16：head 按 kind 分文件）。
- `func (locks *sessionModuleLocks) mutexFor(mod storageModule) *sync.Mutex`
- `func isLayoutSessionDir(sessionRoot string) bool` — layout 判定：目录内存在 metadata/guide.json = 新布局。
- `func (store *storeEngine) sessionRoot(key Key) string` — sessionRoot 返回会话 会话存储布局的物理根（与旧 manifest 布局同会话目录，
- `func (store *storeEngine) metadataDir(key Key) string`
- `func (store *storeEngine) modulePath(key Key, module storageModule) string`
- `func (store *storeEngine) guidePath(key Key) string`
- `func (store *storeEngine) ensureLayoutGuide(key Key) (layoutGuide, error)` — ensureLayoutGuide 创建会话 会话存储布局目录并写入 guide.json（幂等：已存在直接
- `func headChecksum(head moduleHeadFile) (string, error)` — headChecksum 计算模块 head 的校验指纹：对信封（不含 checksum 字段）
- `func (store *storeEngine) publishModuleHead(key Key, mod storageModule, commitID string, payload any, updatedAt time.Time) (string, error)` — publishModuleHead 原子发布模块 head（调用方持对应模块锁）。返回发布后
- `func (store *storeEngine) readModuleHeadFile(key Key, module storageModule) (moduleHeadFile, error)` — readModuleHeadFile 读模块 head（缺失返回 fs.ErrNotExist）——自愈读的
- `func (store *storeEngine) readModuleHeadFileLocked(key Key, module storageModule) (moduleHeadFile, error)` — readModuleHeadFileLocked 是自愈读的**持锁入口**：调用方必须已持该模块锁
- `func (store *storeEngine) readModuleHeadFileHeal(key Key, module storageModule, lockHeld bool) (moduleHeadFile, error)` — readModuleHeadFileHeal 是两条自愈读入口的共享实现；lockHeld 表示调用方已持
- `func (store *storeEngine) retryRawHeadOrCause(key Key, module storageModule, cause error) (moduleHeadFile, error)` — retryRawHeadOrCause 在模块锁不可得时重读一次：成功即返回，仍失败上报原始
- `func (store *storeEngine) retryHeadAfterLock(key Key, module storageModule) (moduleHeadFile, bool)` — retryHeadAfterLock 在拿到/持有模块锁后重读一次：writer 可能刚发布了合法
- `func (store *storeEngine) readModuleHeadFileRaw(key Key, module storageModule) (moduleHeadFile, error)` — readModuleHeadFileRaw 读一次模块 head 并校验 schema/session/module/checksum。
- `func (store *storeEngine) readHeadEnvelopeLenient(key Key, module storageModule) (moduleHeadFile, bool)` — readHeadEnvelopeLenient 宽容读 head 信封（自愈重建的字段取回通道）：只要求
- `func (store *storeEngine) carryModuleHeadCommitID(key Key, module storageModule) string` — carryModuleHeadCommitID 从（可能已损坏的）旧 head 信封原样取回提交凭据。
- `func (store *storeEngine) repairModuleHeadLocked(key Key, module storageModule, cause error) (moduleHeadFile, error)` — repairModuleHeadLocked 按数据文件重建模块 head 并原子发布（§2.0 规则 3 /
- `func (store *storeEngine) repairModuleHeadFromData(key Key, module storageModule) error` — repairModuleHeadFromData 按数据文件重建模块 head 并原子发布（S15）。
- `func (store *storeEngine) rebuildMessageHeadFromData(key Key) (messageHead, error)` — rebuildMessageHeadFromData 从 message/*.jsonl 分片重建 message head。
- `func (store *storeEngine) rebuildEventHeadFromData(key Key) (eventHeadRecord, error)` — rebuildEventHeadFromData 从 event/*.jsonl 分片重建 event head。
- `func (store *storeEngine) rebuildCompactHeadFromData(key Key) (compactHeadRecord, error)` — rebuildCompactHeadFromData 从 session/compact.jsonl 重建 compact head。
- `func moduleStackKindOf(module storageModule) StackKind` — moduleStackKindOf 反查栈模块对应的 kind。
- `func (store *storeEngine) rebuildStackKindHeadFromData(key Key, kind StackKind) (stackModuleHead, error)` — rebuildStackKindHeadFromData 从该 kind 的 active/history.jsonl 重建栈
- `func readCompactFrameRows(path string) ([]compactFrameRecord, error)` — readCompactFrameRows 读取 compact.jsonl 的完整帧行（崩溃残尾跳过）。
- `func decodeHeadPayload[T any](head moduleHeadFile) (T, error)` — decodeHeadPayload 把模块 head 的 payload 解码到 target。
- `func (store *storeEngine) commitModuleHead(key Key, mod storageModule, commitID string, payload any) error` — commitModuleHead 是通用模块提交：先确保布局，再原子发布模块 head。
- `func (store *storeEngine) commitContentModule(key Key, mod storageModule, payload any) error` — commitContentModule 整份替换型内容模块（system/checkpoint）写入：模块 head
- `func modulePayloadCommitID(mod storageModule, payload any) string` — modulePayloadCommitID 按模块 head payload 内容确定性推导凭据（S17/D13：
- `func readModuleHeadPayload[T any](store *storeEngine, key Key, module storageModule) (T, error)` — readModuleHeadPayload 读取通用模块 head 并解码 payload；缺失时返回
- `func readOptionalModuleHead[T any](store *storeEngine, key Key, module storageModule) (T, error)` — readOptionalModuleHead 读一个**可选**模块 head：head 还没写过 = 这个模块还没有任何
- `func readModuleHeadPayloadLocked[T any](store *storeEngine, key Key, module storageModule) (T, error)` — readModuleHeadPayloadLocked 是 readModuleHeadPayload 的持锁版本：调用方必须
- `func (store *storeEngine) deleteModule(key Key, module storageModule) error` — deleteModule 删除模块 head 与对应数据目录（短窗口崩溃/审计模拟用；

### router_storage.go

- `func NewRouterStorage(router *Router, sessionID string) *RouterStorage` — NewRouterStorage 构造 Router 冷存储适配器（绑定会话）。
- `func (s *RouterStorage) Append(ctx context.Context, messages []types.Message) error` — Append 把事件行追加进 message 通道（② 后端写路径）。v8 JSON 布局以
- `func (s *RouterStorage) ReadRange(ctx context.Context, offset, limit int) ([]types.Message, int, error)` — ReadRange 按 [offset, offset+limit) 读消息，返回区间与总数
- `func (s *RouterStorage) Count() int` — Count 返回已落库消息总数。

### router_storage_test.go

- `func TestRouterStorageLifecycle(t *testing.T)` — TestRouterStorageLifecycle 验证 RouterStorage 满足 lifecycle.Storage：
- `func strPtr(value string) *string`
