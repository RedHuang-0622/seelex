# store/message（根包分卷）

## 生态位

message/event 事件行、段落投影、wire 装配与大工具结果归档

覆盖：`message_rows*.go`、`structural_events*.go`、`event_store*.go`、`event_range*.go`、`paragraphs*.go`、`wire_assembler*.go`、`big_tool_result*.go`、`provider_wire*.go`；未归属文件由覆盖自检拦下。

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### big_tool_result.go

- `func (store *storeEngine) blobLimits() (softChars int, hardBytes int, quotaBytes int)` — 三条限额（软截断 / 硬报错 / 会话配额）取自 §11 settings，不在此硬编码。
- `func (store *storeEngine) blobDir(key Key) string`
- `func (store *storeEngine) blobPath(key Key, hash string) string`
- `func (store *storeEngine) writeBlob(key Key, toolName, content string) (toolBlob, error)` — writeBlob 写超大工具输出（主会话键）。返回 blob（Content 为截断后正文；
- `func (store *storeEngine) readBlob(key Key, hash string) (string, error)` — readBlob 读取 blob 正文（返回截断后内容）。
- `func (store *storeEngine) listBlobHashes(key Key) ([]string, error)` — listBlobHashes 枚举会话 blob（GC 引用扫描基础）。
- `func (store *storeEngine) blobGarbageCollect(key Key, referenced map[string]bool, dryRun bool) ([]string, error)` — blobGarbageCollect 删除无引用 blob（referenced 跨主会话与子代理消息）。
- `func blobRefOf(ref string) string` — blobRefOf 归一化 result_ref 为 blob hash。

### event_range.go

- `func (repository *jsonRepository) ReadEventRange(_ context.Context, key Key, fromSeq, toSeq uint64) ([]Event, error)`

### event_range_test.go

- `func rangeEvents(count int) []Event`
- `func TestEventRangeFromMessageRows(t *testing.T)` — TestEventRangeFromMessageRows 契约测试：EventSeq 范围读取（含端点）
- `func sameRangeEvents(got, want []Event) bool` — sameRangeEvents 忽略提交后由存储层补的 commit_id 等扩展字段，只比较
- `func TestEventRangeMissingSessionFails(t *testing.T)` — TestEventRangeMissingSessionFails 验证缺失会话的 event range 读取显式

### event_store.go

- `func NewEventStore(router *Router) *EventStore` — NewEventStore 创建会话级执行事实事件库（惰性追加，不预读）。
- `func (store *EventStore) SetWorkspaceResolver(resolver func(sessionID string) string)` — SetWorkspaceResolver 注入会话绑定项目解析器（wiring/bootstrap 装配点：从
- `func (store *EventStore) projectFor(sessionID string) string`
- `func (store *EventStore) Append(ctx context.Context, event frameworkevent.Event) error` — Append 实现 frameworkevent.Sink：按事件 Location（agent.runtime →
- `func (store *EventStore) Load(ctx context.Context, sessionID string) ([]frameworkevent.Event, error)` — Load 读取会话级事件库（按 Seq 排序，追加顺序）。
- `func (store *EventStore) LoadRange(ctx context.Context, sessionID string, fromSeq, toSeq uint64) ([]frameworkevent.Event, error)` — LoadRange 读取会话级事件库的 Seq 区间（含端点；按追加顺序排序）。
- `func (store *EventStore) loadRange(ctx context.Context, sessionID string, fromSeq, toSeq uint64) ([]frameworkevent.Event, error)` — loadRange 读取并按范围过滤（from=to=0 表示全量）。
- `func sessionIDFromEvent(event frameworkevent.Event) string` — sessionIDFromEvent 从事件的 agent.runtime Location 提取会话标识

### event_store_test.go

- `func TestFrameworkEventLogRoundTrip(t *testing.T)` — TestFrameworkEventLogRoundTrip 验证执行事实事件库
- `func TestFrameworkEventSurvivesGenerationRollover(t *testing.T)` — TestFrameworkEventSurvivesGenerationRollover 契约测试（plan.md 验收：
- `func TestFrameworkEventLogMissingSessionReturnsEmpty(t *testing.T)`
- `func TestEventStoreResolvesSessionBindingProject(t *testing.T)` — TestEventStoreResolvesSessionBindingProject（R3 键漂移回归）：执行事实
- `func TestEventStoreWithoutResolverFallsBackToActiveScope(t *testing.T)` — TestEventStoreWithoutResolverFallsBackToActiveScope 验证未注入解析器时
- `func TestEventStoreLoadRange(t *testing.T)` — TestEventStoreLoadRange（波 4 G7）：事件库按会话/Seq 区间读回（含端点），
- `func eventLogEntry(seq uint64, status frameworkevent.Status) EventLogEntry`

### message_rows.go

- `func fileSize(path string) int64` — fileSize 返回文件字节数（不存在/出错返回 0）。
- `func shardTailTrust(path string, info shardInfo) bool`
- `func (store *storeEngine) trustedTailFor(key Key, head messageHead) *shardInfo` — trustedTailFor 返回提交路径可信任的末分片索引（不可信返回 nil）：
- `func (store *storeEngine) messageDir(key Key) string` — messageDir 返回 message 数据目录。
- `func (store *storeEngine) shardPath(key Key, fromSeq, toSeq uint64) string` — shardPath 返回 message 分片文件路径（显式命名：from_to 含端点）。
- `func emptyMessageHead(key Key) messageHead` — emptyMessageHead 返回空会话 head。
- `func (store *storeEngine) readMessageHead(key Key) (messageHead, error)` — readMessageHead 读取 message head（缺失 = 空会话 head，不报错）。
- `func (store *storeEngine) setMessageTitleStored(key Key, title string) error` — setMessageTitleStored 把会话标题写进 message head 的目录枚举面
- `func (store *storeEngine) messageTitleStored(key Key) (string, bool, error)` — messageTitleStored 只读 message head 的标题（header-only：只打开
- `func userInputText(row Event) (string, bool)` — userInputText 返回一条事件行作为"用户输入"的正文；非用户输入返回 ok=false。
- `func scanShardUserInputs(path string, limit int) ([]string, error)` — scanShardUserInputs 从分片文件**从头按行扫描**，收集前 limit 条用户输入或触到
- `func (store *storeEngine) firstShardUserInputs(key Key, limit int) ([]string, bool, error)` — firstShardUserInputs 读取会话**首个消息分片**里的前若干条用户输入正文
- `func (store *storeEngine) messageCommit(key Key, commitID string, rows []Event) (messageHead, error)` — messageCommit 把一提交（可含多行事件行）append 到 message 通道并原子
- `func (store *storeEngine) messageCommitSync(key Key, commitID string, rows []Event, floor *Floor) (messageHead, error)` — messageCommitSync 是 sequencer 的提交入口：在普通 messageCommit 之上携带
- `func (store *storeEngine) messageCommitLocked(key Key, commitID string, rows []Event) (messageHead, error)` — messageCommitLocked 是 messageCommit 的锁内实现（其它模块/重放逻辑
- `func (store *storeEngine) messageCommitSyncLocked(key Key, commitID string, rows []Event, floor *Floor) (messageHead, error)` — messageCommitSyncLocked 是 sequencer 提交的锁内实现。floor 为 nil 时保持
- `func messageRowsCommitID(rows []Event) string` — messageRowsCommitID 由事件行身份字段确定性推导一次提交的凭据。
- `func (store *storeEngine) rememberMessageAnchor(key Key, head messageHead)` — rememberMessageAnchor 在 message head 发布后盖章内存锚。调用方持 message
- `func (store *storeEngine) messageAnchorCached(key Key) *messageAnchorPoint` — messageAnchorCached 返回内存锚；未装载时返回 nil。
- `func (store *storeEngine) forgetMessageAnchor(key Key)` — forgetMessageAnchor 作废内存锚（删除会话 / 换后端）。
- `func (store *storeEngine) readMessageHeadLocked(key Key) (messageHead, error)` — readMessageHeadLocked 是 readMessageHead 的锁内版本（调用方持 messageMu）。
- `func (store *storeEngine) reapUnpublishedLocked(key Key, head messageHead) error` — reapUnpublishedLocked 清理未发布残迹：
- `func isMessageShardFile(name string) bool`
- `func truncateMessageRowsAfter(path string, headSeq uint64, info shardInfo) error` — truncateMessageRowsAfter 截断 JSONL 文件，保留 seq <= head 的行（先跳过崩溃
- `func readMessageRowsFile(file *os.File) ([]Event, error)` — readMessageRowsFile 读取已打开 JSONL 文件中的全部事件行（跳过空行与崩溃残尾）。
- `func decodeMessageRows(data []byte) []Event` — decodeMessageRows 解析 JSONL 事件行（崩溃残尾跳过；坏行显式失败由调用方
- `func (store *storeEngine) deltaRowsLocked(head messageHead, rows []Event, commitID string) ([]Event, error)` — deltaRowsLocked 计算应 append 的行（提交路径）：基准 = 发布点。调用方已先
- `func assignRowSeqs(rows []Event, base uint64, commitID string) ([]Event, error)` — assignRowSeqs 是唯一的 seq 分配器：base = 该写入路径的续号基准（已落盘的
- `func (store *storeEngine) appendRowsLocked(key Key, head *messageHead, delta []Event) error` — appendRowsLocked 把 delta append 进当前分片（满片滚动到新文件），并
- `func (store *storeEngine) writeShardRowsLocked(key Key, delta []Event, trusted *shardInfo) ([]shardInfo, error)` — writeShardRowsLocked 是唯一的分片写入器：把 delta 顺序写进**物理末分片**
- `func (store *storeEngine) physicalLastShardLocked(key Key) (string, error)` — physicalLastShardLocked 返回物理末尾分片文件名（from_seq 最大的分片；无分片
- `func readMessageRowsFileAt(path string) ([]Event, error)`
- `func appendMessageRowsFile(path string, rows []Event) error` — appendMessageRowsFile 追加完整 JSONL 行（先截崩溃残尾；再 sync）。
- `func appendMessageRowsFileEx(path string, rows []Event) (appended []byte, preSize int64, truncated bool, err error)` — appendMessageRowsFileEx 是 appendMessageRowsFile 的"带细节"版本：除追加外还
- `func appendRowsAndDigest(path string, rows []Event, trustedTail bool) (string, error)` — appendRowsAndDigest 追加 rows 到分片并返回追加后**整文件**的 sha256（head
- `func fileSHA256(path string) string` — fileSHA256 返回整片文件的 sha256。
- `func (store *storeEngine) readRows(key Key, fromSeq, toSeq uint64) ([]Event, error)` — readRows 按 seq 区间读取已发布行（[from, to] 含端点；越界安全）。
- `func (store *storeEngine) withPublishedHead(key Key, decode func(head messageHead) ([]Event, error)) ([]Event, error)` — withPublishedHead 以"已发布 head 快照"为基准做锁外解码。
- `func samePublishedHead(a, b messageHead) bool` — samePublishedHead 报告两次快照的**已发布坐标**是否同一（发布点 + 分片索引）。
- `func (store *storeEngine) readRowsLocked(key Key, fromSeq, toSeq uint64) ([]Event, error)` — readRowsLocked 是 readRows 的锁内版本（调用方持 messageMu）：写路径（LRU 行
- `func (store *storeEngine) decodePublishedRows(key Key, head messageHead, fromSeq, toSeq uint64) ([]Event, error)` — decodePublishedRows 按给定 head 快照解码 [from, to] 区间（不取任何锁）：只读
- `func (store *storeEngine) readAllRows(key Key) ([]Event, error)` — readAllRows 读取全部已发布行（含 LRU 空洞；已淘汰前缀自然缺失）。
- `func (store *storeEngine) readTailRowsForSelection(key Key, tokenBudget, maxUnits int) ([]Event, error)` — readTailRowsForSelection 只读“尾部选择可能需要的”分片，避免每轮
- `func (store *storeEngine) decodeTailRows(key Key, head messageHead, tokenBudget, maxUnits int) ([]Event, error)` — decodeTailRows 按 head 快照从末分片向前累积解码（不取任何锁）。
- `func (store *storeEngine) verifyMessage(key Key) error` — verifyMessage 校验 message 通道：head 可读、分片存在、文件行数与 head
- `func (store *storeEngine) messageCount(key Key) (uint64, error)` — messageCount 返回当前物理行数（淘汰后不包含前缀空洞）。

### message_rows_test.go

- `func messageFixture(t *testing.T, shardRows int) (*storeEngine, Key)` — messageFixture 构造临时数据根 + 一个会话（D.0 通用前置：显式路径、不依赖
- `func messageRow(seq uint64, messageID, role, kind, content string) Event` — messageRow 生成一条事件行。
- `func TestMessageRowsFirstCommitCreatesGuideAndMessageHead(t *testing.T)` — TestMessageRowsFirstCommitCreatesGuideAndMessageHead 对应 T-M1-01。
- `func TestMessageRowsCommitRowsShareCommitIDAndReplayBySeq(t *testing.T)` — TestMessageRowsCommitRowsShareCommitIDAndReplayBySeq 对应 T-M1-02。
- `func TestMessageRowsCrashTornTailIgnored(t *testing.T)` — TestMessageRowsCrashTornTailIgnored 对应 T-M1-03：文件尾部存在半行 → 截断/忽略；
- `func TestMessageRowsUnpublishedRowsInvisible(t *testing.T)` — TestMessageRowsUnpublishedRowsInvisible 对应 T-M1-04（§2.0 通道类型表 +
- `func TestMessageRowsIndependentModuleLocks(t *testing.T)` — TestMessageRowsIndependentModuleLocks 对应 T-M1-05：并发写 stack 与 message，
- `func TestMessageRowsReaderSelfHealsOnChecksumMismatch(t *testing.T)` — TestMessageRowsReaderSelfHealsOnChecksumMismatch 对应 T-M1-06：reader 读到旧
- `func TestMessageRowsShardRolloverAt101Rows(t *testing.T)` — TestMessageRowsShardRolloverAt101Rows 对应 T-M1-07：已有 100 行后 commit 第 101
- `func TestMessageRowsDuplicateCommitIDIdempotent(t *testing.T)` — TestMessageRowsDuplicateCommitIDIdempotent 对应 T-M1-08：重复同一 commit_id →
- `func TestMessageRowsGuidePresentForReadRoutes(t *testing.T)` — TestMessageRowsGuidePresentForReadRoutes 验证 guide 只做读索引：模块 head 已发布
- `func TestMessageRowsVerifyEmptySession(t *testing.T)` — TestMessageRowsVerifyEmptySession 验证空会话 verify 通过（没有 message.json 也
- `func TestMessageRowsMissingSessionReturnsEmpty(t *testing.T)` — TestMessageRowsMissingSessionReturnsEmpty 验证未知会话读取 = 空而非错误（与
- `func TestMessageRowsChecksumCorruptionSelfHeals(t *testing.T)` — TestMessageRowsChecksumCorruptionSelfHeals 校验 checksum 损坏（非并发替换）
- `func TestMessageRowsPublicReadersKeepCommitIDAndWireMaterial(t *testing.T)` — TestMessageRowsPublicReadersKeepCommitIDAndWireMaterial 对应 T-EV-07
- `func marshalMessageRow(row Event) (string, error)`

### paragraphs.go

- `func EventParagraphs(events []Event) []EventParagraph` — EventParagraphs 由 CompleteEventUnits 推导段落表：事件流 → 段落边界
- `func ParagraphEnd(events []Event, seq uint64) (uint64, bool)` — ParagraphEnd 返回包含指定 EventSeq 的段落末端（含端点）；seq 不在任何
- `func IsParagraphBoundary(events []Event, seq uint64) bool` — IsParagraphBoundary 判断 EventSeq 是否落在段落边界（含端点末端）。

### paragraphs_test.go

- `func TestEventParagraphsDeriveCompleteUnitBoundaries(t *testing.T)`
- `func TestParagraphBoundaryChecks(t *testing.T)`

### provider_wire.go

- `func ProviderWireMessages(messages []types.Message) []types.Message` — ProviderWireMessages 把 durable 记录投影出的 provider 历史对齐到框架**实际

### structural_events.go

- `func (store *storeEngine) structuralEventDir(key Key) string`
- `func (store *storeEngine) structuralEventShardPath(key Key, fromID, toID uint64) string`
- `func emptyEventHead(key Key) eventHeadRecord`
- `func (store *storeEngine) structuralEventCommit(key Key, commitID string, events []structuralEvent) (eventHeadRecord, error)` — structuralEventCommit 追加一提交的 EVENT 行并原子发布 event.json。
- `func (store *storeEngine) readEventHeadLocked(key Key) (eventHeadRecord, error)`
- `func (store *storeEngine) readEventHead(key Key) (eventHeadRecord, error)`
- `func structuralEventsCommitID(events []structuralEvent) string` — structuralEventsCommitID 由结构性事件身份字段确定性推导一次提交的凭据。
- `func (store *storeEngine) reapEventUnpublishedLocked(key Key, head *eventHeadRecord) (bool, error)` — reapEventUnpublishedLocked 清理未发布残迹（head 未索引的分片删除；尾分片
- `func eventPublishedTotal(shards []eventShardInfo) uint64` — eventPublishedTotal 按分片索引重算已发布事件行数。
- `func eventShardSize(path string) (int64, error)`
- `func isEventShardFile(name string) bool`
- `func truncateStructuralEventsAfter(path string, headID uint64) ([]structuralEvent, int64, error)` — truncateStructuralEventsAfter 截掉崩溃残尾与 event_id > headID 的行，返回
- `func readStructuralEventsFile(file *os.File) ([]structuralEvent, error)`
- `func decodeStructuralEvents(data []byte) []structuralEvent`
- `func (store *storeEngine) structuralEventDelta(head eventHeadRecord, events []structuralEvent, commitID string) ([]structuralEvent, error)` — structuralEventDelta 按 A.2 水位式幂等计算待追加行：同 commit 重复持久化 =
- `func (store *storeEngine) appendEventsLocked(key Key, head *eventHeadRecord, delta []structuralEvent) error` — appendEventsLocked 把 delta 追加进尾分片（满片滚动新文件），并按内存中的
- `func readStructuralEventsAt(path string) ([]structuralEvent, error)`
- `func appendStructuralEvents(path string, rows []structuralEvent) (int64, error)` — appendStructuralEvents 追加完整 JSONL 行（先截崩溃残尾；再 sync），返回
- `func (store *storeEngine) readEvents(key Key, fromID, toID uint64) ([]structuralEvent, error)` — readEvents 读取 EVENT（[from, to] 含端点；0 = 全量）。
- `func (store *storeEngine) verifyEvents(key Key) error` — verifyEvents 校验 EVENT 通道：head 与分片索引、分片物理行数、水位一致性。

### structural_events_test.go

- `func eventFixture(t *testing.T, shardRows int) (*storeEngine, Key)`
- `func eventHeadShards(t *testing.T, store *storeEngine, key Key) []eventShardInfo`
- `func eventTailShardPath(t *testing.T, store *storeEngine, key Key) string` — eventTailShardPath 返回 head 记录的尾分片物理路径。
- `func turnEvent(round int) structuralEvent`
- `func TestStructuralEventCommitNeverReadsHistoryShards(t *testing.T)` — TestStructuralEventCommitNeverReadsHistoryShards 对应 T-EV-05：分片已滚动多片
- `func TestStructuralEventWatermarkIdempotent(t *testing.T)` — TestStructuralEventWatermarkIdempotent 对应 A.2 规则 1/2/3：event_id=0 续号、
- `func TestStructuralEventUnpublishedAppendInvisible(t *testing.T)` — TestStructuralEventUnpublishedAppendInvisible 对应发布点语义（I5）：append 完成但
- `func TestStructuralEventHeadAbsentMeansNothingPublished(t *testing.T)` — TestStructuralEventHeadAbsentMeansNothingPublished 对应 head = 发布点：删除
- `func TestStructuralEventCrashTailDropped(t *testing.T)` — TestStructuralEventCrashTailDropped 对应崩溃残尾：半行不发布且被截净，恢复
- `func TestStructuralEventForkRowsSurviveConsecutiveForks(t *testing.T)` — TestStructuralEventForkRowsSurviveConsecutiveForks 对应 T-EV-06（§2.0 规则 4、

### wire_assembler.go

- `func (params wireParams) normalized() wireParams`
- `func applyWireBudget(settings storageSettings, base wireParams) wireParams` — applyWireBudget 把 §5.2 的 wire 装配预算落到一次装配请求，返回装配实际使用的
- `func (store *storeEngine) assembleWire(key Key, cache *attemptCache, params wireParams) (wireResult, error)` — assembleWire 构造 wire 消息序列（R2 主入口）。
- `func assembleWireRows(frame *compactFrameRecord, rows []Event, cache *attemptCache, params wireParams) wireResult` — assembleWireRows 是 wire 装配的纯函数核心：给定最新帧（可空）与尾行，
- `func (state *wireState) emit(message wireMessage, cost int)`
- `func unitCost(row Event) int` — unitCost 估算一条 wire 消息的字符成本（TokenCount 缺失时 len/4 近似）。
- `func (state *wireState) consumeRow(row Event)`
- `func (state *wireState) budgetWouldExceed(cost int, boundaryComplete bool) bool` — budgetWouldExceed 判断添加 cost 是否会超软预算；boundaryComplete 表示
- `func (state *wireState) closePendingBeforeUser()` — closePendingBeforeUser 在 user 边界修复仍未完成的工具轮（不跳后续 user）。
- `func (state *wireState) closeTail()` — closeTail 处理流尾：未完成工具轮保留 open + 修复占位。
- `func (state *wireState) attachAttempts(cache *attemptCache)` — attachAttempts 在尝试缓存锚点行后拼接最近 K 条尝试（锚点行必须在 wire 中）。
- `func roleForAttempt(role string) string`
- `func wireDigest(messages []wireMessage) string` — wireDigest 计算 wire 序列版本摘要（前缀稳定性比较基础）。
- `func wirePrefixEqual(left, right []wireMessage) bool` — wirePrefixEqual 判断两次 wire 输出的持久前缀逐字节一致：比较去掉
- `func (cache *attemptCache) recentForAllAnchors(anchorSeq uint64, k int) []attempt` — recentForAllAnchors 归并 AttemptCache.RecentFor 的辅助（按 op 归组取最近
- `func (cache *attemptCache) snapshot() []attempt`
