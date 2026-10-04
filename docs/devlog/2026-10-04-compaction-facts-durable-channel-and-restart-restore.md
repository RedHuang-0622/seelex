# 压缩记录的持久通道：重启后右栏「上下文压缩」从存储读回来

- 日期：2026-10-04
- 范围：`sessionstore`（新增压缩记录通道）、`internal/adapters`（写/读接线）、
  `application/core/session_runtime`（读回端口）、`application/core`（冷恢复 + 冷读 +
  压缩失败痕的 note 带上读数闸报错）、`gui/frontend/dist`（失败条目下面那一行报错）、
  新增用例若干。
- 前置：`docs/research/2026-10-02-compaction-record-and-failure-trace-survey.md`
  （现状调研：§4.2「record 通道退役 → 记录写不进也读不回」是本文要修的那条链）。
- 结论：**压缩记录（成功记录 + 失败痕）现在住在会话自己的 append-only 通道里**，
  与压缩帧（`compact.jsonl`）一样跨重启存活；冷恢复与冷读两条读面都从它还原。
  失败痕还多了两句：**"这次为什么压不成"的报错原文**（§5）与它在界面上的位置
  （失败条目下面一行）。

---

## 0. 一句话

压缩帧一直跨重启活着，压缩记录一直不活——因为记录只随 `record.Execution.Task`
落盘，而那条通道在 v8/S20 已经退役（`SaveRecordRaw` 只写穿 status/title，
`LoadRecordRaw` 交回按 message head 派生的最小 record）。本次给记录另开一条通道，
把「帧在、记录不在」补齐。

## 1. 缺口（现状，逐条有证据）

| 面 | 现状 |
|---|---|
| 写 | `persistCurrentSessionLocked` 把 `record`（含 `Execution.Task.ContextCompactions`）交给 `SessionPort.SaveSessionSnapshotWorkspace` → `SaveRecordRaw` → **v8 只写 status/title** |
| 读 | `resumeSessionCold` / `snapshotOfCold` 只从 `record.Execution.Task` 取压缩记录 → v8 恒空 |
| 后果 1 | 重启后右栏「上下文压缩」整条为空、轨迹「压缩」轨没有刻度、对话区不画压缩分界（唯一数据源 = `snapshot.task.context_compactions`） |
| 后果 2 | 保留窗口起点（`ContextRetainedFrom`）归零 → 下一次装配把已压出的前缀重新计入预算，长会话稳定越线 |
| 后果 3 | 重启后的一次落盘把「空记录」写回去（不可逆）——不过 v8 下它本来就写不回，所以这条在 v8 上表现为"永远为空" |
| 一直在的 | 压缩帧（`session/compact.jsonl` + `metadata/compact.json`）与帧正文（内容存储）**照常跨重启**（R3 用例在钉） |

所以用户看到的就是那句「压缩帧在、记录不在」：模型侧回读不受影响，坏掉的是**可见面**。

## 2. 修法：一条专用通道，不寄生在 record 通道里

### 2.1 存储层（`sessionstore/compaction_records.go`）

- 新模块 `moduleCompaction`：head = `metadata/compaction.json`（只记水位：
  `count` + 末行指纹），数据 = 会话根目录 `compaction-records.jsonl`（append-only）。
- **一行一次压缩**（成功记录或失败痕），`seq` 从 1 单调递增；`payload` 是应用侧
  `model.ContextCompaction` 的序列化原文——存储层**不解析**它，因此应用侧 schema
  增字段不需要动存储层，也不会出现"存储层镜像类型与应用模型两处漂移"。
- 水位式幂等：调用方每次落盘传**全量列表**，存储层只追加 `records[count:]`；
  列表长度与水位相等 → 一次 stat 都不做的空操作（每回合收尾都会走到这条）。
- 前缀指纹不符（内存列表被别的事实源重建过）→ **整份重写**，绝不把两份互不衔接的
  历史缝成一条（否则读者会看到两条"第一次压缩"）。
- `head.Count > len(records)` → 什么都**不删**：append-only 通道不替调用方截断历史。
- 模块锁 `compactionMu` 独立（不与 message / compact 共用）：帧由 seelebridge 推、
  记录由 application/core 写，共用一把锁就是给两个写者造一个共享串行点；模块自愈
  （head 损坏 → 按数据文件重建）也已接上 `repairModuleHeadFromData`。

### 2.2 写路径（`internal/adapters`）

`SaveSessionSnapshot` / `SaveSessionSnapshotWorkspace` 在构建 `Commit` 时顺手把
`record.Execution.Task.ContextCompactions` 投影成 `Commit.CompactionRecords`
（`compactionRecordPayloads`）。落在这一处是因为：**record 在写快照时一定带着这份
列表**（`TaskStateFor` 按会话取），所以这是唯一不需要新增写点、也不会两处漂移的落点。

### 2.3 读路径

```
sessionstore.compaction-records.jsonl
  → SessionGranularStore.LoadCompactionRecords
  → Router.LoadCompactionRecordsWorkspace（handled=false = 未 v8 化）
  → adapters.SessionPort.LoadCompactionRecordsWorkspace（解析成 model.ContextCompaction）
  → session_runtime.SessionCompactionFactPort（新端口，可选能力断言）
  → Coordinator.LoadSessionCompactionRecords(location, sessionID)
  → resumeSessionCold（驻留读面）/ snapshotOfCold（冷读面）
```

两条读面都是**旧址优先**：`record.Execution.Task` 有值就以它为准（非 v8 布局、旧记录
仍有），旧址为空才读新通道——同一份事实的两处副本，不该被新址的空结果覆盖。

## 3. 先红后绿

| 用例 | 层次 | 改前（红） |
|---|---|---|
| `sessionstore.TestCompactionRecordsAppendIsWatermarkedAndIdempotent` | 存储 | 通道不存在（新件；钉水位幂等 + 不截断历史） |
| `sessionstore.TestCompactionRecordsRewritesOnDivergentPrefix` | 存储 | 同上（钉"前缀不符整份重写"） |
| `sessionstore.TestCompactionRecordsHeadRebuildsFromData` | 存储 | 同上（钉 head 自愈） |
| `adapters.TestSessionPortCompactionRecordsRoundTrip` | 适配 | 真存储上写快照 → 重建端口（重启）→ 读回 2 条（成功 + 失败痕）；并先证明 **record 通道确实不承载 `context_compactions`**，否则这条用例测的不是真布局 |
| `core.TestReproCompactionFactsSurviveProcessRestart` | 应用 | 红灯：`task={… ContextCompactions:[] …}`；保留窗口起点没还原 |
| `core.TestReproCompactionFailureTraceSurvivesProcessRestart` | 应用 | 红灯：失败痕没读回来 |

应用层两条用例用的替身把**真布局的形状**写进了代码：`persist` 把 record 的
`execution` 子树丢掉（v8 停写），压缩记录改走 `LoadCompactionRecordsWorkspace`。
把读侧改动回退后两条都红（实测输出见 §5），恢复后绿。

## 4. 遗留：重启后「压缩失败却截断上下文」——已复现（可跑），修法待裁决

按用户要求补了这条场景（`core.TestProbeCompactionFailureAfterRestartEncounter`，
**默认 skip**，可实跑复现）：

```text
SEELEX_REPRO_RESTART_REFILL=1 go test ./application/core/ -run TestProbeCompactionFailureAfterRestartEncounter -v
```

实测两档（每轮约 8000 tokens，窗口 200k、安全线 ≈ 166.8k）：

| 重启后新追加的轮数 | 合计估算 | 结果 |
|---|---|---|
| 22 轮 | ≈ 176k < 窗口 200k | 全部保留，装配照常发出（失败痕 + 上下文原样）✓ |
| 26 轮 | ≈ 208k > 窗口 200k | **只保留最新 24 轮**：最早的 2 轮从 provider 历史里消失 |

即：2026-10-02 那条「压缩失败 → 上下文原样」的口径，在**请求装不进 provider 窗口**
时是**假的**（`target` 收到 `budget.Window` 之后，`fitExecutionHistory` 仍会把装不下的
最旧轮次丢掉）。截断本身在那种尺寸下无法避免——请求不能超过物理窗口——所以修法互斥，
需要先定口径：

1. **拒绝发送**（`ErrProviderContextBudgetExceeded`，错误里带 `estimated/budget/window/dropped`）：
   最诚实，但会中断会话；且与既有 pin
   `TestCompactionFailureDoesNotInterruptOverBudgetSession`（"压缩失败不得用安全线
   中断会话"，现场 281k/200k 那一档）直接冲突，要一起改。
2. **如实告知**：保留 best-effort 发出，但把 `truncated=… dropped_events=…` 写进失败痕，
   前端那句「上下文原样」改成实情。不中断，上下文仍被截断（只不再"偷偷"）。
3. **回退有界 checkpoint 压缩**（把现有 `compressExecutionHistory` / 自主压缩那一跳
   在"失败且装不下"时放开）：既不中断也不静默丢，模型会收到显式的
   「上下文已被压缩、细节请用窄查询回读」；代价是**部分推翻** 2026-10-02
   「没有读后感就不折」（那条口径的成本论证建立在"不折是免费的"之上，而这个尺寸下
   它并不免费）。

本文不替用户选；本轮只把**这一档的留痕与上屏**补齐（见 §5）——那是口径选定前也必须
成立的一半：无论最后选哪条路，"这次失败到底发生了什么"都得查得到。

**夹具的一处修正（本轮）**：这条旅程此前用 `BeginTask(…, previous=nil)` 开新一轮，
而生产传的是上一份状态（`chat.go`：`previousTask := CurrentTaskExecutionFor(sessionID)`）。
传 nil 会造出一个保留窗口起点归零的新状态，让这次装配把**已被压出的前缀**重新计入预算
——那是夹具造的假现场（更容易越线、更容易截断）。现在两条用例都走
`startRestartRound`（传上一份状态），上表的数字是修正后重测的：26 轮那一档仍是
24/26 保留，即截断与夹具无关，是真实行为。

## 5. ② 的留痕补丁（本轮）：报错原文进 note，并上屏到失败条目下面

### 5.1 缺口

失败痕的 note 此前只有 `no_model_summary estimated=… budget=… window=… overhead=…`：
判据量、预算、窗口都写了，**"这次为什么读不到读后感"一个字没有**。而那句话在代码里
是被**丢掉**的：

```go
// application/core/context_runtime/compaction_index.go（改前）
if err != nil {
    // 试了但失败：与「回执里没有模型读后感」同一结论——这次不折。
    return CompactionIndexReceipt{}, true   // ← err 在这里被丢掉，交回零值回执
}
```

零值回执 → 下游的 `readbackNote` 只剩 `source="" note=""`，失败痕于是把
"重放素材为空 / 重放调用报错 / 摘要器没装"三种下一步完全不同的成因读成同一行字。

### 5.2 修法（两处小改，一处协议）

1. `context_runtime/compaction_index.go`：**err 也写进 note** ——
   `return CompactionIndexReceipt{SummaryNote: err.Error()}, true`（一行）。
   回执的 note 本来就是这个位置的定义（`SummaryNote` 的注释写的是"这次为什么没有模型
   摘要"），报错原文放它；非 err 的降级原因（`compact-local:*` + 底层报错）走同一条。
2. `context_runtime/coordinator.go`：把这条读数闸证据接进 `compactionFailure` 的
   **末尾**（数字事实之后、落记录之前）——一份文本喂两个消费者：落记录的失败痕 note
   与带回调用方的回执 `Failure`（`/compact` 的 notice 读它）。
3. 协议字面量 `context_runtime.CompactionFailureNoteErrorMarker`（`" error="`）：
   note 形状 =「原因字面量 [数字事实…] [<标记>报错原文]」。**报错原文是自由文本**
   （可能含空格、引号、冒号），只有放在末尾才不必引号转义——"最后一个标记之后到结尾"
   即原文。前端 `compaction-format.js` 的 `compactionFailureError` 按同一份字面量取末段，
   两侧由 `core.TestFrontendCompactionFailureErrorMarkerMatchesBackend` 互钉（漂移的后果
   是**静默少一行**，所以钉在测试里而不是靠肉眼）。

### 5.3 上屏：报错渲染在失败条目的可视化下面

- `gui/frontend/dist/compaction-format.js`：新增 `compactionFailureError(record)`
  （纯函数，取 note 末段；取不到返回 `""`，**不编报错、不拿数字事实冒充**）。
- `gui/frontend/dist/context-summary.js`：失败行在原因行（`.compaction-stack-note`）
  **下面**再出一行 `.compaction-stack-error`（`报错：<原文>`，失败色）。
  此前报错只活在 6 秒的瞬态进度条与 `/compact` 回执文本里——用户按完回车再抬头就
  查无实据；现在它与失败痕一起跨重启存活（痕住在压缩记录通道，见 §2）。
- `gui/frontend/dist/styles.css`：补 `.compaction-stack-error` 一条规则（跨整行、
  失败色、等宽小字）——原因行是"发生了什么"（暗色），报错行是"底层报了什么"
  （失败色），两行一眼分得清。

### 5.4 先红后绿

| 用例 | 改前（红） |
|---|---|
| `core.TestCompactionFailureNoteCarriesReadbackEvidence` | `回执里的失败原因应带上读数闸的报错原文…：no_model_summary estimated=169046 budget=166808 window=200000 overhead=4979` |
| `core.TestCompactionFailureNoteCarriesReadbackProbeError` | `读数闸调用报错的原文必须进失败痕的 note…：no_model_summary estimated=169046 budget=166808 window=200000 overhead=4979` |
| `node --test context-summary.test.mjs`「失败痕把报错原文渲染在原因下面」 | 改前失败行里没有 `.compaction-stack-error` |
| `node --test compaction-format.test.mjs`「报错原文从 note 末段取回」 | 改前没有 `compactionFailureError` |

两条 Go 用例各钉一种形态：**回执里只有本地压缩**（`summary_source=local` + 降级原因）
与**读数闸调用本身就报错**（err 被丢→零值回执）。前端两条各钉"渲染出来"与"取不到就不编"。

### 5.5 复现一次「重启 → 越线 → 压缩」（可跑用例）

`core.TestRestartRefillCompactionFailureKeepsTraceWithReadbackError`（22 轮档，
**默认跑**）：

```text
go test ./application/core/ -run TestRestartRefillCompactionFailureKeepsTraceWithReadbackError -v
  → PASS
  → 读数闸输入：overflow=70 条 replay=8 条
```

四件事一起钉：重启后先读回那条成功记录 → 上下文再次越线（≈176k > 安全线）→ 这次
压缩压不下去 → 失败痕留下并带上报错原文（`error=compact-local:replay-failed … connect:
connection refused`）→ 快照可见面同源（右栏读的就是它）→ 尚未被压缩覆盖的 22 轮**一轮
不少**。

顺带一条实测事实（回答"重启到底剥夺了哪一步"）：这次读数闸拿到的是**非空**重放素材
（`replay=8 条`，来自重启后装配出来的那份历史），所以这一档的失败不是"没素材"，
是那次调用没成——正是 §5.1 那条被丢掉的 err 要说明的事。

## 6. 回归证据

```text
go build ./...                                   → exit 0
go test ./sessionstore/ -run TestCompactionRecords -count=1        → ok
go test ./internal/adapters/ -run TestSessionPortCompactionRecordsRoundTrip -count=1 → ok
go test ./application/core/ -run 'TestReproCompaction' -count=1    → ok（含既有 3 条 record 通道用例）
# 读侧回退后的红灯（判别力实测）
go test ./application/core/ -run 'TestReproCompactionFactsSurviveProcessRestart' -count=1
  → 红灯：重启后快照任务面丢了压缩记录（task=&{… ContextCompactions:[] …}）
go test ./application/core/ -run 'TestReproCompactionFailureTraceSurvivesProcessRestart' -count=1
  → 红灯：重启后失败痕没从存储读回来
```

本轮的增补（② 的留痕 + 报错上屏）：

```text
go test ./application/core/... -run 'Compact|Compaction|Restart' -count=1          → ok
go test ./application/... ./internal/... ./sessionstore/... -count=1                → ok
node --test "gui/frontend/dist/*.test.mjs"                                          → 622 pass / 0 fail
SEELEX_REPRO_RESTART_REFILL=1 go test ./application/core/ -run TestProbeCompactionFailureAfterRestartEncounter -v
  → 红灯（按设计）：26 轮里只有 24 轮还在 provider 历史里（§4 的待裁决现象仍在）
```

## 7. 边界（如实）

1. **非 v8 布局**：`LoadCompactionRecordsWorkspace` 返回 `handled=false`，读侧落回
   `record.Execution.Task`（旧址），行为与改动前一致。
2. **非原子落盘路径**（宿主未实现 `SessionSnapshotPort`）不写这条通道——那时 record
   通道仍在用（`SaveRecordRaw` 非 v8 分支），旧址照常承载。
3. **fork 子会话**：`SaveSessionSnapshotWorkspace` 是同一条构建路径，子会话自己的
   `ContextCompactions` 随子会话快照落进子会话的通道（不复制父会话的）。
4. **`/compact` 的 notice（system 行）是否落盘**仍未验证（调研 §8.2 的遗留），本轮没碰。
5. **真机 GUI 冒烟未做**：本轮证据是存储/适配/应用三层用例 + 真跑红绿；运行中的 GUI
   是旧构建，要重开进程才会加载新二进制。前端 `dist/` 就是可维护源（无 bundler），
   但 `embed.FS` 在**编译期**打包——不重编二进制就看不到新的失败行。
6. **截断那一档仍无痕**（§4 的待裁决项）：本轮只补了"报错原文"这一条痕。**请求装不进
   provider 窗口**时被丢掉的最旧轮次，note 里仍然没有一个字（修法要等口径）。
   因此 §5 的"上下文原样"在那一档仍然不成立——22 轮档成立、26 轮档不成立。
7. **旧痕无报错段**：已经落盘的失败痕（改前写的）没有 ` error=` 段，前端按"取不到就不
   渲染"处理（少一行，不编）；不追改历史记录。
8. **提交**：按仓库规范不自动提交，改动留在工作区等用户点头。
