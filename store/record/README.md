# record — 工具调用读数（内存累计）

## 生态位

`store/record` 是**工具调用读数**的内存累计：一个会话一份"工具名 → 成功/失败/失败
原因"。它回答两个问题：**哪个工具最该优化**（成功与失败都要有，否则只有分子没有分母）、
**它坏在哪一类原因上**（看板点开柱子看的是原因占比）。

落点沿革：M5-R2 之前是 `internal/toolrecord`（包名 `toolrecord`）；plan §2.10/§2.12.2
把它归到 store 板块，包名随之改为 `record`——同一份代码换落点 + 换名，行为零变化。

主要调用方：

- `application/core`：工具完成段判出结果后调 `Accumulator.Record(tool, ok, reason)`；
  **判据在 core**（`toolStatusFailed` / `toolRecordReason`），本包不解释"什么算失败""原因
  归哪一格"；
- `application/core` 的 `ToolRecordBoard()`：把某会话的累计折成
  `dto.ToolRecordBoard` 给 TUI / GUI；
- **落盘不在这里**：读数的持久化归**会话存储**
  （`store` 的会话作用域 `tool-record.jsonl`），由 core 在真相源提交点
  **批量**调一次（`FlushToolRecord`）。

## 职责与非职责

做什么：

- 按会话累加"工具名 → 成功 / 失败 / 各失败原因"；
- 产出**整份快照**（`Snapshot`）供批次落盘；
- 从落盘快照**填充**（`Load`）——按工具名取较大者合并（同进程内累计只增不减）；
- 原因归一：词表外的标识一律落 `other`（保守默认：宁可归到"其它"，也不要让看板出现一个
  拼错的格子）。

刻意不做什么：

- **不做任何 IO**：读写文件是会话存储的事（本包因此能被三条工具链直接调用而不碰磁盘）；
- **不判断"什么算失败""原因属于哪一类"**：判据在 `application/core`；
- **不控制开关**：计数开关在应用层，关着时根本不会调到这里；
- **不做窗口/趋势统计**（按天聚合、滑动窗口）：累计是唯一口径，加窗口是另一条判据；
- **不决定落盘频率**：批次节奏归 core 的提交点（那决定"写多忙"）。

## 文件结构

| 文件 | 职责 |
|---|---|
| `accumulator.go` | `Accumulator`：单会话累计（`Record` / `Stats` / `Totals` / `Snapshot` / `Load`）+ 原因归一与合并。 |
| `accumulator_test.go` | 累加、排序、快照/dirty 门、Load 的"只增不减"、原因归一与合并。 |

## 核心实现

- `Accumulator`：
  - `Record(tool, ok, reason)` —— 累加一笔（失败才记原因与 `LastAt`）。
  - `Stats()` —— 按**总调用次数**降序（并列按首次出现序 ⇒ 读数确定）。
  - `Totals()` —— （总调用, 成功, 失败）。
  - `Snapshot()` —— 整份快照 + `dirty`（false = 上次落盘后没有新东西，调用方据此跳过写盘）。
  - `Load(snapshot)` —— 用落盘读数填充：**只增不减**（旧快照是子集，按格取较大者）。
- 并发：单把 `sync.Mutex`；`Stats`/`Snapshot` 交出的是**快照副本**（切片也复制），
  调用方改不到内部事实。

## 数据流

```text
工具完成（三族：主代理/后台会话 · 子代理 · teammate）+ 命令退出码（bash.process.exited）
   → core：开关？→ 结果帧？→ toolStatusFailed + toolRecordReason（唯一两处判据）
   → Accumulator.Record（内存，按会话）
   → 真相源提交点：core.FlushToolRecord → store 写一行整份快照
   → core.ToolRecordBoard → TUI 面板 / GUI 柱形图 + 原因饼图
```

## 依赖方向

- 允许：标准库 + `contract/dto`（共享 DTO 与原因词表：形状只有一份）。
- 禁止：任何反向依赖（本包不认识 application/core 的其它部分、seelebridge、store）。

## 并发、存储与错误语义

- 本包**不落盘**：所有错误语义（残尾、坏行、权限）归 `store/tool_record.go`。
- `Load` 是幂等的"填充"：重复调用不会把已有的更大读数改小（同一进程只加载一次，见 core
  的 `loadToolRecord`）。
- `Snapshot` 会清 `dirty`：**调用方必须先成功落盘再丢弃快照**，否则那批读数会丢——
  core 的做法是先落盘、失败时只记错误（读数仍在内存里，下次提交点再写）。

## 扩展方式

- 加维度（会话/角色/耗时）：改 `Accumulator` 的键与 `dto.ToolRecordStat`，并同步 core 的
  判据注释与本 README。
- 加原因：只在 `dto.ToolReason*` 词表加一格 + core 的 `toolRecordReason` 匹配分支
  （词表是唯一事实源，前端标签表跟着加一行）。
- 换存储：`store` 那一侧换实现即可，本包是纯内存。

## Review 指南

- 成功是否也累加？只记失败的表算不出占比，会把优化带偏（这是这张表存在的理由）。
- `running` 帧是否被排除？把发起当结果会让同一件事记两笔、成功率被心跳稀释
  （判据在 core，别在本包补判）。
- `Snapshot` 清 dirty 的时机是否在**落盘成功之后**？提前清就会静默丢掉一批读数。
- `Load` 是否仍然只增不减？把累计改小等于把已经发生的调用抹掉。

## 测试与验证

```text
go test ./store/record/ -count=1
go test ./application/core/ -run TestToolRecord -count=1   # 接线、开关、批次落盘与读回
go test ./store/ -run ToolRecord -count=1           # 会话级一行一批的落盘/读回
node --test gui/frontend/dist/tool-record-board.test.mjs    # 柱形图/原因饼图/空态
```

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### accumulator.go

- `func NewAccumulator() *Accumulator` — NewAccumulator 构造一个空累计。
- `func (accumulator *Accumulator) Record(tool string, ok bool, reason string)` — Record 累加一次调用（成功也记：没有分母的失败计数会把优化带偏）。
- `func (accumulator *Accumulator) Stats() []dto.ToolRecordStat` — Stats 返回全部工具的读数（按总调用次数降序；并列按首次出现序 ⇒ 读数确定）。
- `func (accumulator *Accumulator) Stat(tool string) (dto.ToolRecordStat, bool)` — Stat 返回某工具当前的读数（不存在 = 零值 + false）。
- `func (accumulator *Accumulator) Totals() (int, int, int)` — Totals 返回（总调用, 成功, 失败）。
- `func (accumulator *Accumulator) Snapshot() (dto.ToolRecordSnapshot, bool)` — Snapshot 取一份整份读数快照（落盘用；同时把 dirty 清掉）。
- `func (accumulator *Accumulator) Load(snapshot dto.ToolRecordSnapshot)` — Load 用一份落盘读数**填充**空累计（冷启动/会话切回时读回）。
- `func normalizeReason(reason string) string` — normalizeReason 把外部给的 reason 归一到词表：认得才留，认不得一律 other（保守默认：
- `func bumpReason(reasons []dto.ToolRecordReason, reason string) []dto.ToolRecordReason` — bumpReason 给原因表加一笔。
- `func mergeReasons(current, incoming []dto.ToolRecordReason) []dto.ToolRecordReason` — mergeReasons 按原因取较大者合并（与 Load 的"只增不减"同源）。
- `func cloneStat(stat dto.ToolRecordStat) dto.ToolRecordStat` — cloneStat 复制一格读数（含原因切片）：读面交出去的必须是快照，

### accumulator_test.go

- `func TestAccumulatorCountsBothOutcomes(t *testing.T)`
- `func TestAccumulatorSnapshotHonoursDirtyGate(t *testing.T)`
- `func TestAccumulatorLoadOnlyGrows(t *testing.T)`
- `func TestAccumulatorNormalizesReasonVocabulary(t *testing.T)`

