# middle/switch/history

## 生态位

`middle/switch/history` 是中间层"**历史分页**"的落点（plan §2.4：`application/session/history/history_runtime.go`
→ `middle/switch/history/`）。当前落进来的是**窗口装配判据**：一页读回的历史怎么装进可见窗口——
按**行身份**（接缝）拼接、窗口边界与起点推算。

主要调用方：`application/assembly`（`installVisibleHistory` 在"回到最新 / 冷回读 / 加载更早"三条路径上
调用 `ReplaceTail` / `Prepend`）。

落点沿革：2026-10-16 随 M5-R6-history-page **接收者转移**从 `application/session/history/history_runtime.go`
归位；"从哪读"（冷读面 / 分页读回 / 会话存储端口）与写快照、发布事件仍由宿主负责。

## 职责与非职责

**做**：

1. `ReplaceTail`：组装"回到最新 / 冷回读"的尾部窗口——以冷读页为底，把内存窗口里**发布点之后**的
   热尾（本轮尚未落盘的行）按行身份接回页尾；接上热尾时窗口右界取内存窗口右界，否则取可见总数；
2. `Prepend`：组装"加载更早"的窗口——页里接缝（窗口首行）之前的可见行前置到窗口头部，窗口整体后退；
   接缝对不上就不动窗口（宁可不连续，也不留一段谁都没有的下标区间）；
3. 行身份判据：`IndexOfSameRow` / `SameByContent` / `HasRowIdentity` / `ContainsSameRow` / `DurableRows`。

**不做**：不读盘（一页数据由宿主取回）、不写快照、不发布事件、不持锁；不决定窗口大小（`limits.history_window`
由宿主取数后作参数传入）。

## 目录与文件结构

| 文件 | 职责 |
|---|---|
| `page.go` | `ReplaceTail` / `Prepend` / `DurableRows` / `IndexOfSameRow` / `SameByContent` / `HasRowIdentity` / `ContainsSameRow` |
| `assembler.go` | `Assembler`：Application `WindowAssembler` 端口的结构实现，逐项转发 `page.go`，不复制判据 |
| `page_test.go` | 接缝对位、窗口边界与起点、空页/无锚的保守行为、行身份匹配规则 |
| `assembler_test.go` | 原函数与 `Assembler` 同输入 parity、接缝失败与空占位不参与对位 |

## 核心实现

- **坐标口径**（迁移前注释逐字保留其含义）：`view.Conversation` / `HistoryOffset` / `TotalMessages`
  一律是**可见**空间；冷读面回的行数与下标是**未过滤**空间（内部标记行也占位），两者不能相减相加。
  因此这里只认三条事实：可见总数以会话可见投影为准（不倒退，否则"下方还有 N 条"会谎报、并让不贴尾的
  窗口被 append 路径判成贴尾而插进列表中间）；窗口位置由**行身份**推出；**只拼得上的才拼**。
- 行身份：先按消息 ID 认（内存窗口与磁盘行同 ID 最稳）；旧格式 provider 历史行没有稳定 ID，退回
  「角色 + 正文 + 思考」内容对位；**空占位（占位 assistant、只有工具调用的 tool 行）不参与内容对位**——
  两条空占位长得一模一样，认错就把接缝挪了位；工具行只按**工具调用 ID** 对位。
- `DurableRows` 与 `contract/protocol.DurableConversationCount` 同一口径（`system` 引导行不占
  durable 下标空间）。

## 数据流

```text
宿主取回一页（冷读面 / 分页读回）→ page.rows（可见空间）+ rawRows/diskTotal（未过滤空间）
  → ReplaceTail（回到最新/冷回读）或 Prepend（加载更早）
       ├─ 接缝对得上：拼上热尾 / 前置更早行 → (rows, start)
       └─ 对不上：返回空 → 宿主保持窗口不动，只如实校正 HasMoreHistory
  → 宿主把 (rows, start) 写进会话可见投影 + 镜像快照 + 发布
```

## 依赖方向

允许：`middle/protocol/model`（消息形状）、`contract/protocol`（窗口边界收敛助手）。
禁止：`backend/*`、`application/*`、`store`——窗口大小与分页数据都由宿主给。

## 并发、存储与错误语义

- 无状态、无锁、纯函数：可并发调用；同样输入必得同样输出。
- 不读盘、不写盘、不返回错误：接缝对不上不是错误，用"返回空"表达（调用方据此保持窗口不动）。

## 扩展方式

- 新增装配路径（例如"按时间跳转"）：在本包加一个纯函数，仍以 `(rows, start)` 为契约；
  不要复制接缝判定（那是 `IndexOfSameRow` 一份）。
- 改行身份口径（例如未来引入稳定消息 ID 全覆盖）：只改 `SameByContent` / `IndexOfSameRow`。

## Review 指南

- 接缝对不上时是否仍**返回空**（宁可不动窗口，也不留断层）？
- 可见总数是否仍**只增不减**（宿主侧）？窗口起点是否仍由行身份推出（不得用未过滤下标相减）？
- 空占位是否仍不参与内容对位？工具行是否仍按调用 ID 对位？
- 是否出现第二份接缝/窗口装配实现（宿主侧或前端侧）？

## 测试与验证

```text
go test ./middle/switch/history/ -count=1
go test ./application/assembly/ -run 'History|Pagination|ColdRestore|LoadMore' -count=1   # 宿主级端到端（冷回读/分页/坐标口径）
```

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### assembler.go

- `func (Assembler) ReplaceTail(pageRows, memoryRows []model.Message, memoryStart, memoryTotal, window int) ([]model.Message, int)` — ReplaceTail 转发现有 ReplaceTail 实现。
- `func (Assembler) Prepend(pageRows, memoryRows []model.Message, memoryStart, window int) ([]model.Message, int)` — Prepend 转发现有 Prepend 实现。
- `func (Assembler) DurableRows(messages []model.Message) []model.Message` — DurableRows 转发现有 DurableRows 实现。
- `func (Assembler) IndexOfSameRow(rows []model.Message, want model.Message) int` — IndexOfSameRow 转发现有行身份对位实现。
- `func (Assembler) ContainsSameRow(rows []model.Message, want model.Message) bool` — ContainsSameRow 转发现有同身份行判据。
- `func (Assembler) HasRowIdentity(message model.Message) bool` — HasRowIdentity 转发现有行身份有效性判据。

### assembler_test.go

- `func TestAssemblerMatchesOriginalWindowFunctions(t *testing.T)`
- `func TestAssemblerKeepsWindowOnUnmatchedSeamAndIgnoresEmptyPlaceholders(t *testing.T)`

### boundary_test.go

- `func TestHistoryDoesNotImportViewState(t *testing.T)`

### page.go

- `func ReplaceTail(pageRows, memoryRows []model.Message, memoryStart, memoryTotal, window int) ([]model.Message, int)` — ReplaceTail 组装「回到最新 / 冷回读」的尾部窗口：以冷读页为底，把内存窗口里发布点之后的那段
- `func Prepend(pageRows, memoryRows []model.Message, memoryStart, window int) ([]model.Message, int)` — Prepend 组装「加载更早」的窗口：页里接缝（窗口首行）之前的可见行前置到窗口头部，窗口整体后退
- `func DurableRows(messages []model.Message) []model.Message` — DurableRows 取出可见窗口里参与历史游标的行（system 引导行不占 durable 下标空间，
- `func HasRowIdentity(message model.Message) bool` — HasRowIdentity 报告一行是否带得出身份（空行无法对位）。
- `func SameByContent(left, right model.Message) bool` — SameByContent 按「角色 + 正文 + 思考」对位（没有稳定消息 ID 的行只能这样认）；空行（占位 assistant、
- `func IndexOfSameRow(rows []model.Message, want model.Message) int` — IndexOfSameRow 返回 rows 里第一条与 want 同身份的行下标（-1 = 没找到）。先按消息 ID 认
- `func ContainsSameRow(rows []model.Message, want model.Message) bool` — ContainsSameRow 报告 rows 里有没有与 want 同身份的行。
- `func lastRow(rows []model.Message) model.Message` — lastRow 返回最后一行（空序列返回零值行：与任何行都不是同一条）。

### page_test.go

- `func row(id, role, content string) model.Message`
- `func ids(rows []model.Message) []string`
- `func equalIDs(left, right []string) bool`
- `func TestReplaceTailJoinsHotTailByRowIdentity(t *testing.T)`
- `func TestReplaceTailWithoutSeamUsesVisibleTotal(t *testing.T)`
- `func TestReplaceTailEmptyVisibleKeepsWindow(t *testing.T)`
- `func TestPrependUsesWindowFirstRowAsSeam(t *testing.T)`
- `func TestPrependWithoutSeamStaysPut(t *testing.T)`
- `func TestPrependIntoEmptyWindowTakesPageTail(t *testing.T)`
- `func TestRowIdentityMatching(t *testing.T)`
- `func TestDurableRowsSkipsSystem(t *testing.T)`
