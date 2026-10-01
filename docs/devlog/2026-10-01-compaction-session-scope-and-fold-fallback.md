# 上下文压缩的两个现场：跨会话污染（已修）+ 压缩失败只能回退折叠（已复现、已按读法 A 落地）

- 日期：2026-10-01
- 范围：`application/core/session_draft.go`（+ 新用例）、`gui/frontend/dist/{snapshot-shape.js,client-state.js}`（+ 用例）、
  `gui/frontend/README.md`；诊断面：`application/core/context_runtime/{coordinator.go,compaction_index.go}`、`seelexctx/dag.go`、
  `seelebridge/runtime_compaction_index.go`
- 用户口径（原文）：①「关于上下文压缩的总是污染前端然后在新开会话的时候带到新建会话的问题」……
  ②「对于上下文频繁压缩然后又没压缩的问题，你要做出复现然后修复……找出上下文压缩失败只能回退折叠的原因，
  同时回退路径从折叠变成不做压缩持续上下文 append」

---

## 1. 现象①：压缩记录被带进新建会话 —— 已复现、已修（红 → 绿）

### 1.1 复现（先红）

`application/core/session_draft_context_scope_test.go`（新）在会话 A 上做一次压缩、写一份已读文件，
再 `BeginNewSession()`，断言新草稿会话的快照里不得有 `Task` / `ReadFiles`。修复前：

```text
--- FAIL: TestBeginNewSessionClearsPreviousSessionContextFacts
    新建会话带上了上一个会话的任务面（压缩记录挂在这里）：
    &{RequestID:task-draft-scope Status:progressing
      ContextCompactions:[{Version:2 Reason:context_budget Origin:explicit
        EstimatedTokens:133270 EventFrom:1 EventTo:30 FrameRef:tr-00027bd5…}] …}
```

新会话（草稿 `draft_…_1`）的快照里带着 A 的压缩记录：右栏「上下文压缩」因此列着上一个会话的折叠，
对话区那条「以上已折叠」分界也按它派生。

### 1.2 根因（一行纪律）

压缩记录挂在 `Snapshot.Task.ContextCompactions` 上，而 `Task` / `ReadFiles` 是**会话事实的共享快照镜像**，
随视图会话切换重写。换视图指针的三条路径里，两条清了口径、一条漏了：

| 路径 | 是否清 `Snapshot.Task` / `ReadFiles` | 位置 |
|---|---|---|
| 热挂载 / 恢复（换到已有会话） | 按目标会话重投影（`Task` 取 `VisibleTaskStateFor(sessionID)`；`ReadFiles` 随会话 view 镜像） | `application/core/session_lifecycle.go:72,85` |
| 卸载会话（就地新建草稿） | `Snapshot.Task = nil` / `Snapshot.ReadFiles = nil` | `application/core/session_lifecycle.go:177,179` |
| **新建会话（`+`，`BeginNewSession`）** | **只清了 Plan/Conversation/Chat/Interaction，没清 Task/ReadFiles** | `application/core/session_draft.go`（本次修复点） |

代价不止显示：草稿首次提交时 `PersistCurrentSession` 把 `Execution.Task` / `Execution.ReadFiles` 取自这份
全局镜像（`application/core/session_runtime/archive.go:225`），于是上一个会话的任务面与已读文件会被
**写成新会话自己的持久历史**。

### 1.3 改法（两处，互为防线）

| 层 | 改动 | 为什么两层都要 |
|---|---|---|
| 宿主（根因） | `BeginNewSession` 的 ViewMu 临界区补 `Snapshot.Task = nil` / `Snapshot.ReadFiles = nil`；`unloadSession` 的同名字段清理口径写进注释与函数 doc | 只有它能拦住**落盘**（`PersistCurrentSession` 读这份镜像）；漏清会让污染从"显示错"升级成"历史错" |
| 渲染层（用户口径「前端管控」） | `snapshot-shape.js` 新增 `isDraftSession` / `stripDraftSessionFacts`；`client-state.js` 在 `acceptSnapshot` 归一（草稿会话的 `task` / `read_files` 就地去掉，纯函数返回新对象、不改入参） | 「草稿会话没有任务面」是**结构事实**，不该依赖宿主每条换代路径都记得清；同时 `client.current()` 与 `onSnapshot` 拿到的是同一份归一快照（压缩面板、对话区分界、轨迹压缩轨、状态行四个消费点一次覆盖） |

### 1.4 回归证据

```text
go build ./...                                                    → exit 0
go test ./application/core/ -count=1 -timeout=900s                → ok 11.942s
go test ./gui/ -count=1                                           → ok 3.743s
node --test "gui/frontend/dist/*.test.mjs"                        → exit 0（全绿）
  ├─ snapshot-shape.test.mjs      9 项（含新增 3 项 stripDraftSessionFacts）
  └─ client-state.test.mjs       12 项（含新增 draft sessions drop the previous session's task and read files）
```

---

## 2. 现象②：上下文"频繁压缩然后又没压缩" —— 已复现、根因已定、回退语义已落地

### 2.1 复现与观测

用离线夹具（`compactTestService` + `appendWindowRounds` + `PrepareExecutionContextFor`）跑多回合装配：

```text
# 比例档 Ratio=1（保留窗口=压缩目标），16 轮 ×32k 字符起步，随后每回合 +1 轮：
回合 1: records=0 engineHistory=32
回合 2: records=0 engineHistory=34
回合 3: records=0 engineHistory=36
回合 4: records=0 engineHistory=38
回合 5: records=1 engineHistory=32   ← 越线才折一次，折完落 32 条
回合 6: records=1 engineHistory=34
```

即：**折一次能撑几轮**（出厂档余量 30% 后成立）；真正会变成"每轮都折"的是余量不足的档
（`docs/devlog/2026-09-29-compaction-idempotency-margin.md` 的现场：3.5 分钟 3 条记录、区间都从
`message-1` 起），而"折了但不能真正压缩"的形态由下面 2.2 的兜底决定。

### 2.2 根因：压缩失败**只有**折叠这一条回退（三条结构原因，逐条给锚点）

1. **摘要节点的兜底分支在结构上唯一**：`seelexctx/dag.go` 的 `chapter2Node` 把"模型读后感"的五个失败
   出口（`no-summarizer` / `no-replay-material`×2 / `replay-material-invalid` / `chunk-replay-failed` /
   `replay-failed`，见 `dag.go:350,352,355,359,383,405`）全部汇到同一个兜底：
   `LocalChapter2WithCarry(…)` + `summarySource = local`。文件头把这条契约写死了：
   「chapter2_thick：PrefixReplaySummarizer（一次重试）→ **失败回退本地确定性折叠**」。
   没有任何第二条路。
2. **开关的零值就是"没有摘要器"**：`seelexctx/limits.go:145-176` 的 `context_compaction_summary.enabled`
   零值（块缺失 / 显式 false）= 关，字段注释写明「折叠恒走本地确定性折叠（`summary_source=local`），
   一次模型调用都不发」。所以生效配置里这个块一旦缺失或为 false（或 QuickChat 装配失败、
   重放素材不合 wire 协议、调用失败），这次折叠**必然**落到本地折叠。
3. **折叠发生在推帧之前，"不做压缩"无处可退**：装配层先 `fitExecutionHistory(compacting=true)` →
   `replaceFoldHistory` → 再（锁外）推帧与渲染帧正文（`coordinator.go` 的 A/B/C 三段临界区）。
   等"摘要失败"这件事被知道时，请求前缀已改写、`ContextRetainedFrom` 已前移、记录身份已定稿；
   此时能回退的只剩"这一帧的正文用本地折叠顶上"。推帧失败同样"不中断装配"——降级方向被刻意
   设计成安全（`context_runtime/compaction_index.go` 文件头、`seelebridge/runtime_compaction_index.go`
   的「DAG 失败 → 返回错误（调用方只记日志、折叠照常）」）。

### 2.3 「不做压缩、持续 append」这条路**已经存在**，只是判据里没有它

装配层的"跳过折叠"分支就是 append-only：

| 事实 | 锚点 |
|---|---|
| 落点开关 | `compacting := fold && !ineffectiveFold` — `coordinator.go:649` |
| 跳过后的形态 | 「累积模式：…只追加保留段之后的新事件（append-only，字节稳定）」 — `coordinator.go:670` |
| 跳过时留痕 | `progress.skip("skipped=ineffective_fold …")` / `skipped=epoch_throttled …` — `coordinator.go:812,818` |
| 跳过时终局 | 不落记录、不推帧 → 终局 `folded_without_record`（前端读作「已折叠，本轮纪元未到期不写记录」） |

也就是说：「跳过折叠 = 持续上下文 append」这条回退路径**已经实现**，判据目前只有两条——
① 折叠换不来余量（`ineffectiveFold`）；② 同一 progress 纪元已压过（epoch 节流）。
**"这次写不出模型读后感"不在这两条里**。

### 2.4 回退语义：已裁决采纳读法 A（2026-10-01），并在 `a50aad3` 加严

用户口径直接给了终点：「回退路径从折叠变成不做压缩、持续上下文 append」。当时把它读成两种相反改法，
**裁决为读法 A**；两种读法与各自代价存档如下（B 被否）：

- **读法 A（已采纳）**：把「折不出读后感」变成折叠的前置条件——判据里补一条"这次有没有模型摘要可用"
  （可经窄可选能力探测：装配层 `Summarizer` 是否注入 + 有无重放素材）。命中即
  `compacting=false`、走 append-only、终局 Detail 写 `skipped=no-summary`。
  可实现范围：**结构性出口**（开关关闭 / 无重放素材）能在折叠前判掉；**调用失败**
  （`replay-failed`）要等到锁外推帧阶段才知道，要一并覆盖必须把"推帧/写读后感"提到
  `replaceFoldHistory` 之前——那会改动 A/B/C 三段锁纪律与门禁顺序（`judge→assemble→replace→index→…`
  的顺序被前端文案表与 `TestExplicitCompactGateTimeline` 互钉），属独立批次。
  **代价（必须先说清）**：不折之后请求一路 append 到全量预算上限，终点只有两个——自主 checkpoint
  折叠（`compressExecutionHistory`，本质仍是一次折叠）或 `ErrProviderContextBudgetExceeded` 拒发
  （`coordinator.go` 的"拒绝优于模型失忆"）。也就是"不做压缩"能把折叠延迟，但到不了"永不折"。
- **读法 B（已否）**：现状即回归、跳过太多要恢复折叠——把 `ineffectiveFold` 的跳过收窄或去掉，
  让判据命中就折。代价：回到 2026-09-29 的现场（同一会话每轮一条记录、区间恒从 `message-1` 起），
  而那正是 `TestContextBudgetSkipsFoldWithoutMargin` 与出厂档余量上调所钉住的回归。

**落地（已实现，2026-10-01）**：折叠门补一条窄可选探针 `compactionSummaryProbe`
（`application/core/context_compact.go`；未实现它的 fake/harness 行为逐位不变），命中即
`compacting=false` 走 append-only，终局 `CompactSkippedNoSummary`（`skipped=no_summary`，前端
`compactionOutcomeLabel` 可读）。独立提交 `a50aad3` 补上三条折叠入口里唯一漏闸的那条——自主压缩：
`commitFold` 原按 `newCheckpoint || autonomous` 置位，改为 `if !noSummary && estimated > budget.HardThreshold`。
用例 `context_compact_no_summary_test.go::TestFoldWithoutModelSummaryLeavesContextAndStackUntouched`
（提交时先红后绿，红原文见 `a50aad3` 提交信息）。代价照读法 A 所述：no-summary 宿主一路 append，
到不了「永不折」，真超全量预算时按既有语义显式报错。

---

## 3. 边界与未做

1. 现象①只修了"新建会话（草稿）"这一条换代路径；`switchViewToSession` 与 `unloadSession` 已有同口径清理
   （本次未改动，只把三处口径写进注释）。前端归一闸只管草稿态：非草稿会话的 `task` 原样放行
   （测试里 `stripDraftSessionFacts keeps tasks for real sessions` 钉住）。
2. 现象②**已改代码**（读法 A 已落地，见 2.4）：`compactionSummaryProbe` 窄探针 + 终局
   `CompactSkippedNoSummary`，外加 `a50aad3` 的自主压缩补闸。判据面证据仍可复现：
   `dag.go` 的兜底唯一性、开关零值即关、`ineffectiveFold`/epoch 两条跳过判据与 append-only 落点。
3. 真 API 冒烟（`compactlive`）本轮未跑：改动是会话事实清理与前端归一，判定面在离线夹具与 node 用例；
   真 API 证据断档问题照旧归发布门禁（见 `docs/2026-09-29-context-compaction-fold-review.md` P1-C）。
4. GUI 真机点检未做：运行中的 GUI 是旧构建（`dist/*.js` 需重启进程才加载），
   本轮只做了暂存产物无关的离线验证；重开进程后按 `docs/test/2026-10-01-computer-use-smoke-checklist.md`
   的 S1/S2（新建会话 / 切换会话）点一遍即可复核。
5. 提交状态：本批已按用户明确指示提交（`85e31e0` 现象①②+会话 ID 去 `draft_` 前缀、
   `a50aad3` 自主压缩补闸；附带现场 `ca0a9ec`、`b07bbb4`）。除这些外未自动提交（仓库规范 §7）。
