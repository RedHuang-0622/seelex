# 裁定与证据：内存 / CPU 随时间累积（重启归零）

本文件是 [`README.md`](README.md) 的取证部分。所有条目都标注证据等级：

- **静态确认**：有源码 file:line 或仓储级搜索断言支撑（下文的"无删除面"类结论即为搜索断言）。
- **假设**：尚无证据，需要运行时实测（见 §5）。

本工作包**未**修改任何数据、**未**跑内存曲线实测。

## 1. 现象口径

用户口径：使用过程中内存与 CPU **随时间累积**，**重启归零**。这个形态本身就是证据：
它们是**进程内**状态，且**没有上限、没有回收**，所以只能靠重启清空。

对照"哪些归零、哪些不归零"，可以直接定位到的面：

| 面 | 重启后 | 说明 |
|---|---|---|
| 会话落盘（`sessions-json/project-*/session-*`） | 不归零 | 磁盘事实源，与内存曲线无关 |
| 工作表格台账 / 定时任务（`.seelex/*.jsonl`） | 不归零 | 落盘事实；但**其内存投影**随会话增长 |
| 引擎会话本体（`Runtime.bundles`） | **归零** | 只增不删（§3 A1） |
| 内存遥测（`MemoryTracer` 全部容器） | **归零** | 只增不删（§3 B1） |
| 驻留 LRU / 内容 LRU 的使用序 | **归零** | 进程内切片（`residentOrder` / `loadedContentOrder`） |
| 会话单元（`Domain` unit 注册表） | **归零** | 只在 `Service.UnloadSession`/归档路径被移除（§3 A5） |
| 目录枚举缓存 | **归零** | 每次刷新都重新枚举（§3 C2） |

## 2. 裁定总表

| # | 面 | 机制 | 有上限？ | 是否随规模增长 | 重启归零 | 等级 |
|---|---|---|---|---|---|---|
| A1 | `seelebridge.Runtime.bundles` | 每个打开过的会话一个 `frameworkSession.Session` + `LoopHooks` + `sessionBindings`（含 ctxStore / 主历史）；**只增不删** | 否 | 是（∝ 进程内出现过的会话数） | 是 | 静态确认 |
| A2 | `EnginePort.UnloadSession` | 卸载只删 `engines` / `engineCalls` / `sessionMaxLoops` 三张表；**不触 `bundles`** | — | — | — | 静态确认 |
| A3 | 驻留 LRU 窗口口径 | `reconcileResidentLimit` 的计数把"当前视图会话"算进 `limit` ⇒ `resident_limit` 语义 ≠ 用户口径的"上一个" | 是（6） | 否 | 是 | 静态确认 |
| A4 | 驱逐后残格 | `evictResident` 保留 Unit/View/Runtime 槽（`Resident=false`）；内容层只清 `Conversation` | 是 | 是（∝ 单元数） | 是 | 静态确认 |
| A5 | 删除会话不卸载 | `DeleteSession` 不调 `UnloadSession`：引擎实例 / Unit / 标题 / 任务投影都留在内存 | 否 | 是 | 是 | 静态确认 |
| B1 | `telemetry.MemoryTracer` | `traces` / 每 span `events` / 全局 `audits` / `metrics` 全部 append-only，**无删除面**；同一事件存两份 | **否** | 是（∝ 进程内事件总数） | 是 | 静态确认 |
| B2 | `MemoryTracer.Query` | 遍历**所有** trace × span × event → 收集 → 全量稳定排序 → 才应用 limit | — | 是（O(进程事件数)） | 是 | 静态确认 |
| B3 | 遥测查询在热路径上 | `tokenCountFor` → `TokenCountFor` → `SessionTokenCount` → B2；由回合尾 / 工具边界 / 会话激活 / 交互收敛触发 | — | 是 | 是 | 静态确认 |
| B4 | `Runtime` per-会话/节点残格 | `teamworkCoords` / `liveHistory` / `routingNoticeSeen` / `roleTurns` / `images` **无删除面**（`servedRoutes` 是唯一读即清） | 否 | 是 | 是 | 静态确认 |
| B5 | 后台作业面 | `jobs.manager.prune` 超上限退休最旧终态作业并删托管输出文件 | **是** | 否 | — | 静态确认（反例，不需改） |
| B6 | `EnginePort.handoffLatest` | `UnloadSession` 没删这一格（`pendingHistory` 在交接路径删） | 否 | 是（小） | 是 | 静态确认 |
| C1 | `Bridge.Snapshot()` | 每次 `model.CloneSnapshot` 深拷贝整份快照：正文 + 每条 Tool + Plan 节点/边 + SubAgentTree + WorkTable + WorkTableBatches + TeamworkBoard | — | 是 | 是 | 静态确认 |
| C2 | 目录枚举 | 每次刷新 `os.ReadDir(projectDir)` + 逐会话目录读 `metadata/message.json` + `json.Unmarshal` + 排序 | — | 是（∝ 磁盘会话数） | 是（缓存不驻留） | 静态确认 |
| C3 | 内容 LRU 收敛 | 每次 touch 遍历**全部会话单元**（`UnitIDs`，actor 调用）并逐单元 `View.Read` | — | 是（∝ 单元数） | 是 | 静态确认 |
| C4 | LRU 使用序维护 | `markResidentRecentLocked` / `markContentRecentLocked` 每次整条切片重建（O(n) 常量，窗口调小即缓解） | — | 是 | 是 | 静态确认 |

## 3. 证据

### A. 会话级驻留：释放面不完整

**A1 `bundles` 只增不删**（静态确认）

- 写入面：`seelebridge/runtime_bundle.go:31-45` `bundleFor` —— 不存在即 `&sessionBundle{}` 并
  `r.bundles[sessionID] = bundle`。
- `sessionBundle`（同文件 `:23-27`）持有的就是重物件：`session *frameworkSession.Session`、
  `hooks *frameworkSession.LoopHooks`、`binding sessionBindings`（ctxStore / 主会话 ID / 主历史）。
- 读取面：`runtime_bundle.go:47-60`（`activeBundle`）、`seelebridge/runtime_session.go:72-80`
  （`SessionContextStoreFor`）——都只读注册表。
- **无删除面**：`findstr /s /c:"UnloadSession" /c:"HasSession" seelebridge\*.go` 无命中；
  `seelebridge` 全目录 `delete(r.…)` 只有 `runtime.go:824`（`forkActive`），
  `findstr /s /c:"delete(r.bundles" seelebridge\*.go` 无命中。
- 结论：一个会话只要在进程内被打开过一次（激活 / 冷加载），它的框架会话本体就**活到进程结束**。

**A2 引擎卸载不触 `bundles`**（静态确认）

- `internal/adapters/engine_port.go:896-913` `UnloadSession`：`delete` 的是
  `port.engines` / `port.engineCalls` / `port.sessionMaxLoops` 三张表，外加活跃别名置空。
- `EnginePort.HasSession`（同文件 `:380-390`）报告的是 `port.engines` 里有没有 —— 所以
  `resident_lru.go:30-43` 的 `touchResident` 与 `:62-102` 的驱逐，操作的是**端口层引用**，
  不是 A1 的本体。
- 结论：现有"驱逐"释放的是"引擎端口这一格 + 活跃别名"，框架本体与 ctxStore 仍在（A1）。

**A3 窗口口径与用户口径不一致**（静态确认）

- `resident_lru.go:62-102` `reconcileResidentLimit`：`residentCount` 把**所有**驻留会话
  （含当前视图会话）计入，与 `limit := Limits().ResidentSessionLimit` 比较。
- `content_lru.go:120-148` + `:89-118` `pruneContentOrderLocked` 同构：计数含当前视图会话。
- 后果：`resident_limit = 1` 的真实语义是"**只留当前视图会话**"（切走后连上一个也被驱逐），
  而不是用户口径的"**保留当前 + 上一个**"。要拿到口径语义，应把限额定义改成
  "**受保护集之外**的可释放驻留窗口 = 1"（见 [`release-surface.md`](release-surface.md) §3）。
- 守卫判据本身与口径**已对齐**：`pickEvictableCandidateLocked`（`resident_lru.go:104-126`）排除
  非当前视图、`isRestoringLocked`、非驻留、`sessionBusy`；`sessionBusy`（`:167-185`）覆盖
  `PendingApprovalCount > 0`（待审批）、`Chat.Running` / `QueuedCount > 0` / `PendingRequests`
  （运行 / 排队 / 在飞）。内容层 `pickContentEvictableCandidateLocked`（`content_lru.go:150-179`）
  同款守卫。

**A4 驱逐后的残格**（静态确认）

- `resident_lru.go:128-166` `evictResident`：flush 之后 `engine.UnloadSession`，然后
  `markResidentRecentLocked(sessionID, false)` **保留 Unit**（文件头注释亦声明
  "驱逐后 Unit/View/Runtime 槽保留（Resident=false），重开走 cold_load"）。
- `content_lru.go:181-217` `evictLoadedContent`：只把 `View.Conversation = nil` 且
  `ContentUnloaded = true`（`:196-205`），窗口标志 / 统计 / 聊天运行态 / 文件引用都保留。
- 结论：每会话常驻一份 Unit + View + runtime 槽 + 目录行；单元本身只在 A5 的两条路径被移除。

**A5 删除会话不释放内存**（静态确认）

- `application/core/workspace_usecase.go:21-44` `DeleteSession`：`Sessions.Delete` →
  `releaseTeamRuntime` → `releaseSessionAsync`（杀在途后台命令）→ `Workspace.UnbindSession` →
  刷新目录。**不调** `Service.UnloadSession`。
- 完整的释放面在 `application/core/session_lifecycle.go:120-196` `Service.UnloadSession`：
  `PersistCurrentSession` → 引擎 `UnloadSession` → `sessions.Remove` →
  `tasks.DropPlanProjection` → `tasks.UnloadSessionState` → `sessions.UnloadSessionTitle`
  （运行中拒绝：`ErrChatRunning`）。
- 但它的调用者只有两条：归档（`application/core/archive_session.go:65-70`）与 fork 子会话
  （`application/core/session_fork.go:150-151`）。
- 结论：**删除一个会话后，它的引擎实例 / Unit / 标题 / 任务投影仍留在内存**，直到重启或归档。

### B. 进程级无界：遥测与 per-会话残格

**B1 `MemoryTracer` 无上限**（静态确认，源码在 vendor 内）

- `vendor/github.com/RedHuang-0622/Seele/telemetry/memory.go:63-70`：容器为
  `traces map[string]*memoryTrace`、`metrics []Metric`、`audits []AuditRecord`、`subscribers`。
- `StartTrace`（`:116-149`）只 `t.traces[traceID] = &memoryTrace{…}`；`Record`（`:192-256`）
  把事件 `append` 进 `span.events`（`:227`）**并且** `t.audits = append(t.audits, audit)`
  （`:243`）——同一事件两份；`RecordMetric`（`:281-299`）`t.metrics = append(…)`（`:283-285`）。
- **无删除面**：`func (t *MemoryTracer)` 的全部方法只有 `StartTrace` / `StartSpan` / `Record` /
  `RecordMetric` / `Query` / `Subscribe`（仓储 grep 断言）——没有 prune / reset / drop / TTL。
- 宿主侧构造：`seelebridge/internal/telemetry/telemetry.go:13-15` `NewTracer()` 不传任何
  `MemoryOption` ⇒ 没有使用 `WithTraceSink`（该选项定义在 `vendor/.../telemetry/memory.go:17-24`），
  只有内存一份。
- 上游提供的 `WithStreamBuffer`（`:56-61`）只约束**订阅通道**，不约束存储。

**B2 `Query` 全量扫描**（静态确认）

- `memory.go:300-343`：先遍历 `t.traces` 的全部 traceID 并排序 → 对每个 trace 的每个 span 的
  每个 event 做 `matchesEvent` → 命中的再 `sort.SliceStable` 全量排序 → 最后 `applyLimit(&view, query.Limit)`。
- 即：**limit 只影响返回条数，不影响扫描与排序成本**；成本 ∝ 进程内事件总数。

**B3 遥测查询在热路径上**（静态确认）

- 链路：`application/core/view_state/coordinator.go:283-288` `tokenCountFor`（类型断言
  `TokenCountFor(string) string`）→ `internal/adapters/engine_port.go:1021-1026`
  `EnginePort.TokenCountFor` → `seelebridge/session_telemetry.go:23-39`
  `SessionTokenCount`（`QueryBySessionLimit(ctx, sessionID, 200)`）。
- 收集点：`coordinator.go:171-…` `CollectRuntimeProjectionFor` 里 `Tokens: c.tokenCountFor(sessionID)`。
- 调用方（grep 断言）：`chat.go:280`、`tool_hooks.go:135`、`plan_tools.go:632/653`、
  `service_interaction.go:109/214`、`service_scheduler.go:268`、`work_table.go:1005`、
  `service_snapshot.go:200`（会话激活前重采）等。
- 结论：**每次回合收尾 / 工具边界 / 会话激活 / 交互收敛**都会在"全进程事件集"上扫一遍。

**B4 `Runtime` 的 per-会话/节点残格**（静态确认）

- `seelebridge/runtime_teamwork.go:207-211`：`r.teamworkCoords[key] = coordinator` —— 只写不删
  （`findstr /s /c:"delete(r.teamworkCoords" seelebridge\*.go` 无命中）。
- `seelebridge/runtime_live.go:187-191`：`liveHistory[nodeID]` 追加；窗口只截断**切片**
  （`SubagentLiveWindow`，默认 512），**map 键按 nodeID 累积**（无 `delete(r.liveHistory…)`）。
- `seelebridge/runtime_account.go:61-62`：`routingNoticeSeen[key] = true` 去重表，只写不删。
- `runtime.go:114`（`routingNoticeSeen`）、`:171`（`liveHistory`）、`:223`（`teamworkCoords`）、
  `roleTurns`（`:214-217`）、`images`（`:74`）均为进程内 map。
- 唯一读即清的是 `servedRoutes`（`runtime_account.go:110` `delete(r.servedRoutes, sessionID)`）。

**B5 作业面已封顶（反例）**（静态确认）

- `vendor/.../Seele/jobs/manager.go:772-798` `prune`：注释写明 "retires the oldest terminal jobs
  when the table exceeds its ceiling; live jobs are never evicted"；超过 `m.limits.Records` 时
  逐个 `retireLocked`，并在锁外 `store.remove()` 删除 manager 托管输出文件（`retireLocked:801-…`）。
- 结论：后台作业登记表与输出文件**有上限**，不是本次的泄漏面；剩余成本是"驱逐/删除会话不杀在途
  作业"（由 `ReleaseSessionAsync` 负责，见 A5）与逐作业 stat/读日志末窗的采集开销。

**B6 `EnginePort` 小残格**（静态确认）

- `engine_port.go:896-913`：`handoffLatest map[string]uint64` 未在卸载时删除。
- `pendingHistory` 有清理点（`:672` `delete(port.pendingHistory, sessionID)`，交接路径）。

### C. 热路径成本随规模上升

**C1 快照深拷贝**（静态确认）

- `application/core/view_state/coordinator.go:144-149` `SnapshotView` =
  `model.CloneSnapshot(c.Snapshot)`。
- `application/model/state.go:923-1007` `CloneSnapshot`：`Sessions` 切片、`SessionWorkspaces` map、
  `Conversation`（+ 逐条 `Tool` 指针）、`ReadFiles`、`ReviewRequest`、`Task.ContextCompactions`、
  `Runtime`（`CloneRuntimeState`：VisibleTools / Skills / Plugins / Accounts / PermissionTiers /
  TodoItems / ActiveSkills / ScheduledTasks / ScheduledCommands / Plan（节点 + 边）/ **SubAgentTree** /
  **WorkTable** / WorkTableBatches / **TeamworkBoard**）。
- 消费者：`application/core/service_snapshot.go:15-42` `Snapshot()` 再叠加草稿行 +
  `enrichDirectoryRowsLocked`（逐行 `sessionStatusLocked`）；GUI 侧 `gui/bridge.go:640`
  `Bridge.Snapshot()` 直接把它交给前端（`gui/bridge.go:372` 的 `seelex:ready` 推送同源），
  headless 见 `gui/headless.go:175`。
- 结论：**每次取整份快照**都要付一份 O(正文 + 工作表格 + 子代理树 + 看板) 的拷贝。

**C2 目录枚举**（静态确认）

- `sessionstore/json_layout.go:649-671` `listMeta`：`os.ReadDir(projectDir)` → 每个会话目录
  `readMetaFromDir` → `sort.Slice`。
- `readMetaFromDir`（`:617-647`）：读 `metadata/message.json` 全量（`readSharedFile`）+ `json.Unmarshal`。
- 每个项目一遍；`RequestCatalogRefresh`（`application/core/*` 多处）是触发点。
- 结论：成本 ∝ **磁盘上会话目录数**（单调增长，会话不会自动清理）。

**C3 内容 LRU 的 O(单元) 收敛**（静态确认）

- `content_lru.go:89-118` `pruneContentOrderLocked`：维护使用序时**遍历 `service.sessions.UnitIDs()`**，
  对每个单元调 `sessionContentLoadedLocked`（`:218-230`，逐单元 `View.Read`）。
- `UnitIDs` 是 actor 调用（`session/domain_actor.go:153-156`，channel 命令 + 单 goroutine）。
- 该函数在**每次** `touchContent`（装载 / 热挂载 / 分页读回）收敛时被调用一次。
- 结论：单元数（≈ 进程内出现过的会话数）越大，每次正文装载的收敛越贵。

**C4 LRU 使用序维护**（静态确认）

- `resident_lru.go:46-60` `markResidentRecentLocked`、`content_lru.go:59-73` `markContentRecentLocked`：
  每次整条切片重建。窗口调小后序长随之变小，属"随窗口收敛"的成本。

## 4. 归因排序（我的判断，按"是否值得先动手"）

| 排名 | 面 | 为什么排在前面 |
|---|---|---|
| 1 | A1 + A2 + A4（会话本体不释放） | 每个打开过的会话一份框架会话 + ctxStore + 主历史；"驱逐"几乎不减内存，窗口调小也失效 |
| 2 | B1 + B2 + B3（遥测无界 + 热路径全量扫） | 唯一"纯粹无界"的面：只要在干活就涨；且把成本摊到每次回合收尾 |
| 3 | A5（删除不释放） | 用户视角"清掉的会话"仍在内存；修法明确（复用完整释放面） |
| 4 | C1（快照深拷贝） | 单次成本随当前会话/项目规模上升；调用频率高（前端轮询） |
| 5 | B4（per-会话/节点残格） | 单个体量小，但面多、无删除面，需一并收口 |
| 6 | C2 / C3 / C4（枚举与 LRU 维护） | 规模相关但可控；配合释放面收口后可一并缓解 |
| — | B5（作业） | **已封顶**，不需要改（避免误伤） |

## 5. 未证项（需要运行时实测才能升级为"确认"）

1. **各面的实际占比**：需要 heap profile（`bundles` / `MemoryTracer.audits` / WorkTable / 单元视图）
   在"长期使用后"的采样对照。本文档给的是**机制确认**，不是占用比例。
2. **累积速率**：每秒/每回合增长多少 MB、每回合热路径耗时随事件数上升的斜率（需要
   `testing.Benchmark` 或现场 pprof）。
3. **冷回读正确性前提**：释放面补齐后，驱逐→激活是否能还原同一窗口（正文 / 压缩帧 / ctxStore），
   需要红灯用例验证（见 [`optimization-plan.md`](optimization-plan.md) §验证）。
4. **A4 残格的量级**：Unit/View 槽本身不大，是否值得进一步释放要看实测（可能只有序结构需要收口）。

> 口径说明：以上 1–4 都不改变 §2 的裁定方向（"释放面不完整 + 无界累积 + 热路径随规模增长"），
> 只影响 P0/P1 的先后与是否值得做 P2。
