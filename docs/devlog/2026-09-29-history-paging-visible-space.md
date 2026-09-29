# 「回到最新」够不到尾：分页读回按可见空间对位

日期：2026-09-29
范围：`application/core/session_history.go`、`application/core/session_history_coldload_latest_repro_test.go`（新增红灯）

## 0. 一句话

冷读面（sessionstore 由 message 行展开 conversation）把**内部标记行**也当普通会话行，行数与下标都是
**未过滤**空间；可见会话（`view.Conversation` / `HistoryOffset` / `TotalMessages`）是**可见**空间（内部行被
`RecordConversation` 剔除）。旧实现拿未过滤总数当可见总数、又用「未过滤下标 + 已过滤行数」推窗口起点，
于是**尾窗里有多少内部行，窗口右界就少几格**：长会话冷加载后点「回到最新」永远差那么几格够不到尾——
前端 `protocol.js` 的 `historyWindowed` 恒真（新消息被 reducer 丢掉，「长期看不到最新内容」），
后端 `SessionViewBrowsingHistoryLocked` 恒真（流式增量、推理挂接、工具结果写回三条路径全被跳过）；
尾窗整段是内部行时读回的是空页，窗口原样留在上一页。

## 1. 红灯（两条，实测原文）

`application/core/session_history_coldload_latest_repro_test.go`：

```
--- FAIL: TestReproColdLoadThenReturnToLatestReachesTail
    冷加载后回到最新：窗口仍够不到尾（offset=15 visible=5 total=21）——前端 historyWindowed 会一直为真
--- FAIL: TestReproColdLoadReturnToLatestWhenTailWindowIsInternalRows
    尾窗被内部行占掉后回到最新：窗口仍够不到尾（offset=8 visible=6 total=26）
```

两处 `total` 都是**未过滤**总数（20 条可见 + 1 条 / 6 条内部 checkpoint 行）：夹具里 `pagedSessionStore`
的冷读面与生产同一口径（`sessionstore` 的 `ReadConversationRange` 派生 conversation 时不过滤内部行，
`total = len(messages)`），而冷加载基线是可见空间的 `len(RecordConversation(record)) = 20`。

## 2. 改法：分页读回全部在可见空间里对位

`conversationPage` 从此**两个空间分开记**：`rows`（已过滤的可见行）+ `rawRows`/`diskTotal`/`start`
（未过滤空间的行数与下标）。磁盘的未过滤总数只回答一个问题——**这一页读到发布点了吗**（`reachesPublishedTail`），
不再参与 `TotalMessages`。

- **尾页扩读**（`loadConversationTailPage`）：先探发布点，再从发布点向左按几何扩读，直到可见行攒够一窗或
  读到序列开头——尾段整段是内部行（回合收尾刚落 checkpoint）时不再拿到空页。
- **行身份对齐**（`indexOfSameRow` / `sameByContent`）：先按消息 ID 认（同一条消息在内存窗口与磁盘行里同 ID），
  ID 认不出来退回「角色 + 正文 + 思考」（旧格式 provider 历史行每次读回重新派号，ID 不可比）；空行
  （占位 assistant / 只有工具调用的 tool 行）不参与内容对位，免得两条空占位对上把接缝挪位。
  - 回到最新（`replaceVisibleTail`）：页尾按身份接上**内存窗口里发布点之后的热尾**（本轮尚未落盘的行）；
    窗口右界接上热尾时取**内存窗口右界**（此时内存窗口贴着有效尾），否则取**可见总数**（已发布的最新一条）。
  - 加载更早（`prependVisibleHistory`）：用**窗口首行**在页里找接缝，只把接缝之前的可见行前置进窗口，
    起点 = 内存起点 − 前置条数。
- **前置页接缝对齐**（`loadEarlierVisiblePage`）：从「可见下标 − 一页」起读、几何向右扩读，直到页里出现接缝
  （未过滤下标与可见下标之间隔着内部行，按可见下标读回来的一页可能整段落在窗口之前）。扩到已发布行全在手上
  仍没有对位（窗口首行是在飞行）→ 不假装连续，窗口原地不动；窗口为空（没有对位锚）→ 这一页就是窗口之前的
  一段，按最后一窗装上、起点钳 0。
- **总数只增不减**：安装末尾统一钳「窗口不得越过总数」（`start + durable(窗口) ≤ TotalMessages`）。

## 3. 验收

- 两条红灯转绿；
- 同族回归未受影响：`session_history_pagination_test.go`（翻到开头 / 回到最新并带回回看期间的新消息 /
  回看时追加不动窗口）、`session_history_hot_tail_test.go`（在飞尾不丢、跨发布点拼接、落盘后回到最新）、
  `session_history_browsing_submit_repro_test.go`（回看中发言结束回看）、`content_lru_test.go`
  （正文卸载后冷回读同一窗口）、`service_test.go`（分页读回的 workspace 归属与稳定消息 ID）；
- `go test ./application/core/ -count=1` 全绿（含全部红灯用例）；
- 全量回归与 `go vet`：见同轮提交信息。

## 4. 边界

- 可见总数以**会话可见投影**为唯一事实源（冷加载基线 + 追加路径共同维护），磁盘未过滤总数不再充当它的下界
  ——后者多算的正是内部标记行，那正是这组缺陷的根。可见总数落后于磁盘已发布的可见行（同一会话由单一视图
  台账维护，正常不会发生）时，尾窗给出的起点是**下界**：窗口里仍是真实的最新一行，只有 offset 偏保守。
- 接缝对不上时宁可窗口原地不动：拒绝在列表中间留一段谁都没有的下标区间。
- 读回面仍是区间读（分页语义不变）：扩读只发生在「尾段/前置段被内部标记行占掉」时，最坏读到序列开头。
