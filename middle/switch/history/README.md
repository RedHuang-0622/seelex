# middle/switch/history

## 生态位

`middle/switch/history` 是中间层"**历史分页**"的落点（plan §2.4：`application/core/session_history.go`
→ `middle/switch/history/`）。当前落进来的是**窗口装配判据**：一页读回的历史怎么装进可见窗口——
按**行身份**（接缝）拼接、窗口边界与起点推算。

主要调用方：`application/core`（`installVisibleHistory` 在"回到最新 / 冷回读 / 加载更早"三条路径上
调用 `ReplaceTail` / `Prepend`）。

落点沿革：2026-10-16 随 M5-R6-history-page **接收者转移**从 `application/core/session_history.go`
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
| `page_test.go` | 接缝对位、窗口边界与起点、空页/无锚的保守行为、行身份匹配规则 |

## 核心实现

- **坐标口径**（迁移前注释逐字保留其含义）：`view.Conversation` / `HistoryOffset` / `TotalMessages`
  一律是**可见**空间；冷读面回的行数与下标是**未过滤**空间（内部标记行也占位），两者不能相减相加。
  因此这里只认三条事实：可见总数以会话可见投影为准（不倒退，否则"下方还有 N 条"会谎报、并让不贴尾的
  窗口被 append 路径判成贴尾而插进列表中间）；窗口位置由**行身份**推出；**只拼得上的才拼**。
- 行身份：先按消息 ID 认（内存窗口与磁盘行同 ID 最稳）；旧格式 provider 历史行没有稳定 ID，退回
  「角色 + 正文 + 思考」内容对位；**空占位（占位 assistant、只有工具调用的 tool 行）不参与内容对位**——
  两条空占位长得一模一样，认错就把接缝挪了位；工具行只按**工具调用 ID** 对位。
- `DurableRows` 与 `middle/view/state.DurableConversationCount` 同一口径（`system` 引导行不占
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

允许：`middle/protocol/model`（消息形状）、`middle/view/state`（窗口边界收敛助手，同为中间层）。
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
go test ./application/core/ -run 'History|Pagination|ColdRestore|LoadMore' -count=1   # 宿主级端到端（冷回读/分页/坐标口径）
```

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### page.go

- `func ReplaceTail(pageRows, memoryRows []model.Message, memoryStart, memoryTotal, window int) ([]model.Message, int)` — 组装尾部窗口（页为底 + 按行身份接回热尾）。
- `func Prepend(pageRows, memoryRows []model.Message, memoryStart, window int) ([]model.Message, int)` — 组装"加载更早"窗口（接缝之前的行前置）。
- `func DurableRows(messages []model.Message) []model.Message` — 取出参与历史游标的行（system 不占下标）。
- `func HasRowIdentity(message model.Message) bool` — 一行是否带得出身份（空行无法对位）。
- `func SameByContent(left, right model.Message) bool` — 按「角色 + 正文 + 思考」对位（空占位不参与；工具行按调用 ID）。
- `func IndexOfSameRow(rows []model.Message, want model.Message) int` — 同身份行的下标（先按 ID，再退回内容）。
- `func ContainsSameRow(rows []model.Message, want model.Message) bool` — 是否含同身份行。
