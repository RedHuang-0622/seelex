# store/artifact

## 生态位

不可变内容寻址资产与派生检索索引的 store 领域 owner。主要调用方是
`Stores.Artifacts`、`Stores.Media`（会话作用域媒体入口）、工具结果读写、媒体工具与 fork 复制。

## 职责与非职责

- 职责：大工具输出配额/截断、immutable tool-result 文件、媒体去重/配额/GC、
  `metadata/media.json` 发布、单会话搜索索引构建与查询。
- 非职责：message head 发布、compact 语义、工具执行、看板/团队状态，或用完整
  session repository 作为自有端口。

## 目录结构

- `artifact.go`：共享 layout/settings、分区根路径与锁 ID。
- `session_store.go`：`ScopeResolver` 与 `SessionStore`，把显式项目/会话键解析成 Artifact scope。
- `blob.go`：超大工具文本、tool-result 文件和 blob GC。
- `media.go`：媒体内容寻址、名称清洗、配额、索引和媒体 GC。
- `search.go`：搜索行投影、token 化、索引发布与查询。
- `artifact_test.go`：领域自己的往返、配额、GC、搜索与物理路径回归。

## 数据流

写入先经过领域限额校验，再按内容摘要落到会话内的 `big_tool_result/` 或
`meta/<sha256>/`；媒体成功后发布 `metadata/media.json`。搜索索引由根应用层提供
已发布的 message/compact 投影，本包只负责生成和原子发布派生索引。

## 依赖方向

允许依赖标准库、`contract`、`store/root`、`store/internal/layout`。禁止 import
根 `store`、application/middle/backend/wiring 或其它 store 领域包。

## 并发、存储与错误语义

所有 artifact 分区复用 `layout.Layout` 的同一会话锁身份。媒体/文本硬限整体拒绝，
媒体永不截断；无引用 GC 不改写引用方记录；tool-result 只有进入已发布 refs 索引后才
对读面可见。路径、JSON 字段和 head checksum 与拆分前逐字一致。

## 扩展方式

新增 artifact 类型必须先在 contract 定义跨层形状，再在本包定义内容寻址和 GC 规则；
不得在根包复制摘要、限额、媒体名清洗或 token 化判据。会话作用域实现只能通过
`ScopeResolver` 注入，不得反向 import 根 `store`。

## Review 指南

检查同字节去重、媒体配额独立计账、GC dry-run、delete 父会话后的 fork 资产可读、
tool-result refs 发布顺序，以及根适配器是否只剩 alias/薄转发。

## 测试与验证

```text
go test ./store/artifact -count=1
go test ./store/... ./backend/tool/... ./e2e/... -count=1
```

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### artifact.go

- `func New(layout *storelayout.Layout, settings storeroot.Settings) *Store` — New constructs an artifact store.
- `func (s *Store) SessionRoot(scope storelayout.Scope) string` — SessionRoot returns the session root used by artifact partitions.
- `func (s *Store) BlobDir(scope storelayout.Scope) string` — BlobDir returns <session>/big_tool_result.
- `func (s *Store) MediaRoot(scope storelayout.Scope) string` — MediaRoot returns <session>/meta.
- `func (s *Store) SearchPath(scope storelayout.Scope) string` — SearchPath returns <session>/metadata-index/search.json.
- `func (s *Store) lock(scope storelayout.Scope, id storelayout.ModuleID) *sync.Mutex`

### artifact_test.go

- `func testStore(t *testing.T, settings storeroot.Settings) (*Store, storelayout.Scope)`
- `func TestMediaRoundTripDedupAndPaths(t *testing.T)`
- `func TestMediaLimitsAndGC(t *testing.T)`
- `func TestBlobToolResultAndGC(t *testing.T)`
- `func TestSearchIndexBuildAndQuery(t *testing.T)`

### blob.go

- `func (s *Store) BlobPath(scope storelayout.Scope, hash string) string` — BlobPath returns the content-addressed blob file.
- `func (s *Store) ToolResultPath(scope storelayout.Scope, ref string) string` — ToolResultPath returns the immutable tool-result payload path.
- `func (s *Store) WriteBlob(scope storelayout.Scope, toolName, content string) (Blob, error)` — WriteBlob stores one oversized output record.
- `func (s *Store) ReadBlob(scope storelayout.Scope, hash string) (string, error)` — ReadBlob reads one blob payload.
- `func (s *Store) ListBlobHashes(scope storelayout.Scope) ([]string, error)` — ListBlobHashes lists stored blob hashes in stable order.
- `func (s *Store) CollectBlobs(scope storelayout.Scope, referenced map[string]bool, dryRun bool) ([]string, error)` — CollectBlobs removes unreferenced blobs, or reports candidates for dry-run.
- `func (s *Store) WriteToolResult(scope storelayout.Scope, ref string, payload []byte) error` — WriteToolResult atomically stores an immutable result JSON payload.
- `func (s *Store) ReadToolResult(scope storelayout.Scope, ref string) ([]byte, error)` — ReadToolResult reads an immutable result JSON payload.

### media.go

- `func (s *Store) mediaItemDir(scope storelayout.Scope, digest string) string`
- `func (s *Store) mediaMetaPath(scope storelayout.Scope, digest string) string`
- `func (s *Store) WriteMedia(scope storelayout.Scope, item MediaItem) (MediaRef, error)` — WriteMedia stores an idempotent media asset.
- `func (s *Store) ReadMedia(scope storelayout.Scope, ref string) (MediaRef, []byte, error)` — ReadMedia reads media metadata and bytes.
- `func (s *Store) ListMedia(scope storelayout.Scope) ([]MediaRef, error)` — ListMedia lists media assets in stable creation/hash order.
- `func (s *Store) CollectMedia(scope storelayout.Scope, referenced map[string]bool, dryRun bool) ([]string, error)` — CollectMedia removes unreferenced media directories.
- `func (s *Store) PublishMediaIndex(scope storelayout.Scope) error` — PublishMediaIndex republishes metadata/media.json from the directory source.
- `func (s *Store) writeMediaLocked(scope storelayout.Scope, item MediaItem) (MediaRef, error)`
- `func (s *Store) readMediaLocked(scope storelayout.Scope, ref string) (MediaRef, []byte, error)`
- `func (s *Store) readMediaMetaFile(scope storelayout.Scope, digest string) (MediaRef, error)`
- `func (s *Store) writeMediaMetaFile(scope storelayout.Scope, item MediaRef) error`
- `func (s *Store) listMediaItems(scope storelayout.Scope) ([]MediaRef, error)`
- `func (s *Store) mediaUsageBytes(scope storelayout.Scope) (int, error)`
- `func (s *Store) publishMediaIndexLocked(scope storelayout.Scope) error`
- `func (s *Store) mediaGarbageCollectLocked(scope storelayout.Scope, referenced map[string]bool, dryRun bool) ([]string, error)`
- `func (s *Store) ReadMediaHead(scope storelayout.Scope) (MediaHead, error)` — MediaHead reads the published session media index.
- `func mediaHash(data []byte) string`
- `func MediaHash(data []byte) string` — MediaHash exposes the content-addressing digest for compatibility adapters.
- `func SanitizeMediaName(name, digest string) string` — SanitizeMediaName exposes the single media filename sanitizer.
- `func IsReservedWindowsName(name string) bool` — IsReservedWindowsName reports Windows device names handled by the sanitizer.
- `func MediaRefHash(ref string) string` — MediaRefHash normalizes media:<sha256>.
- `func sanitizeMediaName(name, digest string) string`
- `func isReservedWindowsName(name string) bool`
- `func containsString(values []string, target string) bool`

### search.go

- `func (s *Store) PutSearch(scope storelayout.Scope, rows []SearchRow, toSeq, watermark uint64, compactSummary string) error` — PutSearch builds and atomically publishes an index.
- `func (s *Store) Search(scope storelayout.Scope, query string) ([]SearchHit, error)` — Search reads and queries a session search index.
- `func IsSearchMissing(err error) bool` — IsSearchMissing reports the absent-index state.
- `func appendHit(hits []SearchHit, fromSeq, toSeq uint64, snippet string) []SearchHit`
- `func appendSummaryHit(hits []SearchHit, fromSeq, toSeq uint64) []SearchHit`
- `func tokenize(text string) []string`
- `func isTokenRune(r rune) bool`
- `func isCJK(r rune) bool`
- `func sortSearchHits(hits []SearchHit)`

### session_store.go

- `func NewSessionStore(store *Store, resolver ScopeResolver) *SessionStore` — NewSessionStore constructs a session-scoped media entry.
- `func (s *SessionStore) scope(projectID, sessionID string) (storelayout.Scope, error)`
- `func (s *SessionStore) WriteMedia(_ context.Context, projectID, sessionID string, item MediaItem) (MediaRef, error)` — WriteMedia stores an idempotent media asset.
- `func (s *SessionStore) ReadMedia(_ context.Context, projectID, sessionID, ref string) (MediaRef, []byte, error)` — ReadMedia reads media metadata and bytes.
- `func (s *SessionStore) ListMedia(_ context.Context, projectID, sessionID string) ([]MediaRef, error)` — ListMedia lists media assets.
- `func (s *SessionStore) CollectMedia(_ context.Context, projectID, sessionID string, referenced map[string]bool, dryRun bool) ([]string, error)` — CollectMedia removes unreferenced media assets.
