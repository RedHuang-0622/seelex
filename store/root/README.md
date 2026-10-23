# store/root — 会话存储的基座机制

## 生态位

`store/root` 是 Seelex 会话持久化的**基座机制**包：数据根独占锁、进程存活探测、
无锁共享读、原子发布、JSONL 残尾纪律，以及存储运行参数（`Settings`）。

主要调用方是 `store`：它以 import 别名 `storeroot` 使用本包，`Settings` / 哨兵错误 /
数据根锁读函数一律写限定名（类型身份与错误值身份都不变）。2026-10-17（P8 §5.2）删掉了
过渡期的 `store/root_aliases.go`（已删除）——那批保形别名只为"搬迁期不动调用点"而存在。
除 `store` 与 `wiring/bootstrap` 外没有其它调用方。

来源：M3-S2 按 [`plan.md`](../../docs/2026-10-15-architecture-reshape/plan.md) §2.9
（底座 · store → `store/root/`）从 `store` 抽出，属**物理归位**：
读写路径、锁语义、落盘字节、错误文案逐字不变。

## 职责与非职责

做：

- `lock.owner` 数据根独占锁：`O_EXCL` 建锁、心跳续租、引用计数共享、陈旧判定，
  以及 `allow_multi_process=true` 时"退化为诊断、不夺锁不删锁"的降级臂；
- 进程存活探测（平台分支：unix `signal 0`；Windows `WaitForSingleObject` 判内核终结）；
- 只读共享打开（Windows 带 `FILE_SHARE_*`，与 rename 发布 / 回收删除共存）与
  JSONL 残尾截断；
- 原子发布 `WriteAtomic`（同目录 tmp + rename + 有界退避）与目录删除退避 `RemoveAllWithBackoff`；
- 内容哈希 `Hash`（目录名 `project-<hash>` / `session-<hash>` 的唯一来源）与
  `RandomID`（锁 owner token、临时文件名）；
- `Settings`：v8 存储的运行参数与覆盖链合并/校验（默认值 ← `limits.session_storage.*`）。
  覆盖链只有装配期一层来源，没有第二份落盘配置，也没有「策略/后端」可选档。

不做（刻意）：

- 不持有会话 / 项目 / 模块的存储事实——那是 `store` 的 `Router` /
  `jsonRepository` / `storeEngine`；
- 不认识 message、event、module head 的语义，只提供"怎么落盘"的动作；
- 不决定读侧投影、派生通道与回收策略。

## 目录与文件结构

| 文件 | 职责 |
|---|---|
| `doc.go` | 包级 doc：生态位与"闭包 = 纯标准库"的红线 |
| `data_root_lock.go` | 数据根独占锁的全部实现与锁文件形状（`OwnerRecord`） |
| `process_alive_unix.go` / `process_alive_windows.go` | 平台存活探测 |
| `file_shared_read_other.go` / `file_shared_read_windows.go` | 只读共享打开与整文件共享读 |
| `jsonl_io.go` | JSONL 残尾截断（未提交残行视为可丢） |
| `storage_settings.go` | `Settings`、默认值、覆盖链合并与校验、分片行数 / wire 预算读数 |
| `atomic.go` | 原子发布、目录删除退避、`Hash` / `RandomID` |

## 核心实现

**锁文件（`lock.owner`）**：JSON 记录 `OwnerRecord{pid, owner_token, program, hostname,
data_root, acquired_at, renewed_at}`。取得用 `O_CREATE|O_EXCL`；续租间隔 =
`stale_after_seconds/3` 夹在 10s–2min；陈旧判据 = pid 不可达 **或** 距上次续租超过
`stale_after_seconds`；`auto_recover` 默认 `false`（陈旧锁也只报错，由用户决定是否接管）。
同进程重复打开同一数据根按引用计数共享（`dataRootLocks`），归零时删锁文件并停心跳。

**哨兵错误**：`ErrDataRootLocked`（存活持有者）、`ErrDataRootStaleLock`（陈旧锁未开启接管）。

**原子发布**：`WriteAtomic` 写 `path + "." + RandomID() + ".tmp"` 后 rename，失败按
`renameBackoff`（0/2/5/10/20/40 ms）重试同一个 tmp，超预算删除 tmp 并返回最后一个错误；
重试只能落在 rename 本身（数据 append 已经发生，提交级重试会重复追加）。

**共享读**：Windows 用 `CreateFile` 带 `FILE_SHARE_READ|WRITE|DELETE`（Go 的 `os.OpenFile`
固定不带 DELETE），否则 rename 发布窗口与回收删除都会报 `ERROR_SHARING_VIOLATION`；
非 Windows 直接 `os.Open`（POSIX 语义等价）。语义仍是快照读。

**`Settings`**：字段零值 = 未配置，布尔项用指针（默认值里有 `false`，零值无法区分"未设置"）。
`ResolveSettings` 逐层 `MergeSettings` 叠加后由 `ValidateSettings` 校验；
`ShardRows()` / `WireBudget()` 是分片行数与 wire 预算判据的唯一读数口。

## 数据流或生命周期

本包没有状态机，只有一段持有者生命周期：

```text
store 打开数据根
  └─ AcquireDataRootLock(root, Settings)   # O_EXCL 建锁 → 记锁 → 起续租 goroutine
       ├─ 锁已存在且持有者存活 → ErrDataRootLocked
       ├─ 锁已存在且陈旧       → ErrDataRootStaleLock（auto_recover=true 才接管）
       └─ allow_multi_process=true → 记一条日志后放行（不持锁、不续租、不删锁）
store 关闭（引用计数归零）
  └─ ReleaseDataRootLock(root)             # 停心跳 → 删 lock.owner
```

写路径的动作（`WriteAtomic` / `TruncateCrashTail` / `RemoveAllWithBackoff`）由
`store` 在提交点调用，本包不关心调用时机。

## 依赖方向

- **允许**：仅标准库。
- **禁止**：任何 Seelex 包（含 `store`）与任何第三方包。

这条红线是机器断言的（M3-S3 的 I6）：`go list -deps ./store/root` 除自身外不得出现
任何带点的包；`store` 的测试文件同样不许反向依赖。

## 并发、存储、安全与错误语义

- `dataRootLocks` 是进程级全局表（`sync.Mutex` + 引用计数）：同进程的多个
  `store` 实例共享同一个持有者，不重复建锁；
- 续租写失败只延后一次重试，**绝不**因为续租失败放弃写者身份；
- 锁文件与临时文件权限 0o600，目录 0o700；
- 残尾（未换行收尾的半行）视为**未提交**：`TruncateCrashTail` 直接截断续写，
  不得把残行提升为已提交数据；
- Windows 上"进程已终结但句柄未释放"必须判为死（`WaitForSingleObject`），否则被强杀的
  上一份 GUI 会永久占住数据根（2026-09-18 事故）。

## 扩展方式

新增基座机制时：放本包、补中文 doc 注释（索引摘要的唯一事实源），然后重跑索引生成；
若新机制需要 Seelex 包或第三方依赖，先改 I6 门禁并在里程碑记录里说明，不要悄悄放宽。

## Review 指南

最容易出错的地方：

1. **锁语义**：`lock.owner` 的字段名、心跳间隔的夹取区间、陈旧判据、`auto_recover`
   默认值都是兼容面，改了会直接改变"能否启动"；
2. **`WriteAtomic` 的退避预算**：收窄会让 Windows 上的发布在外部扫描器持柄时失败；
3. **Windows 共享位**：去掉 `FILE_SHARE_DELETE` 会同时打破读者与回收删除；
4. **闭包纯净性**：任何 import 都会让 I6 变红——这正是它存在的意义；
5. **别名不要变成第二份实现**：`store` 侧只许保留类型/常量/值的别名，
   不许写同构包装函数（`AGENTS.md` §8.1）。

## 测试与验证

本包**没有自己的测试文件**：它的机制都被 `store` 以白盒用例覆盖（同一批用例在
搬迁前后逐字不变），因此覆盖证据在 `store` 侧：

| 面 | 用例 |
|---|---|
| 数据根锁（取得/归还/引用计数/外来活锁/陈旧锁/强杀残留） | `store/data_root_lock_test.go`、`data_root_lock_multiprocess_test.go`、`data_root_lock_windows_test.go` |
| 共享读（Windows 共享位契约） | `store/root/file_shared_read_windows_test.go` |
| 原子发布退避 | `store/root/atomic_publish_windows_test.go`、`lock_hotspot_probe_test.go` |
| 残尾与无锁发布前提 | `store/retention_advisory_lock_test.go`、`c1_publish_probe_test.go` |
| `Settings` 默认值/覆盖链/校验 | `store/retention_search_blob_test.go`、`wire_soft_budget_test.go` |

命令：

```text
go vet ./store/...
go test ./store/ -count=1
go list -deps ./store/root        # 期望：除自身外没有带点的包（纯标准库）
go test ./e2e/ -count=1           # I6 与 README 门禁
```

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### atomic.go

- `func RemoveAllWithBackoff(path string) error` — RemoveAllWithBackoff 对目录删除做与 rename 发布同一量级的有界退避：
- `func Hash(value string) string`
- `func RandomID() string`
- `func WriteAtomic(path string, data []byte, permission os.FileMode) error`

### atomic_publish_windows_test.go

- `func holdExclusive(t *testing.T, path string) func()` — holdExclusive 以 share mode 0 打开 path（任何 rename 覆盖都会失败），返回释放函数。
- `func TestWriteAtomicSurvivesTransientHandle(t *testing.T)`
- `func TestWriteAtomicGivesUpWithinBudgetAndLeavesNoTemp(t *testing.T)`

### data_root_lock.go

- `func AcquireDataRootLock(root string, settings Settings) error` — AcquireDataRootLock 抢占数据根写者身份。settings 提供 stale_after_seconds
- `func timeOrHalf(value time.Duration) time.Duration`
- `func ReleaseDataRootLock(root string)` — ReleaseDataRootLock 归还持锁计数；归零时删除锁文件并停掉心跳。
- `func DataRootLockedBy(root string) (OwnerRecord, bool)` — DataRootLockedBy 返回该数据根当前的存活持有者（无人持有/未锁定时 ok=false）。
- `func DataRootLockedByWith(root string, staleAfter time.Duration) (OwnerRecord, bool)` — DataRootLockedByWith 按调用方 stale 口径返回持有者信息。
- `func tryWriteOwnerRecord(path, root string, staleAfter time.Duration, autoRecover bool) (OwnerRecord, bool, error)` — tryWriteOwnerRecord 以 O_EXCL 建锁。返回 acquired=false 表示锁文件已存在，
- `func existingOwner(path string, mine OwnerRecord, staleAfter time.Duration, autoRecover bool) (OwnerRecord, bool, error)` — existingOwner 判定既有锁：属于本进程则接管，陈旧按 auto_recover 决定，存活
- `func ownerIsStale(held OwnerRecord, staleAfter time.Duration) bool`
- `func isDataRootLockConflict(err error) bool` — isDataRootLockConflict 报告 err 是否为「数据根被别的持有者占用」（活跃锁
- `func lockError(base error, held OwnerRecord) error`
- `func describeOwner(held OwnerRecord) string`
- `func NewOwnerRecord(root string) OwnerRecord`
- `func ReadOwnerRecord(path string) (OwnerRecord, error)`
- `func WriteOwnerRecord(path string, record OwnerRecord) error`
- `func (holder *dataRootHolder) renewLoop(interval time.Duration)` — renewLoop 持续续租：写失败（例如别的进程正持柄读锁文件）只延后一次重试，

### file_shared_read_other.go

- `func OpenSharedRead(path string) (*os.File, error)` — OpenSharedRead 在非 Windows 平台直接走 `os.Open`：POSIX 没有共享位概念，
- `func ReadSharedFile(path string) ([]byte, error)` — ReadSharedFile 在非 Windows 平台等价于 `os.ReadFile`。

### file_shared_read_windows.go

- `func OpenSharedRead(path string) (*os.File, error)` — OpenSharedRead 以只读 + `FILE_SHARE_READ\|FILE_SHARE_WRITE\|FILE_SHARE_DELETE`
- `func ReadSharedFile(path string) ([]byte, error)` — ReadSharedFile 读整个只读共享文件（模块 head / 目录枚举 meta），是

### file_shared_read_windows_test.go

- `func holdForDelete(t *testing.T, path string) *os.File` — holdForDelete 以「GENERIC_READ\|DELETE 访问 + 全共享位」打开 path，等价于 rename
- `func TestReadSharedFileSurvivesPublishWindow(t *testing.T)` — TestReadSharedFileSurvivesPublishWindow 钉住缺陷 1：目标被"发布中"的 DELETE 访问
- `func TestOpenSharedReadDoesNotBlockEvictionDelete(t *testing.T)` — TestOpenSharedReadDoesNotBlockEvictionDelete 钉住缺陷 2：读者持柄时，淘汰/回收

### jsonl_io.go

- `func TruncateCrashTail(file *os.File) error` — TruncateCrashTail 把 JSONL 文件末尾未换行收尾的半行截断，保证后续追加

### process_alive_unix.go

- `func ProcessAlive(pid int) bool` — ProcessAlive 用 signal 0 探测 pid。EPERM 说明进程存在但不属于当前用户，

### process_alive_windows.go

- `func ProcessAlive(pid int) bool` — ProcessAlive 判断 pid 是否仍属于某个存活进程。

### root_unit_test.go

- `func TestWriteAtomicCreatesOverwritesAndLeavesNoTemp(t *testing.T)`
- `func TestWriteAtomicFailureLeavesNoPartialTarget(t *testing.T)` — 失败姿态：目标不可替换（这里是"目标路径是个目录"）时，必须返回错误、
- `func TestHashIsStableAndDistinct(t *testing.T)`
- `func TestRandomIDIsHexAndUnique(t *testing.T)`
- `func TestRemoveAllWithBackoff(t *testing.T)`
- `func TestTruncateCrashTailBoundaries(t *testing.T)` — TruncateCrashTail 的残尾裁定：只有"末尾没有换行"才算残尾；残尾整段丢弃，
- `func TestProcessAlive(t *testing.T)`
- `func TestSettingsDefaultsAndOverlayChain(t *testing.T)`
- `func readFile(t *testing.T, path string) string`
- `func entryNames(entries []os.DirEntry) []string`

### storage_settings.go

- `func NewSettings() Settings` — NewSettings 返回一个只含指定覆盖项的 Settings（全部零值 = 用默认）。
- `func boolValue(value *bool, fallback bool) bool` — 布尔项用指针：设计默认值里有 false（auto_recover），零值无法区分「未设置」。
- `func DefaultSettings() Settings` — DefaultSettings 返回设计默认值（§11 表格 + §5.2 预算）。
- `func ResolveSettings(layers ...Settings) Settings` — ResolveSettings 按覆盖链合并：base（默认）← limits ← persisted。
- `func MergeSettings(base, override Settings) Settings`
- `func ValidateSettings(config Settings) error` — ValidateSettings 校验配置覆盖（非法值显式拒绝）。
- `func (settings Settings) ShardRows() int` — ShardRows 返回 message/event 分片行数（已解析，恒 > 0）。
- `func (settings Settings) WireBudget() (int, int, int)` — WireBudget 返回 (budget, softTokens, targetTokens)（§5.2）。

