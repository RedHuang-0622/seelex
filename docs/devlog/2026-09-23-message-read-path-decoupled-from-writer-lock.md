# message 读路径与写者解耦（C2）+ Windows 只读共享位修复

日期：2026-09-23
关联：`docs/2026-09-08-session-storage-architecture/conformance-checklist.md` §5 H2/H10、§6 C2 行；
`my_design.md`（message 读路径补充）；`recovery-order.md` C1/C2 行；`sessionstore/README.md`。
前置：`041dc22`（durable queue + 单写者 + 草稿尾）、`9643ecd`（迭代边界不再重入会话锁）。

## 1. 问题（H2）：读写共用 `messageMu`

pprof 归因显示 20.39 s 锁延迟里 19.77 s（90.4%）落在 message 路径的 `messageMu` 上，
而其中 66% 的等待是**写者在等读者**：一次全量历史读让同会话提交多等 91.7 ms（基线
63.7 ms，预算 30 ms）。根因不是锁太多，而是**读路径持写锁做整段解码**。

## 2. 改法：无锁 head 快照 + 锁外解码

- `readRows` / `readTailRowsForSelection` 不再取 `messageMu`：只做一次无锁 head 快照
  （`readMessageHead` → 自愈读入口，快路径零锁、慢路径 `TryLock` 不抢锁、绝不把
  `seq > last_seq` 的未发布行提升为已提交），随后在锁外解码分片；
- 统一入口 `storeEngine.withPublishedHead`：快照取一次 → 按快照解码 → 解码失败且
  **坐标已漂移**（分片索引/发布点变了，即并发清理删掉了快照引用的分片）时以新快照
  **重试一次**；坐标没变还失败就是真错误（损坏/缺片），原样上抛，不掩盖（有界，不构成
  循环）；
- `readRowsLocked` 留给写路径（LRU 行删除、压缩重写、fork 前置读）——它们要与自己的写
  临界区同锁；
- 唯一保留独占锁的**只读**面 = `verifyMessage`：完整性检查器的判据就是「head 与分片同时
  定格」，与并发清理共用无锁快照会把正常清理报成损坏；
- 可见性契约（D2 红线不变）：读者看到的是快照那一刻的发布点（MVCC 式陈旧读），解码一律
  以快照 `last_seq` 为上限，草稿尾天然不可见。

## 3. 有牙证明（把解码放回锁内 → 红）

| 测试 | 修复后 | PROBE（解码放回锁内） |
|---|---|---|
| `TestMessageReadDecodeOutsideWriterLock`（结构性：读者钉在锁外解码里，提交仍须完成） | 绿 | 红：提交被读者挡住，10 s 超时 |
| `TestCommitNotBlockedByFullHistoryRead`（时序：读者造成的额外等待 ≤ 30 ms 预算） | 绿（本次复跑额外等待 −4.0 ms / 基线 24.5 ms） | 红：额外等待 72.6 ms（基线 34.5 ms） |

保留前缀淘汰那条同样有牙：去掉 `newPaths` 守卫 → `TestRetentionPrefixEvictionKeepsRewrittenShard`
红（`head 引用的分片不存在: message_21_30.jsonl`）。

**坑（记下来）**：老的时序验收用 `sleep(5ms)` 赌读者先进临界区，在「解码放回锁内」的
PROBE 下会**假绿**（读者被自己的前置 head 读拖到提交之后，两者根本没重叠）。现在用
`publishedReadHook` 事件同步把「读者已在解码」变成确定性前提。

## 4. 全量回归暴露的 Windows 缺陷（H10）与修复

`go test ./... -count=1` 让根包 `TestStorageConcurrentSessionLockProfile` 变红（HEAD 上同
测试绿，即 C2 引入）：4 写 + 4 读并发时，读者 `LoadEventTail` 读
`metadata/message.json` 报 `The process cannot access the file because it is being used by
another process`（`ERROR_SHARING_VIOLATION(32)`）。

**机制**：Go 的 `os.ReadFile`/`os.OpenFile` 在 Windows 上固定 `share=READ|WRITE`（不带
`DELETE`），而 head 由 `writeAtomic` 的 rename 原子发布（rename 内部以 DELETE 访问占住
目标）、分片会被 LRU/reap/压缩重写删除。读写同锁时代这个窗口被锁序列化掉了，解耦后
读者真并发 → ① 读者 open 撞发布窗口失败；② 读者持柄让 `os.Remove` 失败（
`readMessageRowsFileAt` 历史还用 `O_RDWR` 打开分片）。

**实测（单文件单点实验，4 s）**：

| 形态 | 读者 open 失败 | 删除失败 | rename 失败 |
|---|---|---|---|
| 默认读句柄（`os.Open`） | 141 / 5248（全 `err32`） | 2077 / 2192（全 `err32`） | 527 / 1535 |
| 带 `FILE_SHARE_DELETE` | 0 / 9130 | 0（`os.Remove` 全部成功） | 960 / 2631（仍会失败） |

确定性等价形态（目标被一个 DELETE 访问句柄占住 = rename 窗口）：默认读句柄失败
`err32`、带 DELETE 共享位的读成功。

**修法**：新增 `openSharedRead` / `readSharedFile`
（`sessionstore/file_shared_read_windows.go` 用
`CreateFile(GENERIC_READ, FILE_SHARE_READ|FILE_SHARE_WRITE|FILE_SHARE_DELETE)`，
`file_shared_read_other.go` 退化为 `os.Open`/`os.ReadFile`；错误保持 `*fs.PathError`
形状，`errors.Is(err, fs.ErrNotExist)` 口径不变），接线 5 个无锁只读点：
`readModuleHeadFileRaw`（全部模块 head）、`readHeadEnvelopeLenient`、`readMetaFromDir`
（目录枚举 meta，本就无锁，属于同类潜在缺陷）、`readMessageRowsFileAt`、
`scanShardUserInputs`。

**边界（别夸大）**：rename 覆盖仍要求目标无任何打开句柄（实测任何共享位下都返回
`ERROR_ACCESS_DENIED(5)`）→ 写侧 `renameBackoff` 依旧必要；本次只消除「读者自己失败」
与「读者挡住删除」。非 head 的其它只读文件（`compact.jsonl`、media、搜索索引等）仍是同
类未收口面，归 message 读写专项后续批次。

## 5. 验收（本机，2026-09-23）

| 命令 | 结果 |
|---|---|
| `go test ./sessionstore -race -count=1` | ok 55.0 s，无竞争报告 |
| `go test ./... -count=1 -timeout=600s` | 全绿（根包 51.3 s） |
| `go test . -count=5 -run TestStorageConcurrentSessionLockProfile` | ok 11.9 s（修复前单独跑 2/2 红） |
| 4 写 + 4 读探针（同形状，按侧包错误） | 错误数 1 → 0 |
| `TestReadSharedFileSurvivesPublishWindow` / `TestOpenSharedReadDoesNotBlockEvictionDelete` | 绿（各带反向护栏：同一时刻 Go 默认句柄必然失败） |
| `go vet ./sessionstore`、`go build ./...`、`gofmt -l sessionstore docs`、`git diff --check` | 干净 |

## 6. 未做（同专项后续）

- H3：写侧 `appendRowsLocked` 每次整片重读 + `fileSHA256` 整片重算；
- C1：head 体积随分片数线性增长、每次提交整份重写；
- 非 head 只读文件的共享位收口（`compact.jsonl`、media、搜索索引）。
