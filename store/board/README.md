# store/board

## 生态位

项目看板追加投影的唯一边界上下文实现。主要调用方是根 `store.Router` 的迁移期兼容
转发、`kernel/board` 的投影同步/读取端口，以及 `kernel/board/boardtest` 的测试替身。

## 职责与非职责

- 职责：行信封校验、按会话分目录、追加、崩溃残尾修复、折叠读、有界聚合读、压实、
  按路径分片锁、会话投影回收和确定性 `BOARD.md` 渲染。
- 非职责：判断任务/团队事实、生成投影行、读写消息会话、决定工具注册。看板是可删除、
  可重建的派生视图，不是事实源。

## 目录结构

- `board.go`：常量、路径与输入闸、折叠、追加/压实/读、聚合读、回收与 Markdown 刷新。
- `markdown.go`：折叠行的纯函数 Markdown 渲染与文本清洗调用。
- `board_test.go`：领域拥有的路径、写读、半写恢复、上限、锁身份与渲染回归。

## 核心实现

`contract/dto.ProjectBoardRow` 是跨层行身份。同一 `(Session, Key)` 取最大 `Seq`；
最新行 `payload.removed=true` 作为 tombstone 隐藏条目；输出按 `(Kind, Key, Session)`
稳定排序。`Mutex(path)` 是投影路径的唯一锁身份表，不进入会话模块锁枚举。

## 数据流与生命周期

同步器或根兼容入口提交一批行 → 校验项目根、会话目录段、白名单文件、会话归属与上限 →
截断崩溃残尾 → 幂等检查/必要时压实 → 追加并 sync → 重新折叠本项目各会话目录 →
原子发布 `BOARD.md`。删除会话只回收该会话目录，并在成功后重渲染汇总件。

## 依赖方向

允许依赖 `contract/dto`、`store/root`、`store/internal/textsanitize` 与标准库。
禁止 import 根 `store`、application、middle、backend、wiring，也禁止 import 其他
`store/<domain>` 实现。

## 并发、存储与错误语义

同一物理投影路径映射到同一把 `sync.Mutex`，不同路径互不阻塞。空批不建文件；单批或
压实后累计越界显式报错；越界批在落盘前失败。坏 JSON 行和未换行残尾不构成可见行。
`BOARD.md` 重渲染失败只记日志，不回滚已成功的行级追加。

## 扩展方式

新增投影 kind 时先修改 `contract/dto/project_board.go` 的唯一词表，再更新 `Kinds`、
折叠兼容性与渲染；新增文件必须在 `SessionFile` 白名单登记。禁止在根包复制追加、
折叠或路径判据。

## Review 指南

重点检查会话 ID 是否始终经过 `SessionSegment`，外来会话行是否整批拒绝，压实后水位是否
单调，空批是否零落盘，锁是否仍按物理路径独立，以及 Markdown 是否不含绝对路径、
`ws-<nano>` 和抖动时间。

## 测试与验证

```text
go test ./store/board -count=1
go test ./store ./kernel/board ./wiring/registry ./e2e -count=1
```

根包 `project_board_compat_test.go` 单独验证 Router 薄转发和跨模块锁身份。

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### board.go

- `func Kinds() []string` — Kinds returns the accepted row-kind vocabulary in stable order.
- `func isKind(kind string) bool`
- `func SessionFile(file string) (string, error)` — SessionFile validates a file in a session projection directory.
- `func SessionSegment(sessionID string) (string, error)` — SessionSegment validates and normalizes a session ID used as a directory name.
- `func ValidateProjectRoot(projectRoot string) error` — ValidateProjectRoot rejects an empty project root explicitly.
- `func Fold(rows []dto.ProjectBoardRow, logf func(string, ...any)) []dto.ProjectBoardRow` — Fold is the single read rule for append-only projection rows.
- `func rowLess(left, right dto.ProjectBoardRow) bool`
- `func Dir(projectRoot string) string` — Dir returns the board directory beneath a project root.
- `func SessionDir(projectRoot, sessionID string) string` — SessionDir returns the conventional directory for one session.
- `func sessionPath(projectRoot, sessionID, file string) (string, error)`
- `func markdownPath(projectRoot string) string`
- `func Mutex(path string) *sync.Mutex` — Mutex returns the single lock identity for a physical board path.
- `func readFile(path string) ([]dto.ProjectBoardRow, error)`
- `func decodeRows(data []byte) []dto.ProjectBoardRow`
- `func encodeRows(rows []dto.ProjectBoardRow) ([]byte, error)`
- `func batchAlreadyAppended(existing, rows []dto.ProjectBoardRow) bool`
- `func stampSession(sessionID string, rows []dto.ProjectBoardRow) error`
- `func watermark(rows []dto.ProjectBoardRow) uint64`
- `func maxSeq(rows []dto.ProjectBoardRow) uint64`
- `func readRowsForAppend(path string) ([]dto.ProjectBoardRow, error)`
- `func compactFile(path string, existing []dto.ProjectBoardRow) ([]dto.ProjectBoardRow, error)`
- `func Append(projectRoot, sessionID, file string, rows []dto.ProjectBoardRow) (uint64, error)` — Append adds one batch and returns the file watermark.
- `func ReadRows(projectRoot, sessionID, file string) ([]dto.ProjectBoardRow, error)` — ReadRows returns one session file folded to its current view.
- `func Watermark(projectRoot, sessionID, file string) (uint64, error)` — Watermark returns the maximum Seq in one session file, or zero when absent.
- `func Read(projectRoot string, opts dto.ProjectBoardReadOptions) ([]dto.ProjectBoardRow, bool, error)` — Read returns bounded, folded rows for one file across project sessions.
- `func readFolded(projectRoot string) ([]dto.ProjectBoardRow, error)`
- `func sessionDirs(projectRoot string) []string`
- `func refreshMarkdown(projectRoot string)`
- `func Reclaim(projectRoot, sessionID string) error` — Reclaim removes one session's projection directory.
- `func ReadMarkdown(projectRoot string) ([]byte, error)` — ReadMarkdown reads the deterministic human-readable projection.

### board_test.go

- `func boardRow(kind, key string, seq uint64, removed bool) dto.ProjectBoardRow`
- `func withSession(row dto.ProjectBoardRow, sessionID string) dto.ProjectBoardRow`
- `func lineCount(t *testing.T, path string) int`
- `func TestFoldLatestWinsAndDropsTombstones(t *testing.T)`
- `func TestFoldSkipsUnknownRows(t *testing.T)`
- `func TestAppendIdempotentAndCrashTail(t *testing.T)`
- `func TestPathGuardsAndForeignSessionRejection(t *testing.T)`
- `func TestBoundsEmptyAndCompaction(t *testing.T)`
- `func TestReadAggregatesAndBoundsSessions(t *testing.T)`
- `func TestMarkdownDeterministicAndSanitized(t *testing.T)`
- `func TestReclaimOnlyRemovesOneSessionAndMarkdownRefreshes(t *testing.T)`
- `func TestPathLocksHaveFileIdentity(t *testing.T)`
- `func workRow(kind, key string, payload map[string]any) dto.ProjectBoardRow`

### markdown.go

- `func Markdown(rows []dto.ProjectBoardRow) []byte` — Markdown renders folded projection rows as deterministic BOARD.md bytes.
- `func factsWriter(write func(string) (int, error)) func(key, value string)`
- `func payloadText(payload map[string]any, key string) string`
- `func payloadTextList(payload map[string]any, key string) []string`
- `func cell(value string) string`

