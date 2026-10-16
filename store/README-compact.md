# store/compact（根包分卷）

## 生态位

压缩帧与压缩记录、检查点、运行期材料/尝试缓存

覆盖 9 个文件（前缀 `compact_frames`/`compaction_records`/`checkpoint_store`/`context_state`/`material_cache`/`attempt_cache`）。

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_store_readme_volumes.py`。

### attempt_cache.go

- `func NewAttemptCache(maxItems, maxChars int) *attemptCache` — NewAttemptCache 构造尝试缓存；maxItems/maxChars <= 0 时取设计默认值
- `func (cache *attemptCache) Add(anchorSeq uint64, operationKey, role, content, status string)` — Add 追加一次尝试；超出 max_items/max_chars 时淘汰最旧条目。
- `func (cache *attemptCache) RecentFor(anchorSeq uint64, operationKey string, k int) []attempt` — RecentFor 返回指定锚点 + 操作的最近 K 条尝试（时间升序）。
- `func (cache *attemptCache) Clear()` — Clear 清空缓存（重启/测试用）。

### checkpoint_store.go

- `func NewCheckpointStore(router *Router, projectID string) *CheckpointStore` — NewCheckpointStore 创建绑定到指定项目作用域的 checkpoint 存储。
- `func (store *CheckpointStore) Save(id string, snap *workplanTypes.Snapshot) error` — Save 序列化并持久化 workplan 快照（幂等：同 id 覆盖为最新快照）。
- `func (store *CheckpointStore) Load(id string) (*workplanTypes.Snapshot, error)` — Load 读取 workplan 快照；不存在时返回 ErrCheckpointNotFound。
- `func checkpointStorageKey(id string) string` — checkpointStorageKey 把调用方 checkpoint ID 收敛为安全的会话状态键。

### checkpoint_store_test.go

- `func TestCheckpointStoreRoundTrip(t *testing.T)`
- `func TestCheckpointStoreOverwriteAndMissing(t *testing.T)`
- `func TestCheckpointStoreIsolationAcrossProjects(t *testing.T)`

### compact_frames.go

- `func (store *storeEngine) compactFilePath(key Key) string`
- `func (store *storeEngine) compactCommit(key Key, frame compactFrameRecord) (compactHeadRecord, error)` — compactCommit 追加摘要帧并原子发布 compact head。frame.MessageToSeq
- `func (store *storeEngine) readCompactHeadLocked(key Key) (compactHeadRecord, error)`
- `func (store *storeEngine) readCompactHead(key Key) (compactHeadRecord, error)` — readCompactHead 返回最新 compact head（缺失 = 零值，不报错）。
- `func appendCompactFrameRows(path string, rows []compactFrameRecord) error`

### compaction_records.go

- `func (store *storeEngine) compactionRecordsPath(key Key) string` — compactionRecordsPath 返回压缩记录数据文件（与 compact.jsonl 同层，但**不是
- `func (store *storeEngine) commitCompactionRecords(key Key, records []json.RawMessage) (compactionHeadRecord, int, error)` — commitCompactionRecords 按水位把新增压缩记录追加进本会话通道，并原子发布 head。
- `func (store *storeEngine) readCompactionHeadLocked(key Key) (compactionHeadRecord, error)` — readCompactionHeadLocked 返回最新聚合 head（缺失 = 零值，不报错）。
- `func (store *storeEngine) readCompactionRecords(key Key) ([]CompactionRecordRow, error)` — readCompactionRecords 读取该会话全部压缩记录行（[1, head.Count] 含端点；缺失 =
- `func (store *storeEngine) replaceCompactionRecordsLocked(key Key, records []json.RawMessage) error` — replaceCompactionRecordsLocked 用调用方给的列表整份重写数据文件（前缀指纹不符
- `func compactionRecordChecksum(payload json.RawMessage) string` — compactionRecordChecksum 是单行 payload 的指纹（水位校验用，不承担内容防腐）。
- `func compactionRecordsCommitID(head compactionHeadRecord) string` — compactionRecordsCommitID 由 head 本身推导提交凭据（S17/D13：不用存储层随机号）。
- `func appendCompactionRecordRows(path string, rows []CompactionRecordRow) error` — appendCompactionRecordRows 追加完整 JSONL 行（先截崩溃残尾；再 sync）。
- `func readCompactionRecordRows(path string) ([]CompactionRecordRow, error)` — readCompactionRecordRows 读取 compaction-records.jsonl 的完整行（崩溃残尾跳过，
- `func (store *storeEngine) rebuildCompactionHeadFromData(key Key) (compactionHeadRecord, error)` — rebuildCompactionHeadFromData 从 compaction-records.jsonl 重建 compaction head

### compaction_records_test.go

- `func compactionPayload(t *testing.T, version int, failed bool) json.RawMessage`
- `func TestCompactionRecordsAppendIsWatermarkedAndIdempotent(t *testing.T)`
- `func TestCompactionRecordsRewritesOnDivergentPrefix(t *testing.T)`
- `func TestCompactionRecordsHeadRebuildsFromData(t *testing.T)`

### context_state.go

- `func (repository *jsonRepository) WriteContextState(_ context.Context, key Key, state []byte) error`
- `func (repository *jsonRepository) ReadContextState(_ context.Context, key Key) ([]byte, error)`

### context_state_test.go

- `func TestContextStateIsolatedFromSessionState(t *testing.T)` — TestContextStateIsolatedFromSessionState 契约测试：context 模块独立通道
- `func TestSessionContextStorePersistsToIsolatedChannel(t *testing.T)` — TestSessionContextStorePersistsToIsolatedChannel 验证 SessionContextStore
- `func TestContextStateMissingReturnsNotFound(t *testing.T)` — TestContextStateMissingReturnsNotFound 验证缺失 context 返回 not-found

### material_cache.go

- `func NewMaterialCache(maxEntries int) *MaterialCache` — NewMaterialCache 构造缓存；maxEntries <= 0 时取 256。
- `func (cache *MaterialCache) keyString(key MaterialCacheKey) string`
- `func (cache *MaterialCache) Get(key MaterialCacheKey) (MaterialCacheValue, bool)` — Get 命中返回缓存值；未命中或已失效返回 false。
- `func (cache *MaterialCache) Put(key MaterialCacheKey, value MaterialCacheValue)` — Put 写入/更新缓存并按 LRU 淘汰最旧条目。
- `func (cache *MaterialCache) InvalidateRole(roleSessionID, roleName string)` — InvalidateRole 使某个角色会话的缓存失效（message head/compact/floor 变化后
- `func (cache *MaterialCache) InvalidateAll()` — InvalidateAll 清空缓存。
- `func itoa(value uint64) string`
