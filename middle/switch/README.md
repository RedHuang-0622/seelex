# middle/switch

> 包名 = `switching`（目录名不能当包名的情形：`switch` 是 Go 关键字，与 `middle/view/state` 落成
> `view_state` 是同类处理）。

## 生态位

`middle/switch` 是中间层"**会话切换与多会话运行**"的落点（plan §2.4：`middle/residency` 管 LRU 驻留、
`middle/switch` 管多会话运行切换）。当前已有：

- 本包（`switching`）：**会话展示元数据**的读写判据（`session_meta.go` 家族）；
- 子域 [`history/`](history/README.md)：历史分页的**窗口装配判据**。

生命周期 / 切换 / 队列 / 驻留调度器家族（`session_{lifecycle,release,draft,fork,ctx,pending_tail,queue_recover}`、
`service_{scheduler,queue,release_window}`、`async_completion`、`service_interaction`、`workspace_usecase`、
顶层 `session/`）随 R6 续做搬入。

## 职责与非职责

**做**：

1. `SetMeta`：写单个会话的展示元数据（置顶 / 别名 / 手动排序位），成功后唤醒目录刷新让所有客户端重拉；
2. `ReadMeta`：读单个会话的展示元数据；
3. 释放面判据（`release.go`，M5-R6-release-surface 接收者转移）：`Busy`（待审批 / 运行中 / 排队输入 /
   在飞请求都算忙）与 `Protected`（受保护集：当前视图 / 待审批 / 运行·排队·在飞 / 正在恢复）——
   引擎驻留层（`resident_lru`）与正文层（`content_lru`）的收敛循环都只调这一份；
4. 草稿尾恢复的**决策表**（`pendingtail.go`，M5-R6-pending-tail 接收者转移）：运行中不抢发布点、
   `clean`/未知状态静默、`recoverable` 恢复并报"恢复了 N 行（发布点 a → b）"、
   `gap` **只报告**（明说内容仍在磁盘，不发布不清理不猜测）；
5. 判据与文案只此一份：空会话 ID 拒绝（`session ID is required`）、未装配存储
   （`ErrMetaUnsupported` = "session meta storage is not assembled"）、写盘失败冒泡且**不**唤醒。
6. 队列编辑面判据（`queue.go`，M5-R6-queue-edit 接收者转移）：`QueueEditGate`（关闭优先于排空；缺错误值
   响亮失败）、`QueueTargetSession` / `QueueTargetError`（空 = 视图会话；视图会话未注册按"没有回合在跑"）、
   以及两个越界文案构造器与三个哨兵（宿主侧保留旧名作为**同一错误值别名**）。

**不做**：不做会话事实（谁在跑、绑定哪个工作区）；不持久化（元数据落在存储层的项目级 meta blob，
由宿主端口 `MetaStore` 代劳）；不发布事件（目录刷新由宿主的 `Wake` 回调负责）。

## 目录与文件结构

| 文件 | 职责 |
|---|---|
| `meta.go` | `MetaDeps`（`MetaStore` + `Wake`）+ `SetMeta` / `ReadMeta` + `ErrMetaUnsupported` |
| `meta_test.go` | 空 ID / 未装配 / 写盘失败不唤醒 / 成功唤醒一次 / 读回转发 |
| `release.go` | `BusyState` + `Busy`、`ProtectedInput` + `Protected`：释放面的两条判据 |
| `release_test.go` | 忙态四轴、受保护集四条来源、空会话 ID 与空视图 ID 的边界 |
| `pendingtail.go` | `PendingTailDeps` + `RecoverPendingTail`：草稿尾恢复的决策表与四条提示文案 |
| `pendingtail_test.go` | 运行中不探测 / clean·未知静默 / 恢复并报数 / gap 只报告 / 探测与恢复失败的文案 |
| `queue.go` | 队列编辑面：`ErrQueue*` 三哨兵 + `QueueEditGate` / `QueueTargetSession` / `QueueTargetError` / 两个越界文案 |
| `queue_test.go` | 门禁次序与装配缺陷、目标会话解析（空/trim/未注册）、三条拒绝文案、越界文案与哨兵身份 |
| `unload.go` | `UnloadGate` / `UnloadDeps` + `Unload`：会话卸载的判据与次序（非活跃先持久化 → 释放 → 活跃收口视图 → 发布 → 刷新目录） |
| `unload_test.go` | 空 ID 与门控两轴（含装配缺陷）、非活跃/活跃两条次序、持久化/释放失败即停与三条文案 |
| `fork.go` | `ForkGate` + `MaxTranscriptEventSeq` + `DeepCopyRecord`：会话分叉的门控次序与两个纯助手 |
| `fork_test.go` | 门控三轴次序与装配缺陷、事件最大 seq、深拷贝（消息/Tool 指针/四个切片/注入的 PlanStack 克隆器） |
| `async.go` | `AsyncCompletionTriggers` + 两条触发正文（`AsyncCompletionPrompt` / `TeamworkCompletionPrompt`）+ teammate 作业表窄端口读取 |
| `async_test.go` | 终态词表（done/failed 触发，killed/running 不触发）、两条正文的字段收窄与工具指向、窄端口读取与未装配降级 |
| `history/` | 历史分页的窗口装配判据（见其 README） |

## 核心实现

- `MetaDeps` 是**值语义**的宿主面：`Store`（窄接口，结构满足 `session.SessionMetaPort`，nil = 未装配）
  与 `Wake`（唤醒目录刷新；nil = 不唤醒）。
- **唤醒只在成功之后**：写盘失败就不推目录（否则所有客户端会被叫起来重拉一份没变化的数据）。
- 展示元数据的语义：全零值 = 清除该会话的条目（取消置顶 + 清空别名 + 复位排序）——由存储侧表达，
  本包只负责"校验 → 写 → 唤醒"这条次序。

## 数据流

```text
前端设元数据 → 门面 SetSessionMeta（断言 Deps.Sessions 上的可选元数据端口）
  → switching.SetMeta（校验会话 ID → Store.SetSessionMeta → 成功则 Wake）
  → 宿主 Wake = 请求会话目录 worker 重拉（发布全局 snapshot.changed）
```

## 依赖方向

允许：标准库、`middle/protocol/model`（`SessionMeta` 形状）。
禁止：`session`（端口由宿主断言后注入）、`store`、`application/*`、`backend/*`。

## 并发、存储与错误语义

- 本包**无状态、无锁**：宿主端口各自持锁；`Wake` 由宿主保证可并发调用。
- 不落盘：持久化在存储层；本包不读盘、不写盘。
- 错误语义：参数错误直接返回（文案固定）；未装配存储返回 `ErrMetaUnsupported`（调用方可用 `errors.Is`）；
  存储错误原样冒泡；三条路径都不唤醒目录。

## 扩展方式

- 新增会话级展示项：加进 `SessionMeta`（契约/协议层）并由存储侧落地，本包的"校验 → 写 → 唤醒"次序不变。
- 新增会话切换用例：在本包加纯判据或薄编排，端口仍由宿主注入；不要把 `session` 域类型带进来。

## Review 指南

- 失败路径是否仍**不唤醒**目录刷新？
- 空会话 ID 与未装配存储的两条文案是否仍唯一（`rg` 只能命中本包）？
- 是否出现第二份元数据校验/唤醒逻辑（宿主侧或前端侧）？

## 测试与验证

```text
go test ./middle/switch/... -count=1
go test ./application/core/ -run 'SessionMeta|Meta' -count=1   # 门面级：断言端口 + 转发
go test ./wiring/adapters/ -run SessionSetting -count=1        # 存储端口实现
```

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### meta.go

- `type MetaStore interface` — 会话展示元数据的读写口（结构满足 `session.SessionMetaPort`）。
- `type MetaDeps struct` — 宿主面：`Store`（nil = 未装配）+ `Wake`（nil = 不唤醒）。
- `var ErrMetaUnsupported` — 未装配元数据存储（目录枚举不受影响，元数据恒为零值）。
- `func SetMeta(deps MetaDeps, sessionID string, meta model.SessionMeta) error` — 写元数据并唤醒目录刷新（失败路径不唤醒）。
- `func ReadMeta(deps MetaDeps, sessionID string) (model.SessionMeta, error)` — 读元数据。

### release.go

- `type BusyState struct` — "会话是否正忙"的四个读数（待审批 / 运行中 / 排队输入 / 在飞请求）。
- `func Busy(state BusyState) bool` — 四个读数任一非零即忙（零值 = 不忙，同 `sessionBusy(nil)` 口径）。
- `type ProtectedInput struct` — 受保护集判据的输入（会话 ID + 视图会话 ID + 恢复态 + 忙态）。
- `func Protected(input ProtectedInput) bool` — 是否属于受保护集（绝不释放）；空会话 ID 亦判为受保护。

### pendingtail.go

- `type PendingTailDeps struct` — 草稿尾决策的宿主面（`Running` / `Probe` / `Recover` 三个窄口）。
- `func RecoverPendingTail(deps PendingTailDeps, sessionID string) string` — 冷加载时探测/恢复草稿尾，返回要补进可见会话的 system 提示（"" = 静默）。

### queue.go

- `var ErrQueueNotRunning` / `ErrQueueSessionNotFound` / `ErrQueueIndexOutOfRange` — 队列编辑的三个哨兵（宿主侧保留同值别名）。
- `type QueueGateErrors struct` — 门禁要原样返回的宿主错误值（`Closed` / `Draining`）。
- `func QueueEditGate(closed, draining bool, errs QueueGateErrors) error` — 关闭优先于排空的队列编辑门禁；缺错误值即 `ErrQueueGateUnassembled`。
- `func QueueTargetSession(sessionID, viewSessionID string) string` — 解析目标会话（空 = 视图会话）。
- `func QueueTargetError(sessionID, viewSessionID string, known, running bool) error` — 目标会话可否编辑（未知 → SessionNotFound；未运行 → NotRunning；都带会话 ID）。
- `func ReorderIndexError(from, to int) error` / `func RecallIndexError(index int) error` — 两个越界文案构造器。

### unload.go

- `type UnloadGate struct` — 卸载门控读数（`Restoring` / `Running`）与宿主忙碌错误值。
- `func (gate UnloadGate) Reject() error` — 恢复中或运行中一律拒绝（缺错误值即 `ErrUnloadGateUnassembled`）。
- `type UnloadDeps struct` — 宿主面：门控取数、是否视图会话、持久化、释放、视图收口、发布、目录刷新。
- `func Unload(deps UnloadDeps, sessionID string) error` — 卸载用例的判据与次序（空 ID → 门控 → 非活跃先持久化 → 释放 → 活跃收口视图 → 发布 → 刷新）。

### fork.go

- `type ForkGate struct` — fork 门控读数（关闭/排空/父会话在跑）与三条宿主错误值。
- `func (gate ForkGate) Reject() error` — 关 → 排空 → 在跑的次序判决；缺错误值即 `ErrForkGateUnassembled`。
- `func MaxTranscriptEventSeq(events []model.TranscriptEvent) uint64` — 事件流最大 Seq（子会话序号基线对齐用）。
- `func DeepCopyRecord(record model.SessionRecord, clonePlanStack func([]model.SessionPlanFrame) []model.SessionPlanFrame) model.SessionRecord` — fork 深拷贝判据（消息/Tool 指针/四个派生切片深拷贝；PlanStack 用注入的克隆器）。

### async.go

- `const TeamworkTriggeredPrefix = "teamwork:"` — 幂等账的按表键前缀（表身份优先于字面量）。
- `func AsyncCompletionTriggers(state dto.AsyncState) bool` — 触发口径认的两个终态（done / failed 触发；killed / running 不触发）。
- `func AsyncCompletionPrompt(record dto.AsyncRunRecord, evidenceChars int) string` — 后台作业完成触发的用户行正文（字段收窄 + bg_manage 指向）。
- `func TeamworkCompletionPrompt(record dto.TeamworkJobCompletionRecord, evidenceChars int) string` — teammate 完成回执正文（指向 team_context / jobs_manage / team_accept）。
- `func TeamworkJobCompletions(runtime any) []dto.TeamworkJobCompletionRecord` / `func TeamworkJobEvents(runtime any) <-chan struct{}` — teammate 作业表的窄可选端口读取（未装配 = nil）。
