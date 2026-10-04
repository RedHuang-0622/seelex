# 真实会话记录上实跑「第一次压缩触发点 → 被动压缩链路」

- 日期：2026-10-04
- 范围：新增 `application/core/context_compaction_real_session_probe_test.go`
  （build tag `realprobe` + `SEELEX_REAL_COMPACT_PROBE=1` 双门禁，默认 skip）；
  现场报告 `_tmp/real-compaction-probe-report.md`（探针跑出来的产物）。
- 前置：`docs/devlog/2026-10-04-compaction-facts-durable-channel-and-restart-restore.md`
  （压缩记录持久通道 + 失败痕带报错原文）。
- 本文只做一件事：把之前只在**合成夹具**上跑过的压缩链路，放到**真实会话存储**上跑
  一遍，并把"第一次压缩到底从哪一步被触发、折出了哪一段、与真实帧对不对得上"用
  可复现的命令钉下来。

---

## 0. 一句话

真实数据的第一次压缩**在事件序号空间被逐字复现**：把内存 transcript 按读尾重建为
`seq[123..316]` 后走被动路径（自动判据），折出区间正好是真实帧的 `seq[123..217]`；
而按"从会话开头"注入同一份数据会折出 `seq[1..217]`——**这一条差别就是"第一次压缩
触发时的上下文从哪里开始"的判据**。帧里的 `message_to=message-197` 与落盘行
`seq 217 → message-217` 差 20 个 id，是一处**新发现的、尚未钉死**的两套 id 空间现象
（见 §4）。

## 1. 现场：第一次压缩帧的位置锚点（真实存储直读）

数据根 `dist/seelex-gui-dev/.seelex`（dev GUI 的真实会话数据）：

| 事实 | 值 |
|---|---|
| 会话目录 | `sessions-json/project-2e6431eb439414d0/session-3a5c432dda14d33b` |
| 存储键（Router 用） | project=`ws-1790770751331994500`、session=`seelex-1791107450690096600-1` |
| 事件行 | 316 行（seq 1..316），落盘 `token_count` 合计 347,165 |
| 压缩帧 | 1 帧 = **第一次压缩** |
| 帧锚点 | `compact-seelex-1791107450690096600-1-1791111964999`：`message-123 → message-197`、事件 `seq 123 → 217` |
| 压缩时间 | 2026-10-04T19:06:04.9991006+08:00 |
| 压缩记录通道 | **不存在**（无 `compaction-records.jsonl` / `metadata/compaction.json`）→ 该会话的压缩发生在记录通道之前 |

**落盘 token_count 剖面**（判据之外，但它把"触发点在哪"指了出来）：

```text
[1..122]   = 173,726     ← 读尾预算（166,808）装不下，冷加载被切掉的头部
[123..217] =  77,117     ← 真实帧折出的区间
[218..316] =  96,322     ← 真实帧的保留窗口
累积首次越过硬阈值 163,471 的位置 = seq 114
```

`TranscriptPrefixRange(transcript, compressedTo)` 的语义是"被折前缀的首/末事件"
（`task_context/plan_transcript.go`），所以 **`EventFrom=123` 这一条本身就说明：触发
那一刻内存 transcript 的起点是 seq 123**，不是会话开头。这一点被 §3 的对照臂独立
复现（臂① 折出 123..217、臂② 折出 1..217）。

## 2. 探针：怎么把真实数据喂进真实链路

`application/core/context_compaction_real_session_probe_test.go`，四条臂共用一份输入：

1. **只读拷贝**真实会话目录到 `t.TempDir()`（MEMORY.md 铁律：`.seelex` 是用户数据；
   原目录一个字节都不写），用 `sessionstore.NewRouter` 在副本上按生产读面读
   `LoadEventRangeWorkspace` / `CompactFramesWorkspace` / `LoadCompactionRecordsWorkspace`；
   存储目录名是 `project-<hash>` / `session-<hash>`，会话键从 message head 的
   `payload.session_id` 读回、项目键从 `workspace_index.json` 的 bindings 读回（哈希反推不出来）。
2. 事件行**原样注入**运行时（ReasoningContent / ToolCalls / ResultRef / TokenCount /
   真实 task_id 都照搬），`SeedTranscriptSeqFor` 把序号基线抬到真实起点 → 注入后的
   seq 与真实存储逐行相等，区间事实才可比。
3. 走**被动**路径：`PrepareExecutionContextFor`（自动判据，判据命中才折），不是
   `/compact` 这种显式路径。
4. 逐臂打印：判据关数字、装配关、读数闸输入、推帧区间、压缩记录 / 失败痕、快照可见面，
   并与真实帧区间逐字对照。

**替身边界（如实）**：读数闸（前缀重放厚摘要）是夹具——探针不调真模型，回执由替身给。
走真实代码的是判据与阈值、保留窗口决策、区间推导、推帧请求、记录 / 失败痕的生成与形状、
快照可见面；替身只替"这一次模型回读答了什么"。

```text
$env:SEELEX_REAL_COMPACT_PROBE='1'
go test -tags realprobe ./application/core/ -run TestRealSessionCompactionChainProbe -v -count=1
  → PASS（3.34s）；现场写入 _tmp/real-compaction-probe-report.md
```

## 3. 实跑结果（四条臂）

闸门（读真实 `window` 段 + 账号窗口，不写死）：`window=200000 budget=166808 hard=163471
target=133446`——与既有 devlog 里那条现场（`budget=166808 window=200000`）**逐字一致**，
即探针的判据口径与生产同源。

| 臂 | 注入 | 判据关 `compared` | 折出区间 | 推帧 | 压缩记录 |
|---|---|---|---|---|---|
| ① 真实触发点（读尾重建 `123..316`，落盘 173,439 tokens） | 194 行 | 168,241 ≥ 163,471 ✓ | **seq[123..217]** | overflow=95 replay=180 | 成功记录（snapshot 同源） |
| ② 全量（`1..316`，落盘 347,165 tokens） | 316 行 | 168,241（同①） | seq[1..217] | overflow=217 replay=180 | 成功记录 |
| ③ 只取被折区间（`123..217`，落盘 77,117 tokens） | 95 行 | 未命中（无进度轮） | — | — | 无 |
| ④ 真实触发点 + 读数闸拿不到读后感 | 194 行 | 168,241 ≥ 163,471 ✓ | 不折 | **不推帧** | 失败痕 1 条 |

四条各自说明的事：

1. **臂① 复现了真实帧**：折出区间 `seq[123..217]` 与真实帧 `EventFrom/EventTo` 逐字相等，
   推帧溢出的 95 条是 `transcript[123..217]`、重放素材 180 条是"本次装配出来的那份历史"；
   装配后 99 条消息，记录与快照读同一份成功记录。
2. **臂② 是判别力所在**：同一份数据、同一条链路，只把内存 transcript 的起点换成会话
   开头（seq 1），折出区间就变成 `seq[1..217]`——与真实帧不符。两臂的判据数字完全相同
   （`compared=168241`），因为判据量取的是**预算内的那份尾窗**，所以"多老的历史还在
   内存里"不影响判据、只影响**折出区间的起点**。这正是"真实触发点 = 读尾（seq 123 起）"
   的独立证据。
3. **臂③** 说明被折区间自身（77k）离判据线还远——它不可能是触发时的全部上下文。
4. **臂④ 是 2026-10-04 那条 patch 在真实数据上的形态**：一条失败痕，note =
   `no_model_summary estimated=178264 budget=166808 window=200000 overhead=4986 error=compact-local:replay-failed 前缀重放两次调用均失败，已回退本地确定性压缩：connect: connection refused`
   ——读数闸的报错原文接在末尾（前端据此在失败条目下面渲染一行）；且
   `estimated(178,264) > budget(166,808)` 而 `< window(200,000)`：这次是**带着超安全线
   的上下文 best-effort 发出、未被截断**那一档（与 devlog §4 的 22 轮档同形，不是 26 轮档）。

**两处数字别混用**（实跑顺带钉住口径）：记录里的 `EstimatedTokens=168,241` 是**判据量**
（`rawTokens`），失败痕 note 里的 `estimated=178,264` 是**装配后估算**（`estimated`）——
两个量在同一个失败痕上相邻出现，混读就会得出"同一个请求有两个大小"的错误结论。

## 4. 新发现：帧里的 `message_to` 与落盘行差 20 个 id（未钉死）

| 位置 | 帧里写的 | 该 seq 的落盘行给的 |
|---|---|---|
| 被折区间起点（seq 123） | `message-123` | `message-123` ✓ |
| 被折区间终点（seq 217） | `message-197` | `message-217` ✗ |

`TranscriptPrefixRange` 的区间两端取自**同一段**事件：事件序号取最后一个 `Seq`，消息号取
最后一个**非空** `MessageID`。两者在真实帧上不等（217 vs 197），只能说明**触发那一刻内存
transcript 的那个前缀里，seq 198..217 这一段事件没有消息号**（20 条）。探针注入的是落盘
行的消息号（都在），所以臂① 产出的 `message_to=message-217`。

机制线索（**Hypothesis，未单独钉**）：冷加载走 `session_runtime/archive.go` 的
`enrichTranscriptMessageIDs`——按 CallID / 角色+内容**配对**会话投影，配不上的事件
`MessageID` 留空；而落盘侧的会话投影把 id 派生为 `message-<事件 seq>`
（同文件 `RecordConversationTranscript`）。触发那一刻还在飞的那一轮事件尚未进会话投影，
于是没有 UI 消息号。要钉死它需要另跑一条"压缩那一刻内存 id vs 落盘行 id"的对照探针——
本轮的证据只够指出位置与差值（20 条），不够断言成因。

**影响面（不夸大）**：这只影响记录/帧里的**消息号标签**；事件序号区间（`EventFrom/EventTo`）
是权威定位事实，`RetainedFromForCompactions` 与前端分界都按它走，行为不受影响。

## 5. 证据清单

```text
# 现场定位（真实存储直读）
metadata/compact.json            → last_frame_id=…-1791111964999 / message-123..message-197 / seq 123..217
compact.jsonl                    → 1 帧（第一次压缩）
message/message_*.jsonl          → 316 行；token_count 合计 347,165；[1..122]=173,726、[123..217]=77,117
Test-Path compaction-records.jsonl → False（记录通道尚未产生）；metadata/compact.json → True
workspace_index.json bindings    → seelex-1791107450690096600-1 → ws-1790770751331994500

# 实跑（可复现）
$env:SEELEX_REAL_COMPACT_PROBE='1'; go test -tags realprobe ./application/core/ -run TestRealSessionCompactionChainProbe -v -count=1
  → PASS；报告 _tmp/real-compaction-probe-report.md
go vet -tags realprobe ./application/core/   → exit 0
```

## 6. 边界（如实）

1. **替身**：读数闸的回执是夹具（§2），因此臂①/②/④ 的"模型读后感"正文是占位文本；
   `summary_source` 的两值（replay/local）是**受控输入**，不是这次真跑出来的模型行为。
2. **会话选择**：本轮跑的是"有压缩帧"的 5 个真实会话里帧链最完整、时间最近的一个。
   另外 4 个：2 个单帧（`session-81341aceb2def41f`、`session-b6ae0703c44a884c`）、
   2 个 3 帧（一条是写这份 devlog 的当前会话自己，另一条是 2026-09-29 devlog 里那条
   "3.5 分钟 3 条记录"的现场）。探针用环境变量选会话（`SEELEX_REAL_PROJECT` /
   `SEELEX_REAL_SESSION`），换会话不需要改代码。
3. **压缩记录通道为空的真实含义**：该会话压缩于 19:06，而记录通道是**同一天**才接上的
   ——所以"帧在、记录不在"在真实数据上就是这么出现的（不是缺陷复现，是时间序）。
4. **§4 的 id 空间问题未修**：本轮只留下定位与差值；成因是 Hypothesis。
5. **提交**：按仓库规范不自动提交，改动留在工作区等用户点头。
