# store/media（根包分卷）

## 生态位

媒体分区、检索索引与派生通道（工具记录、定时任务事件）

覆盖：`media*.go`、`search_index*.go`、`tool_record*.go`、`schedule_events*.go`；未归属文件由覆盖自检拦下。

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### media.go

- `func (router *Router) WriteMediaWorkspace(ctx context.Context, projectID, sessionID string, item MediaItem) (MediaRef, error)`
- `func (router *Router) ReadMediaWorkspace(ctx context.Context, projectID, sessionID, ref string) (MediaRef, []byte, error)`
- `func MediaRefHash(ref string) string`
- `func ReferencedMediaHashes(results []ToolResult) map[string]bool`
- `func (repository *jsonRepository) WriteMedia(_ context.Context, key Key, item MediaItem) (MediaRef, error)`
- `func (repository *jsonRepository) ReadMedia(_ context.Context, key Key, ref string) (MediaRef, []byte, error)`
- `func (repository *jsonRepository) ListMedia(_ context.Context, key Key) ([]MediaRef, error)`
- `func (repository *jsonRepository) CollectMedia(_ context.Context, key Key, referenced map[string]bool, dryRun bool) ([]string, error)`
- `func (store *storeEngine) mediaLimits() (maxItemBytes, quotaBytes, maxItems, maxLongSide int)`
- `func (store *storeEngine) mediaRoot(key Key) string`
- `func (store *storeEngine) mediaItemDir(key Key, digest string) string`
- `func (store *storeEngine) mediaItemMetaPath(key Key, digest string) string`
- `func mediaHash(data []byte) string`
- `func sanitizeMediaName(name, digest string) string`
- `func isReservedWindowsName(name string) bool`
- `func (store *storeEngine) writeMediaLocked(key Key, item MediaItem) (MediaRef, error)`
- `func (store *storeEngine) readMediaLocked(key Key, ref string) (MediaRef, []byte, error)`
- `func (store *storeEngine) listMediaItems(key Key) ([]MediaRef, error)`
- `func (store *storeEngine) publishMediaIndexLocked(key Key) error`
- `func (store *storeEngine) mediaGarbageCollectLocked(key Key, referenced map[string]bool, dryRun bool) ([]string, error)`
- `func containsString(values []string, target string) bool`

### media_router_test.go

- `func openMediaRouter(t *testing.T) *Router` — openMediaRouter 构造走真实 JSON 后端的 Router（媒体分区在 jsonRepository 上）。
- `func TestRouterWriteReadMediaWorkspace(t *testing.T)` — TestRouterWriteReadMediaWorkspace 验证 Router 级媒体接缝：显式项目作用域
- `func TestRouterMediaWorkspaceUnsupportedBackend(t *testing.T)` — TestRouterMediaWorkspaceUnsupportedBackend 验证后端没有媒体分区能力时

### media_test.go

- `func mediaTestPNG(t *testing.T, width, height int) []byte` — mediaTestPNG 生成一张可识别尺寸的真 PNG，避免用假字节掩盖编码问题。
- `func openMediaTestStore(t *testing.T, settings storeroot.Settings) (Repository, MediaStore)`
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
- `func (store *storeEngine) rebuildSearchIndex(key Key) error`
- `func (store *storeEngine) searchQuery(key Key, query string) ([]searchHit, error)`

### tool_record_test.go

- `func TestToolRecordSnapshotIsSessionScopedAndLastLineWins(t *testing.T)` — 工具记录读数的会话级落盘：一行一批（整份快照）、读侧取最后一行、按会话隔离、
- `func TestToolRecordSnapshotSkipsCrashTail(t *testing.T)` — 崩溃语义：最后一行没写完（残尾）= 没提交，读侧回退到**上一行完整快照**。
