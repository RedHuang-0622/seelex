# store/session（根包分卷）

## 生态位

会话句柄与上下文状态：Router/SessionGranularStore、历史读面与运行期 API

覆盖：`sessionstore*.go`、`session_granular*.go`、`session_context*.go`、`session_meta*.go`、`durable_history*.go`、`runtime_api*.go`、`conversation*.go`、`history_resume_readers*.go`；未归属文件由覆盖自检拦下。

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### conversation.go

- `func decodeConversationRange(payload []byte, sessionID string, offset, limit int) ([]ConversationMessage, int, error)` — decodeConversationRange 从 state blob 只解析 conversation 模块（不解析
- `func (repository *jsonRepository) ReadConversationRange(_ context.Context, key Key, offset, limit int) ([]ConversationMessage, int, error)`

### conversation_derivation_test.go

- `func derivedRowsFixture() []Event` — derivedRowsFixture 复刻真实长会话的 message 行形状：一条 assistant 行里
- `func TestDerivedConversationKeepsMessageOrder(t *testing.T)` — TestDerivedConversationKeepsMessageOrder 是长会话恢复顺序的契约测试：派生

### conversation_test.go

- `func TestConversationRangeFromMessageRows(t *testing.T)` — TestConversationRangeFromMessageRows 契约测试：conversation 模块范围读取
- `func TestConversationRangeDerivedFromMessageRows(t *testing.T)` — TestConversationRangeDerivedFromMessageRows 验证 S20 后 conversation 由
- `func TestStateChannelRetiredOnJSON(t *testing.T)` — TestStateChannelRetiredOnJSON 验证 S20：JSON v8 的 state/record 通道停写
- `func TestDecodeConversationRangeSkipsNonConversationModules(t *testing.T)` — TestDecodeConversationRangeSkipsNonConversationModules 验证部分解码结果

### durable_history.go

- `func IsSessionNotFound(err error) bool` — IsSessionNotFound 判断「会话不存在」错误——**全仓唯一判据**。会话恢复的语义是
- `func NewDurableHistory(router *Router, sessionID string) *DurableHistory` — NewDurableHistory 创建绑定到指定会话的持久化适配器。
- `func (d *DurableHistory) SetWorkspaceResolver(resolver func() string)` — SetWorkspaceResolver 注入会话绑定 workspace 解析器：DurableHistory 的
- `func (d *DurableHistory) workspace() string`
- `func (d *DurableHistory) SetTailBudget(tokenBudget, maxUnits int)` — SetTailBudget 注入滑动窗口读尾预算（plan.md §3.7.2：Load 只装载窗口
- `func (d *DurableHistory) SetGapCoverer(coverer GapCoverer)` — SetGapCoverer 注入真空区覆盖回调：每次尾窗 Load 时携带完整事件流与尾窗
- `func (d *DurableHistory) SessionID() string` — SessionID 返回绑定的会话 ID。
- `func (d *DurableHistory) PrepareNextLoad(messages []types.Message)` — PrepareNextLoad hands the application-assembled bounded history to the
- `func (d *DurableHistory) Load(ctx context.Context) ([]types.Message, error)` — Load 读取 ProviderHistory 消息。配置了窗口预算（SetTailBudget）时按
- `func eventsToMessages(events []Event) []types.Message` — eventsToMessages 把完整协议单元事件流转为 types.Message（窗口读尾的
- `func providerRoleForEvent(event Event) string` — providerRoleForEvent 把存储事实映射为 provider 可见 role（AT9）：
- `func strPtrOrNil(value string) *string`
- `func providerContentOrContent(event Event) string` — providerContentOrContent 返回事件在 provider wire 上的真实正文：
- `func (d *DurableHistory) Save(ctx context.Context, messages []types.Message) error` — Save 编排 ProviderHistory 持久化；若存在会话上下文存储则同步持久化
- `func (d *DurableHistory) loadAllEvents(ctx context.Context) ([]Event, error)` — loadAllEvents 读取会话完整事件流（MaxInt 双限 = selectEventTail 无截断，
- `func (d *DurableHistory) LoadEventTail(_ context.Context, tokenBudget, maxUnits int) ([]Event, error)` — LoadEventTail 按 token 预算 + 最大轮数读取最新完整协议单元（滑动窗口读，
- `func (d *DurableHistory) Clear(_ context.Context) error` — Clear 是 Reset 语义：删除会话的 ProviderHistory 与状态 blob。
- `func (d *DurableHistory) AttachStateStore(store *SessionContextStore)` — AttachStateStore 绑定会话上下文存储，使 Save 时同步编排 state blob。

### durable_history_test.go

- `func newTestRouter(t *testing.T) *Router`
- `func TestDurableHistoryRoundTrip(t *testing.T)`
- `func TestDurableHistoryClearIsResetSemantics(t *testing.T)`
- `func TestDurableHistorySaveOrchestratesStateBlob(t *testing.T)`
- `func TestDurableHistoryLoadEventTailKeepsWindowUnits(t *testing.T)`
- `func TestDurableHistoryLoadUsesWindowTailBudget(t *testing.T)` — TestDurableHistoryLoadUsesWindowTailBudget 验证滑动窗口加载区间（D1）：
- `func TestDurableHistoryLoadInvokesGapCoverer(t *testing.T)` — TestDurableHistoryLoadInvokesGapCoverer 验证真空区覆盖钩子：尾窗 Load 时
- `func TestDurableHistoryGapCovererErrorIsIgnored(t *testing.T)` — TestDurableHistoryGapCovererErrorIsIgnored 覆盖失败不阻断 Load（保守回退）。
- `func TestDurableHistoryNilRouterIsInMemory(t *testing.T)`
- `func TestDurableHistoryPreparedLoadWinsOnce(t *testing.T)`

### history_resume_readers.go

- `func (store *storeEngine) pageHistoryRows(key Key, offset, limit int) ([]historyReadRow, int, error)` — pageHistoryRows 返回 message 分页（offset/limit 基于行坐标 1..last_seq；
- `func (store *storeEngine) resumeTail(key Key) (resumePoint, error)` — resumeTail 定位断点并返回尾段。不读 EVENT 也能正确恢复（EVENT 只加速
- `func (store *storeEngine) findOpenTail(key Key) openTailInfo` — findOpenTail 从最后一个 user 单元后扫描（无 user 则从首行）判断流尾

### runtime_api.go

- `func (router *Router) jsonRepositoryLocked() (*jsonRepository, bool)` — jsonRepositoryLocked 返回当前 repository（若为 JSON 会话存储布局）。
- `func (router *Router) jsonRepository() (*jsonRepository, bool)` — jsonRepository 在 RLock 下返回当前 repository 快照（若为 JSON 会话存储
- `func (router *Router) AssembleWireWorkspace(projectID, sessionID string, budget, k int) ([]types.Message, bool, error)` — AssembleWireWorkspace 对会话执行 wire 装配（frame 摘要 + tail + 最近 K
- `func (router *Router) CommitCompactFrameWorkspace(projectID, sessionID string, frame CompactFrame) (bool, error)` — CommitCompactFrameWorkspace 把运行期压缩帧写入 compact 通道（帧摘要
- `func (router *Router) RetentionAdvisoryWorkspace(projectID, sessionID string) (RetentionAdvisory, error)` — RetentionAdvisoryWorkspace 返回会话 retention 水位建议（v8；非 v8 返回
- `func (router *Router) LRUDeleteWorkspace(projectID, sessionID string, upToSeq uint64, confirmed bool) (bool, error)` — LRUDeleteWorkspace 用户确认后删除 watermark 前连续前缀（v8；manual 模式
- `func (router *Router) LifecycleRecoverWorkspace(projectID, sessionID string) (int, bool, error)` — LifecycleRecoverWorkspace 重启恢复 lifecycle 队列（发送未确认项回
- `func (router *Router) PendingMessageTailWorkspace(projectID, sessionID string) (PendingTailReport, bool, error)` — PendingMessageTailWorkspace 探测 message 通道的草稿尾部（seq > head
- `func (router *Router) RecoverPendingMessageTailWorkspace(projectID, sessionID string) (PendingTailReport, bool, error)` — RecoverPendingMessageTailWorkspace 显式恢复草稿尾部（L2）：基座一致时
- `func (router *Router) DiscardPendingMessageTailWorkspace(projectID, sessionID string) (PendingTailReport, bool, error)` — DiscardPendingMessageTailWorkspace 显式清理草稿尾部（等价于提交路径的
- `func (router *Router) QueueEnqueueWorkspace(projectID, sessionID, requestID, content string) error` — QueueEnqueueWorkspace 把一条排队输入镜像落盘（durable queue 写入侧；
- `func (router *Router) QueueMarkConsumedWorkspace(projectID, sessionID, turnID string) error` — QueueMarkConsumedWorkspace 把 lifecycle 队列中全部待发送项标记为被 turnID
- `func (router *Router) QueueConfirmConsumedWorkspace(projectID, sessionID, turnID string) error` — QueueConfirmConsumedWorkspace 该轮 message 已发布 → 消费项出队。
- `func (router *Router) QueueFailConsumedWorkspace(projectID, sessionID, turnID string) error` — QueueFailConsumedWorkspace 该轮未发布（失败）→ 消费项内容回草稿并出队。
- `func (router *Router) QueueItemsWorkspace(projectID, sessionID string) ([]QueueItem, bool, error)` — QueueItemsWorkspace 读 lifecycle 队列投影（UI/诊断）。
- `func (router *Router) QueueRecoverItemsWorkspace(projectID, sessionID string) (QueueRecoveryReport, bool, error)` — QueueRecoverItemsWorkspace 重启恢复队列并返回条目级结果（重发项/出队项/
- `func (router *Router) StackPush(projectID, sessionID string, kind StackKind, batchID string, items []StackItemInput) (StackMutation, error)` — StackPush 把一批条目压入指定栈（batchID 为空 = 本次入栈自成一批）。
- `func (router *Router) StackSetStatus(projectID, sessionID string, kind StackKind, itemID, status string) (StackMutation, error)` — StackSetStatus 更新栈条目状态；所属批次全部完成时整批弹栈归档。
- `func (router *Router) StackPopTop(projectID, sessionID string, kind StackKind, itemID, status string) (StackMutation, error)` — StackPopTop 弹出栈顶条目（goal LIFO）。
- `func (router *Router) StackReplace(projectID, sessionID string, kind StackKind, items []StackItemInput, closedStatus string) (StackMutation, error)` — StackReplace 用新的条目集合替换 active 栈，并推导出逐条迁移。
- `func (router *Router) stackMutate(projectID, sessionID string, kind StackKind, mutate func(*stackState) (StackMutation, error)) (StackMutation, error)` — stackMutate 是栈通道的统一写入口：变更以闭包（actor 消息）交给当前后端的
- `func (router *Router) StackActive(projectID, sessionID string, kind StackKind) ([]StackItemRecord, error)` — StackActive 返回指定栈当前 active 条目（按 seq 升序）。
- `func (router *Router) StackHistory(projectID, sessionID string, kind StackKind) ([]StackItemRecord, error)` — StackHistory 返回指定栈的归档条目。
- `func (router *Router) StackVerify(projectID, sessionID string) error` — StackVerify 校验栈通道 head 水位与数据计数一致（M5 巡检的栈通道部分）。
- `func (router *Router) StackStorageStats() (stackJournalStats, error)` — StackStorageStats 返回栈通道的延迟归因：栈锁等待与 active / history / head /
- `func (router *Router) ForkStacks(parentProjectID, parentSessionID, childProjectID, childSessionID string, fromSeq uint64) error` — ForkStacks 按 message 锚把父会话三栈重建进子会话（§8.1 + T-FK-02）。

### runtime_api_inflight_test.go

- `func testInFlightOps(router *Router) int` — testInFlightOps 读取在途 repository 操作计数（Configure/Close 的等待依据）。
- `func TestJSONRuntimeEntryRegistersInFlightOp(t *testing.T)` — TestJSONRuntimeEntryRegistersInFlightOp 钉住 §2.13：运行期「仅 JSON 布局」
- `func TestRuntimeEntriesKeepOKFalseOnClosedRouter(t *testing.T)` — TestRuntimeEntriesKeepOKFalseOnClosedRouter 是 §2.13 的**口径锁**：

### session_context.go

- `func NewSessionContextStore(router *Router, sessionID string) *SessionContextStore` — NewSessionContextStore 创建会话上下文存储（惰性加载）。
- `func (s *SessionContextStore) SetWorkspaceResolver(resolver func() string)` — SetWorkspaceResolver 注入会话绑定 workspace 解析器（PushCompact 的 v8
- `func (s *SessionContextStore) workspace() string` — workspace 返回本会话的落盘/回读项目键（R3 键漂移纪律）。
- `func (s *SessionContextStore) Router() *Router` — Router 返回绑定的事件/历史路由（DurableHistory 装配接缝：滑动窗口加载
- `func (s *SessionContextStore) Load(ctx context.Context) error` — Load 装载会话上下文：blob（system/skill/compact/audit）+ 三栈（§2.4 通道，
- `func (s *SessionContextStore) loadBlob(_ context.Context) error` — loadBlob 从 context blob 读取记录到内存缓存。损坏或版本不兼容显式失败。
- `func (s *SessionContextStore) Persist(ctx context.Context) error` — Persist 把内存缓存写入 context blob。三栈不在此落盘（权威只有 §2.4 栈
- `func (s *SessionContextStore) ResetCache()` — ResetCache 清空内存缓存与记录（Clear/Reset 语义配合）。
- `func (s *SessionContextStore) SetSystemPrompt(prompt string) error` — SetSystemPrompt 设置会话级基础提示。
- `func (s *SessionContextStore) SystemPrompt() string` — SystemPrompt 返回当前会话级基础提示。
- `func (s *SessionContextStore) Snapshot() SessionContextRecord` — Snapshot 返回记录的内存拷贝（供 Assembler 渲染，不暴露内部状态）。
- `func (s *SessionContextStore) writeStack(kind StackKind, mutate func() error) error` — writeStack 把一次栈变更写进通道并回读该 kind 的 active 投影。
- `func (s *SessionContextStore) refreshStack(kind StackKind) error` — refreshStack 从栈通道回读指定 kind 的 active 条目覆盖内存投影。
- `func stackPayload(frame any) json.RawMessage` — stackPayload 编码栈帧为通道 payload。
- `func (s *SessionContextStore) PushPlan(frame PlanFrame) error` — PushPlan 在 plan_load/plan_run 进入时压栈（一个计划 = 一个批次）。
- `func (s *SessionContextStore) CloseTopPlan(planID string) error` — CloseTopPlan 关闭栈顶匹配的 Plan 帧；该计划批次随即整批归档（§2.4）。
- `func (s *SessionContextStore) PushTask(frame TaskFrame) error` — PushTask 在任务开始时压栈（一个任务 = 一个批次）。
- `func (s *SessionContextStore) CloseTopTask(taskID string) error` — CloseTopTask 关闭栈顶匹配的任务帧（终态工具接受后）。
- `func (s *SessionContextStore) ensureStackItem(kind StackKind, itemID, label string) error` — ensureStackItem 校验条目确实在该 kind 的 active 栈上（保持旧的显式失败语义）。
- `func (s *SessionContextStore) PushGoal(frame GoalFrame) error` — PushGoal 在 goal_begin 压栈时追加一帧（goal 治理活栈，单条目批次 LIFO）。
- `func (s *SessionContextStore) CloseTopGoal(goalID string) error` — CloseTopGoal 弹掉栈顶 goal 帧并归档（goal 是 LIFO：只有栈顶可收口/放弃；
- `func (s *SessionContextStore) GoalStackSnapshot() []GoalFrame` — GoalStackSnapshot 返回 goal 栈的深拷贝（供 goal.Controller Reload 使用）。
- `func (s *SessionContextStore) ReplaceGoalStack(frames []GoalFrame) error` — ReplaceGoalStack 用当前栈投影替换 goal active 栈：通道按逐条迁移落地
- `func (s *SessionContextStore) AppendGoalAudit(entry GoalAuditEntry) error` — AppendGoalAudit 追加一条 goal 审计条目：Seq 由本会话账本单调递增
- `func (s *SessionContextStore) GoalAuditSnapshot() []GoalAuditEntry` — GoalAuditSnapshot 返回本会话 goal 审计账本的深拷贝（按 Seq 追加顺序；
- `func (s *SessionContextStore) PushSkill(frame SkillFrame) error` — PushSkill 在 skill 激活时压栈。
- `func (s *SessionContextStore) PopSkill(skillID string) error` — PopSkill 在 skill 退出时弹栈（匹配最近一帧）。
- `func (s *SessionContextStore) PushCompact(frame CompactFrame) error` — PushCompact 在窗口外压缩时压入摘要帧。
- `func (s *SessionContextStore) bridgeCompactFrame(frame CompactFrame)` — bridgeCompactFrame 把运行期压缩帧写进 compact 通道（S19：单源写入；
- `func (s *SessionContextStore) mutate(apply func(*SessionContextRecord) error) error` — mutate 在 s.mu 内执行栈操作，**不落盘**：锁内只允许改内存（§2.8——落盘
- `func (s *SessionContextStore) update(apply func(*SessionContextRecord) error) error` — update 在加锁下执行栈操作并持久化 state blob。
- `func cloneSessionContextRecord(record SessionContextRecord) SessionContextRecord`
- `func cloneGoalFrames(frames []GoalFrame) []GoalFrame`
- `func cloneGoalAudit(entries []GoalAuditEntry) []GoalAuditEntry`

### session_context_lock_test.go

- `func TestPushCompactBridgesOutsideStoreLock(t *testing.T)` — TestPushCompactBridgesOutsideStoreLock 钉住 §2.8 的两条性质：

### session_context_scope_drift_repro_test.go

- `func TestSessionContextStoreNeverFollowsActiveWriteScope(t *testing.T)` — ── 错误复现：会话上下文的落盘键跟随「活跃写作用域」────────────────────────
- `func pathExists(path string) bool` — pathExists 报告路径是否存在（文件或目录）。

### session_context_test.go

- `func TestSessionContextStoreStackLifecycle(t *testing.T)`
- `func TestSessionContextStorePersistsAcrossInstances(t *testing.T)`
- `func TestSessionContextStoreRejectsUnsupportedSchemaVersion(t *testing.T)`
- `func TestSessionContextStoreRejectsCorruptState(t *testing.T)`
- `func TestSessionContextStoreValidation(t *testing.T)`
- `func TestSessionContextStoreSystemPromptInvariant(t *testing.T)`
- `func TestSessionContextRecordJSONShape(t *testing.T)`
- `func TestGoalStackPersistReload(t *testing.T)` — TestGoalStackPersistReload 验证 goal 第五栈持久化/恢复：Begin → 落盘 →
- `func TestGoalStackSessionScopedIsolation(t *testing.T)` — TestGoalStackSessionScopedIsolation 验证 goal 栈按会话隔离：两会话各自
- `func TestGoalStackLiveStackStartsAndEndsEmpty(t *testing.T)` — TestGoalStackLiveStackStartsAndEndsEmpty 固化第五栈"活栈投影"语义：
- `func TestGoalStackLegacySchemaRejected(t *testing.T)` — TestGoalStackLegacySchemaRejected 验证「旧链路不兼容」：旧 schema 版本的
- `func TestGoalAuditAppendOnlySessionScoped(t *testing.T)` — TestGoalAuditAppendOnlySessionScoped 验证 goal 审计账本按会话隔离且只
- `func TestCompactFrameRangeFieldsPersist(t *testing.T)` — TestCompactFrameRangeFieldsPersist 验证 CompactFrame 正式定位字段
- `func TestCompactFrameIndexAndChainFieldsPersist(t *testing.T)` — TestCompactFrameIndexAndChainFieldsPersist 验证 2026-09-06 压缩 DAG 详设
- `func TestPushCompactIndexAndChainValidation(t *testing.T)` — TestPushCompactIndexAndChainValidation 验证详设 §3.4 追加校验：

### session_granular.go

- `func NewSessionGranularStore(router *Router) *SessionGranularStore` — NewSessionGranularStore 构造会话粒度存储；router 为 nil 时所有方法
- `func (store *SessionGranularStore) SetWorkspaceResolver(resolver func(sessionID string) string)` — SetWorkspaceResolver 注入会话绑定项目解析器（wiring/bootstrap 装配点：从
- `func (store *SessionGranularStore) resolver() func(string) string` — resolver 返回注入的会话绑定解析器（可能为 nil）。回调在锁外调用。
- `func (store *SessionGranularStore) SetProjectSource(source func() []string)` — SetProjectSource 注入应用已知项目列表（wiring/bootstrap 装配点：workspace.Repo.List），
- `func (store *SessionGranularStore) projectList() func() []string` — projectList 返回注入的项目来源（可能为 nil）。回调在锁外调用。
- `func (store *SessionGranularStore) projectID(projectID string) string`
- `func (store *SessionGranularStore) SaveSession(projectID string, record Record) error` — SaveSession 原子写会话记录（幂等：同键重写不漂移；B6）。
- `func (store *SessionGranularStore) SaveRecordRaw(projectID, sessionID string, payload []byte) error` — SaveRecordRaw 以会话粒度原子写 record 通道原始字节（state.json；
- `func (store *SessionGranularStore) SaveSessionTitle(projectID, sessionID, title string) error` — SaveSessionTitle 写会话标题（v8：message head 的目录枚举面
- `func (store *SessionGranularStore) SessionTitle(projectID, sessionID string) (string, bool, error)` — SessionTitle 只读会话标题（header-only：只打开 metadata/message.json，
- `func (store *SessionGranularStore) FirstUserInputs(projectID, sessionID string, limit int) ([]string, bool, error)` — FirstUserInputs 读取会话最早的若干条用户输入正文（标题回填用；有界读：
- `func (store *SessionGranularStore) LoadRecordRaw(projectID, sessionID string) ([]byte, error)` — LoadRecordRaw 读取 record 通道原始字节；不存在原样返回 fs.ErrNotExist
- `func (store *SessionGranularStore) sessionIndexed(projectID, sessionID string) bool` — sessionIndexed 报告会话是否已出现在项目索引（物理实现 = Router
- `func (store *SessionGranularStore) EnsureIndexed(projectID, sessionID string) error` — EnsureIndexed 在项目索引尚无该会话时生成一次空 commit（manifest/meta），
- `func (store *SessionGranularStore) LoadSession(projectID, sessionID string) (Record, bool, error)` — LoadSession 读取会话记录；不存在返回 (zero, false, nil)。
- `func (store *SessionGranularStore) History(sessionID string) *DurableHistory` — History 返回会话的框架工作历史句柄（session:<id>:history）。
- `func (store *SessionGranularStore) HistoryForProject(projectID, sessionID string) *DurableHistory` — HistoryForProject 返回绑定到显式项目作用域的会话历史句柄（会话粒度
- `func (store *SessionGranularStore) ResolveProjectForSession(sessionID string) string` — ResolveProjectForSession 返回会话归属项目，定序：
- `func (store *SessionGranularStore) SaveHistory(projectID, sessionID string, messages []types.Message) error` — SaveHistory 以会话粒度原子写 provider 历史（session:<id>:history；
- `func (store *SessionGranularStore) HistoryRange(projectID, sessionID string, offset, limit int) ([]types.Message, int, error)` — HistoryRange 按窗口读取会话 provider 历史，返回 [offset, offset+limit)
- `func (store *SessionGranularStore) Transcript(projectID, sessionID string) ([]TranscriptEvent, error)` — Transcript 读取会话事件日志（session:<id>:transcript；事件通道与
- `func (store *SessionGranularStore) TranscriptTail(projectID, sessionID string, tokenBudget, maxUnits int) ([]Event, error)` — TranscriptTail 读取会话事件日志尾部窗口（token + 单元上限；会话粒度键）。
- `func (store *SessionGranularStore) EventRange(projectID, sessionID string, fromSeq, toSeq uint64) ([]Event, error)` — EventRange 按 EventSeq 范围读取会话事件流（fork 切断点解析用）。
- `func (store *SessionGranularStore) ToolResults(projectID, sessionID string) ([]ToolResultRef, error)` — ToolResults 读取会话工具结果归档引用（session:<id>:toolresults）。
- `func (store *SessionGranularStore) ToolResult(projectID, sessionID, resultRef string) (ToolResult, error)` — ToolResult 读取会话工具结果原始内容（按 resultRef）。
- `func (store *SessionGranularStore) ListToolResults(projectID, sessionID string) ([]ToolResult, error)` — ListToolResults 读取会话 tool-results 通道全量（fork 深拷贝物理复制用）。
- `func (store *SessionGranularStore) Context(projectID, sessionID string) (ContextStack, error)` — Context 读取会话上下文四栈（session:<id>:context）。
- `func (store *SessionGranularStore) SaveContext(projectID, sessionID string, payload []byte) error` — SaveContext 保存会话上下文四栈（session:<id>:context；幂等）。
- `func (store *SessionGranularStore) LoadContextRaw(projectID, sessionID string) ([]byte, error)` — LoadContextRaw 读取会话上下文模块原始字节（fork 四栈深拷贝用）；不存在
- `func (store *SessionGranularStore) SaveCommit(projectID, sessionID string, commit Commit) error` — SaveCommit 以会话粒度原子提交快照（record + transcript + tool-results）。
- `func (store *SessionGranularStore) LoadCompactionRecords(projectID, sessionID string) ([]json.RawMessage, error)` — LoadCompactionRecords 读取该会话的压缩记录（应用侧记录原文，按写入顺序）。
- `func (store *SessionGranularStore) CurrentGeneration(projectID, sessionID string) (string, error)` — CurrentGeneration 返回会话当前已发布 generation（fork 血缘来源）。
- `func (store *SessionGranularStore) QueueEnqueue(projectID, sessionID, requestID, content string) error` — QueueEnqueue 把一条排队输入镜像落盘（同 requestID 重放幂等）。
- `func (store *SessionGranularStore) QueueMarkConsumed(projectID, sessionID, turnID string) error` — QueueMarkConsumed 把 lifecycle 队列中全部待发送项标记为被 turnID 消费。
- `func (store *SessionGranularStore) QueueConfirmConsumed(projectID, sessionID, turnID string) error` — QueueConfirmConsumed 该轮已发布 → 消费项出队。
- `func (store *SessionGranularStore) QueueFailConsumed(projectID, sessionID, turnID string) error` — QueueFailConsumed 该轮未发布 → 消费项内容回草稿并出队。
- `func (store *SessionGranularStore) QueueItems(projectID, sessionID string) ([]QueueItem, bool, error)` — QueueItems 读 lifecycle 队列投影。
- `func (store *SessionGranularStore) QueueRecoverItems(projectID, sessionID string) (QueueRecoveryReport, bool, error)` — QueueRecoverItems 重启恢复队列（条目级结果）。
- `func (store *SessionGranularStore) PendingMessageTail(projectID, sessionID string) (PendingTailReport, bool, error)` — PendingMessageTail 探测 message 通道草稿尾部（只读；v8 未装配返回
- `func (store *SessionGranularStore) RecoverPendingMessageTail(projectID, sessionID string) (PendingTailReport, bool, error)` — RecoverPendingMessageTail 显式恢复草稿尾部（基座一致才发布）。
- `func (store *SessionGranularStore) DiscardPendingMessageTail(projectID, sessionID string) (PendingTailReport, bool, error)` — DiscardPendingMessageTail 显式丢弃草稿尾部。
- `func (store *SessionGranularStore) ConversationRange(projectID, sessionID string, offset, limit int) ([]ConversationMessage, int, error)` — ConversationRange 读取会话 conversation 模块窗口（只解析 conversation
- `func (store *SessionGranularStore) Delete(projectID, sessionID string) error` — Delete 以会话粒度删除会话（record/history/transcript/toolresults/context
- `func (store *SessionGranularStore) SessionsOf(projectID string) ([]SessionInfo, error)` — SessionsOf 按项目索引枚举会话（project = 会话集合；物理实现 = Router
- `func (store *SessionGranularStore) loadSessionLiteral(projectID, sessionID string) (Record, bool)` — loadSessionLiteral 以字面项目 ID 读取会话记录（不替换 "" 为活跃作用域；
- `func (store *SessionGranularStore) derivedRecord(projectID, sessionID string) (Record, bool)` — derivedRecord 按 §2.5.4 从 message head.Meta + lifecycle 派生会话记录
- `func parseRecordInfo(payload []byte) (Record, bool)` — parseRecordInfo 宽容解析会话记录头：兼容薄封装 Record（title 字符串）与
- `func parseRecordTitle(raw json.RawMessage) string` — parseRecordTitle 兼容 title 的两种形态：字符串（薄封装 Record）与
- `func (store *SessionGranularStore) Bind(projectID, sessionID string, binding Binding) error` — Bind 写会话绑定（session:<id>:binding；幂等合并进 record）。

### session_granular_drift_test.go

- `func TestDefaultProjectSessionPersistDoesNotFollowActiveScope(t *testing.T)` — TestDefaultProjectSessionPersistDoesNotFollowActiveScope 复现「未关联

### session_granular_race_test.go

- `func TestSessionGranularStoreConcurrentProjectScope(t *testing.T)` — TestSessionGranularStoreConcurrentProjectScope 发散式并发（-race）：多 goroutine
- `func TestEventStoreConcurrentAppendAndLoad(t *testing.T)` — TestEventStoreConcurrentAppendAndLoad 发散式并发（-race）：多会话并发追加与

### session_granular_test.go

- `func newSessionGranularRouter(t *testing.T, _ Backend) *Router` — newSessionGranularRouter 构造 JSON v8 Router（R1 后仅保留该实现）。
- `func TestSessionGranularityPersistence(t *testing.T)` — TestSessionGranularityPersistence（T2.6）：session:<id> 五片原子读写、
- `func TestPersistenceIdempotent(t *testing.T)` — TestPersistenceIdempotent（B6）：会话粒度重复写/恢复幂等，键不漂移。
- `func TestSessionsOfToleratesProductionRecordSchema(t *testing.T)` — TestSessionsOfToleratesProductionRecordSchema（左侧栏历史会话回归）：
- `func TestSessionsOfDefaultProjectIndependentOfActiveScope(t *testing.T)` — TestSessionsOfDefaultProjectIndependentOfActiveScope（视图切换污染列表
- `func TestSessionsOfCarriesManifestTimeline(t *testing.T)` — TestSessionsOfCarriesManifestTimeline（左侧栏日期占位回归）：目录枚举
- `func TestResolveProjectForSessionUsesWorkspaceResolver(t *testing.T)` — TestResolveProjectForSessionUsesWorkspaceResolver（删除/读历史归属回归）：
- `func TestResolveProjectForSessionFallsBackToDefaultProject(t *testing.T)` — TestResolveProjectForSessionFallsBackToDefaultProject（未关联会话打不开

### session_meta.go

- `func (meta SessionDisplayMeta) Empty() bool` — Empty 报告元数据是否已无信息量（调用方据此删除条目而不是写零值）。
- `func NewSessionMetaStore(router *Router) *SessionMetaStore` — NewSessionMetaStore 构造项目级元数据存储；router 为 nil 时读写退化为空操作。
- `func (store *SessionMetaStore) Meta(projectID string) (map[string]SessionDisplayMeta, error)` — Meta 返回指定项目下 sessionID → 元数据 的映射（无记录时返回空映射）。
- `func (store *SessionMetaStore) Set(projectID, sessionID string, meta SessionDisplayMeta) (map[string]SessionDisplayMeta, error)` — Set 写入/清除单个会话的**展示**元数据（置顶/别名/排序位），返回更新后的整个项目
- `func (store *SessionMetaStore) SetPermissionTier(projectID, sessionID, tier string) (SessionDisplayMeta, error)` — SetPermissionTier 写入单个会话的**权限档位**（会话级用户设置；不影响置顶/别名/
- `func (store *SessionMetaStore) PermissionTier(projectID, sessionID string) (string, error)` — PermissionTier 读取单个会话的权限档位设置（空 = 从未选择，运行时回退进程默认）。
- `func (store *SessionMetaStore) load(projectID string) (map[string]SessionDisplayMeta, error)` — load 读取 blob；调用方持有 mu。缺键/反序列化失败都按"无元数据"处理：展示

### session_meta_test.go

- `func TestSessionMetaStoreRoundTrip(t *testing.T)` — TestSessionMetaStoreRoundTrip 验证项目级展示元数据 blob：读写、清除、项目隔离，
- `func TestSessionMetaStoreConcurrentSets(t *testing.T)` — TestSessionMetaStoreConcurrentSets 并发写同项目不同会话（-race）：读改写必须
- `func TestSessionMetaStorePermissionTierIsolation(t *testing.T)` — TestSessionMetaStorePermissionTierIsolation：会话级权限档位与展示元数据共用一份

### sessionstore.go

- `func retiredBackendError(backend Backend) error`
- `func (config Config) Normalize(defaultPath string) (Config, error)`
- `func (config Config) Safe() Config` — Safe returns a configuration suitable for the GUI. Credentials are never
- `func EventKindOf(event Event) string` — EventKindOf 返回事件的显式类别；旧数据 Kind 为空时按 Role/ToolCalls 回退。
- `func (key Key) validate() error`
- `func validateProjectID(projectID string) error` — validateProjectID 校验项目级记录（WriteProjectRecord/ReadProjectRecord）的
- `func NewRouter(configPath, defaultPath string, limits ...storageSettings) (*Router, error)`
- `func (router *Router) SetWorkspace(projectID string)`
- `func (router *Router) Workspace() string`
- `func (router *Router) Save(sessionID string, messages []types.Message) error`
- `func (router *Router) SaveWorkspace(projectID, sessionID string, messages []types.Message) error` — SaveWorkspace 在显式项目作用域下原子写 provider 历史（framework
- `func (router *Router) SaveCommit(sessionID string, commit Commit) error`
- `func (router *Router) SaveCommitWorkspace(projectID, sessionID string, commit Commit) error`
- `func (router *Router) Load(sessionID string) ([]types.Message, error)`
- `func (router *Router) LoadWorkspace(projectID, sessionID string) ([]types.Message, error)` — LoadWorkspace reads a session from an explicit project scope without
- `func (router *Router) LoadRange(sessionID string, offset, limit int) ([]types.Message, int, error)`
- `func (router *Router) LoadRangeWorkspace(projectID, sessionID string, offset, limit int) ([]types.Message, int, error)` — LoadRangeWorkspace reads a history window from an explicit project scope
- `func (router *Router) LoadEventTail(sessionID string, tokenBudget, maxUnits int) ([]Event, error)` — LoadEventTail reads newest complete protocol units within a token budget.
- `func (router *Router) LoadEventTailWorkspace(projectID, sessionID string, tokenBudget, maxUnits int) ([]Event, error)`
- `func (router *Router) LoadConversationRangeWorkspace(projectID, sessionID string, offset, limit int) ([]ConversationMessage, int, error)` — LoadConversationRangeWorkspace 读取会话 conversation 模块的指定窗口
- `func (router *Router) LoadEventRangeWorkspace(projectID, sessionID string, fromSeq, toSeq uint64) ([]Event, error)` — LoadEventRangeWorkspace 按 EventSeq 范围（含端点）读取事件流
- `func (router *Router) LoadToolResult(sessionID, resultRef string) (ToolResult, error)`
- `func (router *Router) LoadToolResultWorkspace(projectID, sessionID, resultRef string) (ToolResult, error)`
- `func (router *Router) ListToolResultsWorkspace(projectID, sessionID string) ([]ToolResult, error)` — ListToolResultsWorkspace 读取会话 tool-results 通道全部结果（显式项目
- `func (router *Router) CurrentGenerationWorkspace(projectID, sessionID string) (string, error)` — CurrentGenerationWorkspace 读取会话当前已发布 generation（显式项目
- `func (router *Router) SaveState(sessionID string, state []byte) error` — SaveState stores application-owned session state next to the engine history.
- `func (router *Router) SaveStateWorkspace(projectID, sessionID string, state []byte) error`
- `func (router *Router) SaveContextState(sessionID string, state []byte) error` — SaveContextState 保存会话 context 模块（system prompt + 四栈）到独立
- `func (router *Router) SaveContextStateWorkspace(projectID, sessionID string, state []byte) error`
- `func (router *Router) LoadContextState(sessionID string) ([]byte, error)` — LoadContextState 读取会话 context 模块；不存在时返回 fs.ErrNotExist。
- `func (router *Router) LoadContextStateWorkspace(projectID, sessionID string) ([]byte, error)`
- `func (router *Router) LoadState(sessionID string) ([]byte, error)`
- `func (router *Router) LoadStateWorkspace(projectID, sessionID string) ([]byte, error)`
- `func (router *Router) SaveCheckpointWorkspace(projectID, sessionID string, payload []byte) error` — SaveCheckpointWorkspace 写入引擎续跑快照（S24：JSON v8 布局落
- `func (router *Router) LoadCheckpointWorkspace(projectID, sessionID string) ([]byte, error)` — LoadCheckpointWorkspace 读取引擎续跑快照；不存在返回 fs.ErrNotExist。
- `func (router *Router) SaveSystemPromptWorkspace(projectID, sessionID, prompt, version string) (bool, error)` — SaveSystemPromptWorkspace 把会话 system prompt 快照写进 metadata/system.json
- `func (router *Router) LoadSystemPromptWorkspace(projectID, sessionID string) (systemPromptSnapshot, bool, error)` — LoadSystemPromptWorkspace 读取会话 system prompt 快照；handled=false 表示
- `func (router *Router) CompactFramesWorkspace(projectID, sessionID string) ([]CompactFrame, bool, error)` — CompactFramesWorkspace 读取 compact 通道已发布帧（S19：CompactStack 权威
- `func (router *Router) LoadCompactionRecordsWorkspace(projectID, sessionID string) ([]json.RawMessage, bool, error)` — LoadCompactionRecordsWorkspace 读取该会话的压缩记录通道（应用侧压缩记录的
- `func (router *Router) LayoutV8() bool` — LayoutV8 报告当前后端是否 v8 JSON 布局（S19 消费面迁移判据）。
- `func (router *Router) SessionMetaWorkspace(projectID, sessionID string) (frameworkStorage.SessionMeta, bool, error)` — SessionMetaWorkspace 返回会话枚举 meta（message head.Meta）；handled=false
- `func (router *Router) SetSessionTitleWorkspace(projectID, sessionID, title string) (bool, error)` — SetSessionTitleWorkspace 写会话标题（v8：message head 的目录枚举面
- `func (router *Router) SessionTitleWorkspace(projectID, sessionID string) (string, bool, error)` — SessionTitleWorkspace 只读会话标题（header-only：只打开 metadata/message.json
- `func (router *Router) FirstUserInputsWorkspace(projectID, sessionID string, limit int) ([]string, bool, error)` — FirstUserInputsWorkspace 读取会话最早的若干条用户输入正文（标题回填用；
- `func (router *Router) SetSessionArchivedWorkspace(projectID, sessionID string, archived bool) (bool, error)` — SetSessionArchivedWorkspace 写/清 lifecycle archived_at（S20：已归档判据）。
- `func (router *Router) SessionArchivedWorkspace(projectID, sessionID string) (time.Time, bool, error)` — SessionArchivedWorkspace 返回会话归档时间（零值 = 未归档）。
- `func (router *Router) SaveSessionDisplayMetaWorkspace(projectID string, payload []byte) error` — SaveSessionDisplayMetaWorkspace / LoadSessionDisplayMetaWorkspace：项目级
- `func (router *Router) LoadSessionDisplayMetaWorkspace(projectID string) ([]byte, error)`
- `func (router *Router) DerivedRecordWorkspace(projectID, sessionID string) ([]byte, bool, error)` — DerivedRecordWorkspace 返回派生 SessionRecord JSON（S20：record 通道退役后
- `func (router *Router) AppendGoalAuditEvent(projectID, sessionID string, entry GoalAuditEntry) (bool, error)` — AppendGoalAuditEvent 把 goal 审计条目写进 EVENT 通道（S19：GoalAudit 的
- `func (router *Router) GoalAuditEvents(projectID, sessionID string) ([]GoalAuditEntry, bool, error)` — GoalAuditEvents 读取 EVENT 通道里的 goal 审计条目（按事件序，Seq 顺序编号）。
- `func (router *Router) SaveProjectRecord(projectID string, record ProjectRecord) error` — SaveProjectRecord 写入项目级模块语义记录（project_refresh 工具的落盘路径）。
- `func (router *Router) AppendFrameworkEvent(ctx context.Context, sessionID string, entry EventLogEntry) error` — AppendFrameworkEvent 追加执行事实事件到会话级事件库
- `func (router *Router) AppendFrameworkEventWorkspace(ctx context.Context, projectID, sessionID string, entry EventLogEntry) error` — AppendFrameworkEventWorkspace 在显式项目作用域下追加执行事实事件。
- `func (router *Router) ReadFrameworkEvents(ctx context.Context, sessionID string) ([]EventLogEntry, error)` — ReadFrameworkEvents 读取会话级执行事实事件库（按 Seq 排序）。
- `func (router *Router) ReadFrameworkEventsWorkspace(ctx context.Context, projectID, sessionID string) ([]EventLogEntry, error)` — ReadFrameworkEventsWorkspace 在显式项目作用域下读取执行事实事件库。
- `func (router *Router) LoadProjectRecord(projectID string) (ProjectRecord, error)` — LoadProjectRecord 读取项目级模块语义记录（会话开始前即可读）。
- `func (router *Router) List() []frameworkStorage.SessionMeta`
- `func (router *Router) ListWorkspace(projectID string) []frameworkStorage.SessionMeta` — ListWorkspace lists sessions from an explicit project scope without
- `func (router *Router) Delete(sessionID string) error`
- `func (router *Router) DeleteWorkspace(projectID, sessionID string) error` — DeleteWorkspace deletes a session from an explicit project scope without
- `func (router *Router) MessageCount(sessionID string) (int, error)`
- `func (router *Router) Config() Config`
- `func (router *Router) Test(ctx context.Context, config Config) error`
- `func (router *Router) Configure(ctx context.Context, config Config) error`
- `func (router *Router) Close() error`
- `func (router *Router) withRepository(fn func(Repository, string) error) error`
- `func (router *Router) withRepositoryAt(projectID string, fn func(Repository, string) error) error`
- `func (router *Router) withJSONRepositoryAt(projectID string, fn func(*jsonRepository, string) error) (bool, error)` — withJSONRepositoryAt 是"仅 JSON 会话存储布局"运行期入口的统一包装（§2.13）：
- `func (router *Router) acquireRepository(projectID string) (Repository, string, bool)` — acquireRepository 短暂取锁获取当前 repository 并登记在途操作；数据操作在
- `func (router *Router) releaseRepository()`
- `func (router *Router) ensureOpsCond()`
- `func (router *Router) waitOpsIdleLocked()` — waitOpsIdleLocked 等待在途操作归零（调用方持 router.mu）。
- `func Open(ctx context.Context, config Config) (Repository, error)`
- `func newJSONRepository(root string, settings storageSettings) (*jsonRepository, error)`
- `func (repository *jsonRepository) WriteAtomic(_ context.Context, key Key, messages []types.Message) error`
- `func (repository *jsonRepository) WriteCommit(_ context.Context, key Key, commit Commit) error`
- `func (repository *jsonRepository) Read(_ context.Context, key Key) ([]types.Message, error)` — Read 只服务 v8 布局会话（D2/S12：旧 manifest 布局会话不再打开，读接口
- `func (repository *jsonRepository) ReadRange(ctx context.Context, key Key, offset, limit int) ([]types.Message, int, error)`
- `func (repository *jsonRepository) ReadEventTail(_ context.Context, key Key, tokenBudget, maxUnits int) ([]Event, error)`
- `func (repository *jsonRepository) ReadToolResult(_ context.Context, key Key, resultRef string) (ToolResult, error)`
- `func (repository *jsonRepository) ListToolResults(_ context.Context, key Key) ([]ToolResult, error)`
- `func (repository *jsonRepository) CurrentGeneration(_ context.Context, key Key) (string, error)`
- `func (repository *jsonRepository) WriteState(_ context.Context, key Key, state []byte) error`
- `func (repository *jsonRepository) ReadState(_ context.Context, key Key) ([]byte, error)`
- `func (repository *jsonRepository) WriteProjectRecord(_ context.Context, projectID string, record ProjectRecord) error`
- `func (repository *jsonRepository) AppendFrameworkEvent(_ context.Context, key Key, entry EventLogEntry) error`
- `func (repository *jsonRepository) prepareEventLogForAppendLocked(path, directory string) error` — prepareEventLogForAppendLocked 让事件库文件处于「可逐行追加」状态，是追加
- `func writeEventLogLines(path string, entries []EventLogEntry) error` — writeEventLogLines 原子发布逐行日志（迁移路径专用：每会话只发生一次）。
- `func appendEventLogLine(path string, line []byte) error` — appendEventLogLine 以 O_APPEND 追加一行（一条事实 = 一行）：不读旧内容、
- `func inspectEventLog(path string) (first byte, size int64, tornCut int64, err error)` — inspectEventLog 一次打开就拿到追加前要判断的三件事：形态（首字节）、体积、
- `func scanEventLogTailCut(file *os.File, size int64) (int64, error)` — scanEventLogTailCut 从尾部向前按块找最后一条完整记录的末尾
- `func encodeEventLogLines(entries []EventLogEntry) ([]byte, error)` — encodeEventLogLines 把事件序列编成逐行日志（每行一条 compact JSON + 换行）。
- `func isLegacyEventLogArray(data []byte) bool` — isLegacyEventLogArray 判定事件库是不是升级前的「整份 JSON 数组」形态。
- `func decodeLegacyEventLogArray(data []byte) ([]EventLogEntry, error)`
- `func decodeEventLog(data []byte) ([]EventLogEntry, error)` — decodeEventLog 解事件库：逐行日志（现形态）与整份 JSON 数组（升级前形态）都认。
- `func dedupeEventLogEntries(entries []EventLogEntry) []EventLogEntry` — dedupeEventLogEntries 同 Seq 只留最后追加的那条（后写者胜），与旧「读整份 →
- `func (repository *jsonRepository) ReadFrameworkEvents(_ context.Context, key Key) ([]EventLogEntry, error)`
- `func (repository *jsonRepository) legacyFrameworkEventEntriesLocked(directory string) ([]EventLogEntry, error)` — legacyFrameworkEventEntriesLocked 扫描 v1 布局遗留的 events.json（所有
- `func mergeEventLogEntries(primary, extra []EventLogEntry) []EventLogEntry` — mergeEventLogEntries 合并两个按 Seq 升序的事件库切片，同 Seq 保留先出现的
- `func (repository *jsonRepository) readEventLogLocked(path string) ([]EventLogEntry, error)` — readEventLogLocked 读取事件库（不存在 → 空库；损坏 → 显式错误），按 Seq
- `func (repository *jsonRepository) ReadProjectRecord(_ context.Context, projectID string) (ProjectRecord, error)`
- `func (repository *jsonRepository) List(_ context.Context, projectID string) ([]frameworkStorage.SessionMeta, error)`
- `func (repository *jsonRepository) Delete(_ context.Context, key Key) error`
- `func (repository *jsonRepository) Ping(context.Context) error`
- `func (repository *jsonRepository) Close() error`
- `func (repository *jsonRepository) writeToolResultLocked(key Key, result ToolResult) error`
- `func (repository *jsonRepository) projectDir(projectID string) string`
- `func (repository *jsonRepository) sessionDir(key Key) string`
- `func (repository *jsonRepository) toolResultPath(key Key, resultRef string) string`
- `func eventTokenCount(events []Event) int`
- `func selectEventTailWindow(events []Event, tokenBudget, maxUnits int) (tail []Event, startUnit int)` — selectEventTailWindow 选取尾窗，并回报尾窗的起始单元下标。
- `func selectEventTail(events []Event, tokenBudget, maxUnits int) []Event`
- `func CompleteEventUnits(events []Event) [][]Event` — CompleteEventUnits 把事件流切分为可见协议轮次单元（轮）：user 轮、
- `func userEventUnit(events []Event, start int) ([]Event, int)`
- `func toolEventUnit(events []Event, start int) ([]Event, int, bool)`
- `func appendUnique(values []string, value string) []string`
- `func containsValue(values []string, value string) bool`
- `func loadConfig(path string) (Config, error)`
- `func saveConfig(path string, config Config) error`

### sessionstore_test.go

- `func messages(count int, marker string) []types.Message`
- `func TestRouterPersistsConfiguredBackend(t *testing.T)`
- `func TestRetiredBackendsReturnExplicitError(t *testing.T)`
- `func TestRouterReadsExplicitWorkspaceWithoutChangingActiveScope(t *testing.T)`
- `func TestJSONRepositoryReadRangeWindowed(t *testing.T)` — TestJSONRepositoryReadRangeWindowed 验证窗口读优化：manifest shard 计数
- `func firstContent(messages []types.Message) string`
- `func TestJSONRepositoryReadRangeTotalOnly(t *testing.T)` — TestJSONRepositoryReadRangeTotalOnly 验证 limit<=0 语义：只读 manifest 拿
