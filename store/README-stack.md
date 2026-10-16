# store/stack（根包分卷）

## 生态位

上下文四栈的通道与日志（Plan/Task/Skill/Compact）

覆盖 4 个文件（前缀 `stack_channel`/`stack_journal`）。

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_store_readme_volumes.py`。

### stack_channel.go

- `func validStackKind(kind StackKind) bool`
- `func stackKindIndex(kind StackKind) int` — stackKindIndex 把 kind 映射到固定下标（内存读投影数组用）。
- `func (state *stackState) batchItems(batchID string) []StackItemRecord`
- `func (state *stackState) batchDone(batchID string) bool`
- `func (state *stackState) remove(itemID string)`
- `func (state *stackState) closeBatch(batchID, status string) []StackItemRecord` — closeBatch 把整批从 active 移出并盖上批次弹栈坐标。
- `func (state *stackState) archive(batch []StackItemRecord) []StackItemRecord` — archive 给弹栈条目盖批次弹栈坐标并登记待归档（revision = 本次发布）。
- `func (state *stackState) pushItem(item StackItemInput, batchID, batchFrom string) (StackItemRecord, error)` — pushItem 将一条 StackItemInput 落成 active 行（压栈语义的唯一实现点）。
- `func containsStackItem(rows []StackItemRecord, itemID string) bool`
- `func maxSeqOf(rows []StackItemRecord) uint64` — maxSeqOf 返回一组条目行的最大 seq。
- `func maxU64(left, right uint64) uint64`
- `func stackMutationCommitID(kind StackKind, mutation StackMutation) string` — stackMutationCommitID 把一次栈变更映射为确定性、逐操作唯一的 commit_id
- `func dedupeStackRowsByItemID(rows []StackItemRecord) []StackItemRecord` — dedupeStackRowsByItemID 按条目键去重、高 revision 胜出（§2.0 规则 4 读侧、
- `func stackPushMessage(kind StackKind, batchID string, items []StackItemInput) func(*stackState) (StackMutation, error)` — stackPushMessage 压入一批条目（batchID 为空 = 本次入栈自成一批）。
- `func stackSetStatusMessage(itemID, status string) func(*stackState) (StackMutation, error)` — stackSetStatusMessage 更新条目状态；所属批次全部完成时整批弹栈归档（§2.4）。
- `func stackPopTopMessage(itemID, status string) func(*stackState) (StackMutation, error)` — stackPopTopMessage 弹出栈顶条目（goal LIFO：只有栈顶可收口）。
- `func stackCloseBatchMessage(batchID, status string) func(*stackState) (StackMutation, error)` — stackCloseBatchMessage 显式把整批标记为完成并归档（调用方已知批次收口时用）。
- `func stackReplaceMessage(kind StackKind, items []StackItemInput, closedStatus string) func(*stackState) (StackMutation, error)` — stackReplaceMessage 用新的条目集合替换 active 栈，并推导出逐条迁移：
- `func stackEventPayload(row StackItemRecord) json.RawMessage` — stackEventPayload 是栈迁移 EVENT 的 payload（附录 A.1：只记摘要，不复制正文）。
- `func stackTransitionEvents(kind StackKind, mutation StackMutation) []structuralEvent` — stackTransitionEvents 把一次迁移展开成 EVENT 行（plan.*/task.*/goal.*）。

### stack_channel_test.go

- `func newJSONStackHarness(t *testing.T) *stackHarness`
- `func forEachStackBackend(t *testing.T, fn func(t *testing.T, harness *stackHarness))` — forEachStackBackend 保留表驱动入口；R1 收口后只有 JSON 后端。
- `func (harness *stackHarness) seedMessages(n int)` — seedMessages 把 message 事实写成恰好 n 条（锚的来源）。
- `func (harness *stackHarness) anchorID(index int) string`
- `func (harness *stackHarness) push(kind StackKind, batchID string, items ...StackItemInput) (StackMutation, error)`
- `func (harness *stackHarness) setStatus(kind StackKind, itemID, status string) (StackMutation, error)`
- `func (harness *stackHarness) popTop(kind StackKind, itemID, status string) (StackMutation, error)`
- `func (harness *stackHarness) closeBatch(kind StackKind, batchID, status string) (StackMutation, error)`
- `func (harness *stackHarness) replace(kind StackKind, items []StackItemInput, closedStatus string) (StackMutation, error)`
- `func (harness *stackHarness) active(kind StackKind) []StackItemRecord`
- `func (harness *stackHarness) history(kind StackKind) []StackItemRecord`
- `func (harness *stackHarness) readEventKinds() []string` — readEventKinds 返回该会话已记录的结构性 EVENT kind 列表。
- `func (harness *stackHarness) stats() stackJournalStats` — stats 返回该后端的延迟归因。
- `func (harness *stackHarness) verify() error`
- `func (harness *stackHarness) mustVerify()`
- `func (harness *stackHarness) head(kind StackKind) stackModuleHead` — head 返回指定 kind 的栈模块 head（metadata/stack_<kind>.json）。
- `func TestStackChannelHeadCarriesWatermarkOnly(t *testing.T)` — TestStackChannelHeadCarriesWatermarkOnly 断言 head 只有水位/计数/开放批次
- `func TestStackChannelBatchStaysActiveUntilAllComplete(t *testing.T)` — TestStackChannelBatchStaysActiveUntilAllComplete 断言 §2.4 批次语义：批次内
- `func TestStackChannelBatchPopsWhenAllComplete(t *testing.T)` — TestStackChannelBatchPopsWhenAllComplete 断言整批完成 → 整批弹栈归档，并盖上
- `func TestStackChannelAnchorsFromMessageRows(t *testing.T)` — TestStackChannelAnchorsFromMessageRows 断言 item_message_id = 条目进入 active
- `func TestStackChannelRecordsTransitionEvents(t *testing.T)` — TestStackChannelRecordsTransitionEvents 断言状态迁移由 EVENT（plan.*/task.*/
- `func TestStackChannelGoalRejectsNonTopPop(t *testing.T)` — TestStackChannelGoalRejectsNonTopPop 断言 goal 是 LIFO：只有栈顶可收口。
- `func TestStackChannelReplaceActiveDerivesTransitions(t *testing.T)` — TestStackChannelReplaceActiveDerivesTransitions 断言「保存栈投影」落到逐条迁移：
- `func TestStackChannelRejectsUnknownKindAndDuplicate(t *testing.T)` — TestStackChannelRejectsUnknownKindAndDuplicate 断言非法 kind 与重复入栈显式
- `func TestStackChannelMissingHeadIsNoop(t *testing.T)` — TestStackChannelMissingHeadIsNoop 断言从未写过栈的会话读取为空且不报错。
- `func TestStackChannelPersistsAcrossInstances(t *testing.T)` — TestStackChannelPersistsAcrossInstances 断言通道数据只属于存储，不依赖某个
- `func TestStackChannelForkByMessageAnchor(t *testing.T)` — TestStackChannelForkByMessageAnchor 断言 fork 只带「锚 ≤ from」的条目
- `func TestStackChannelConcurrentKindsSingleWriter(t *testing.T)` — TestStackChannelConcurrentKindsSingleWriter 断言多 kind 并发提交不丢更新：
- `func stackStatsDelta(before, after stackJournalStats) stackJournalStats` — stackStatsDelta 将两个后端归因快照相减（累加器在 store 生命周期内单调）。
- `func TestStackChannelLockAttribution(t *testing.T)` — TestStackChannelLockAttribution 回答两件事：
- `func TestStackChannelWritesDesignedFiles(t *testing.T)` — TestStackChannelWritesDesignedFiles 断言 §3.2 实体→文件：栈条目落在
- `func TestStackChannelUnpublishedProjectionInvisible(t *testing.T)` — TestStackChannelUnpublishedProjectionInvisible 断言 head 是发布点：revision
- `func TestStackChannelCrashTailIgnored(t *testing.T)` — TestStackChannelCrashTailIgnored 断言 JSONL 崩溃残尾被忽略且下一次提交从
- `func TestStackChannelUnpublishedHistoryReaped(t *testing.T)` — TestStackChannelUnpublishedHistoryReaped 断言 head 未发布的归档行在下次提交
- `func assertStackFilesExist(t *testing.T, store *storeEngine, key Key, kind StackKind, wantActive, wantHistory bool)`
- `func slicesContains(values []string, target string) bool`

### stack_journal.go

- `func stackCommit(journal stackJournal, key Key, kind StackKind, mutate func(*stackState) (StackMutation, error)) (StackMutation, error)` — stackCommit 是栈通道的唯一提交入口：载入 → 闭包变更 → 数据 → head → EVENT。
- `func stackCommitLocked(journal stackJournal, key Key, kind StackKind, mutate func(*stackState) (StackMutation, error)) (bool, StackMutation, error)` — stackCommitLocked 执行提交的临界区（load → mutate → publish）。返回
- `func stackReadActive(journal stackJournal, key Key, kind StackKind) ([]StackItemRecord, error)` — stackReadActive 返回该 kind 当前 active 投影（读者入口，后端无关）。
- `func stackReadHistory(journal stackJournal, key Key, kind StackKind) ([]StackItemRecord, error)` — stackReadHistory 返回该 kind 的归档行（后端无关）。
- `func stackVerify(journal stackJournal, key Key) error` — stackVerify 校验 head 水位与数据计数一致（M5 巡检的栈通道部分）。
- `func stackSnapshotAt(journal stackJournal, key Key, kind StackKind, fromSeq uint64) (active []StackItemRecord, archived []StackItemRecord, err error)` — stackSnapshotAt 返回「message 坐标 ≤ fromSeq」时的栈快照（fork 重建，
- `func stackRestoreSnapshot(journal stackJournal, key Key, kind StackKind, active, archived []StackItemRecord) error` — stackRestoreSnapshot 把快照写进目标会话（fork 深拷贝）：归档条目续写归档
- `func stackFork(journal stackJournal, parentKey, childKey Key, fromSeq uint64) error` — stackFork 把父会话三栈按 message 锚重建进子会话（Router.ForkStacks 的
- `func (stats *stackStats) snapshot(backend string) stackJournalStats`
- `func timeSection(counter *atomic.Int64, fn func() error) error` — timeSection 执行 fn 并把耗时（含其内部全部 IO）计入 counter。

### stack_journal_json.go

- `func (repository *jsonRepository) stackJournal() stackJournal` — stackJournal 是 jsonRepository 的 Repository 契约方法：栈通道对每个后端
- `func (store *storeEngine) stackJournal() stackJournal`
- `func (journal *jsonStackJournal) backend() string`
- `func (store *storeEngine) stackDir(key Key, kind StackKind) string` — stackDir / 文件路径（§2.4 放置位置）。
- `func (store *storeEngine) stackActivePath(key Key, kind StackKind) string`
- `func (store *storeEngine) stackHistoryPath(key Key, kind StackKind) string`
- `func (journal *jsonStackJournal) lock(key Key, kind StackKind) func()` — lock 按「会话 × kind」串行化栈提交（S16/D6：每 kind 一份 head 一把锁，
- `func (journal *jsonStackJournal) load(key Key, kind StackKind) (stackLoaded, error)` — load 返回 head 水位与 active 投影：命中内存快照则零 IO，未命中才冷读磁盘
- `func (journal *jsonStackJournal) readHistory(key Key, kind StackKind) ([]StackItemRecord, error)` — readHistory 直接读归档文件（append-only，发布后的行不再改写 → 无锁读）。
- `func (journal *jsonStackJournal) publish(key Key, entry stackPublish) error` — publish 落数据并发布 head：归档回收 → 归档追加 → active 原子替换 → head
- `func (journal *jsonStackJournal) publishView(key Key, head stackModuleHead, kind StackKind, active []StackItemRecord, historyCount uint64)` — publishView 只重建被本次提交改动的 kind 的读投影（S16：各 kind head
- `func (journal *jsonStackJournal) reapHistoryTail(path string, water stackWatermark) error` — reapHistoryTail 把归档文件截回 head 记录的字节水位（head 未发布的追加是
- `func (journal *jsonStackJournal) readStackHead(key Key, kind StackKind) (stackModuleHead, error)` — readStackHead 读取该 kind 的 stack 模块 head（缺失 = 空水位，不报错）。
- `func (journal *jsonStackJournal) anchor(key Key) (string, uint64)` — anchor 返回当前已发布 message 坐标。优先用 message 提交时发布的内存锚：
- `func (journal *jsonStackJournal) watermark(key Key) (uint64, error)` — watermark 返回 message LRU 水位（fork 起点判定，I7）。
- `func (journal *jsonStackJournal) recordEvents(key Key, kind StackKind, mutation StackMutation) error` — recordEvents 把状态迁移写进 EVENT 通道（plan.*/task.*/goal.*）。EVENT 不
- `func (journal *jsonStackJournal) dropCache(key Key)` — dropCache 丢弃该会话的三份读投影（删除会话 / fork 覆盖子会话后）。
- `func (journal *jsonStackJournal) stats() stackJournalStats` — stats 返回 JSON 后端的延迟归因。
- `func readStackRowsFile(path string, headSeq uint64) ([]StackItemRecord, error)` — readStackRowsFile 读取栈 JSONL（跳过空行与崩溃残尾），只保留
- `func decodeStackRows(data []byte) []StackItemRecord`
- `func encodeStackRows(rows []StackItemRecord) []byte`
- `func appendStackRows(path string, rows []StackItemRecord) error` — appendStackRows 以 append-only 方式追加归档行（崩溃残尾先截掉）。
- `func fileSizeOrZero(path string) (uint64, error)`
- `func cloneStackRows(rows []StackItemRecord) []StackItemRecord` — cloneStackRows 复制条目行：读者与写者拿到的都是独立副本，投影不被就地改。
- `func cloneStackWatermarks(kinds map[StackKind]stackWatermark) map[StackKind]stackWatermark`
- `func (store *storeEngine) stackStats() *stackStats` — stackStats 返回会话存储布局的栈通道诊断累加器（newStoreEngine 创建）。
- `func (store *storeEngine) stackViewLoad(key Key, kind StackKind) *stackView` — stackViewLoad 返回该会话该 kind 的已发布读投影；未装载时返回 nil。
- `func (store *storeEngine) stackViewPublish(key Key, kind StackKind, view *stackView)` — stackViewPublish 发布新的读投影（只在提交临界区内调用 → 单写者）。
- `func (store *storeEngine) dropStackViews(key Key)` — dropStackViews 丢弃该会话全部栈读投影（目录被删除/替换后必须作废）。
- `func (store *storeEngine) dropSessionCaches(key Key)` — dropSessionCaches 作废该会话的全部内存读投影（删除会话、换后端、fork 覆盖
