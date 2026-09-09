# v8 会话存储设计稿符合度打点表（2026-09-09）

> 性质：一次性工作包打点表（可勾选台账）。权威口径 = [my_design.md](./my_design.md)（v8.2）
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

## 0. 用户口径决策（本轮新增，优先于早期表述）

| 编号 | 决策 | 出处 |
|---|---|---|
| D1 | skill 不是栈文件：**skill 跟随 message 走**，作为「像会话记录一样的提示词工程」内容 | 本轮问答 2026-09-09 |
| D2 | 旧 manifest 布局会话**彻底退役，不再打开**（链路上不留只读回退） | 本轮问答 2026-09-09 |
| D3 | 栈按设计稿分文件放置，消除 context 单文件写竞争 | 本轮指令 2026-09-09 |
| D4 | **栈存储必须兼容其它存储格式**：JSON / SQLite / PostgreSQL / Redis 都要有栈通道，不允许「只有 JSON 有栈」 | 本轮指令 2026-09-09 |
| D5 | 栈锁延迟必须**分域归因**（active.jsonl vs history.jsonl），并判定是数据竞争还是锁竞争；先拆锁粒度 + actor 闭包，不奏效则暂停上报 | 本轮指令 2026-09-09 |

## 1. §2.0 metadata 模块化（head + guide 读索引）

| 项 | 状态 | 证据 / 说明 |
|---|---|---|
| guide.json 只做读索引/路由 | `[x]` | `module_heads.go` `layoutGuide`/`registerModule`；I9 由 `T-M1-01` 覆盖 |
| 模块 head 各 9 件（message/compact/event/stack/lifecycle/retention/subagent/media） | `[ ]` | 现状缺 stack 数据通道与 media（预留）；**多出白名单外的 `metadata/toolresult.json`**（`moduleToolResult`，见 §6 归属） |
| guide.module_index 内容 | `[x]` | `registerModule(key, mod)` 现只记相对路径 `metadata/<module>.json`（原实现把本机绝对路径写进 guide.json，违反 §3.2 与 AGENTS.md §4） |
| 写锁按模块、模块间互不阻塞 | `[~]` | 三栈已脱离仓库级锁（走「会话 × stack 模块」锁）；**context blob 剩余字段（system_prompt/skill/compact/goal 审计）仍经 `context_state.go:22` 的仓库级 `repository.mu` → 由 S2/S4 收口** |
| reader 校验 schema/checksum + 自愈重读 | `[x]` | `readModuleHeadFile` 双读 + `readSelfHealHook`；T-M1-06 |
| 可重建派生物放 metadata-index/ | `[ ]` | 检索索引已在 `metadata-index/search.json`（`search_index.go:36`）；**blob/toolresult 清单未放** |

## 2. §2.4 / §3.2 plan / task / goal 栈（S1 已完成）

| 项 | 状态 | 证据 / 说明 |
|---|---|---|
| `session/{plan,task,goal}/{active,history}.jsonl` | `[x]` | `sessionstore/stack_channel.go`（`stackDir`/`stackActivePath`/`stackHistoryPath`）；`T-STK-01` 断言显式路径与行数 |
| head 在 `metadata/stack.json`（只存水位/head_seq） | `[x]` | `stackModuleHead`/`stackWatermark` 只有 head_seq/active_count/history_count/open_batches；`T-STK-02` 断言 head 内不出现 item_id/条目内容 |
| 批次语义：未完成整批留 active、全完成整批归档 | `[x]` | `stackState.batchDone`/`closeBatch`；`T-STK-03`（部分完成整批留栈）/`T-STK-04`（整批弹栈）|
| history 锚：item_message_id / batch_message_from / batch_message_to | `[x]` | `messageAnchor` 压栈时盖章；`T-STK-05` 断言逐条各自坐标、同批共享 batch_from；`BatchMessageToSeq` 供 fork 过滤 |
| 状态迁移由 EVENT goal.*/task.*/plan.* 记录 | `[x]` | `recordStackEvents`（数据→head→EVENT 写序）；`T-STK-06` 断言 `goal.active`/`goal.completed` 与锚点 |
| head 未发布不可见 / 崩溃残尾 | `[x]` | 条目带 `Revision`，读侧按 head 过滤 + `truncateCrashTail`；`T-STK-07`/`T-STK-08` |
| context blob 不再承载三栈 | `[x]` | `SessionContextStore.Persist` 落盘前剥离 Plan/Task/Goal；`Load` 装载后按通道回读；schema 升 **v3**，v2 及更早显式拒绝（`TestGoalStackLegacySchemaRejected`） |
| 运行期入口 | `[x]` | `Router.StackPush/StackSetStatus/StackPopTop/StackReplace/StackActive/StackHistory/StackVerify/StackStorageStats/ForkStacks`；`ErrChannelUnsupported` 已删除（不再有「某后端没有栈」的分支） |
| **栈通道后端无关（D4）** | `[x]` | `stackJournal` 契约（`stack_journal.go`）+ 三份实现：JSON（`stack_journal_json.go`）、SQL（`stack_journal_sql.go`：`seelex_session_stack_item`/`_stack_head`/`seelex_session_structural_event`，事务=发布点）、Redis（`stack_journal_redis.go`：`<session>:stack:*` 列表 + head + `MULTI/EXEC`）；`Repository.stackJournal()` 是**接口方法且不可包外实现** → 新后端必须显式给栈通道 |
| SQL/Redis 锚的粒度缺口 | `[ ]` | 两后端消息通道仍是整块 shard 快照（无 v8 事件行键）→ `item_message_id` 为空、只有 `seq`；fork 过滤按 seq 不受影响，但「v8 事件行落到 SQL/Redis」仍是 §4 待办 |
| 通道语义跨后端一致 | `[x]` | `forEachStackBackend` 把 11 条语义用例在 json + sqlite 各跑一遍（head 只装水位 / 整批弹栈 / 锚 / 迁移 EVENT / LIFO / replace 派生 / 并发单写者 / fork 按锚 / 跨实例持久化） |
| fork 子会话栈 | `[x]` | 存储层 `stackSnapshotAt`+`stackRestoreSnapshot`（起点 < watermark 报 `ErrForkBeforeWatermark`）；app 层经 `SaveSessionSnapshotWorkspace` 调 `Router.ForkStacks`；`T-FK-02` 改为按真实 message 锚断言 |
| 现状替代实现 | `[x]` 已替换 | 原 `SessionContextRecord` 三栈 + `context_state.go:22` 仓库级独占锁路径不再被三栈使用；blob 仍承担 system_prompt/skill 记录/compact 栈/goal 审计（S2、S4 收口） |
| 锁延迟归因（D5） | `[x]` | 见 §5「S5/S6 归因结果」：写路径不再解析 history（`history_read=0`）、读者走 `atomic.Pointer` 快照（`cold_loads=0`，均值 5.96 µs/次）、EVENT 提交移出栈临界区、guide 锁由仓库级降为会话级 |
| 行为变更（需知悉） | `[~]` | 关闭的 plan/task 帧不再原地留在 active（§2.4 整批弹栈归档）；「now using = 栈顶」因此只看到未收口帧 |


## 3. §1 事实模型 / §2.2 compact / §2.3 message

| 项 | 状态 | 证据 / 说明 |
|---|---|---|
| message = 唯一正文事实源、append-only | `[x]` | `message_rows.go`（分片/续号/幂等/残尾）+ T-M1-01..08 |
| compact = `session/compact.jsonl` + head | `[x]` | `compact_frames.go`；head 冗余 LatestFrame 供 R2 单读 |
| compact 双写（context.json CompactStack 与 compact.jsonl 并存） | `[ ]` | `PushCompact` 既写 blob 又桥接 compact 通道（`session_context.go:608`）= 兼容式双写，需收口为单源 |
| R2 只装配栈顶帧 Chapter2、锚点章节不发模型 | `[ ]` | `assembleWireRows` 直接投 `frame.Summary` 全文；运行期渲染侧已有 `FrameChapter2`（`seelexctx/assembler.go:201`） |
| `history.json` provider 缓存 | `[ ]` | 非白名单产物且**读侧缓存优先于权威**（`readAllLayoutMessages` 先读缓存）；D1/D3 口径下删除，全量读由 message 行派生 |
| `state.json` 每轮整文件重写 | `[ ]` | `writeCommitLayout` 直接 `writeAtomic(state.json)`；设计稿 SESSION 实体无该文件 |
| §5.3 步骤 2「tail 从 frame.message_to 下一行」 | `[x]` | `tailStart = frame.MessageToSeq + 1`（`wire_assembler.go:89`），T-R2-02 |
| §5.2 budget 默认 200k / 软 75% / 目标 60% | `[ ]` | 只有单一 `Budget`（默认 200_000），无软/目标比例 |

## 4. §2 生命周期、EVENT、blob、检索、§9、§11

| 项 | 状态 | 证据 / 说明 |
|---|---|---|
| draft/queue 权威在 `input/draft.json(l)` / `queue/queue.json(l)` | `[ ]` | 当前权威在 `metadata/lifecycle.json` payload（整队列内联），data 文件只是「非权威镜像」且 `queue.jsonl` 用非原子 `os.WriteFile`（`lifecycle.go:111`） |
| EVENT 运行期写入 | `[ ]` | 仅 fork 路径写 EVENT（`fork_store.go:130/188/203`）；compacted/interrupted/request·turn/token_usage/session_archived 无生产写入方 |
| R3 合成 interrupted 规则（T-R3-02/03） | `[ ]` | `history_resume_readers.go` 有 open-tail 探测，未接 EVENT 合成/不合成判据 |
| big_tool_result（软截断/硬报错/配额/GC 跨引用） | `[x]` | `big_tool_result.go`；T-BL-01..03；subagent 复用主会话 blob（I11）由 T-FK-06 覆盖 |
| 检索 = 单会话关键词模糊索引、可重建 | `[x]` | `search_index.go`；T-SR-01..03 |
| §9 单数据根独占锁（owner_process/陈旧锁/auto_recover） | `[ ]` | 仅配置字段存在（`storage_settings.go:22`），无实现 |
| §11 配置并入 seele.yaml limits 覆盖链 | `[ ]` | 默认值 + 校验已就位，覆盖链未接（`storage_settings.go:3` 注释自陈） |
| §2.1 PROJECT_CONTENT `project/{system,project}.jsonl` | `[ ]` | 全仓库无该文件（grep `system.jsonl` 无命中） |
| fork 拷贝携带 frame 摘要承接 watermark 前区间 | `[ ]` | `forkStore.forkSession` 只拷 message 行 + 栈条目，未拷 compact head/帧 |
| retention raw_bytes 精确度量 | `[ ]` | `rawBytesEstimate = TotalRows * 1024` 是粗估（`json_layout.go:273`），设计稿要求按原始字节告警 |
| M5 verify / reconcile | `[ ]` | 仅 `verifyMessage`；无跨通道 reconcile/巡检出口 |
| SQLite/PostgreSQL/Redis 的 v8 化 | `[?]` | 设计稿 §2/§3 只规定文件布局与 ER，未给 SQL schema；implementation-M1-M4 §5 把它列为遗留。是否本轮做需用户定 |
| §10 媒体/多模态 | `[~]` 不做 | 设计稿自标「后续规划」，media.json 为预留位 |

## 5. message 读写热点（本轮只记录，研究放到最后）

| 编号 | 事实（实测/静态） | 状态 |
|---|---|---|
| H1 | 读者持 `history.json` 句柄 → 提交 `rename` 失败（`Access is denied`），全链路零重试 | `[ ]` 由 S2 删除该文件消除该实例；发布形态问题归入 message 读写专项 |
| H2 | 一次全量历史读使同会话提交多等 91.7 ms（基线 63.7 ms，预算 30 ms）；读与写共用 `messageMu` | `[?]` **待专项**：先完善设计稿再实施（用户 2026-09-09 指令） |
| H3 | 提交基线 63.7 ms 中相当部分来自写侧 O(分片) 重复 IO：`appendRowsLocked` 每次整片重读 + `fileSHA256` 整片重算 | `[ ]` 属 message 读写专项，随 H2 一并入稿 |
| H4 | EVENT 幂等去重每次提交全量重读所有 event 分片（`readEventShardsLocked`） | `[ ]` 同上，随专项入稿。**S6 实测后升级为主热点**（见下） |
| H5 | 栈提交读 `metadata/message.json` 取锚 → 打开一个正被 rename 覆盖的文件（Windows 句柄撞发布，跨模块） | `[x]` 已消 | message head 发布时同步盖章内存锚（`rememberMessageAnchor`，`atomic.Pointer`），栈通道 `anchor()` 优先取内存锚 → 栈链路不再打开 message head 文件 |
| H6 | 任一会话写 guide 时持有**仓库级** `metaMu` → 所有会话的提交互相排队 | `[x]` 已消 | `metaMu` → 会话级 `sessionModuleLocks.guideMu`；S6 归因：guide 域 0.13 s / 150 次提交（此前参与跨会话串行） |

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

红灯验收测试：`sessionstore/lock_hotspot_red_test.go`（`-tags redprobe`，当前 2/2 红；H1 由 S2 删文件消除）。

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

