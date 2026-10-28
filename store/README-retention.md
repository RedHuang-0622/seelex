# store/retention（根包分卷）

## 生态位

草稿尾与未提交批次、队列消费与 LRU 回收

覆盖：`retention*.go`、`pending_tail*.go`、`lifecycle*.go`、`queue_consume*.go`；未归属文件由覆盖自检拦下。

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### lifecycle.go

- `func (store *storeEngine) lifecycleInputDir(key Key) string`
- `func (store *storeEngine) lifecycleQueueDir(key Key) string`
- `func (store *storeEngine) lifecycleDraftPath(key Key) string`
- `func (store *storeEngine) lifecycleQueuePath(key Key) string`
- `func (store *storeEngine) readLifecycleStateLocked(key Key) (lifecycleState, error)` — readLifecycleStateLocked 读权威投影（调用方持 lifecycle 模块锁）。
- `func (store *storeEngine) readLifecycleState(key Key) (lifecycleState, error)` — readLifecycleState 返回权威投影，并在 head 落后于数据时修补水位。
- `func lifecycleStateEmpty(state lifecycleState) bool`
- `func lifecycleHeadMatches(head lifecycleHead, state lifecycleState) bool` — lifecycleHeadMatches 判定 head 水位是否与权威数据一致（缺失即不一致）。
- `func queueMaxSeq(items []queueItem) uint64`
- `func queueHeadState(items []queueItem) queueState`
- `func (store *storeEngine) publishLifecycleLocked(key Key, _ string, state lifecycleState) error` — publishLifecycleLocked 提交一次 lifecycle 变更：先落权威数据（原子替换），
- `func (store *storeEngine) readLifecycleHead(key Key) (lifecycleHead, error)` — readLifecycleHead 返回水位 head（缺失 = fs.ErrNotExist）。
- `func (store *storeEngine) updateLifecycleHeadLocked(key Key, mutate func(*lifecycleHead)) error` — updateLifecycleHeadLocked 在 lifecycle 锁内读取 head、交给 mutate 修改并原子
- `func (store *storeEngine) setLifecycleOrder(key Key, policy string, roles []string) error` — setLifecycleOrder 写群聊顺序策略（§8.3）。不会触碰 draft/queue 数据文件。
- `func (store *storeEngine) setRoleLifecycle(key Key, joinSeq uint64, ref *CompactRef) error` — setRoleLifecycle 写角色冷恢复坐标：join_seq_id 与 compact_ref。compact_ref
- `func (store *storeEngine) setLifecycleArchived(key Key, archived bool) error` — setLifecycleArchived 写入/清除 §2.5.4 的会话归档标记（S20：record 状态
- `func (store *storeEngine) lifecycleArchivedAt(key Key) (time.Time, error)` — lifecycleArchivedAt 返回归档时间（未归档 = 零值）。
- `func (store *storeEngine) readLifecycleHeadLocked(key Key) (lifecycleHead, error)`
- `func nextQueueSeq(items []queueItem) uint64` — nextQueueSeq 返回新条目的 seq。
- `func newQueueItem(state lifecycleState, requestID, content string) queueItem`
- `func (store *storeEngine) draftToQueue(key Key, content string) error` — draftToQueue 草稿 D → 入队（同一次 lifecycle 提交：queue 含 D，draft
- `func (store *storeEngine) saveDraft(key Key, content string) error` — saveDraft 保存/更新草稿。
- `func (store *storeEngine) draftDirectSend(key Key, content string) error` — draftDirectSend 草稿直发：
- `func (store *storeEngine) queueEnqueue(key Key, requestID, content string) error` — queueEnqueue 队尾入队（草稿直发失败/外部入队）。
- `func (store *storeEngine) queueFront(key Key) (queueItem, bool)` — queueFront 返回队首（不出队）。
- `func (store *storeEngine) queueSendFront(key Key) (queueItem, error)` — queueSendFront 队首进入发送态（可发即发；Q1 阻塞时 Q2 不插队）。
- `func (store *storeEngine) queueConfirmSent(key Key, requestID string) error` — queueConfirmSent 确认发送成功（message.json 已发布对应 request）后出队。
- `func (store *storeEngine) queueSendFailed(key Key) error` — queueSendFailed 队首发送失败 → 内容回草稿，队列移除该项（T-LC-05）。
- `func (store *storeEngine) directSendFailed(key Key, content string) error` — directSendFailed 草稿直发失败 → 内容回草稿（T-LC-06）。
- `func (store *storeEngine) queueRecover(key Key) ([]queueItem, error)` — queueRecover 重启恢复：已发送但 message.json 未发布的项回 queued 重发
- `func (store *storeEngine) queueRecoverLocked(key Key, report *QueueRecoveryReport) ([]queueItem, error)` — queueRecoverLocked 是恢复的锁内实现（调用方持 lifecycle 模块锁）。
- `func (store *storeEngine) readAllRowsLocked(key Key) ([]Event, error)`

### lifecycle_fork_test.go

- `func lifecycleFixture(t *testing.T) (*storeEngine, Key)`
- `func TestLifecycleDraftToQueueAtomic(t *testing.T)` — TestLifecycleDraftToQueueAtomic 对应 T-LC-01：D→入队，queue 含 D、draft 清空
- `func TestLifecycleQueueFIFO(t *testing.T)` — TestLifecycleQueueFIFO 对应 T-LC-02：Q1 阻塞时 Q2 不插队；Q1 成功后 Q2 才发。
- `func TestLifecycleDirectSendQueueEmpty(t *testing.T)` — TestLifecycleDirectSendQueueEmpty 对应 T-LC-03：队列空 + 草稿 D → 立即发送（不
- `func TestLifecycleDirectSendQueueNonEmptyEnqueueTail(t *testing.T)` — TestLifecycleDirectSendQueueNonEmptyEnqueueTail 对应 T-LC-04：队列非空 + 草稿
- `func TestLifecycleQueueSendFailedReturnsDraft(t *testing.T)` — TestLifecycleQueueSendFailedReturnsDraft 对应 T-LC-05：队首发送失败 → 内容回
- `func TestLifecycleDirectSendFailedReturnsDraft(t *testing.T)` — TestLifecycleDirectSendFailedReturnsDraft 对应 T-LC-06：草稿直发失败 → 内容回
- `func TestLifecycleAlreadySentNotPublishedResends(t *testing.T)` — TestLifecycleAlreadySentNotPublishedResends 对应 T-LC-07：已发送但 message.json
- `func TestLifecyclePublishedMessageDequeues(t *testing.T)` — TestLifecyclePublishedMessageDequeues 对应 T-LC-08：message.json 已发布 → 不重
- `func TestLifecycleIllegalStateTransitionRejected(t *testing.T)` — TestLifecycleIllegalStateTransitionRejected 对应 T-LC-09：queued→sent 无发送
- `func TestForkStoreSessionCopyRange(t *testing.T)` — TestForkStoreSessionCopyRange 对应 T-FK-01：主会话 [0..100] from=88 → 子会话含
- `func TestForkStoreStackSnapshotRebuiltByAnchor(t *testing.T)` — TestForkStoreStackSnapshotRebuiltByAnchor 对应 T-FK-02：批次部分完成，from 落
- `func TestForkStoreForkBeforeWatermarkRejected(t *testing.T)` — TestForkStoreForkBeforeWatermarkRejected 对应 T-FK-03：from < watermark → 显式
- `func TestForkStoreConsistentSnapshot(t *testing.T)` — TestForkStoreConsistentSnapshot 对应 T-FK-04：fork 基于固定 head 的一致快照，
- `func TestForkStoreSubagentTree(t *testing.T)` — TestForkStoreSubagentTree 对应 T-FK-05：spawn 生成 subagent_<hash>/ 子树
- `func TestForkStoreSubagentReusesMainBlob(t *testing.T)` — TestForkStoreSubagentReusesMainBlob 对应 T-FK-06：子代理大工具输出复用主会话
- `func TestForkStoreChildSurvivesParentDelete(t *testing.T)` — TestForkStoreChildSurvivesParentDelete 对应 T-FK-07：fork 后删除父会话，子会话
- `func TestForkStoreArchiveSubagentParentOK(t *testing.T)` — TestForkStoreArchiveSubagentParentOK 对应 T-FK-08：归档 subagent 后主会话不受
- `func TestForkStoreBlobGC(t *testing.T)` — TestForkStoreBlobGC 对应 T-FK-09：主会话与子代理消息均不再引用某 blob → GC
- `func repeatStr(value string, count int) string`
- `func containsStr(haystack, needle string) bool`
- `func TestLifecycleFactsLiveInDesignedFiles(t *testing.T)` — TestLifecycleFactsLiveInDesignedFiles 断言 §3.2 实体→文件：draft/queue 的
- `func TestLifecycleHeadLagRepairsFromData(t *testing.T)` — TestLifecycleHeadLagRepairsFromData 断言 head 落后/丢失时按数据文件修补
- `func TestLifecycleQueueCrashTailDropped(t *testing.T)` — TestLifecycleQueueCrashTailDropped 断言 queue.jsonl 的未换行残尾按未提交丢弃。

### pending_tail.go

- `func (store *storeEngine) messageShardFilesLocked(key Key) ([]string, error)` — messageShardFilesLocked 列出 message 目录下的分片文件，按文件名解析出的
- `func (store *storeEngine) pendingTailLocked(key Key, head messageHead) ([]Event, []string, bool, error)` — pendingTailLocked 读出「seq > head.LastSeq」的全部物理行（调用方持
- `func classifyPendingTail(head messageHead, rows []Event, duplicate bool) (string, string)` — classifyPendingTail 判定草稿尾部的可恢复性（基座校验）。
- `func (store *storeEngine) pendingTailReportLocked(key Key) (PendingTailReport, error)` — pendingTailReportLocked 是探测的锁内实现。
- `func (store *storeEngine) pendingTailReport(key Key) (PendingTailReport, error)` — pendingTailReport 探测草稿尾部（只读，不发布、不清理）。
- `func (store *storeEngine) draftBaseSeqLocked(key Key, head messageHead) (uint64, error)` — draftBaseSeqLocked 返回"物理末行 seq"作为草稿续号基准（调用方持 messageMu）。
- `func (store *storeEngine) appendDraftRowsLocked(key Key, commitID string, rows []Event) ([]Event, error)` — appendDraftRowsLocked 逐步 append（写者草稿尾）：把行写进物理尾并 sync，但
- `func (store *storeEngine) appendDraftRows(key Key, commitID string, rows []Event) ([]Event, error)` — appendDraftRows 是 appendDraftRowsLocked 的加锁入口（逐步 append 的公开面）。
- `func (store *storeEngine) publishDraftTail(key Key) (PendingTailReport, error)` — publishDraftTail 把本轮逐步 append 的草稿尾一次性发布（写者视角的发布入口，
- `func (store *storeEngine) recoverPendingTail(key Key) (PendingTailReport, error)` — recoverPendingTail 把基座一致的草稿尾部**显式发布**为已提交（L2 恢复）。
- `func (store *storeEngine) publishPendingTailLocked(key Key, head messageHead, rows []Event) error` — publishPendingTailLocked 重建分片索引并把草稿尾部提升为已发布，然后原子
- `func (store *storeEngine) discardPendingTail(key Key) (PendingTailReport, error)` — discardPendingTail 显式清理草稿尾部（与提交路径 reap 的清理口径一致），
- `func (repository *jsonRepository) pendingTailWorkspace(key Key) (PendingTailReport, bool, error)`
- `func (repository *jsonRepository) appendDraftRowsWorkspace(key Key, rows []Event) ([]Event, bool, error)` — appendDraftRowsWorkspace 追加运行期草稿后缀，但不推进 message head。
- `func (repository *jsonRepository) recoverPendingTailWorkspace(key Key) (PendingTailReport, bool, error)`
- `func (repository *jsonRepository) discardPendingTailWorkspace(key Key) (PendingTailReport, bool, error)`

### pending_tail_test.go

- `func appendPendingTail(t *testing.T, store *storeEngine, key Key, shardName string, rows []Event)` — appendPendingTail 用生产续号函数算出未发布增量，再直接追加到指定分片
- `func appendRawPendingTail(t *testing.T, store *storeEngine, key Key, shardName string, rows []Event)` — appendRawPendingTail 原样写盘（保留调用方给的显式 seq，用于构造基座断裂）。
- `func lastShardName(t *testing.T, store *storeEngine, key Key) string`
- `func TestPendingTailProbeDecodesOnlyCandidateTail(t *testing.T)` — TestPendingTailProbeDecodesOnlyCandidateTail 钉住冷加载探测的读放大边界：
- `func TestPendingTailCleanAfterNormalCommit(t *testing.T)` — TestPendingTailCleanAfterNormalCommit 正常提交后没有草稿尾部。
- `func TestPendingTailRecoversRowsBeyondPublishedPoint(t *testing.T)` — TestPendingTailRecoversRowsBeyondPublishedPoint 覆盖 L2 主路径：已 append
- `func TestPendingTailRecoversExtraShardFile(t *testing.T)` — TestPendingTailRecoversExtraShardFile 分片滚动后崩溃：head 未索引的分片
- `func TestPendingTailGapIsReportedButNotPublished(t *testing.T)` — TestPendingTailGapIsReportedButNotPublished 基座断裂（尾部号与发布点不
- `func TestPendingTailDiscardIsExplicit(t *testing.T)` — TestPendingTailDiscardIsExplicit 显式丢弃把「清理」变成可观测操作，并让
- `func TestPendingTailRecoverKeepsHeadOnlyFields(t *testing.T)` — TestPendingTailRecoverKeepsHeadOnlyFields 恢复发布不得丢 head 独有字段

### queue_consume.go

- `func queueItemView(item queueItem) QueueItem`
- `func queueItemsView(items []queueItem) []QueueItem`
- `func (store *storeEngine) queueMarkConsumed(key Key, turnID string) error` — queueMarkConsumed 把当前全部待发送项标记为被 turnID 这一轮消费（提升批次
- `func (store *storeEngine) queueConfirmConsumed(key Key, turnID string) error` — queueConfirmConsumed 该轮 message 已发布 → 该轮的消费项出队（幂等）。
- `func (store *storeEngine) queueFailConsumed(key Key, turnID string) error` — queueFailConsumed 该轮未发布（失败）→ 该轮的消费项内容回草稿并出队
- `func (store *storeEngine) queueItems(key Key) ([]queueItem, error)` — queueItems 只读投影（UI/诊断用）。
- `func (store *storeEngine) queueRecoverItems(key Key) (QueueRecoveryReport, error)` — queueRecoverItems 重启恢复（返回条目而非仅计数）：把"已发送未确认"的项
- `func joinQueueContent(contents []string) string` — joinQueueContent 把失败批次的多个内容拼回一份草稿（与提升时的拼接口径
- `func (repository *jsonRepository) queueEnqueueWorkspace(key Key, requestID, content string) (bool, error)` — queueEnqueueWorkspace 把一条排队输入写入 lifecycle 队列（布局门控）。
- `func (repository *jsonRepository) queueMarkConsumedWorkspace(key Key, turnID string) (bool, error)`
- `func (repository *jsonRepository) queueConfirmConsumedWorkspace(key Key, turnID string) (bool, error)`
- `func (repository *jsonRepository) queueFailConsumedWorkspace(key Key, turnID string) (bool, error)`
- `func (repository *jsonRepository) queueItemsWorkspace(key Key) ([]QueueItem, bool, error)`
- `func (repository *jsonRepository) queueRecoverItemsWorkspace(key Key) (QueueRecoveryReport, bool, error)`

### queue_consume_test.go

- `func queueFixture(t *testing.T) (*storeEngine, Key)` — queueFixture 建一个空会话（lifecycle 通道无需先写 message）。
- `func publishTurnRow(t *testing.T, store *storeEngine, key Key, turnID string)`
- `func mustEnqueue(t *testing.T, store *storeEngine, key Key, requestID, content string)`
- `func queueSnapshot(t *testing.T, store *storeEngine, key Key) []QueueItem`
- `func TestQueueConsumeConfirmClearsBatch(t *testing.T)` — TestQueueConsumeConfirmClearsBatch 提升 → 确认 出队，且幂等。
- `func TestQueueConsumeFailReturnsContentToDraft(t *testing.T)` — TestQueueConsumeFailReturnsContentToDraft 该轮未发布（失败）→ 内容回草稿。
- `func TestQueueRecoverResendsUnpublishedConsumedBatch(t *testing.T)` — TestQueueRecoverResendsUnpublishedConsumedBatch 崩溃窗口：已消费但该轮未
- `func TestQueueRecoverDropsConsumedBatchWhenTurnPublished(t *testing.T)` — TestQueueRecoverDropsConsumedBatchWhenTurnPublished 该轮已发布 → 出队，
- `func TestQueueRecoverKeepsUnconsumedItems(t *testing.T)` — TestQueueRecoverKeepsUnconsumedItems 未消费项（用户已发出但还没被提升）
- `func TestQueueRecoverSeparatesTurns(t *testing.T)` — TestQueueRecoverSeparatesTurns 两轮交错：更早的未确认轮不被后一轮的标记
- `func TestQueueRecoverItemsMatchesLegacyQueueRecover(t *testing.T)` — TestQueueRecoverItemsMatchesLegacyQueueRecover 改造后的条目级恢复与既有

### retention.go

- `func (store *storeEngine) retentionThresholds() (int, uint64)` — retentionThresholds 返回 §11 的压缩帧数阈值与原始字节告警线。
- `func (store *storeEngine) readRetentionHeadLocked(key Key) (retentionHead, error)`
- `func (store *storeEngine) readRetentionHead(key Key) (retentionHead, error)` — readRetentionHead 读取 retention head（缺失时先补建默认 manual head）。
- `func (store *storeEngine) lRUDelete(key Key, upToSeq uint64, confirmed bool) (retentionHead, error)` — lRUDelete 删除 watermark 之前的连续前缀（用户确认后调用）。
- `func writeMessageRowsFile(path string, rows []Event) error` — writeMessageRowsFile 用新文件完整写一批行（LRU 重写用）。
- `func sortShards(shards []shardInfo)` — sortShards 按 FromSeq 排序分片索引（防御读取顺序）。

### retention_advisory_lock_test.go

- `func TestRetentionAdvisoryDoesNotPublishMessageHeadOutsideModuleLock(t *testing.T)` — TestRetentionAdvisoryDoesNotPublishMessageHeadOutsideModuleLock 钉住：advisory
- `func TestRetentionAdvisoryHealsMessageHeadWhenLockIsFree(t *testing.T)` — TestRetentionAdvisoryHealsMessageHeadWhenLockIsFree 是上一条的**反向护栏**：

### retention_prefix_eviction_test.go

- `func TestRetentionPrefixEvictionKeepsRewrittenShard(t *testing.T)` — TestRetentionPrefixEvictionKeepsRewrittenShard 回归（小分片淘汰前缀）：

### retention_search_blob_test.go

- `func retentionFixture(t *testing.T) (*storeEngine, Key)`
- `func TestRetentionManualModeRejectsAutoDelete(t *testing.T)` — TestRetentionManualModeRejectsAutoDelete 对应 T-WM-01：mode=manual 触发淘汰 →
- `func TestRetentionConfirmDeleteKeepsHoles(t *testing.T)` — TestRetentionConfirmDeleteKeepsHoles 对应 T-WM-02：用户确认删除前 30 行 →
- `func TestRetentionVerifyAfterDelete(t *testing.T)` — TestRetentionVerifyAfterDelete 对应 T-WM-03：删除后 verify 通过，空洞被接受。
- `func TestRetentionCrashMidRewriteNoIntermediate(t *testing.T)` — TestRetentionCrashMidRewriteNoIntermediate 对应 T-WM-04：删除重写中途崩溃只能
- `func TestRetentionSearchSummaryAfterDelete(t *testing.T)` — TestRetentionSearchSummaryAfterDelete 对应 T-WM-05：行删除后无引用 blob；检索
- `func TestStructuralEventAnchorAfterMessage(t *testing.T)` — TestStructuralEventAnchorAfterMessage 对应 T-EV-01：compacted anchor=msg7 → 事件位于
- `func TestStructuralEventDuplicateCommitIdempotent(t *testing.T)` — TestStructuralEventDuplicateCommitIdempotent 对应 T-EV-02：同 commit 重复持久化 →
- `func TestStructuralEventMissingEventOK(t *testing.T)` — TestStructuralEventMissingEventOK 对应 T-EV-03：message 完整但 compacted 事件缺失 →
- `func TestStructuralEventAnchorAtOrBelowWatermarkOK(t *testing.T)` — TestStructuralEventAnchorAtOrBelowWatermarkOK 对应 T-EV-04：event 锚 ≤ watermark 视
- `func TestSearchPerSessionHits(t *testing.T)` — TestSearchPerSessionHits 对应 T-SR-01：关键词命中正确片段区间；范围=本会话
- `func TestSearchLagAndRebuild(t *testing.T)` — TestSearchLagAndRebuild 对应 T-SR-02：append 后未重建允许落后；重建后结果
- `func TestSearchDeletedRegionSummaryOnly(t *testing.T)` — TestSearchDeletedRegionSummaryOnly 对应 T-SR-03：LRU 删除后重建 → 被删区只
- `func TestBigToolResultHardLimitNotPersisted(t *testing.T)` — TestBigToolResultHardLimitNotPersisted 对应 T-BL-01：输出 > hard_limit → 不落盘、
- `func TestBigToolResultSoftLimitTruncatesWithRef(t *testing.T)` — TestBigToolResultSoftLimitTruncatesWithRef 对应 T-BL-02：输出 > soft_limit → 截断 +
- `func TestBigToolResultQuotaDryRun(t *testing.T)` — TestBigToolResultQuotaDryRun 对应 T-BL-03：超会话配额 GC dry-run 输出可回收清单，
- `func TestStorageSettingsOverrideAndValidation(t *testing.T)` — TestStorageSettingsOverrideAndValidation 对应 T-CFG-01：修改 wire_recent_errors/阈值
