# v8 会话存储设计稿符合度打点表（2026-09-09）

> 性质：一次性工作包打点表（可勾选台账）。权威口径 = [my_design.md](./my_design.md)（v8.3）
> 与代码/测试；本表只记录「设计稿条目 → 实现状态 → 证据」，不冒充长期事实源。
>
> 本轮用户指令：
> 1. 按设计稿意图**完全执行**，链路上的存储方式按设计稿，不采用兼容做法；
> 2. 设计稿的文件放置位置/放置内容/ER 图必须遵循；
> 3. 每完成一项做 race + pprof；出现数据竞争先降锁粒度 + actor 无锁化，不奏效则**暂停上报**；
> 4. **message 读写的细化研究放到最后**：先完善设计稿内容，用户确认后再按稿实施（本轮不动
>    message 读写的锁形态设计）。
>
> 状态图例：`[x]` 已完成（附证据）｜`[ ]` 缺口（待做）｜`[~]` 核查后不需要改（附证据）｜
> `[?]` 待决（需用户决策）。
>
> 阶段编号：`S#` = §6 验证记录表里的阶段行（S1/S5/S6/S8/S9/S10 已在该表）；v8.3 新增待办的
> 阶段号（S11 起）与其验收口径见 §7，本表引用的所有 S# 都必须能在 §6 或 §7 找到对应行。

## 0. 用户口径决策（本轮新增，优先于早期表述）

| 编号 | 决策 | 出处 |
|---|---|---|
| D1 | skill **不是栈文件、也不是特例**：它就是普通 message 事件行，与 user 发送的信息同语义、同序装配进 wire；无专属段、无专属过滤器、无专属读取器 | 本轮问答 2026-09-09 |
| D2 | 旧 manifest 布局会话**彻底退役，不再打开**（链路上不留只读回退） | 本轮问答 2026-09-09 |
| D3 | 栈按设计稿分文件放置，消除 context 单文件写竞争 | 本轮指令 2026-09-09 |
| D4 | **栈存储必须兼容其它存储格式**：JSON / SQLite / PostgreSQL / Redis 都要有栈通道，不允许「只有 JSON 有栈」 | 本轮指令 2026-09-09 |
| D5 | 栈锁延迟必须**分域归因**（active.jsonl vs history.jsonl），并判定是数据竞争还是锁竞争；先拆锁粒度 + actor 闭包，不奏效则暂停上报 | 本轮指令 2026-09-09 |
| D6 | **栈按 kind 分锁**：plan/task/goal/subagent 各一份 head 一把锁（共享 head = 共享串行点，拆锁必须先拆发布点）；`subagent` 是第四个批次栈（全批终态才弹栈）；**fork session 不入栈**（无完成点，按 §8.1 拷贝 + 父侧 EVENT 留档） | 本轮问答 2026-09-09 |
| D7 | **message 只承载「给 LLM 看的会话正文事实」**：血缘/状态位/检查点等非上下文元数据不得写成 message 行 | 本轮问答 2026-09-09 |
| D8 | **Checkpoints 的生态位在 EVENT**（kind=`checkpoint`，投影由最后一条事件派生）；渲染正文仍留 message internal 行。**前置已补（S19）**：`model.TranscriptEvent.WireMaterial` + 适配层往返 + `appendTranscriptEventLocked` 对 internal 行置位，internal 行重启后仍进 wire | 本轮问答 2026-09-09 / 落地 2026-09-10 |
| D9 | **旧包袱全部退役**：`history.json`/`transcript.log`/`manifest+generation`/`tool-results`+`metadata/toolresult.json`/`context.json`/`state.json` 停止读写；blob 单通道；**dev 阶段丢字段已接受**（Title/Status/压缩帧扩展字段等不搬运）；guide 取消 `module_index` 登记（无读者，且是跨会话写热区根因） | 本轮问答 2026-09-09 |
| D10 | **head 发布失败后的重放必须幂等（栈通道两条都做）**：写侧 `commit_id` 取调用方逻辑操作标识 + head 记 `last_commit_id` 判重；读侧按 `item_id` 去重、高 `revision` 胜出。只做一条不够（写侧挡不住冷重载重复条目，读侧挡不住 subagent 重复派发） | 本轮问答 2026-09-09 |
| D11 | **数据根锁被占用 = 立即报错**：不等待、不自旋、不接管；陈旧只由心跳 `renewed_at` 判，接管还需 `auto_recover=true`。不设 `wait_timeout_seconds` | 本轮问答 2026-09-09 |
| D12 | **通道分两类，判据各归各**（只收口语义，实现不动）：**追加型**（message / event / 各 kind 的 `history.jsonl` / `compact.jsonl`）用 `revision ≤ head_seq` + 条目键取最高 revision；**整份替换型**（各 kind 的 `active.jsonl`、`queue.jsonl`、`draft.json`、`system.json`、`retention.json`）以「文件原子替换成功」为发布点，**戳号不得当可见性闸门**。T-M1-04 只约束追加型（§2.0 通道类型表、§2.5.2） | 复核 2026-09-09 |
| D13 | **commit_id 逐逻辑操作唯一**：随机与**常量**同判违规（随机 → 重放认不出、留重复行；常量 → 不同提交被误判重复而整次丢弃，丢真数据）；义务适用全通道（message/event/栈/队列/blob），且公开读接口不得擦除凭据（§2.0 规则 4、§5.1） | 复核 2026-09-09 |

## 1. §2.0 metadata 模块化（head + guide 版本判定）

| 项 | 状态 | 证据 / 说明 |
|---|---|---|
| guide.json 只做版本判定，**不登记模块地址**（D9） | `[x]` | 已删（S14）：`layoutGuide.ModuleIndex`/`moduleRef`/`registerModule` 全数移除，各模块写路径不再触碰 guide；模块路径由 `modulePath()` 按枚举名推导。验收 `TestGuideNotRewrittenAfterModuleHeadWrites`（T-DP-04：首写 message/event/compact/stack/lifecycle/retention/subagent head 后 guide 字节不变） |
| 模块 head 清单（v8.3 §2.0：guide + 11 模块 = 12 个文件，media 预留） | `[x]` 存储层 | head 枚举：message/event/compact/**stack_plan/stack_task/stack_goal**（S16）/lifecycle/retention/subagent/**system**（S19 存储层）/media——`toolresult` 已删（S13）；E.2 另含整份替换型 `checkpoint.json`（S24）。`system.json` 生产写入（SessionContextStore 迁移）与 T-DP-01 全链路门禁仍待 S19 消费面收口 |
| 写锁按「模块 × 栈 kind」、彼此互不阻塞（D6） | `[x]` JSON 侧 | JSON 栈已按 kind 拆 head（`metadata/stack_{plan,task,goal}.json`）与三把锁（`stackPlanMu/stackTaskMu/stackGoalMu`，`journal.lock(key, kind)`）；跨 kind 不再共享串行点（S16，`TestStackChannelWritesDesignedFiles` 断言旧 `stack.json` 不再存在）。SQL/Redis 后端 head 行仍为会话级共享（锁按会话串行），其按 kind 拆分随 SQL/Redis v8 化范围（§4 待决）一并落。**同一把仓库级 `jsonRepository.mu` 的其余 19 个上锁点**仍是独立缺口，不得按「一个字段的历史遗留」处理 |
| reader 校验 schema/checksum + 自愈重读 | `[x]` | `readModuleHeadFile` 双读 + `readSelfHealHook`；T-M1-06。判据已在 v8.3 定稿（§2.0 规则 3 + §4「guide 读」行） |
| 自愈第二次仍不符 → **按数据文件重建该模块 head** | `[x]` | 已通用化（S15）：`readModuleHeadFile` 二次自愈失败后按数据文件重建并发布 head——message/event/compact/stack 有数据文件重建实现（`rebuild*HeadFromData`），lifecycle 沿用既有 `lc-repair-*`；retention/subagent/media 无独立数据文件时保留原错误。验收：T-M1-09（`TestHeadRepairRebuildsMessageFromData`/`...Event...`/`...Compact...`） |
| 可重建派生物放 metadata-index/ | `[~]` | 检索索引在 `metadata-index/search.json`（`search_index.go:36`）；blob 不需要清单（§2.0 规则 6 + D9）。**分片索引的边界已收口**：规则 5 原句把 `shard_index` 也赶进 metadata-index，与同节通道类型表互斥（M1）；现定为「head 装发布时顺手可得的**本通道分片路由**，需重扫才能重建的大索引进 metadata-index」→ 实现现状（`messageHead.Shards`、`eventHeadRecord.Shards`）**合规，无需改** |

## 2. §2.4 / §3.2 四栈（S1 已交付三栈；v8.3 待拆 head + 第四栈）

| 项 | 状态 | 证据 / 说明 |
|---|---|---|
| `session/{plan,task,goal,subagent}/{active,history}.jsonl` | `[x]` 存储侧 | 四 kind 已落（S18）：`StackKindSubagent` 走同一栈通道文件布局，`registerSubagent` 以「dispatch:<operationID>」批次压条目，`updateSubagentStatus` 批内全完成才整批归档；条目 payload = `subagentInfo`。验收 T-STK-12（`TestStackSubagentBatchSemantics`）。协调器/结果字段消费仍在应用层 |
| head 按 kind 拆：`stack_plan`/`stack_task`/`stack_goal` + `subagent.json` | `[x]` JSON 侧 | JSON：`moduleForStackKind` 路由到三份独立 head 文件；`readStackHead(key, kind)`/`publishModuleHead(key, moduleForStackKind(kind))` 各自水位；verify 按 kind 加锁校验；`open_batches` 已删（S23）。`T-STK-02` 的「head 内不出现 item_id/条目内容」断言保持。SQL/Redis 同步随 §4 v8 化待决 |
| head 内字段的真实职责（改判：原稿把两个关键支点贬成「只服务 verify」） | `[x]` | `history_count` = **免读归档**（冷装载 `stack_journal_json.go:106/110/216`、写路径推进 `stack_journal.go:132`），`history_bytes` = 截回未发布归档尾行（`:233-243`），`active_count` = 装载关键路径兼校验；三者都在关键路径上。多余字段 **`open_batches` 已删（S23）**（零读者；`stackWatermark`、`stack_journal.go` 写点、`cloneStackWatermarks` 复制点与断言全部移除） |
| `metadata/subagent.json` 现装**条目全清单**，违反 §2.0 规则 1 | `[x]` 已修（S18） | `subagentHead{Items}` 删除；条目走 `session/subagent/active.jsonl`，head 由栈通道发布为水位（T-STK-12 断言 head 内无 `subagent_id`/payload） |
| 栈写侧重放幂等：稳定 `commit_id` + head 记 `last_commit_id`（D10） | `[ ]` | 现状不合规：`stack_journal_json.go:183` 用 `"stack-"+randomID()` 当发布凭据 → head 发布失败后重放无法判重。验收 T-STK-10 |
| 栈读侧按 `item_id` 去重、高 `revision` 胜出（D10） | `[ ]` | 现状不合规：`readStackRowsFile`（`stack_journal_json.go:316-333`）只按 `Revision ≤ headSeq` 过滤 → 重放留下的同 `item_id` 双行在冷重载后同时可见。进程内 `pushItem` 的 already-active 守卫挡不住跨重启。验收 T-STK-11 |
| 批次语义：未完成整批留 active、全完成整批归档 | `[x]` | `stackState.batchDone`/`closeBatch`；`T-STK-03`（部分完成整批留栈）/`T-STK-04`（整批弹栈）|
| history 锚：item_message_id / batch_message_from / batch_message_to | `[x]` | `messageAnchor` 压栈时盖章；`T-STK-05` 断言逐条各自坐标、同批共享 batch_from；`BatchMessageToSeq` 供 fork 过滤 |
| 状态迁移由 EVENT `goal.*/task.*/plan.*/subagent.*` 记录 | `[~]` | `recordStackEvents`（数据→head→EVENT 写序），`T-STK-06` 断言 `goal.active`/`goal.completed` 与锚点。`subagent.*` 待第四栈落地时一并补 |
| head 未发布不可见 / 崩溃残尾 | `[~]` **仅对追加型成立** | 条目带 `Revision`、按 head 过滤 + `truncateCrashTail`，`T-STK-08` 覆盖崩溃残尾；但 `T-STK-07`（`stack_channel_test.go:752-777`）只测「追加一条高戳号幽灵行不可见」。`active.jsonl` 属**整份替换型**（`stack_journal_json.go:168` 整体原子替换），不存在「已 append 未发布」中间态，因此该 [x] 原本是对 §2.0「append 未发布 = 未提交」的**过度概括**——现按 D12 限定适用范围（H9 同步改判），并补 T-STK-13 断言整份替换型语义 |
| context blob 不再承载三栈 | `[x]` | `SessionContextStore.Persist` 落盘前剥离 Plan/Task/Goal；`Load` 装载后按通道回读；schema 升 **v3**，v2 及更早显式拒绝（`TestGoalStackLegacySchemaRejected`） |
| 运行期入口 | `[x]` | `Router.StackPush/StackSetStatus/StackPopTop/StackReplace/StackActive/StackHistory/StackVerify/StackStorageStats/ForkStacks`；`ErrChannelUnsupported` 已删除（不再有「某后端没有栈」的分支） |
| **栈通道后端无关（D4）** | `[x]` | `stackJournal` 契约（`stack_journal.go`）+ 三份实现：JSON（`stack_journal_json.go`）、SQL（`stack_journal_sql.go`：`seelex_session_stack_item`/`_stack_head`/`seelex_session_structural_event`，事务=发布点）、Redis（`stack_journal_redis.go`：`<session>:stack:*` 列表 + head + `MULTI/EXEC`）；`Repository.stackJournal()` 是**接口方法且不可包外实现** → 新后端必须显式给栈通道 |
| 栈语义用例的后端覆盖面（证据降格） | `[ ]` | `forEachStackBackend` 只跑 json + sqlite（`stack_channel_test.go:69-73`）；**Redis / PostgreSQL 的栈通道零语义测试**（`BackendRedis` 仅出现在 `state_test.go:215`）。D4 要求四后端都有栈通道，测试面上仍是 2/4；上一行的 `[x]` 证据只到「编译期强制 + SQLite 双跑」 |
| SQL/Redis 锚的粒度缺口 | `[ ]` | 两后端消息通道仍是整块 shard 快照（无 v8 事件行键）→ `item_message_id` 为空、只有 `seq`；fork 过滤按 seq 不受影响，但「v8 事件行落到 SQL/Redis」仍是 §4 待办 |
| 通道语义跨后端一致 | `[x]` | `forEachStackBackend` 把 11 条语义用例在 json + sqlite 各跑一遍（head 只装水位 / 整批弹栈 / 锚 / 迁移 EVENT / LIFO / replace 派生 / 并发单写者 / fork 按锚 / 跨实例持久化） |
| fork 子会话栈 | `[x]` | 存储层 `stackSnapshotAt`+`stackRestoreSnapshot`（起点 < watermark 报 `ErrForkBeforeWatermark`）；app 层经 `SaveSessionSnapshotWorkspace` 调 `Router.ForkStacks`；`T-FK-02` 改为按真实 message 锚断言 |
| 锁延迟归因（D5） | `[x]` | 见 §5「S5/S6 归因结果」：写路径不再解析 history（`history_read=0`）、读者走 `atomic.Pointer` 快照（`cold_loads=0`，均值 5.96 µs/次）、EVENT 提交移出栈临界区、guide 锁由仓库级降为会话级 |
| 行为变更（需知悉） | `[~]` | 关闭的 plan/task 帧不再原地留在 active（§2.4 整批弹栈归档）；「now using = 栈顶」因此只看到未收口帧 |


## 3. §1 事实模型 / §2.2 compact / §2.3 message

| 项 | 状态 | 证据 / 说明 |
|---|---|---|
| message = 唯一正文事实源、append-only | `[x]` | `message_rows.go`（分片/续号/幂等/残尾）+ T-M1-01..08 |
| compact = `session/compact.jsonl` + head | `[x]` | `compact_frames.go`；head 冗余 LatestFrame 供 R2 单读 |
| compact 双写（context.json CompactStack 与 compact.jsonl 并存） | `[x]` JSON 侧 | S19：`PushCompact` 先桥接 compact 通道（单源写入），`Persist` 对 v8 JSON 剥离 `compact_stack`（blob 不再双写），`Load` 从 `CompactFramesWorkspace` 回读；扩展字段不搬运（§3.2 已接受），相关契约测试改为断言核心字段 + 退化字段为零。SQL/Redis 未 v8 化仍走 blob |
| R2 只装配栈顶帧 Chapter2、锚点章节不发模型 | `[ ]` | `assembleWireRows` 直接投 `frame.Summary` 全文；运行期渲染侧已有 `FrameChapter2`（`seelexctx/assembler.go:201`） |
| `history.json` provider 缓存 | `[x]` | 已退役（S11）：`writeCommitLayout` 不再写 `history.json`，`ProviderHistory` 一律不再落盘（同一提交以 `Events` 为准；dev 丢字段已接受）；`readAllLayoutMessages`/`readRangeLayout` 一律由 message 事件行派生（`rowsToProviderMessages`）。回归探针 `TestProbeNoHistoryCacheWritten`（redprobe）；T-DP-02 全链路门禁待 S13/S19/S20 齐后复跑 |
| `state.json` 每轮整文件重写 | `[x]` JSON 侧 | S20：`writeCommitLayout` 停写 state；`WriteState` 空操作、`ReadState` 恒 `fs.ErrNotExist`；record/会话枚举按 §2.5.4 由 `message head.Meta` + message 事件行 + `lifecycle.archived_at` 派生（`DerivedRecordWorkspace`/`derivedRecordPayload`），conversation 同源派生；`Title`/`Kind`/子侧血缘/`ReadFiles`/`Continuation`/`Checkpoints` 不再持久化（dev 已接受），展示元数据（pin/alias/order）迁 `project-*/session-meta.json`。未 v8 化后端保留 state 通道 |
| 队列「出队」的落盘语义（此前稿子未规定） | `[~]` | 实现是整份替换（`lifecycle.go:220`，清空即删文件 `:180`）；v8.3 §2.0 通道类型表把 `queue.jsonl` 归入**整份替换型**并写清「出队 = 新内容不含该项，无需墓碑行、无需戳号闸门」→ 语义收口，实现不动。补 T-STK-14 断言该型文件无半更新中间态 |
| §5.3 步骤 2「tail 从 frame.message_to 下一行」 | `[x]` | `tailStart = frame.MessageToSeq + 1`（`wire_assembler.go:89`），T-R2-02 |
| §5.2 budget 默认 200k / 软 75% / 目标 60% | `[ ]` | 配置侧已就位：`Settings.WireBudgetTokens/WireSoftRatio/WireTargetRatio` = 200000/0.75/0.60，`wireBudget()` 返回 200000/150000/120000（T-CFG-01 断言）。**装配侧未消费**：`wire_assembler.go:58` 仍硬编码单一 `Budget = 200_000`（字符口径），无软阈值触发压缩、无裁剪到目标比例 |

## 4. §2 生命周期、EVENT、blob、检索、§9、§11

| 项 | 状态 | 证据 / 说明 |
|---|---|---|
| draft/queue 权威在 `input/draft.json` / `queue/queue.jsonl`（v8.3 钉死，不再写 `json(l)`） | `[x]` | `lifecycle.go` 重写：权威 = `session/input/draft.json` + `session/queue/queue.jsonl`（`lifecycleState`），两者都走 `writeAtomic`（原 `queue.jsonl` 用非原子 `os.WriteFile` 且吞错，已改）；`lifecycleHead` 只装水位（`draft_revision`/`queue_count`/`queue_head_seq`/`head_state`/`sending_item_id`/`commit_id`，符合 §2.0 规则 1）；写序 = 数据 → head（发布点）；**待改**：现仍多一步 `registerModule` 写 guide（D9 取消，见 §1）；读侧 `readLifecycleState` 发现 head 落后即按数据修补（`lc-repair-*`），数据与 head 双空时不落盘（不造假 head）；`queue.jsonl` 崩溃残尾由 `truncateCrashTail` 丢弃。测试 `TestLifecycleFactsLiveInDesignedFiles`（head 内不出现 `"content"`）/`TestLifecycleHeadLagRepairsFromData`（删 head 后事实仍在且水位被修补）/`TestLifecycleQueueCrashTailDropped`，T-LC-01..09 全部改为读数据文件断言。**新增待办**：lifecycle head 补 `archived_at`（§2.5.4 的已归档判据） |
| EVENT 写形态 = 与 message 同构的 append-only（§7/附录 A.2） | `[x]` | `structural_events.go` 重写：提交只读 head（水位 + 分片路由 `count`/`bytes`）→ 定位尾分片追加 → 原子替换 `metadata/event.json`，**不读任何已发布事件行**；幂等改成 A.2 水位式（`event_id=0` 续号 / 显式 id ≤ 水位跳过 / `commit_id == last_commit_id` 整次空操作），删除 `readEventShardsLocked` 全量重读与 `structuralEventFingerprint`；分片索引不再重算 `fileSHA256`（死字段）；恢复只在字节数与 head 不一致时读一次尾分片，并把修补后的 head 发布出去。计量 `store.event.shardReads` 断言常态为 0。测试 `TestStructuralEventCommitNeverReadsHistoryShards`（T-EV-05）/`WatermarkIdempotent`/`UnpublishedAppendInvisible`/`HeadAbsentMeansNothingPublished`/`CrashTailDropped`，T-EV-01..04 保持 |
| EVENT 运行期写入（生产者） | `[ ]` | 仅 fork 路径写 EVENT（`fork_store.go:130/188/203`）+ 栈迁移 `goal.*/task.*/plan.*`；compacted/interrupted/request·turn/token_usage/session_archived/**checkpoint**（§2.7）无生产写入方 |
| R3 合成 interrupted 规则（T-R3-02/03） | `[ ]` | `history_resume_readers.go` 有 open-tail 探测，未接 EVENT 合成/不合成判据 |
| big_tool_result（软截断/硬报错/配额/GC 跨引用） | `[x]` | `big_tool_result.go`；T-BL-01..03；subagent 复用主会话 blob（I11）由 T-FK-06 覆盖 |
| 检索 = 单会话关键词模糊索引、可重建 | `[x]` | `search_index.go`；T-SR-01..03 |
| §9 单数据根独占锁（owner_process/陈旧锁/auto_recover） | `[x]` | 已接入（S9）：`newJSONRepositoryWithLayout` 打开 JSON 数据根即 `acquireDataRootLock`，`jsonRepository.Close` 归还（同进程引用计数共享）；冗余 `acquireViaLink` 分支删除；`DataRootLockedByWith(root, staleAfter)` 按配置 stale 口径判定（`DataRootLockedBy` 保留 300 s 默认包装）。契约测试 `data_root_lock_test.go`：取得/归还/引用计数（`TestJSONDataRootLockAcquiredAndReleased`）、跨进程存活锁立即报错（`TestJSONDataRootLockForeignProcessRejected`）、陈旧锁默认拒绝 + `auto_recover=true` 接管（`TestJSONDataRootStaleLockRespectsAutoRecover`） |
| §2.1 `project/{system,project}.jsonl` + `metadata/system.json` 会话快照 | `[ ]` | 两个待办：① `project/project.jsonl` 全仓库不存在（grep 无命中）；② v8.3 把 system prompt 改判为**会话侧快照**（`metadata/system.json`，规则 1 的内容例外）——理由：项目配置一旦编辑，所有历史会话的 wire 前缀当场变化，破坏 R2-STABLE-1；现值仍在 `context.json.SystemPrompt`，由 `seelexctx` 渲染 |
| §11 配置并入 seele.yaml limits 覆盖链 | `[x]` | 覆盖链 = 内置默认 ← `seele.yaml` `limits.session_storage.*` ← `session-storage.json`：`storage_settings.go` `resolveStorageSettings(layers...)`/`mergeStorageSettings`（零值 = 未设置，布尔项用 `*bool` 让显式 `false` 生效）、`Settings` 对外别名、`Config.Normalize` 走 resolve+validate、`NewRouter(configPath, defaultPath, limits ...Settings)`；`seelexctx/limits.go` 新增 `SessionStorageLimits`（不重复默认值）；`main.go:990` `sessionStorageLimits()` 在 composition root 映射；`config/seelex.yaml:54` 全注释说明块。各通道魔法常量已收口为单一 resolved 对象（`store.settings.shardRows()`/`blobLimits()`/`retentionThresholds()`/`NewAttemptCache(...)`）。测试 `TestStorageSettingsOverrideAndValidation`（T-CFG-01：逐层覆盖 + 非法值拒绝 + 默认自洽） |
| fork 拷贝携带 frame 摘要承接 watermark 前区间 | `[ ]` | `forkStore.forkSession` 只拷 message 行 + 栈条目，未拷 compact head/帧 |
| retention raw_bytes 精确度量 | `[ ]` | `rawBytesEstimate = TotalRows * 1024` 是粗估（`json_layout.go:273`），设计稿要求按原始字节告警 |
| M5 verify / reconcile | `[ ]` | 「仅 verifyMessage」已失真：现有三把——`verifyMessage`（`message_rows.go:586`）、`verifyEvents`（`structural_events.go:510`）、`Router.StackVerify`（`runtime_api.go:150`）；但**三者零生产调用方**，跨通道 reconcile/巡检出口仍缺（结论不变、原措辞失准） |
| 「检查点」是两个东西：workplan 续跑快照落点 | `[x]` 已定并落地 | **上下文检查点**：渲染正文 = message internal 行、结构事实 = EVENT `checkpoint`（§2.7，消费面随 S19/S20 EVENT 生产者）。**引擎续跑快照**：判据 = 不能从 message+EVENT 完全重算（各节点中间产出）→ 用户口径选会话侧整份替换型 `metadata/checkpoint.json`；`checkpoint_store.go` 已从 state 通道迁至 `Router.SaveCheckpoint/LoadCheckpointWorkspace`（JSON v8 布局；SQL/Redis 沿 state 通道待 v8 化）。T-CK-01 扩展断言：不再产生 state.json |
| SQLite/PostgreSQL/Redis 的 v8 化 | `[~]` 搁置 | 用户口径（2026-09-10）：**先搁置，随 message 读写热点专项一并优化**；本轮不做 SQL/Redis 的 v8 化与按 kind head 拆分 |
| §10 媒体/多模态 | `[~]` 不做 | 设计稿自标「后续规划」，media.json 为预留位 |

## 5. message 读写热点（本轮只记录，研究放到最后）

| 编号 | 事实（实测/静态） | 状态 |
|---|---|---|
| H1 | 读者持 `history.json` 句柄 → 提交 `rename` 失败（`Access is denied`）。~~全链路零重试~~ **表述已过期**：`renameBackoff`（6 次退避累计 ≈77 ms，实测持柄者 0–18 ms 松开）**已复测**（见 §6 S10）；且 `history.json` 已随 S11 退役（写路径不再产生该文件，探针 `TestProbeNoHistoryCacheWritten`），该实例消除 | `[x]` 消除于 S11（D9）；发布形态开销归 message 读写专项 |
| H2 | 一次全量历史读使同会话提交多等 91.7 ms（基线 63.7 ms，预算 30 ms）；读与写共用 `messageMu` | `[?]` **待专项**：先完善设计稿再实施（用户 2026-09-09 指令） |
| H3 | 提交基线 63.7 ms 中相当部分来自写侧 O(分片) 重复 IO：`appendRowsLocked` 每次整片重读 + `fileSHA256` 整片重算 | `[ ]` 属 message 读写专项，随 H2 一并入稿 |
| H4 | EVENT 幂等去重每次提交全量重读所有 event 分片（`readEventShardsLocked`） | `[x]` 已消（**且重新定性**）：按「EVENT 与 message 同构走 append-only」改为 head 水位幂等（设计稿 A.2 已同步修订）。同轮 A/B（同一 `TestStackChannelLockAttribution`，新旧实现交替各跑）：**150 次提交的 EVENT 域没有可测差异**（旧 15.0/20.3 s，新 16.4/18.9 s，本机抖动 ≫ 差值）。改用规模探针（30 次单行提交取 mean，历史行数递增）后才看清真相：**旧实现 ~41 → ~69 ms/次（0→3000 行，+70%）；新实现 38.7/29.6/34.6 → 43.3/33.9/35.4 ms，与历史规模无关**。所以全量重读是**真实的 O(history) 项但非常数项里的主项**，约 40 ms 的地板来自「fsync 数据 + rename 发布 head」本身——与 message 提交基线（63.7 ms）同源，属 H2/H3 专项 |
| H5 | 栈提交读 `metadata/message.json` 取锚 → 打开一个正被 rename 覆盖的文件（Windows 句柄撞发布，跨模块） | `[x]` 已消：message head 发布时同步盖章内存锚（`rememberMessageAnchor`，`atomic.Pointer`），栈通道 `anchor()` 优先取内存锚 → 栈链路不再打开 message head 文件 |
| H6 | 任一会话写 guide 时持有**仓库级** `metaMu` → 所有会话的提交互相排队 | `[x]` 已消：`metaMu` → 会话级 `sessionModuleLocks.guideMu`；S6 归因 guide 域 0.13 s / 150 次提交（此前参与跨会话串行） |
| H7 | 栈 head 发布 `rename → metadata/stack.json` 偶发 `Access is denied`（`-race` 4 次 1 次、普通全量约 1/4 轮；写者全程同一把锁，非并发写者造成） | `[x]` 已修（发布原语层）：归因探针 `TestProbeStackHeadPublishHandle`（`-tags redprobe`，share=0 独占打开探测持柄者）实测**失败瞬间持柄者在 0–18 ms 内松开**；对照组 = 空闲进程 400 次同名 rename **0 失败** → 触发条件是 IO 风暴下的瞬时外部句柄（Defender/索引器），不是本进程读者（该场景内没有任何代码为读打开 `stack.json`）。修法：`writeAtomic` 对**同一个 tmp** 做有界退避重试（2/5/10/20/40 ms ≈77 ms 预算），超预算删除 tmp 并显式报错。**禁止提交级重试**——数据 append 已发生，重放会重复追加 history。契约测试 `TestWriteAtomicSurvivesTransientHandle`（25 ms 持柄 → 41 ms 内发布成功）/`TestWriteAtomicGivesUpWithinBudgetAndLeavesNoTemp`（超预算 → 报错 + 不留残 tmp + 目标内容未变） |
| H8 | **常量 commit_id 丢事件**：`fork_store.go:126` 给 EVENT 传常量 `"fork-event"` → 同一父会话第二次 fork 的事件被 A.2 规则 3 判成重复提交、**整次丢弃**（丢的是真数据，比随机值严重）。根因是旧措辞只写「稳定」没写「逐操作唯一」 | `[x]` 已修（D13）：fork/EVENT 凭据改为逐操作唯一（父会话号 + fork 点 + 序号），`TestStructuralEventForkRowsSurviveConsecutiveForks`（T-EV-06）与探针 `TestProbeForkEventConstantCommitID` 双绿 |
| H9 | **（已重定性，非实现缺陷）**原判「未发布的状态迁移照样可见」的前提是把 `revision ≤ head_seq` 当通用闸门；但 `active.jsonl` 是**整份替换型**通道（`stack_journal_json.go:168`），该文件不存在「已 append 未发布」中间态，戳号只是标签 | `[~]` 集稿即闭合：§2.0 通道类型表 + D12 已限定判据范围；探针 `TestProbeStackStatusUpdateUnpublishedInvisible` 转成 T-STK-13 的语义断言（不要求实现改追加） |

### S5/S6 归因结果（回答「延迟落在 active 还是 history」）

`TestStackChannelLockAttribution`：3 个 kind 各 25 轮 push+pop（150 次提交）与
2 个读者同轮（读者每轮 sleep 100µs，不抢 CPU）。本机 Windows / go1.25.8，
**同一条测试跑 3 次的区间**（绝对值抖动大，只有结构性零值稳定）：

| 数据域 | 3 次运行区间 | 判定 |
|---|---|---|
| 栈锁等待（写者） | 0.30 / 0.60 / 2.31 s | 与 EVENT 抢 CPU 相关，非栈侧结构 |
| active.jsonl IO | 0.46 – 0.99 s（≈4 ms/次） | 只剩投影写入 |
| history **append** | 1.63 – 2.70 s | 归档只追加 |
| **history read（解析）** | **0 / 0 / 0** | 写路径完全不读归档文件 |
| head 发布（rename） | 3.61 – 7.57 s（≈30 ms/次） | 栈临界区内最大项 |
| guide | 0.09 – 0.36 s | 已降为会话级锁 |
| **EVENT** | **16.6 – 41.1 s（≈150 ms/次）** | 主导项，已移出栈临界区 |
| **冷读（磁盘载入投影）** | **0 / 0 / 0 次** | 预热后读写都不再整文件载入 |
| 读者 | 6.5 万次读，均值 **5.7 – 13.2 µs** | 原子装载 + 小切片复制 |

mutexprofile（同一测试）：20.39 s 锁延迟中 **19.77 s（90.4%）在
`structuralEventCommit` 的 `eventMu`**，`jsonStackJournal.load`（栈自己的读）
只占 0.43 s。

**判定**：栈通道自身既无数据竞争（`-race` 全绿）也不再是锁瓶颈 —— 残余竞争
是 **EVENT 通道写者之间排队**，根因即 H4（去重全量重读）。三条对策中
「拆锁粒度 + actor 无锁化」已把栈侧榨干；继续降栈锁无收益。EVENT 侧的两种
做法各有代价，**按指令暂停上报等用户判断**：
(a) 修 EVENT 去重（把全量重读换成 head 内指纹索引/布隆式增量）；
(b) 栈→EVENT 改异步有序落盘（§2.0 规则 4 允许 EVENT 短窗口落后，崩溃时按
    `interrupted` 补），代价是「提交返回时 EVENT 未必已落盘」。

红灯验收测试：`sessionstore/lock_hotspot_red_test.go`（`-tags redprobe`；H1 实例随 D9 删 `history.json` 消除，见 §7 S11）。

**S10 修正（回答上面的「暂停上报」）**：用户口径 = 「EVENT 跟 message 一样走
append-only」→ 采用 (a) 的后继形态（不做指纹索引，直接取消回看历史）。但同轮
A/B 与规模探针（H4 行）证明：**当初把 150 次提交的 EVENT 域归因给「去重全量重读」
是错的**——去重重读只在历史 ≥1000 行时才显形（旧 ~41 → ~69 ms/次），而 40 ms 的
地板来自「fsync 数据 + rename 发布 head」这一持久化本身，与 message 提交基线同源。
新加的探针：`head_publish_probe_test.go`（H7 持柄归因，`-tags redprobe && windows`）。
剩余待决不再是「EVENT 怎么改」，而是「发布形态（每提交一次 fsync+rename）要不要
在 message 读写专项里一并换成能摊薄持久化开销的形态」。

## 6. 验证记录（每阶段回填）

```text
# 基线（2026-09-09 本轮开工前）
go test ./sessionstore -tags redprobe -run 'TestRedCommit...'        # 2/2 红（H1/H2 复现）
go env CGO_ENABLED                                                   # 1（Windows race 可用）
```

| 阶段 | race 命令 | pprof 命令 | 结果 |
|---|---|---|---|
| S1 栈通道 | `go test -race ./sessionstore -run 'TestStackChannel\|TestSessionContextStore\|TestFork\|TestMessageRowsIndependent' -count=1` | `go test . -run 'TestStorageConcurrentSessionLockProfile' -mutexprofile` | **race 真实执行**（CGO_ENABLED=1，go1.25.8）→ ok；全量 `go test ./sessionstore` ok；`go test ./application/core ./application/core/session_runtime ./application/core/goal` ok；`go build ./...` 与 `go build -tags "gui,desktop,production" ./...` ok；`gofmt -l` 对本次改动文件干净 |

S1 的 mutex 观察（同机、同场景）：

| 场景 | 本次 | 既有记录 |
|---|---:|---:|
| `TestStorageConcurrentSessionLockProfile` 单跑 | 8.78 s | — |
| 该测试 + 冷热切换合跑 | 12.66 s（合跑）/ 8.78 s（单跑） | 9.55 s（合跑） |
| `TestSessionSwitchHotColdProfile` 单跑锁延迟 | 9.6 ms（2 次跑，墙钟 2.5 s） | 1–4 s 量级（actor/channel 空转） |

同一测试在合跑与单跑之间就差 3.7 s（本机磁盘/调度抖动），而冷热切换自身锁延迟只有毫秒级，**因此无法把「12.66 s vs 基线 9.55 s」归因于 S1 改动，也不能宣称改善**；定论需 `git stash`/worktree 做同轮 A/B 复测，记为下一步。

| 阶段 | race 命令 | pprof 命令 | 结果 |
|---|---|---|---|
| S5 栈通道多后端 | `go test ./sessionstore -count=1`（全量） | — | 全量 ok（80.9 s）；`go build ./...` 与 `go build -tags "gui,desktop,production" ./...` ok；`go test ./application/... ./internal/...` 全绿；11 条语义用例 json+sqlite 双跑通过 |
| S6 锁归因 | `go test ./sessionstore -race -run 'TestStack\|TestSessionContext\|TestFork\|TestMessageRows\|TestGoal' -count=1` | `go test ./sessionstore -run TestStackChannelLockAttribution -mutexprofile=stack.mutexpb`；`go test . -run TestStorageConcurrentSessionLockProfile -mutexprofile=lock.mutexpb` | **race 真实执行且无竞争报告** → ok（72.7 s）。归因见 §5；仓库级锁画像 12.87 s 延迟仍在 message 路径（`SaveCommitWorkspace` 66% / `AssembleWireWorkspace` 4.2%），与 S5/S6 无关，仍属 H2/H3/H4 专项。本轮 lock profile 测试单跑 7.14 s（历史：8.78 s 单跑 / 9.55 s 合跑 / 12.66 s 合跑），抖动量级大于差值，不作改善声明 |
| S8 lifecycle 数据权威 + §11 覆盖链 | `go test -race ./sessionstore -run 'TestLifecycle\|TestForkStore\|TestStorageSettings' -count=1` → ok（5.8 s，无竞争报告） | `go test . -run TestStorageConcurrentSessionLockProfile -mutexprofile=...` | 全量 `go test ./sessionstore -count=1` ok（79.9 s）；`go build ./...` 与 `go build -tags "gui,desktop,production" ./...` ok；`go test ./seelexctx/... ./application/... ./internal/...` 全绿；`gofmt -l sessionstore seelexctx main.go config` 空。锁画像：总延迟 12.89 s，其中 `SaveCommitWorkspace` 8.36 s（64.9%）——**lifecycle / stack / guide 域完全未出现在画像里**，剩余竞争全部在 message 通道（H2/H3/H4）。与 S6 记录的 12.87 s / 66% 同量级，按本机抖动特性不作改善或劣化声明。**同批 `-race` 含栈归因用例时未全绿**：`-run 'TestLifecycle\|TestForkStore\|TestStorageSettings\|TestStack'` 首跑失败，复测 4 次失败 1 次（栈 head `rename` Access denied）→ 记为 H7，非本轮 lifecycle/§11 改动引入（`stack_journal_json.go` 自 S5 提交后未改动） |
| S9 §9 数据根独占锁 | — | — | 已接入并验证：锁在 `newJSONRepositoryWithLayout`/`jsonRepository.Close` 生命周期内取得/归还；三份契约测试全绿；全量 `go test ./sessionstore -count=1` ok（49.9 s）。race 复跑见 S14/S23/S9 合记行 |
| S10 EVENT append-only + 发布 rename 退避 | `go test -race ./sessionstore -run 'TestStructuralEvent\|TestStack\|TestWriteAtomic\|TestLifecycle\|TestFork' -count=1` → **ok 58.4 s，无竞争报告** | 规模探针（临时文件，测完删除；`-tags redprobe`）：历史行数 0/50/1000/3000 各 30 次提交取 mean，新旧实现同轮交替；持柄探针保留在仓库 `head_publish_probe_test.go`（`-tags redprobe && windows`，share=0 独占打开） | 全量 `go test ./sessionstore -count=1` **连跑 3 轮全绿**（78.8 / 103.9 / 117.2 s；H7 修复前为约 1/4 轮失败）；`go build ./...` 与 `go build -tags "gui,desktop,production" ./...` ok；`gofmt -l`/`go vet` 干净。**同轮 A/B 结论**：150 次提交的 EVENT 域无可测差异（抖动 ≫ 差值），规模扫描才显形 → H4 重新定性为 O(history) 项（见 §5 H4/S10 修正）；H7 实测持柄 0–18 ms 松开 → 落在 `writeAtomic` 的 rename 退避（≈77 ms 预算）+ 两个契约测试 |
| S11 history.json 退役 | `go test -race ./sessionstore -count=1` → **ok 70.9 s，无竞争报告** | — | 全量 `go test ./sessionstore -count=1` ok（60.6 s）；`go test ./session/... ./seelexctx/... ./internal/... ./application/... ./seelebridge/... ./workspace/... ./plugin/... ./skill/... ./mcpstack/...` 全绿（受影响的旧「整段替换」用例改写为 v8 事件行语义）；`go build ./...` 与 `go build -tags "gui,desktop,production" ./...` ok；`go vet ./sessionstore` 干净。行为变更：JSON v8 布局不再产生 `history.json`，`Read/ReadRange` 由事件行派生，`ProviderHistory` 不再落盘 |
| S14/S23/S9 guide 去登记 + open_batches 删除 + 数据根锁接入 | `go test -race ./sessionstore -run 'TestGuideNotRewrittenAfterModuleHeadWrites\|TestJSONDataRootLock\|TestStackChannel\|TestForkCopiesDoNotShareParentGeneration' -count=1` → **ok**（race 真实执行） | — | 全量 `go test ./sessionstore -count=1` ok（50.0 s）；三份数据根锁契约测试全绿；T-DP-04 断言 guide 在模块首写后字节不变。附带修复：`ListToolResults` 删除「head 缺失回退目录扫描」分支——首次提交「文件已写、refs head 未发布」过渡窗口会把一次 commit 的半对 ref 暴露成撕裂快照（T-FK torn，`TestForkCopiesDoNotShareParentGeneration` 由偶发失败转稳定绿） |
| S15 head 按数据重建 | — | — | 全量 `go test ./sessionstore -count=1` ok（约 50–67 s）；T-M1-09 三份重建测试 + 栈/事件回归绿；`go build ./...`/`go vet ./sessionstore`/`gofmt -l` 干净。说明：retention/subagent/media 无独立数据文件，重建保留原错误（不判损坏也不伪造） |
| S12 旧布局读分支退役 | `go test -race ./sessionstore -run 'TestJSONRepositoryReadRange\|TestEventRange\|TestRouter\|TestState' -count=1` → **ok 5.4 s，无竞争报告** | — | 全量 `go test ./sessionstore -count=1` ok（65.2 s）；`go vet ./sessionstore`/`gofmt -l` 干净。Read/ReadRange/ReadEventRange/ReadEventTail/ReadToolResult/ListToolResults/CurrentGeneration 对非 v8 目录一律返回 `fs.ErrNotExist`；WriteState/ReadState/Conversation 只读根 `state.json`；List 跳过只含 manifest 的旧目录；`readAll`/`readEventShards`/`readManifest(directory)`/`readCurrentStateLocked`/transcript.log 工具与 `jsonManifest` 类型删除。保留：独立 legacy_storage API（`legacy_storage.go`，非布局读路径）与 framework-events v1 generation 扫描（独立事实轨迁移） |
| S21 通道语义测试面 | — | — | 新增 T-STK-13（`TestStackChannelActiveWholeReplacement`：active.jsonl 整份替换、无旧快照残留）与 T-STK-14（`TestQueueFileWholeReplacement`：queue 出队整份替换、无墓碑）；T-M1-04 注释限定为追加型。全量 `go test ./sessionstore -count=1` ok（71.8 s） |
| S17 栈重放幂等（JSON/SQL/Redis 同步） | `go test -race ./sessionstore -run 'TestStackChannel\|TestQueueFile' -count=1` → **ok 19.8 s，无竞争报告** | — | 全量 `go test ./sessionstore -count=1` ok（55.2 s）。写侧：`stackMutationCommitID`（kind+迁移类型+batch/item/status 确定性推导）写入 head watermark `last_commit_id`，EVENT 迁移同凭据；读侧三后端按 item_id 高 revision 去重；T-STK-10/11 新增并通过。说明：message 通道存储层空凭据回退与 lifecycle/blob 生产方凭据审计随 S13/S19 一并清点 |
| S13 toolresult 归一进 big_tool_result | `go test -race ./sessionstore -run 'TestToolResultLivesInBigToolResultChannel\|TestForkCopiesDoNotShareParentGeneration\|TestListToolResults\|TestBigToolResult' -count=1` → **ok 2.6 s，无竞争报告** | — | 全量 `go test ./sessionstore -count=1` ok（58.1 s）；依赖面 `application/core/session_runtime`/`internal/adapters`/`seelebridge/session` 全绿。写序 = 结果文件 → refs 索引（metadata-index）→ 事件行；`moduleToolResult`/`metadata/toolresult.json`/`tool-results/` 不再产生；用户口径记录于 §7 S13 |
| S18 第四栈 subagent（存储侧） | `go test -race ./sessionstore -run 'TestStackSubagentBatchSemantics\|TestForkStoreSubagent\|TestStackChannel' -count=1` → **ok 27.8 s，无竞争报告** | — | 全量 `go test ./sessionstore -count=1` ok（58.0 s）。`StackKindSubagent` 走栈通道；批量注册/整批归档/水位 head 断言通过（T-STK-12）；`stackVerify` 覆盖四栈；fork 循环保持 plan/task/goal |
| S17 余量：message/EVENT/lifecycle/compact 凭据去随机化 | `go test -race ./sessionstore -run 'TestLifecycle\|TestMessageRows\|TestStructuralEvent\|TestCompact\|TestStackChannelReplay' -count=1` → **ok 10.1 s，无竞争报告** | — | 全量 `go test ./sessionstore -count=1` ok（55.2 s）。message/EVENT 空凭据回退 → 按行内容确定性推导（`messageRowsCommitID`/`structuralEventsCommitID`），message head 补 `last_commit_id`；通用模块 head/retention init/LRU 用 `modulePayloadCommitID`；lifecycle head 凭据按状态内容推导（调用侧随机号不再参与发布）；compact FrameID/CommitID 确定性；栈 head 信封 commit_id = watermark `last_commit_id`；queue 条目键 = requestID 派生（同 requestID 重放幂等）。说明：lifecycle 调用点遗留的 `"lc-"+randomID()` 参数已不被发布使用（签名忽略）；batchID 空时自动批号与 SQL/Redis generation 命名不在 commit 凭据语义内 |
| S24 checkpoint 快照落点（S20 前置） | `go test -race ./sessionstore -run 'TestCheckpointStore\|TestGuideNotRewrittenAfterModuleHeadWrites\|TestHeadRepair' -count=1` → **ok 1.9 s，无竞争报告** | — | 全量 `go test ./sessionstore -count=1` ok（68.8 s）。`moduleCheckpoint`（metadata/checkpoint.json）+ `moduleSystem`（metadata/system.json）加入白名单与锁；`checkpoint_store` 迁至 checkpoint 内容模块（JSON），SQL/Redis 沿 state 通道（v8 化搁置）。T-CK-01 断言无 state.json |
| S19 SystemPrompt → metadata/system.json（消费面首片） | `go test ./sessionstore -count=1` → **ok 62.5 s**；依赖面 `application/core`/`application/core/goal`/`application/core/session_runtime`/`seelebridge/session` 全绿 | — | `Router.SaveSystemPromptWorkspace/LoadSystemPromptWorkspace` 走 `moduleSystem` 内容模块；`SessionContextStore.Persist` 落 system.json 并从 blob 剥离 `system_prompt`（tag omitempty），`Load` 从 system.json 回填；非 JSON 后端 handled=false 时保留旧 blob 行为（v8 化搁置）。回归：`TestSessionContextStoreSystemPromptInvariant` 断言 system.json 存在且 blob 无 `system_prompt`；`TestCheckpointStoreRoundTrip` 同类断言 |
| S19 CompactStack 停双写 | `go test -race ./sessionstore -run 'TestSessionContext\|TestContextState\|TestCompact\|TestPushCompact' -count=1` → **ok 4.1 s，无竞争报告** | — | 全量 `go test ./sessionstore -count=1` ok（66.0 s）；全仓 `seelexctx/... application/... seelebridge/...` 全绿。`Router.CompactFramesWorkspace` 从 compact 通道回读；`PushCompact` 单源先桥接后持久；v8 JSON blob 剥离 `compact_stack`；`TestSessionContextStorePersistsToIsolatedChannel`（JSON 走通道、SQLite 走 blob）与 compact 帧契约测试按 §2.2 退化口径更新 |
| S19 收尾：GoalAudit→EVENT / SkillStack 停 blob / wire_material 置位 | `go test -race ./sessionstore -run 'TestSessionContext\|TestGoalAudit\|TestContextState\|TestCompact' -count=1` → **ok 3.4 s，无竞争报告** | — | 全量 `go test ./sessionstore -count=1` ok（50.2 s）；`go test ./application/... ./seelebridge/...` 全绿（64.3 s）；`TestTranscriptEventWireMaterialRoundTrip` 通过。实现：`AppendGoalAuditEvent/GoalAuditEvents`（EVENT goal.*，Seq 按事件序派生，blob 剥离）；SkillStack 在 v8 仅内存（`Persist` 剥离、`Load` 不再回读，正文随普通 message 行）；`model.TranscriptEvent.WireMaterial` + 适配往返 + 生产端 internal 置位。`update()` 置 `loaded=true` 修复“内存态被已剥离 blob 覆盖”缺陷 |
| S20 state/record 通道退役 | `go test ./sessionstore -count=1 -timeout=400s` → **ok 42.7 s**；`go test -race ./sessionstore -run 'TestRetiredChannelsAbsentAfterFullCommit\|TestStateChannelRetiredOnJSON\|TestConversationRangeDerived\|TestSessionGranularityPersistence\|TestCheckpointStore' -count=1` → **ok 5.7 s，无竞争报告** | — | 全仓（除仓库根 repro 诊断）：`go test ./application/... ./internal/... ./seelebridge/... ./seelexctx/... ./session/... ./workspace/... ./plugin/... ./skill/... ./mcpstack/...` 全绿（75.1 s）；`go build ./...` 与 gui tags 构建 ok。实现：state 停写停读；`DerivedRecordWorkspace`/`derivedRecordPayload` 派生 record+conversation（tool/tool_result 两段形状与运行期一致）；`lifecycle.archived_at` 归档判据；record 直读客户端改派生；`removeAllWithBackoff` 修 Windows 并发删除瞬时句柄。**基线对照**：仓库根 `repro_*` 诊断用例（`TestTwoRunningViewThirdThenSwitchedFinishes*`、`TestBackgroundCompletionWhileSwitchingToC`）在 HEAD worktree 同样失败（预存在红灯，非本轮引入；其中 record/hot-attach 顺序类已在本轮转为通过） |
| S20 lifecycle `archived_at` | 同上 | — | `lifecycleHead.ArchivedAt` + `setLifecycleArchived/lifecycleArchivedAt` + Router `SetSessionArchivedWorkspace/SessionArchivedWorkspace`；`SaveRecordRaw` 的 archived 状态改写走该标记，目录过滤不再看 EVENT |
| S16 JSON 栈 head 按 kind 拆分 | `go test -race ./sessionstore -run 'TestStackChannel\|TestGuideNotRewrittenAfterModuleHeadWrites' -count=1` → **ok 20.3 s，无竞争报告** | — | 全量 `go test ./sessionstore -count=1` ok（59.9 s）；`go vet ./sessionstore`/`gofmt -l` 干净。JSON：三份 head 文件 + 三把锁；`TestStackChannelWritesDesignedFiles` 断言旧 `metadata/stack.json` 不存在。SQL/Redis head 拆分待 §4 v8 化范围决策 |

## 7. v8.3 待办工作项（新会话接手清单，按依赖排序）

> 每项做完回填 §6 验证记录（race + pprof + 全量测试），并勾掉本表；验收用例编号见 my_design 附录 D。

| 阶段 | 待办 | 设计稿条款 | 主要落点 | 验收 |
|---|---|---|---|---|
| **S11** | 退役 `history.json` provider 缓存：删写入 + 读侧不再"缓存优先"，全量读由 message 行派生 | §2.3、附录 B、D9 | `json_layout.go`（`historyCacheFile`/`readAllLayoutMessages`/`writeCommitLayout`）；`RouterStorage.Append` 改为事件行追加；`DurableHistory.Save` 在 v8 JSON 只编排 state blob | T-DP-02（全链路门禁待 S13/S19/S20 齐后复跑） |
| **S12** | 退役旧布局读写：`manifest.json`+`generation-*/`+`transcript.log` 全部分支；枚举遇旧目录只跳过并告警 | 附录 B、D2 | `sessionstore.go`（`readAll`/`readEventShards`/`readCurrentStateLocked`/`List`）、`event_range.go`、`json_layout.go`、`conversation.go` | T-DP-02（全链路门禁待 S13/S19/S20 齐后复跑） |
| **S13** | blob 归一：`tool-results/` + `metadata/toolresult.json` 并入 `big_tool_result`；删 `moduleToolResult` | §2.0 规则 6、§2.6、D9 | 用户口径（2026-09-10）：本质是走 big_tool_result 链路、退役旧链路；超过软限进长结果存储（result_ref）、超过硬限返回“结果过大”。落点：`json_layout.go`（写序 = 结果文件 → refs 索引 → 事件行）、`sessionstore.go`（`toolResultPath` → big_tool_result/*.result.json）、`module_heads.go`（`moduleToolResult` 删除）；refs 索引落 `metadata-index/toolresult.json`（可重建派生索引） | T-DP-01（`TestToolResultLivesInBigToolResultChannel`）、T-BL-02 |
| **S14** | guide 去登记：删 `module_index` + `registerModule` 调用点（含 lifecycle 写序里那一步） | §2.0 规则 3、D9 | `module_heads.go:57/266`、`lifecycle.go`、各 `registerModule` 调用点 | T-DP-04（`TestGuideNotRewrittenAfterModuleHeadWrites`） |
| **S15** | 自愈补全：第二次校验仍不符 → 按数据文件重建该模块 head（通用化 `lc-repair-*`） | §2.0 规则 3、§4「guide 读」 | `module_heads.go:373-376`（`repairModuleHeadFromData` + message/event/compact/stack 重建） | T-M1-09（`TestHeadRepair*`） |
| **S16** | 栈 head 按 kind 拆三份 + 各一把锁；`active_count`/`history_count`/`open_batches` 降级为 verify 校验和 | §2.0 规则 1/2、§2.4、D6 | `module_heads.go`（`moduleStackPlan/Task/Goal`、`moduleForStackKind`）、`stack_journal_json.go`（`lock(key, kind)`/per-kind publish/read）、`stack_channel.go`、`stack_journal.go`（接口 `lock` 带 kind） | T-STK-09、T-STK-02（`TestStackChannelWritesDesignedFiles` 断言旧 `stack.json` 不存在）。SQL/Redis head 拆分随 §4「SQLite/PostgreSQL/Redis 的 v8 化」待决一并落 |
| **S17** | 重放幂等（**全通道，不只栈**）：写侧逐操作唯一 `commit_id`（**随机与常量都禁止**）+ head 记 `last_commit_id`；读侧条目键去重、高 `revision` 胜出 | §2.0 规则 4、§2.4、D10/D13 | 凭据站点：`stack_journal_json.go:183/297`（随机已替换为 `stackMutationCommitID`）、`fork_store.go` 各站点（常量已修、逐操作唯一）；读侧去重 JSON/SQL/Redis 三后端同步（`dedupeStackRowsByItemID`） | T-STK-10（`TestStackChannelReplayUsesDeterministicCommitID`）、T-STK-11（`TestStackChannelColdReloadDeduplicatesByItemID`）、**T-EV-06**、T-EV-07 |
| **S17b** | 公开读接口不得擦除幂等凭据与 `wire_material` | §5.1、D13 | `json_layout.go:436-445` `stripRowFields` 在返回前清掉 `commit_id`/`in_out_json`/`wire_material` → 既让补了置位方也白置，又让「读出来改改再提交回去」的回路必然丢凭据 | T-EV-07 |
| **S18** | 第四栈 `subagent`（批次语义）+ 条目元数据；`fork session` 不入栈 | §2.4、§8.2、D6 | `stack_channel.go`（`StackKindSubagent`/valid/index）、`fork_store.go`（`registerSubagent`/`updateSubagentStatus`/`readSubagents` 走通道）、`module_heads.go`（views[4]、`moduleSubagent` 作栈 head） | T-STK-12（`TestStackSubagentBatchSemantics`）。fork 循环保持 plan/task/goal（fork session 不入栈）；subagent 协调器上层消费另记待办 |
| **S19** | `context.json` 退役（JSON 侧）：`SystemPrompt`→`metadata/system.json`；`CompactStack` 停双写（compact 通道单源）；`GoalAudit`→EVENT `goal.*`；`SkillStack` 停 blob（v8 内存态，正文随普通 message 行）；`wire_material` 生产置位 | §2.1、§2.2、§2.7、§5.1、D1/D8/D9 | `session_context.go`、`json_layout.go`（system/compact/goal-event 通道）、`application/model/context.go`、`internal/adapters`、`task_context_state.go` | SystemPrompt/Compact/GoalAudit 回归见 §6；`TestTranscriptEventWireMaterialRoundTrip`；SQL/Redis 未 v8 化仍走 blob（搁置） |
| **S20** | `state.json` 退役（JSON 侧）：`Commit.State` 通道停读写；会话枚举/record 只依赖 `message head.Meta` + message 行 + lifecycle 派生；`archived_at` 落 lifecycle；conversation 由事件行派生；展示元数据迁 `project-*/session-meta.json` | §3.2、§2.5.4、D7/D8/D9 | `json_layout.go`/`sessionstore.go`（Write/ReadState、DerivedRecordWorkspace）、`session_granular.go`、`session_meta.go`、`conversation.go`、`lifecycle.go`、`internal/adapters/session_workspace_ports.go` | T-DP-02（`TestRetiredChannelsAbsentAfterFullCommit`）已绿；Checkpoint 结构事实→EVENT 的生产方仍列 §4「EVENT 运行期写入」待办 |
| **S21** | ~~写形态改造~~ **取消**：按 D12 实现不动。要做的是把测试面补上——整份替换型语义（T-STK-13/14）、`T-M1-04` 的适用声明限定为追加型 | §2.0 通道类型表、D12 | 只改测试与稿 | T-STK-13（`TestStackChannelActiveWholeReplacement`）、T-STK-14（`TestQueueFileWholeReplacement`）；T-M1-04 注释限定追加型 |
| **S22** | 分片索引边界：**无需改实现**，head 内的通道分片路由已合规；后续若引入需重扫的全量索引，落 `metadata-index/` | §2.0 规则 5 | 无 | T-M1-07（`TestMessageRowsShardRolloverAt101Rows`）、T-EV-05（`TestStructuralEventCommitNeverReadsHistoryShards`）均在库且全绿 |
| **S23** | `open_batches` 删字段（零读者）+ 压缩/事件写序按 I5 校正为 message → compact → event | §2.0、I5、§2.2 | `stack_journal.go:133/421`、压缩路径、`structural_events.go` | `open_batches` 已删；写序由 T-EV-01/T-EV-03 保持（message → compact head；compacted EVENT 生产方仍属 EVENT 运行期写入待办，见 §4 该行） |
| **S24** | workplan 续跑快照落点（S20 前置）：判据 = 不能从 message+EVENT 完全重算 → 会话侧 `metadata/checkpoint.json`（整份替换型，过准入门） | §2.5.5、D9 | `checkpoint_store.go:46/66`（state 通道 → `Router.SaveCheckpoint/LoadCheckpointWorkspace`）、`json_layout.go`（`moduleCheckpoint`/`checkpointContent`） | T-CK-01（`TestCheckpointStoreRoundTrip` 扩展：checkpoint.json 存在、state.json 不存在） |

**仍未闭合的待决项（实施前需用户点）**：

| 项 | 状态 |
|---|---|
| §9 只读进程能否共享打开同一数据根 | `[?]` 未表态；且**栈/事件读侧的无锁内存快照以 I10 单进程写者为前提**，不点这条，快照正确性无根 |
| blob 软限/硬限/配额的度量单位（soft 用字符、hard 与配额用字节） | `[?]` §2.6 未统一，跨后端 GC 算不平 |
| R1 契约缺失（分页游标在 seq 空洞下的语义、LRU 摘要占位取哪一帧、internal/context 行的展示规则） | `[ ]` 稿子只给了 R2 契约（§5） |
| EVENT / compact 分片是否共用 `shard_rows` 还是各自成文（§11 现列两键） | `[?]` 代码现状是 `store.settings.shardRows()` 四通道共用 |
| budget 口径（200k 是 token 还是字符；软 75% 与目标 60% 的先后） | `[ ]` 见 §3 表；R2-BUDGET-1 前必须定 |
| SQLite/PostgreSQL/Redis 的 v8 化范围 | `[~]` 搁置（2026-09-10 用户口径：随 message 读写热点专项一并优化） |
