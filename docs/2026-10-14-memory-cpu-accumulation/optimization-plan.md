# 优化方案：分级改动、验证与残留风险

- 规范：见 [`release-surface.md`](release-surface.md)
- 证据：见 [`verdict.md`](verdict.md)
- 原则：**先修释放面（P0），再治无界面（P1），最后做结构收口（P2）**；每一步都有红灯用例；
  不做破坏性操作（不杀用户进程、不动 `.seelex`、不做"跑一遍看内存涨多少"的现场实验）。

## P0：让"释放"真的释放（窗口口径 + 完整释放面）

| 项 | 改动落点 | 收益 | 风险 | 验证 |
|---|---|---|---|---|
| P0-1 窗口口径 | `seelexctx/limits.go`（新增 `SessionReleaseWindow`，默认 **1**；`resident_limit` / `loaded_content_limit` 作为兼容别名映射）+ `application/core/resident_lru.go:62-102`、`application/core/content_lru.go:89-148`（计数只数**受保护集之外**的已加载会话） | 窗口=1 的用户口径成立；`resident_limit=6` 带来的"6 个框架会话常驻"直接消失 | 语义改变会影响既有测试对 `resident_limit` 的断言（需一并改口径） | INV-RS3 / INV-RS8 |
| P0-2 共用保护谓词 | `application/core/resident_lru.go` 提 `protectedSessionLocked(sid) bool`，`content_lru.go:150-179` 改为调用它 | 保护集只有一处事实源，避免两层漂移 | 无（纯重构） | INV-RS1 / INV-RS2 |
| P0-3 桥侧释放面 | `seelebridge/runtime_bundle.go`（新增 `DropSession(sessionID) bool`：删 `bundles[sid]`，若 `activeSessionID == sid` 需由调用方保证不会发生）+ `seelebridge` 导出 `HasSession` | 释放框架会话本体（含 ctxStore / 主历史 / hooks）——这是"驱逐几乎不减内存"的根因 | 删早了会让在飞回合失去会话本体（必须由保护谓词 + 二次确认挡住） | INV-RS4 / INV-RS6 |
| P0-4 端口残格 | `internal/adapters/engine_port.go:896-913` `UnloadSession` 补 `delete(port.handoffLatest, sessionID)` | 端口对已卸载会话不留任何状态（与 `sessionMaxLoops` 同一纪律） | 无 | 单测：卸载后 `handoffLatest` 无该键 |
| P0-5 释放面统一 | `application/core/resident_lru.go:128-166` 的 `evictResident` 改为调用统一的 `releaseSessionMemory(sid, releaseIdle)`（内容层 + 引擎端口 + 桥本体 + 残格，保留单元）；`application/core/session_lifecycle.go:120-196` 的 `Service.UnloadSession` 复用同一函数的 `releaseFull` 变体 | 一条路只有一个释放语义；LRU / 卸载 / 归档 / 删除共用 | 归档与 fork 路径的既有行为必须逐条回归（`archive_session.go:65-70`、`session_fork.go:150-151`） | 既有归档 / fork / 卸载测试 + INV-RS4 |
| P0-6 删除即释放 | `application/core/workspace_usecase.go:21-44` `DeleteSession` 末尾补 `releaseSessionMemory(sid, releaseFull)` | "删掉的会话"不再留在内存（当前会一直留到重启） | 释放失败不应让删除失败（记日志、留待下轮） | INV-RS7 |
| P0-7 忙→闲收敛 | 在状态迁移点（回合收尾、审批结案、队列排空）补一次 `reconcileSessionRelease()`（不新增常驻 goroutine，只在迁移点调用） | "切走之后跑完的那个会话"立刻被收回，不必等下一次 touch | 收敛点若在持锁区内调用会死锁（必须锁外调用，遵循 `touch*` 的既有约定） | 用例：A 视图 → 切到 B → A 跑完 ⇒ A 被释放（窗口=1 时） |

## P1：治无界面与热路径

| 项 | 改动落点 | 收益 | 风险 | 验证 |
|---|---|---|---|---|
| P1-1 遥测查询去全量扫 | `seelebridge/session_telemetry.go:23-39` `SessionTokenCount` 不再调 `QueryByLimit`；改为**按会话增量计数**（回合收尾时把本轮 usage 累加进 per-会话计数器，读面 O(1)） | 把"每次回合收尾 O(进程事件数)"从热路径拿走（`verdict.md` §3 B3） | 增量计数需覆盖所有 usage 来源（含失败重试、子代理），否则读数与 trace 不一致 | 用例：计数器与 trace 查询结果在单测场景下相等；`TokenCountFor` 在 10 万事件下成本恒定 |
| P1-2 遥测本身封顶 | 上游 `telemetry.MemoryTracer`（`vendor/.../telemetry/memory.go:63-70`）**没有**删除面 ⇒ 两条路：① 上游 Seele 增加保留策略（`MaxTraces` / `MaxEventsPerSpan` / `PurgeBefore`）；② 宿主侧只在**不影响 trace 视图**的前提下减少写入（例如按需开启） | 唯一"纯粹无界"的面被压住 | 改 vendor 属上游漂移，需走上游（不在本仓直接 patch）；减少写入会影响 trace 视图与 `SessionTokenCount` 一致性 | 用例：写 N 条后 traces/events 有界；现场开关对照 |
| P1-3 快照深拷贝瘦身 | `application/core/view_state/coordinator.go:144-149` + `application/model/state.go:923-1007`：`SnapshotView` 改为"共享不可变帧 + 版本号"，或把 `WorkTable` / `TeamworkBoard` / `SubAgentTree` 从深拷贝改为按需分片读 | 前端每次取快照不再付 O(正文 + 台账 + 树 + 看板) 的拷贝（`verdict.md` §3 C1） | 快照是并发读者的共享值，浅拷贝会读到一半的写（`CloneTeamworkBoardView` 的注释就是这个教训）⇒ 必须配套"写时换帧"纪律 | 用例：工作表格灌 1 万行后 `Snapshot()` 分配次数不随行数线性增长（`testing.AllocsPerRun`） |
| P1-4 目录枚举缓存 | `sessionstore/json_layout.go:649-671` `listMeta` 结果按 (project, 目录列表指纹) 缓存；`RequestCatalogRefresh` 只失效缓存 | 每次刷新不再 `ReadDir` + 逐会话读 head（`verdict.md` §3 C2） | 会话目录不是单写入者时缓存会陈旧（本进程写入自己是单写入者；外部改动需兜底：mtime/目录条目指纹） | 用例：1000 个会话目录下刷新不产生 N 次文件读；新增/删除会话后列表正确 |
| P1-5 内容 LRU 收敛不再遍历全部单元 | `application/core/content_lru.go:89-118` 改为维护"持有正文的会话集合"（`loadedContentOrder` 自身即是），去掉每次 touch 的 `UnitIDs()` 遍历 | 去掉 O(单元数) 的重复成本（`verdict.md` §3 C3） | 收养语义（"有正文但不在序里"）需保留一条低频兜底路径 | 用例：1000 个单元下 `touchContent` 不随单元数增长 |

## P2：结构收口（长期）

| 项 | 内容 |
|---|---|
| P2-1 会话内存账 | 把所有 per-会话内存登记进一处台账（`bundles` / 引擎端口 / 单元 / `residentOrder` / `loadedContentOrder` / `teamworkCoords` / `liveHistory` / `roleTurns` / 标题 / 任务投影），释放面按"账"逐项清；配套 `describeSessionMemory(sid)` 只读诊断口 |
| P2-2 桥侧残格清单化 | `seelebridge` 的会话段残格（`teamworkCoords` / `liveHistory` / `routingNoticeSeen` / `roleTurns` / `images`）收进同一释放函数，避免"新增一格忘了清"（`verdict.md` §3 B4） |
| P2-3 trace 视图改读归档 | GUI trace 视图改为读磁盘归档（内存只留最近 N 条），彻底解除"视图需要全量内存事件"的耦合 |
| P2-4 体检打点 | 周期性只读打点：会话数 / 驻留数 / 有正文数 / `tracer` 的 traces·events·audits / 作业行数 / 目录行数，写入 `perf` 目录供回归对比（不参与业务逻辑） |

## 验证方式（统一口径）

1. **红灯用例先行**：每个 P0/P1 项先写会失败的用例（命名建议
   `TestReleaseSurface*` / `TestTelemetry*` / `TestSnapshot*`），固定断言"面变小 / 不随规模增长"，
   而不是断言"内存数字"（内存断言不可靠）。
2. **回退验证**：修完把改动临时回退，确认用例转红——只凭"改完就绿"不算证据。
3. **规模对照**：用 `testing.AllocsPerRun` / `testing.Benchmark` 比较 100 行 vs 1 万行台账、
   1 千 vs 1 万事件的斜率。
4. **现场口径**（用户在场时执行）：`pprof` heap 对比"重启后"与"长期使用后"的
   `bundles` / `audits` / `WorkTable` 分布；CPU profile 关注 `CloneSnapshot` / `Query` / `listMeta`。
   现场测量不改数据、不杀进程。
5. **只读对账**：会话目录/绑定/重复目录的存量清点脚本（本工作包已在用的只读诊断口径），
   用于判断"磁盘规模"对 C2/C3 的放大倍数。

## 明确不做的事

- 不修改 `.seelex/`、`config/`、`dist/` 下任何内容；不做清理动作（任何清理需先预警 + 备份 + 与用户确认）。
- 不直接 patch `vendor/github.com/RedHuang-0622/Seele`（上游漂移）；遥测保留策略走上游改动。
- 不为"看内存涨多少"而长时间压测用户的真实会话。
- 不在持锁区内做释放（flush 含文件 I/O，释放含锁外调用约定）。

## 残留风险

1. **窗口=1 的体验代价**：只留一个"上一个"，切回更早的会话必然冷加载（正文尾部窗口 + 引擎重建）。
   这是口径要求；若要更顺滑，用 §3.5 的分层逃生门把内容窗口放大，而不是把引擎窗口放大。
2. **桥本体被释放后的重开成本**：ctxStore / 主历史重建依赖磁盘 record 与 `SessionContextStore`，
   必须先有用例证明"释放→激活"等价于冷加载（INV-RS6），否则宁可保留本体。
3. **并发窗口**：挑选与释放之间状态可能变化（现有代码用二次确认兜住，`resident_lru.go:137-145` /
   `content_lru.go:187-195`），新增桥本体释放时不得削弱这两个二次确认。
