# store/media（根包分卷）

## 生态位

媒体分区、检索索引与派生通道（工具记录、定时任务事件）

覆盖：`media*.go`、`search_index*.go`、`tool_record*.go`、`schedule_events*.go`；未归属文件由覆盖自检拦下。

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### media.go

- `func MediaStoreOf(repository Repository) (MediaStore, bool)` — MediaStoreOf 从 Repository 取媒体分区能力；后端不支持时返回 false。
- `func (router *Router) WriteMediaWorkspace(ctx context.Context, projectID, sessionID string, item MediaItem) (MediaRef, error)` — WriteMediaWorkspace 在显式项目作用域下写入一条媒体资产（截屏/看图类工具
- `func (router *Router) ReadMediaWorkspace(ctx context.Context, projectID, sessionID, ref string) (MediaRef, []byte, error)` — ReadMediaWorkspace 在显式项目作用域下按 `media:<hash>` 读回媒体资产
- `func MediaRefHash(ref string) string` — MediaRefHash 归一化 ref 为内容 hash；非 `media:` 引用返回空串。
- `func ReferencedMediaHashes(results []ToolResult) map[string]bool` — ReferencedMediaHashes 从工具结果里收集被引用的媒体 hash（GC 的 referenced 集）。
- `func (repository *jsonRepository) WriteMedia(_ context.Context, key Key, item MediaItem) (MediaRef, error)` — WriteMedia 落盘一条媒体资产并返回引用；内容相同则复用已有文件（幂等）。
- `func (repository *jsonRepository) ReadMedia(_ context.Context, key Key, ref string) (MediaRef, []byte, error)` — ReadMedia 读取媒体元数据与二进制原文。
- `func (repository *jsonRepository) ListMedia(_ context.Context, key Key) ([]MediaRef, error)` — ListMedia 列出会话内全部媒体资产（按创建时间、hash 稳定排序）。
- `func (repository *jsonRepository) CollectMedia(_ context.Context, key Key, referenced map[string]bool, dryRun bool) ([]string, error)` — CollectMedia 删除无引用的媒体目录（referenced 来自 ReferencedMediaHashes）；
- `func (store *storeEngine) mediaLimits() (maxItemBytes, quotaBytes, maxItems, maxLongSide int)` — mediaLimits 取 §11 覆盖链解析后的媒体限额。
- `func (store *storeEngine) mediaRoot(key Key) string` — mediaRoot 是会话媒体分区根目录（与 big_tool_result 平级、配额独立）。
- `func (store *storeEngine) mediaItemDir(key Key, digest string) string` — mediaItemDir 是单条媒体的内容寻址目录。
- `func (store *storeEngine) mediaItemMetaPath(key Key, digest string) string` — mediaItemMetaPath 是单条媒体的索引文件。
- `func mediaHash(data []byte) string` — mediaHash 是媒体内容寻址摘要：完整 sha256 十六进制（目录名兼作去重键）。
- `func sanitizeMediaName(name, digest string) string` — sanitizeMediaName 保留原文件名，仅剥离目录与文件系统非法字符；
- `func isReservedWindowsName(name string) bool` — isReservedWindowsName 判断 Windows 保留设备名（CON/PRN/AUX/NUL/COM1..9/LPT1..9）。
- `func (store *storeEngine) writeMediaLocked(key Key, item MediaItem) (MediaRef, error)` — writeMediaLocked 在已持锁的前提下落盘媒体（调用方负责加锁）。
- `func (store *storeEngine) readMediaLocked(key Key, ref string) (MediaRef, []byte, error)` — readMediaLocked 在已持锁的前提下读取媒体元数据与原文。
- `func (store *storeEngine) readMediaMetaFile(key Key, digest string) (MediaRef, error)` — readMediaMetaFile 读单条媒体索引；缺失视为「未落盘」。
- `func (store *storeEngine) writeMediaMetaFile(key Key, item MediaRef) error` — writeMediaMetaFile 写单条媒体索引（原子替换）。
- `func (store *storeEngine) listMediaItems(key Key) ([]MediaRef, error)` — listMediaItems 枚举会话媒体（目录为事实来源，索引缺失条目跳过而非报错）。
- `func (store *storeEngine) mediaUsageBytes(key Key) (int, error)` — mediaUsageBytes 统计媒体分区实际占用（配额按真实字节计）。
- `func (store *storeEngine) publishMediaIndexLocked(key Key) error` — publishMediaIndexLocked 发布 metadata/media.json（会话级媒体索引 head，可重建）。
- `func (store *storeEngine) mediaGarbageCollectLocked(key Key, referenced map[string]bool, dryRun bool) ([]string, error)` — mediaGarbageCollectLocked 删除无引用的媒体目录，返回被删除的 hash 列表。
- `func containsString(values []string, target string) bool` — containsString 判断切片是否已含目标值。

### media_router_test.go

- `func openMediaRouter(t *testing.T) *Router` — openMediaRouter 构造走真实 JSON 后端的 Router（媒体分区在 jsonRepository 上）。
- `func TestRouterWriteReadMediaWorkspace(t *testing.T)` — TestRouterWriteReadMediaWorkspace 验证 Router 级媒体接缝：显式项目作用域
- `func TestRouterMediaWorkspaceUnsupportedBackend(t *testing.T)` — TestRouterMediaWorkspaceUnsupportedBackend 验证后端没有媒体分区能力时

### media_test.go

- `func mediaTestPNG(t *testing.T, width, height int) []byte` — mediaTestPNG 生成一张可识别尺寸的真 PNG，避免用假字节掩盖编码问题。
- `func openMediaTestStore(t *testing.T, settings Settings) (Repository, MediaStore)`
- `func mediaTestKey() Key`
- `func TestMediaStoreKeepsOriginalNameUnderHashFolder(t *testing.T)` — TestMediaStoreKeepsOriginalNameUnderHashFolder 锁定布局契约：
- `func TestMediaStoreNeverTruncates(t *testing.T)` — TestMediaStoreNeverTruncates 是不变式测试：媒体没有字符软限，
- `func TestMediaStoreRejectsByLimitAxis(t *testing.T)` — TestMediaStoreRejectsByLimitAxis 覆盖三条硬限轴（单件字节 / 像素长边 / 条目数 / 配额）。
- `func TestMediaStoreDeduplicatesByContent(t *testing.T)` — TestMediaStoreDeduplicatesByContent 验证内容寻址：同字节只落一份，
- `func TestMediaStoreCollectsUnreferenced(t *testing.T)` — TestMediaStoreCollectsUnreferenced 验证 GC：引用集内的保留，集合外的删除，
- `func TestMediaStoreRejectsBadRef(t *testing.T)` — TestMediaStoreRejectsBadRef 覆盖非法与缺失引用：必须显式报错，不得静默空结果。
- `func TestMediaQuotaIsIndependentFromTextBlob(t *testing.T)` — TestMediaQuotaIsIndependentFromTextBlob 验证分轴配额的独立性：
- `func TestMediaRefsRoundTripThroughToolResult(t *testing.T)` — TestMediaRefsRoundTripThroughToolResult 验证多模态引用随工具结果落盘可还原。

### schedule_events.go

- `func (store *storeEngine) scheduleEventCommit(key Key, kind structuralEventKind, payload ScheduleEventPayload) error` — scheduleEventCommit 写一条 schedule.* EVENT（唯一写序 = message → event）。
- `func (router *Router) ScheduleRegisterWorkspace(projectID, sessionID string, payload ScheduleEventPayload) error` — ScheduleRegisterWorkspace 写 schedule.registered EVENT（冷启动重建 timer）。
- `func (router *Router) ScheduleCancelWorkspace(projectID, sessionID string, payload ScheduleEventPayload) error` — ScheduleCancelWorkspace 写 schedule.cancelled EVENT。
- `func (router *Router) ScheduleFireWorkspace(projectID, sessionID string, payload ScheduleEventPayload) error` — ScheduleFireWorkspace 写 schedule.fired EVENT（审计；timer 实际触发由运行期处理）。
- `func (router *Router) scheduleEvent(projectID, sessionID string, kind structuralEventKind, payload ScheduleEventPayload) error`

### search_index.go

- `func (store *storeEngine) searchIndexPath(key Key) string`
- `func (store *storeEngine) rebuildSearchIndex(key Key) error` — rebuildSearchIndex 从 message 全量重建索引（可重建断言）；LRU 已删前缀
- `func appendHit(hits []searchHit, fromSeq, toSeq uint64, snippet string) []searchHit`
- `func appendSummaryHit(hits []searchHit, fromSeq, toSeq uint64) []searchHit`
- `func tokenize(text string) []string` — tokenize 把中文/英文内容切成关键词 token（长度 ≥ 2；中文按单字滑窗
- `func isTokenRune(r rune) bool`
- `func isCJK(r rune) bool`
- `func (store *storeEngine) searchQuery(key Key, query string) ([]searchHit, error)` — searchQuery 模糊检索本会话（允许落后：索引为上次重建快照；缺失时先
- `func sortSearchHits(hits []searchHit)`

### tool_record.go

- `func ToolRecord(repository Repository) (ToolRecordRepository, bool)` — ToolRecord 把 Repository 收窄到工具记录面（与 Teamwork 同一手法：有就有、没有就是没装配）。
- `func (router *Router) ToolRecordFor() (ToolRecordRepository, bool)` — ToolRecordFor 返回**当前活跃后端**的工具记录读/写面（JSON 后端实现它；其他后端 false）。
- `func (store *storeEngine) toolRecordPath(key Key) string` — toolRecordPath 返回该会话的工具记录文件路径。
- `func (repository *jsonRepository) AppendToolRecordSnapshot(_ context.Context, key Key, payload any) error` — AppendToolRecordSnapshot 追加一行整份读数。
- `func (repository *jsonRepository) ReadToolRecordSnapshot(_ context.Context, key Key) (json.RawMessage, bool, error)` — ReadToolRecordSnapshot 读回最后一行整份读数。

### tool_record_test.go

- `func TestToolRecordSnapshotIsSessionScopedAndLastLineWins(t *testing.T)` — 工具记录读数的会话级落盘：一行一批（整份快照）、读侧取最后一行、按会话隔离、
- `func TestToolRecordSnapshotSkipsCrashTail(t *testing.T)` — 崩溃语义：最后一行没写完（残尾）= 没提交，读侧回退到**上一行完整快照**。
