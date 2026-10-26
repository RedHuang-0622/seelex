# middle/residency

## 生态位

`middle/residency` 是中间层的**常驻与预算**落点（plan §2.4），当前已有六件判据：

1. 可见快照的**预览截断**（`Preview`，来自 `snapshot_budget.go`）；
2. **LRU 使用序维护**（`Order.Touch` / `Order.Forget`）与**释放额度**（`Budget`）——
   迁移前这两段算法在引擎驻留层、正文层、归档路径与释放面**四处各写了一份**（AGENTS §8.3 的
   同一判据多处），M5-R6-residency-order 收敛到此一处。
3. **目录刷新等待**（`WaitCatalogRefresh`）：等一轮覆盖本次请求的目录刷新，ctx 超时按"最佳努力"
   返回 `ctx.Err()` 而不是失败（M5-R6-session-catalog）。
4. **会话归档**（`Archive`）：门控次序 → 读 record → 驱逐前置 flush / 释放引擎 bundle / 摘驻留序 →
   写归档状态 → 释放后台命令 → 按项目刷新目录（M5-R6-archive，来自 `archive_session.go`）。
5. **两层的超限收敛**（`EvictUntilWithinLimit`）：按使用序逐个卸载**最旧且可卸载**的会话，
   直到占用本层的会话数进入额度；受保护集既不计数也不参与挑选，没有候选即容忍超限。
   正文层（可见正文窗口）与引擎层（引擎 bundle 驻留）逐字同形，因此**只有这一份循环**
   （M5-R6 的宿主编排现归 `application/session/residency/runtime.go`；本包只保留
   纯顺序、预算与驱逐判据）。

`session_cold_read` / `session_input_index` / `window` 随 R6 续做搬入；两层的**受保护集读数**
（要读会话单元状态与引擎桥）留在宿主，只把判据送进来。

主要调用方：`application/core`（`snapshot_budget.go` 的 `boundToolResultForSnapshot` 与
`handleToolCompleteObserved` / 会话恢复路径）。

外部端口：`application/core` 已不再 import 本包，而是依赖
`application/session.ResidencyPort`；唯一 adapter 在 `wiring/adapters`。
本包仍是 preview、Order/Budget、input index、eviction 与 archive 判据的唯一实现。

落点沿革：2026-10-16 随 M5-R6-residency-preview **接收者转移**从
`application/core/snapshot_budget.go` 归位（plan §2.4 的 `snapshot_budget.go` → `middle/residency/`），
是本包的建包片。

## 职责与非职责

**做**：

1. `Preview(content, limit)` —— 把超限内容截断成快照预览：按 **UTF-8 rune 边界**安全切割
   （不切坏多字节字符）、去掉尾部空白、追加一行可读的省略说明（含按 **rune** 计的总字符数）；
2. `Order.Touch` / `Order.Forget` —— 使用序的唯一维护实现（索引 0 = 最近使用；Touch 去重前置、
   Forget 保持相对顺序；空 ID 视为无效）；
3. `Budget(legacyLimit, protectedCount, window)` —— 释放额度的唯一判据（分层口径 = 总上限 − 受保护数，
   否则用统一窗口）。
4. `Archive(deps, sessionID)` —— 会话归档的**判据与次序**：门控（关闭 / 排空 / 忙·恢复中）→ 读 record
   （缺失或归属别的会话即 not-found）→ 已归档则幂等返回 → 驻留时 flush → unload → 摘驻留序 →
   写归档状态 → 释放后台命令 → 按项目刷新目录。
5. `EvictUntilWithinLimit(deps)` —— 两层 LRU 超限收敛的**判据与循环**：一次读数（使用序 / 占用本层 /
   受保护 / 触发会话 / 额度配置）→ 挑最旧的可卸载候选 → 卸载 → 再读数，直到进入额度；
   失败即停、无候选即容忍超限。

**不做**：不读配置（进程级预算、窗口、分层上限的取数都在宿主侧）；不做**工具结果归档**（超限全文落
`result_ref` 由宿主的任务域端口负责）；不写快照；不读盘、不自己持锁、不 import 宿主域类型——会话单元
状态、存储落点、引擎实例都由端口带进来（受保护集与驻留标记的判定留在宿主）。

## 目录与文件结构

| 文件 | 职责 |
|---|---|
| `preview.go` | `Preview`：预览截断的唯一实现（`limit <= 0` = 不截断） |
| `preview_test.go` | 截断/透传、UTF-8 边界与 rune 计数、`limit <= 0` 语义 |
| `order.go` | `Order`（`Touch` / `Forget`）+ `Budget`：使用序与额度的唯一实现 |
| `order_test.go` | 去重前置、不改原序、摘除保持相对顺序、额度两种口径与边界 |
| `prune_test.go` | `PruneOrder` 的收敛口径（丢弃已卸载 / 去重 / 按单元表收养） |
| `catalog.go` | `CatalogRefresher`（窄口）+ `WaitCatalogRefresh`：目录刷新等待的判据 |
| `catalog_test.go` | 回执到达即返回、恰好请求一次、ctx 超时返回 `ctx.Err()` 且之后仍可等待 |
| `archive.go` | `ArchiveGate` / `ArchiveStore` / `ArchiveEngine` / `ArchiveDeps` + `Archive`：会话归档的唯一判据 |
| `archive_test.go` | 门控四轴次序与装配缺陷、空 ID 与 trim、record 缺失/归属不符、已归档幂等、驻留次序、跳过引擎、三条失败即停 |
| `inputindex.go` | `InputIndexSummaryLimit` + `SummarizeInputText` / `AlignLoadedInputTexts` / `BuildInputIndex`：用户输入索引的装配判据 |
| `inputindex_test.go` | 摘要截断（rune / 折空白 / 默认上限）、对齐算法（尾窗 / 整段 / 退让 / 不对齐）、命中段标 Loaded 与序号偏移 |
| `eviction.go` | `EvictionSnapshot` / `EvictionDeps` + `EvictUntilWithinLimit`：正文层与引擎层**共用**的超限收敛判据（宿主只提供"一次读数"与"卸载一个"两个动作） |
| `eviction_test.go` | 统一窗口与分层额度、受保护集不计数不挑选、不占本层/空 ID 跳过、最旧端先走、失败即停、无候选容忍超限、缺装配不 panic |

## 核心实现

- `Preview(content string, limit int) (string, bool)`：判据只有一份。生产调用点由宿主先取进程级预算
  （`limit.Get().SnapshotToolOutputChars`，缺省 8000）再传进来——**计算入中间层、取数留宿主**
  （与 `middle/view` 同一条口径，计划 R5c 的先例）。
- `limit <= 0` 显式定义为"不截断"：宿主保证给正数，本包不猜默认值（否则默认值会有第二份）。
- `Archive(deps ArchiveDeps, sessionID string) error`：判据只有一份。宿主把"关闭 / 排空 / 忙·恢复中"
  四个读数与**三条轴对应的宿主错误值**装进 `ArchiveGate`（拒绝时原样返回，`errors.Is` 拿到的仍是宿主
  自己的错误）；把存储面（读 record / flush / 写归档状态 / 按项目刷新）与引擎面（驻留标记 / 引擎是否真有
  该会话 / unload / 摘驻留序）装成函数值端口——"按会话定位落点"留在宿主的闭包里，本包因此不需要命名
  后端域类型。门控读数是**惰性**回调：空 ID 不该白读一眼宿主状态。
- 失败方向的口径：门控某轴拒绝、宿主却没给错误值时报 `ErrArchiveGateUnassembled`（装配缺陷必须响亮地
  失败，静默放行才是危险的）；record 缺失或归属别的会话都归 `ErrArchiveNotFound`（宿主侧保留的名字是
  `ErrSessionNotFoundArchive`）；`record.Status` 已是 archived 时**幂等返回 nil**，且不碰引擎、不写盘、
  不释放后台命令、不刷新目录。
- `EvictUntilWithinLimit(deps EvictionDeps)`：判据只有一份（两层共用）。宿主把"同一临界区内取齐的
  一组读数"装成 `EvictionSnapshot`（含额度配置的**取数**：分层上限与统一窗口），把"卸载一个会话
  在本层的占用"与"记一次失败"装成函数值端口——**挑选不做二次确认**，因为并发激活/卸载会让读数
  过期，二次确认是宿主动作内部的事（正文层 `evictLoadedContent` 在视图锁内重判单元/受保护/正文三格；
  引擎层 `releaseSessionMemory` 同款）。
  `Protect`（本次触发的会话）绝不参与本轮卸载但计入受保护数：否则会多放掉别的会话；引擎层没有这个
  角色，留空即可。两层差异（使用序是否先收敛、占用判据、额度来源）**全部在宿主的读数闭包里**。

## 数据流

```text
工具完成 / 会话恢复
  → 宿主取进程级预算（limits.snapshot_tool_output_chars，缺省 8000）
  → residency.Preview(content, budget) → (预览, 是否截断)
  → 超限时宿主把全文归档为 result_ref，快照只放预览
```

```text
归档命令（gui/tui → Application.ArchiveSession）
  → 宿主取会话过渡锁、装配端口（落点解析 / 引擎断言 / 过渡锁全在宿主闭包里）
  → residency.Archive：门控 → 读 record → [flush → unload → 摘驻留序] → 写 archived
  → 宿主释放该会话名下后台命令 + 按项目刷新目录（归档行从该项目格子过滤）
```

## 依赖方向

允许：标准库、`middle/protocol/model`（record 与状态词表）。
禁止：`backend/*`、`application/*`、`session`、`store`——本包是中间层叶子，预算、落点与状态读数
一律由参数/端口注入（不读配置、不自己定位会话）。

## 并发、存储与错误语义

- `Preview` / `Order` / `Budget` / `WaitCatalogRefresh` 是无状态纯计算：可并发调用，同样输入必得同样输出；
  不读盘、不写盘。
- `Archive` 是**次序判据**：本身不持锁、不读盘，全部副作用发生在宿主端口上，因此"并发安全"取决于宿主
  （宿主在视图锁内取读数、在会话过渡锁内跑整条链，见 `application/core/archive_session.go`）。
- 错误语义：`Archive` 只包装与转发宿主错误（`archive load %q` / `archive flush %q` / `archive unload %q` /
  `archive %q` 与"应用已关闭 / 排空 / 忙"三轴文案逐字保留迁移前的实现），另有两条本包自己的错误：
  `ErrArchiveNotFound`（record 缺失或归属不符）与 `ErrArchiveGateUnassembled`（门控装配缺陷）。

## 扩展方式

- 新增预算判据（例如"事件载荷上限"）：在本包加纯函数，预算仍由宿主取数传入。
- 新增调用点：只调 `Preview`，不要复制一份截断（省略说明的文案属于同一判据）。
- 会话归档要加/改一条拒绝轴（例如"会话已被删除"）：改 `ArchiveGate` + `Reject` 一处，宿主只负责给读数与
  错误值；不要在门面里再写一遍门控次序，也不要在门面里复制 `archive load/flush/unload` 的包装文案。

## Review 指南

- 截断点是否仍落在 rune 起点？省略说明里的字符数是否仍按 **rune** 计（不是字节）？
- `limit <= 0` 的语义是否仍是"不截断"（不得偷偷回退到某个默认值）？
- 是否出现第二份预览/截断实现（宿主侧或前端侧）？
- 归档门控的次序是否仍是关闭 → 排空 → 忙 / 恢复中（同时命中多条时先返回哪条是**可观测**行为）？
- `ArchiveGate` 的三条错误值是否都由宿主给出（缺一个就必须报装配缺陷，不得回退成放行）？
- 驻留分支的次序是否仍是 flush → unload → 摘驻留序（先落盘、再放引擎、最后摘标记）？
- 已归档的幂等路径是否仍然"什么都不做"（不 flush、不 unload、不释放后台命令、不刷新目录）？
- 超限收敛的额度是否仍只由 `Budget` 一份判据给出（宿主侧不得再算一遍"还能留几个"）？
- 受保护集是否仍然**既不计数也不参与挑选**（把它算进额度会多放掉别的会话）？
- 挑选与卸载之间状态变化时，是否仍由宿主在**动作内部**二次确认（不得把二次确认搬到本包）？

## 测试与验证

```text
go test ./middle/residency/ -count=1
go test ./application/core/ -run 'SnapshotBudget|ToolResult|Truncation' -count=1   # 宿主级：归档 + 预算注入 + 分页读回
go test ./application/core/ -run 'Archive' -count=1                                # 宿主适配：门控 / 落点 / 引擎断言仍在同一条链上
go test ./application/core/ -run 'Content|LRU|Resident|Release|Evict' -count=1      # 宿主适配：两个 LRU 的收敛仍在同一条链上
```

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### preview.go

- `func Preview(content string, limit int) (string, bool)` — 把超限内容截断为快照预览（UTF-8 边界安全，含按 rune 计数的省略说明）。

### order.go

- `type Order []string` — "最近使用序"（索引 0 = 最近使用；空 ID 视为无效）。
- `func (o Order) Touch(id string) Order` — 移到序首（重复则先去重再前置）；返回新切片，不改原序。
- `func (o Order) Forget(id string) Order` — 移出序，其余相对顺序不变。
- `func PruneOrder(order Order, loaded func(sessionID string) bool, unitIDs []string) Order` — 收敛使用序：去重、丢弃已不再持有正文的会话、按单元表顺序收养。
- `func Budget(legacyLimit, protectedCount, window int) int` — 释放额度：分层上限 − 受保护数，或统一窗口。

### catalog.go

- `type CatalogRefresher interface` — 请求一次目录刷新的窄口（结构满足会话协调器的同名方法）。
- `func WaitCatalogRefresh(ctx context.Context, refresher CatalogRefresher) error` — 等一轮覆盖本次请求的目录刷新；ctx 超时返回 `ctx.Err()`（不是失败）。

### inputindex.go

- `const InputIndexSummaryLimit = 60` — 刻度摘要的字符上限（rune）。
- `func SummarizeInputText(text string, limit int) (string, int)` — 空白折叠 + rune 截断（省略号），返回摘要与原文 rune 数。
- `func AlignLoadedInputTexts(texts, loaded []string) (int, int)` — 把已加载窗口对齐到全量序列（最长整段命中 → 退让到更短前缀 → 不命中 0/0）。
- `func BuildInputIndex(conversation []model.Message, loaded []string, limit int, userText func(model.Message) string) []model.SessionInputIndexRow` — 从完整可见会话构建索引（只保留宿主谓词判定的用户输入行，命中段标 Loaded）。

### archive.go

- `var ErrArchiveNotFound` — 目标会话不存在或 record 缺失/版本不兼容（宿主侧名字 `ErrSessionNotFoundArchive`）。
- `var ErrArchiveGateUnassembled` — 门控某轴拒绝但宿主没带该轴错误值（装配缺陷，不静默放行）。
- `type ArchiveGate struct` — 归档门控的读数（关闭 / 排空 / 忙 / 恢复中）与对应轴上原样返回的宿主错误值。
- `type ArchiveStore struct` — 归档用例的存储面（读 record / flush / 写归档状态 / 按项目刷新目录）。
- `type ArchiveEngine struct` — 归档用例的引擎面（驻留标记 / 引擎是否真有该会话 / unload / 摘驻留序）。
- `type ArchiveDeps struct` — 归档用例的宿主能力面（门控读数回调 + 存储面 + 引擎面 + 后台命令释放面）。
- `func (gate ArchiveGate) Reject() error` — Reject 按迁移前的**原次序**判决门控（关闭 → 排空 → 忙 / 恢复中），返回 nil 即放行。
- `func gateError(err error) error` — gateError 把"该轴拒绝"翻译成一个错误：宿主没带错误值即装配缺陷。
- `func Archive(deps ArchiveDeps, sessionID string) error` — Archive 归档指定会话：门控通过后把 record.Status 写为 archived，驻留会话先 flush 并释放引擎 bundle。

### eviction.go

- `type EvictionSnapshot struct` — 一次超限收敛的读数（使用序 / 触发会话 / 占用本层集合 / 受保护集合 / 分层上限与统一窗口）。
- `type EvictionDeps struct` — 超限收敛的宿主面（取一次自洽读数 + 卸载一个会话 + 记录一次失败）。
- `func EvictUntilWithinLimit(deps EvictionDeps)` — 反复卸载最旧且可卸载的会话，直到占用本层的会话数进入额度；无候选即容忍超限，失败即停（正文层与引擎层共用）。
