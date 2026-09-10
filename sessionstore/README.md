# Session Store

## Application state sidecar

Alongside framework message history, every backend can persist an opaque application-owned state blob keyed by `(project_id, session_id)`. `Router.SaveState` and `LoadState` use the same JSON, SQLite, PostgreSQL, or Redis selection as history. The store does not inspect the blob; application code uses it for the visible transcript, Plan projection, and provenance caches without putting those records into provider history.

**v8 JSON 布局下 state/record 通道已退役（S20/D9）**：`WriteState` 为空操作、
`ReadState` 恒 `fs.ErrNotExist`；会话 record 与 conversation 由
`message head.Meta` + message 事件行 + `lifecycle.archived_at` 派生
（`DerivedRecordWorkspace`），会话展示元数据（置顶/别名/排序）落
`project-*/session-meta.json`。SQLite/PostgreSQL/Redis 仍使用 state 通道
（v8 化随 message 读写热点专项搁置）。

## Unified partition and shard contract

Every backend partitions first by `project_id`, then isolates `session_id`, then stores immutable history generations in fixed-size message shards. A manifest atomically switches the active generation, so readers see either the old complete generation or the next one. JSON uses generation directories; SQLite and PostgreSQL use `seelex_session_manifest` plus `seelex_session_shard`; Redis uses a project hash-tagged manifest and shard keys in one cluster slot. This keeps range/recovery semantics independent of the chosen storage strategy.

## 模块定位

`sessionstore` 提供统一、原子、项目作用域的会话持久化。当前 backend 为 JSON shards、SQLite、PostgreSQL 和 Redis；调用方只依赖 `Repository`/`Router`。

## 数据模型

`Key{ProjectID, SessionID}` 是唯一存储键。名称不是索引。空 ProjectID 表示未绑定项目的默认 scope。

`Repository` 契约：

- `WriteAtomic`：完整替换一个逻辑 history，读者只能看到旧版本或新版本。
- `Read`/`ReadRange`/`List`/`Delete`：都显式接收 project scope。
- `Ping`/`Close`：生命周期管理。

## Backends

### JSON

**v8 布局（M1–M4 已实现，新会话默认）**：新会话不再写 generation/manifest，
而是：

- `session/metadata/guide.json` 只做读索引/路由；各模块 head 独立成
  `metadata/<module>.json`（message/event/compact/stack/lifecycle/retention/
  subagent/toolresult），写锁按模块、各自原子发布；
- 正文事实源 = `session/message/message_{m}_{n}.jsonl` 事件行（分片按默认
  100 行），`commit_id` 标记一次持久提交，同提交多行共享；head 是发布点，
  崩溃残尾/未发布行按恢复语义截断或不可见；
- provider 整段历史缓存（`history.json`）**已退役（S11/D9）**：Read/ReadRange
  全量读一律由 message 事件行派生；`Router.Save`/`WriteCommit` 的
  `ProviderHistory` 不再落盘（同一提交以 `Events` 为准，dev 阶段丢字段已
  接受）；
- 结构性 EVENT（compacted/fork/subagent/interrupted/…）独立进
  `session/event/event_{m}_{n}.jsonl`；compact 摘要帧进 `compact.jsonl`；
- LRU 行删除、draft/queue lifecycle、fork session/subagent、单会话关键词
  索引、big_tool_result blob 由 `v8_*.go` 引擎承载（契约测试 T-M1/T-R1/
  T-R2/T-R3/T-LC/T-FK/T-WM/T-EV/T-SR/T-BL/T-CFG）。

旧会话（只含 `manifest.json` / `transcript.log` 的目录）**已彻底退役（D2/
S12）**：读路径不再判定、不再打开（返回 `fs.ErrNotExist`），目录枚举跳过并
保留磁盘内容；新会话与新写入一律走 message 事件行 + 模块 head 布局。
SQLite/PostgreSQL/Redis 后端仍为 generation snapshot 布局（后端自身存储
形态，不在旧 JSON 文件布局退役范围内），v8 化范围见打点表 §4。
[`docs/2026-09-08-session-storage-architecture/README.md`](../docs/2026-09-08-session-storage-architecture/README.md)）。

旧布局写路径描述（保留作 legacy 语义参考）：

每次写创建新的 `generation-*` 目录，将 history 按 100 条分 shard，最后原子替换 `manifest.json` 指向新 generation。旧 generation 不会在 manifest 提交前暴露。

**transcript 事件按增量追加到会话目录 `transcript.log`**（append-only JSONL，一行一个
`Event`，含显式 `kind`：user_input/llm/tool_call/tool_output/error/…），不再随
generation rollover 整代重写 `events.NNN.json`。追加按 `Seq > 已落盘 head` 求增量，
重复提交幂等；崩溃残尾（未换行收尾的半行）按恢复语义跳过。事件读取
（`ReadEventTail`/`ReadEventRange`）以日志为物理事实源；history/state 仍走
generation 快照，属于派生投影。

状态：**JSON 后端统一走 message 事件行 + 模块 head 布局；旧 manifest 会话
不再打开（S12）**。SQLite/PostgreSQL/Redis 的 transcript 仍为 generation
snapshot 布局（有序日志后端子设计见
[`docs/2026-09-07-session-order-log/README.md`](../docs/2026-09-07-session-order-log/README.md)）。

**rollout 全序日志（P2 垂直切片）已删除/退役**：运行期恢复不再走 rollout
重放，JSON 会话统一走 wire 装配（compact 摘要 + 尾窗 + 最近 K 条尝试）；
rollout 实现文件、读接口与双写已随旧链路删除（历史设计见
[docs/2026-09-08-session-rollout-p2/README.md](../docs/2026-09-08-session-rollout-p2/README.md)。

### SQLite/PostgreSQL

统一使用 `seelex_session_manifest` 与 `seelex_session_shard`：manifest 以 `(project_id, session_id)` 定位当前 immutable generation，shard 以 `(project_id, session_id, generation, shard_index)` 保存固定大小的消息片。事务先写新 generation，再原子切换 manifest。旧版单行 `seelex_sessions.messages_json` 仍可读取，下一次写入自动迁移到分片表。SQLite 使用 modernc，无 CGO；PostgreSQL 使用 pgx stdlib。

SQLite 本地库打开时固定附加 `_pragma=busy_timeout(5000)` 并把连接池收敛为单连接：SQLITE_BUSY 只发生在多连接之间，单连接后由 `database/sql` 排队，多会话并行落盘不再随机失败（只设 busy_timeout 不够 —— 它覆盖不了同进程内读事务与写事务的升级冲突）。

### Redis

Redis 使用 `redis://` 或 `rediss://` DSN。每个项目拥有一个 hash-tagged keyspace；同项目的 manifest、history shards、state 和 session index 位于同一 Cluster slot，因此一次 `MULTI/EXEC` 可以原子切换该 session 的 generation。DSN 仅写入本地配置，GUI 只显示 `configured`。

## Router

Router 用 RWMutex 把 active repository、config 和 project ID 绑定为原子视图。`Configure` 先 normalize/open/ping/save config，再在锁内 swap，最后关闭旧 backend。进行中的旧操作完成后，新操作才看到 replacement。

显式 `LoadWorkspace` 等方法不修改 active write scope，避免恢复其他项目会话时读错 shard。

## 会话展示元数据（`SessionMetaStore`）

`SessionMeta`（置顶、别名、手动排序位）描述"用户怎么看这个会话"，不参与执行与存储
归属。它以**项目级 blob** 落在 state 通道的伪会话键 `seelex:project:session-meta`
上：

- 为什么不并进 `SessionRecord`：record 每次回合结束都由存活状态整体重建，夹在其中的
  展示字段会被覆盖；独立 blob 与记录写路径完全隔离。
- 为什么不造新通道：JSON/SQLite 的目录枚举只认有 manifest 的会话目录，只写 state 的
  键不会出现在 `SessionsOf`/`List` 里（回归用例 `TestSessionMetaStoreRoundTrip` 钉住
  这一点，避免造出幽灵会话）；`Delete(session)` 也删不到它。
- 写是读-改-写，进程内由 `SessionMetaStore.mu` 串行化（桌面单进程形态；实例由
  `main.go` 构造一次并以指针注入 `SessionPort`，值拷贝后仍共用同一把锁）。
  `TestSessionMetaStoreConcurrentSets` 在 `-race` 下断言无丢失更新。
- 读失败或缺键一律按"无元数据"处理：展示元数据不得让会话目录整体失败。

## Seele v2 会话适配

两个适配器把 Router 接到 Seele v0.0.8 的会话契约：

- `durable_history.go` — `DurableHistory` 实现 `seelectx.DurableHistory`
  （Load/Save/Clear）：Session 每次 Chat 前 Load、结束后 Save；`Reset`
  显式清空。v8 JSON 布局（S11）：ProviderHistory 不再落盘，Save 只负责
  会话状态 blob（SaveState 编排），正文事实源 = `SaveCommit` 的 message
  事件行，Load 由行派生；SQLite/Redis 等未 v8 化的后端沿用整段写。
- `session_context.go` — `SessionContextStore` 读写会话级上下文记录
  （state blob）：SystemPrompt + Skill 记录 + Compact 栈 + GoalAudit。
  Schema 当前 **v3**：v3 起 blob **不再承载 plan/task/goal 三栈**（权威在
  §2.4 栈通道，见下节），v2 及更早的「栈内联在 blob」记录一律显式拒绝加载
  （不静默迁移、不降级为内存栈），走会话恢复错误路径。`SessionContextStore`
  的四栈 API 因此是两个后端的组合门面：blob 字段读写 blob，三栈读写通道。

### goal 帧：走栈通道，不走 blob

goal 帧与 plan/task 同属 §2.4 栈通道（`session/goal/{active,history}.jsonl`
+ `metadata/stack.json` 水位），`SessionContextStore` 的 `PushGoal` /
`CloseTopGoal` / `ReplaceGoalStack` / `GoalStackSnapshot` 只做「goal 域 DTO ↔
栈条目 payload」的编解码，落盘一律经 `Router.Stack*`。语义边界不变：

- 栈内 goal **不进模型上下文**：seelexctx 的栈块渲染（稳定前缀/动态尾部）
  只消费 Plan/Task/Skill/Compact，goal 帧不渲染、不做记忆前缀与匹配；
- 聊天记录中的 `#goal` 文本是普通转录内容，随上下文窗口/压缩一起被压缩；
- goal 域 `Controller` 每次状态机变更经 `ReplaceGoalStack` 全量写回当前栈
  投影；**goal 是活栈投影 + 单条目批次 LIFO**（只有栈顶可弹），批次收口即
  整批归档进 `history.jsonl`，恢复时读 active 重建；JSON 后端 head 按 kind
  分文件：`metadata/stack_{plan,task,goal}.json`（S16），每 kind 一把锁。

fork 子会话**不继承父 goal/plan/task 栈的"未来"条目**：按 message 锚重建
"该点"快照（`Router.ForkStacks`，起点早于 LRU 水位显式报错）。

### goal 审计账本（GoalAudit，append-only，按会话隔离）

`SessionContextRecord.GoalAudit` 是 goal 生命周期审计（begin/update/finish/
abort/restore 各一条），Seq 由本会话单调递增，**只追加不回改**。审计与活栈
正交：GoalStack 弹栈即删除、终态为空；审计保留收口记录（reason/result 与
可选 SourceSession 出处——用户可能在其它会话完成了该 goal，装配方收口时可
携带出处写回原会话账本，**不写入其它会话、不做跨会话回放**）。fork 子会话
同样不继承父 GoalAudit。

主 Runtime 通过 `seelebridge.Runtime.AttachHistoryRouter` 独立装配 `DurableHistory`，不复用 `SessionContextStore` 的 application-owned state blob。恢复会话时 DurableHistory 与框架 Session 使用同一个 session ID；Application 成功提交完整 `SessionRecord` 后才释放 provider working history，下一轮再从 durable tail 冷加载。

## plan / task / goal 栈通道（v8 §2.4，已实现）

三栈是**独立数据通道**，不再塞进任何 blob（塞进 context 单文件会让一次栈写入
与整份上下文重写抢同一把锁）。

- 放置（§3.2）：`session/{plan,task,goal}/active.jsonl`（当前投影）与
  `history.jsonl`（append-only 归档）；JSON head 在
  `metadata/stack_{plan,task,goal}.json`（SQL/Redis 仍为会话级 head 行，
  **只装逐 kind 水位**（head_seq / active_count / history_count /
  open_batches / history_bytes），条目内容不进 head。
- 语义（§2.4 + §0 条目 4）：批次内未完成 → 整批留 active；全部完成 → 整批
  弹栈归档；goal 是单条目批次且 LIFO（只有栈顶可收口）；每条带 message 锚
  `item_message_id` / `batch_message_from` / `batch_message_to`，fork 按锚
  重建"该点"快照。
- 写序（I5）：数据文件 → 模块 head（提交发布点）→ EVENT（`plan.*`/`task.*`/
  `goal.*` 状态迁移摘要，I4：不参与装配）。head 未发布的行按 `revision` 判定
  不可见，崩溃残尾按 JSONL 行边界丢弃，head 记录的 `history_bytes` 让下一次
  提交 O(1) 回收「已落盘但未发布」的归档尾行。

### 后端矩阵

`Repository` 暴露 `stackJournal()`（不可在包外实现），**每个后端必须给出栈
通道**，运行期没有「这个后端没有栈」的分支。通道语义（批次、水位、锚、迁移、
fork、verify）只在 `stack_channel.go` + `stack_journal.go` 写一次，后端只实现
`load / publish / anchor / watermark / recordEvents / dropCache / stats`：

| 后端 | 数据放置 | 发布点 | 锚（message 坐标） |
|---|---|---|---|
| JSON | `{kind}/active.jsonl` + `history.jsonl` + `metadata/stack_{kind}.json` | 先数据后 head（rename），读侧按 revision 过滤 | 事件行 `message_id` + `seq` |
| SQLite / PostgreSQL | `seelex_session_stack_item`（ER 逐列）+ `seelex_session_stack_head` + `seelex_session_structural_event` | 一个事务（COMMIT 即发布） | 只有 `seq`（消息通道仍是整块 shard，无事件行键 → id 为空） |
| Redis | `<session>:stack:<kind>:{active,history}` 列表 + `<session>:stack:head` + `<session>:structural-events` | 一次 `MULTI/EXEC` | 同上，只有 `seq` |

### 并发与读路径（为什么读者不等写者）

- **单写者 actor**：一次变更是一个提交闭包（`StackMutation` 工厂），在同一
  会话的 `stackMu` 临界区内执行；可变投影只被该临界区触碰 → 无数据竞争。
  head 是跨 kind 共享的发布点，所以临界区按会话而非按 kind。
- **读走内存快照**：每次发布把 active 投影连同 head 水位塞进
  `atomic.Pointer[stackView]`，读者只做一次原子装载 + 小切片复制，既不取锁
  也不打开文件句柄 —— Windows 上任何句柄都会让 rename 发布失败，读者持柄只会
  把失败转成退避等待（发布原语的重试预算 ≈77 ms，见「结构性 EVENT 通道」节末）。
- **写路径不解析归档文件**：归档计数/字节水位取自 head，history.jsonl 只在
  读者与 fork 需要时解析。
- **EVENT 移出栈临界区**：写序不变（数据 → head → EVENT），只是 EVENT 的分片
  IO 不再拉长栈锁；EVENT 失败不撤销已发布 head（§2.0 规则 4 的短窗口分离）。
- `Router.StackStorageStats()` 逐域计量（锁等待 / active / history_append /
  history_read / head / guide / EVENT + 冷读次数），`-race` 与延迟归因由
  `TestStackChannelLockAttribution` 验收。
- 作废点：删除会话、fork 覆盖子目录后必须 `dropSessionCaches`（快照与本进程
  写者同源，目录不在了就作废）。单数据根 = 单进程写者（§9）是该内存快照
  成立的前提：JSON 数据根在打开时抢 `<root>/lock.owner`（同进程引用计数
  共享；跨进程存活锁立即报错，陈旧锁默认拒绝、`auto_recover=true` 才接管，
  心跳按 `lock.stale_after_seconds/3` 续约），`jsonRepository.Close` 归还
  （契约测试见 `data_root_lock_test.go`）。

## 结构性 EVENT 通道（v8 §7 / 附录 A）

`sessionstore/structural_events.go` 承载「对会话做了什么」的结构性摘要，不参与
模型上下文装配（I4），不承担幂等计数。

- **放置**：`session/event/event_{from}_{to}.jsonl`（append-only，按
  `message.shard_rows` 滚动）；发布点在 `metadata/event.json`。行 = `event_id`、
  `kind`、`anchor_message_id`/`anchor_seq`（事件发生在该行**之后**）、`frame_id`、
  `commit_id`、`payload`、`created_at`。
- **写形态 = 与 message 同构的 append-only**：`structuralEventCommit` 只读
  head（水位 + 分片路由 `count`/`bytes`），据此定位尾分片剩余容量并追加，
  **不读任何已发布事件行**。
- **幂等 = 水位式**（三条，全部只看 head）：`event_id = 0` → 引擎续号追加；
  显式 `event_id ≤ last_event_id` → 已发布，跳过；`commit_id` 等于
  `last_commit_id` 且 head 已推进 → 同 commit 重复持久化，整次空操作。幂等窗口
  = 紧邻一次发布，需要更大窗口的生产者必须自带稳定坐标。**跨 commit 的重复行
  属审计噪音**，不为它回看历史。
- **崩溃语义**：head 是发布点。append 完成但 head 未替换 = 未提交，下次提交由
  `reapEventUnpublishedLocked` 删除未索引分片并把尾分片截回水位；半行残尾由
  `truncateCrashTail` 丢弃。只有字节数与 head 记录不一致时才读一次尾分片，
  常态提交零重读。
- **计量**：`store.event`（`commits` / `appended` / `shardReads`）。
  `shardReads` 是写路径读历史分片次数，正常负载必须为 0 ——
  `TestStructuralEventCommitNeverReadsHistoryShards`（T-EV-05）逐次断言。
  `verifyEvents` 校验分片行数、id 区间递增与 head 一致（行数可小于水位：
  显式 `event_id` 允许留空洞）。
- 测试：`TestStructuralEventAnchorAfterMessage`（T-EV-01）、
  `TestStructuralEventDuplicateCommitIdempotent`（T-EV-02）、
  `TestStructuralEventMissingEventOK`（T-EV-03）、
  `TestStructuralEventAnchorAtOrBelowWatermarkOK`（T-EV-04）、
  `TestStructuralEventWatermarkIdempotent`、`...UnpublishedAppendInvisible`、
  `...HeadAbsentMeansNothingPublished`、`...CrashTailDropped`、
  `TestStructuralEventCommitNeverReadsHistoryShards`（T-EV-05）。

### 原子发布的 rename 退避

`writeAtomic`（全部模块 head 与 config 的发布原语）在 rename 失败时按
`renameBackoff`（2/5/10/20/40 ms，累计 ≈77 ms）重试同一个 tmp，超预算则删除
tmp 并显式报错。原因：Windows 上目标文件被任何句柄打开即令 rename 失败，实测
持柄者松开耗时 0–18 ms（`-tags redprobe` 的
`TestProbeStackHeadPublishHandle` 用 share=0 独占打开探测）。**提交级重试禁止**：
数据 append 已发生，重放会重复追加归档行 —— 重试只能落在 rename 本身。契约
见 `TestWriteAtomicSurvivesTransientHandle` 与
`TestWriteAtomicGivesUpWithinBudgetAndLeavesNoTemp`。

## 子代理会话记录（NodeSessionRecord）

子代理（fork/plan 的 `kind:agent` 节点）会话记录按主会话索引落盘：

```text
sessions-json/<project>/session-<mainID>/subagents/<mainID>-<subID>.json
```

文件名为 `<mainSessionID>-<subSessionID>.json`（用户约定：从主会话索引可直接
列举全部子会话，无需全局扫描）。内容为 opaque JSON 记录
（`NodeSessionRecord`：NodeID/SessionID/Goal/Status/History/ContextJSON/
StagesJSON/ResultJSON/Worktree 现场 + schema 版本）。

生命周期（2026-08-24 策略）：运行期由 `seelebridge/session` 的
`SubagentSessions` actor 在注册/阶段/结果/终态时写入（进程中断可恢复）；
节点结束（done/failed）时最终结论经 `seelex.subagent.result` 事件写入主会话
事件库（"结论跟随 mainagent"），随后删除节点记录文件。删除后详情数据面保留
在进程内存快照；重启后恢复锚点从主会话事件库重建。

`NodeSessionStore`（JSON backend 已实现）提供 `Save/Load/List/Delete`，显式
项目作用域，不改变 Router 的 active write scope；SQLite/PostgreSQL/Redis
接入为后续项（当前返回明确错误）。

## 配置与安全

- JSON/SQLite 使用本地 path；PostgreSQL/Redis 使用 DSN。
- `Config.Safe` 不把 DSN 返回 GUI，只报告 configured。
- 配置文件写入采用原子替换和私有权限。
- project/session ID 进入路径前经过 hash/安全编码，不能直接形成逃逸路径。

## Review 指南

- 所有 backend 是否保持相同逻辑 snapshot 语义。
- JSON manifest 是否最后提交；失败 generation 是否不会被读取。
- SQL migration/upsert 是否兼容 SQLite 与 PostgreSQL placeholder，Redis key 是否保留同一 project hash tag。
- Router 是否在任何错误路径关闭 replacement、保留 old repository。
- range offset/limit 和 empty history 的语义是否一致。
- 栈通道：新增后端时 `stackJournal()` 是否实现（接口编译期强制，禁止用
  `ok=false` 兜底）；head 是否仍只装水位；写路径是否又开始解析 history。
- EVENT 通道：提交是否仍只读 head（`store.event.shardReads` 必须为 0，只在崩溃
  恢复路径允许 +1）；幂等是否还靠回看历史算指纹；发布失败是否被误当成可以
  提交级重放（append 已发生，只有 rename 可重试）。
- 任何"让读者不持锁读文件"的改动：先确认发布形态不再是 rename 覆盖，否则
  锁等待会被换成提交失败。

## 测试

```text
go test ./sessionstore -count=1
go test ./sessionstore -run 'TestV8' -count=1   # M1–M4 契约（65 条）
```

## Atomic transcript and result contract

`WriteCommit` publishes bounded provider history, append-only transcript events, opaque application state, and immutable tool-result objects under one `(project_id, session_id)` scope. `WriteAtomic` remains a compatibility wrapper for history-only callers.

`ReadEventTail` returns newest complete protocol units within token and unit limits. A user turn may include sequential or parallel tool rounds, but it is omitted if any tool call lacks a matching result; orphan tool events are never returned alone. `ReadToolResult` is read-only. JSON manifests publish the committed result-reference set, SQL stores all parts in one transaction, and Redis uses one `MULTI/EXEC` in the project hash slot.

测试覆盖 JSON/SQLite 的 generation 原子性与状态 sidecar、SQLite 分表分片、Redis 的配置和 key 分片策略、backend 切换和显式 workspace read 不污染 active scope。plan/task/goal 三栈的用例经 `forEachStackBackend` 在 **json 与 sqlite 两个后端各跑一遍**（head 只装水位、批次整批弹栈、message 锚、迁移 EVENT、LIFO、投影替换、并发单写者、fork 按锚重建、跨实例持久化）；JSON 后端另测物理放置、head 未发布不可见、崩溃残尾与归档回收，以及 `-race` 下的延迟归因（`TestStackChannel*`）。goal 侧另覆盖按会话隔离与帧校验（`TestGoalStack*`）；blob 版本不再兼容 v2 及更早（`TestGoalStackLegacySchemaRejected`）。

## 会话 fork 存储契约（一期，已实现）

对话 fork（新开会话的分支，**非 subagent fork**）在存储层的契约：

- **段落边界**：`EventParagraphs` 由完整协议单元（`CompleteEventUnits`）推导
  段落表（EventFrom/EventTo/RequestID/MessageFrom/MessageTo）；fork 切断点
  必须落在段落边界（`IsParagraphBoundary`），EventSeq 含端点，避免截出半截
  轮次。
- **通道枚举**：`ListToolResults` 返回 tool-results 通道全部不可变结果（含
  `compressed:<segment_id>` 压缩原文归档），供 fork 深拷贝物理复制。
- **快照版本**：`CurrentGeneration` 返回会话当前已发布 generation，是血缘
  `forked_from_generation` 的事实来源（不可变快照绑定，禁止引用父的最新
  内存状态或后续提交）。
- **落盘路径**：fork 由 application 层构建截断后的 `Commit`（继承事件流 +
  截断 SessionRecord + 父通道全量 tool-results；ProviderHistory 整段缓存
  已随 S11 退役），经 `WriteCommit` 原子写入子会话 key；两端此后完全独立。
  删父安全的前提是
  tool-results 物理复制——`read_tool_result`/`read_compressed_turn` 在删父后
  仍从子会话自己的通道读回。
- **不随 fork 传播**：子代理节点记录（`subagents/`）与执行事实事件库
  （`framework-events.json`）归属父会话执行，不复制到子会话。

血缘 meta（`forked_from`/ForkPoint）与截断重写属于 application 层
（`application/model` + `application/core/session_runtime`），存储层只负责
通道与原子提交，不解释内容。
