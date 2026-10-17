# Seelex 架构重排方案（三大层 + 底座）

日期：2026-10-15
状态：**方案；已从 2026-10-16 起实施**。实施记录见同目录 `m1-record.md` … `m5-record.md`
（M1–M5 均已达成：`seelebridge/` 目录已删除，中间层/门面/装配瘦身与
迁移后行为项 P1/P2/V10 已收口；出口验收见 `m5-record.md` §30/§52–§54）。本文正文里的"待裁决"字样只指**尚未落地的项**，
不要读成"目标模型还没定"；§1 的模块图与依赖方向已是**现行落位口径**。
（本文最初写作时标注"方案／待裁决，不是实施记录"，那句描述的是当时状态。）
门面落点：§1 写的 `appface` 与 §2.2 的"或原地保留 `application/`"二者等价，当前实现取后者
（`application/application.go` 保形层 + 五组窄接口，见
[`r10-r11-p13-contract.md`](r10-r11-p13-contract.md) §1）。
范围：模块重排（物理归位 + 包改名 + 依赖方向纠正）与配套里程碑、长程约束。
非范围：行为变更、数据格式变更、Seele 引擎能力重写、产品/协议新设计。

事实口径：

- **已核实**：本轮读取的源码、`go list` 依赖图、文件行数统计、模块 README。
- **推断**：由已核实证据推出的架构判断，已标注成立条件。
- **待核实**：尚未读完整调用链的行为归属，集中在 §3.6。

关联文档：

- 目标模块图与状态所有权：[`../2026-10-15-product-architecture-pre-design/global-review.md`](../2026-10-15-product-architecture-pre-design/global-review.md) §6
- 上一轮 `application/core` 拆分结论：[`../2026-08-22-application-split/design.md`](../2026-08-22-application-split/design.md)
- 去重与解耦历史：[`../2026-08-14-decoupling/00-index.md`](../2026-08-14-decoupling/00-index.md)
- 模块拆分历史：[`../2026-08-12-module-split/plan.md`](../2026-08-12-module-split/plan.md)
- 模块文档规范：[`../arch/readme-spec.md`](../arch/readme-spec.md)

---

## 1. 目标模型

### 1.1 三层 + 底座 + 装配（自顶向下）

```text
前端     gui/  tui/  headless/  console/            ← 只 import 门面；本轮零改动
            │
门面     appface（现有 application/ 保形层）          ← 别名再导出 + 五组窄接口；只依赖契约
            │
中间层   middle/protocol  middle/view                 ← 会话视图 / 投影 / 快照 / 事件
         middle/residency  middle/switch              ← LRU 驻留 / 多会话运行切换
            │
后端     backend/engine                               ← ReAct 循环、plugin/skill/mcp 注册、tool 注册
         backend/tool                                 ← 工具注册表/路由、computer、plan、worktree、jobmanage
         backend/limit                                ← 权限中间件链 + 唯一循环判据 + 预警
         backend/multiagent                           ← goal、team、teamwork、workunit、subagent
            │
底座     store/（会话与项目持久事实）  contract/（零依赖契约）  kernel/state（唯一共享锁）
装配     wiring/（组合根 + 适配器 + 注册表）   internal/（低层工具）
```

依赖方向恒为 **前端 → 门面 → 中间层 → 后端 → 底座**；底座与契约**不得反向依赖任何一层**。

### 1.2 各层权威状态与边界

| 层 | 包 | 权威状态（谁说了算） | 允许依赖 | 明确禁止 |
|---|---|---|---|---|
| 前端 | `gui`、`tui`、`headless`、`console` | 纯视图状态 | 门面 | 直接 import store/engine/tool/limit |
| 门面 | `appface` | 无 | `contract`（+ 零依赖 protocol） | 持有 store 句柄、暴露构造函数、暴露完整 Service |
| 中间层 | `middle/*` | 会话视图、驻留、切换 | 契约 + 窄端口（store 读面经端口注入，中间层包不直接 import `store`） | 写会话事实、调用 Seele |
| 后端 | `backend/engine` | 运行会话、在途执行 | 契约 + store 写端口 + Seele | import 中间层/前端 |
| 后端 | `backend/tool` | 工具注册表、执行句柄、作业行 | 契约 + `backend/limit` | 自判权限 |
| 后端 | `backend/limit` | 权限判据、档位、循环预算 | 契约 | 持有工具实现 |
| 后端 | `backend/multiagent` | 团队/角色/子代理/作业单元生命周期 | 契约 + store | import 中间层/前端 |
| 底座 | `store` | 会话与项目持久事实 | 契约 | 反向依赖任何人 |
| 底座 | `contract` | 端口、DTO、事件协议 | stdlib | 任何实现依赖 |
| 底座 | `kernel/state` | 唯一共享锁 + Snapshot + Deps + Events + Approval | `contract` | 业务判定 |
| 装配 | `wiring` | 无 | 所有层 | 承载业务规则 |

### 1.3 门面的生态位与内容

**生态位（一句话）**：门面是前端与真实三层之间的**唯一缝（seam）**，也是契约驱动的落点。
前端只依赖门面，门面只依赖契约，`wiring` 在启动时把各层实现注入进门面持有的端口。

现状（已核实）：`application/application.go` 共 209 行，内容为**约 110 个类型别名 + 12 个函数**；
被 `gui`/`tui`/`headless`/`console`/`main.go`/`e2e` 与约 30 个根目录测试 import；
前端去重后实际使用约 **120 个名字 / 260 个调用点**。

它现在同时承担四件事，新模型只保留 1 与 3：

| 现职责 | 新归属 |
|---|---|
| ① 类型身份（≈110 别名） | 门面保留，但目标指向 `contract/` 与 `middle/protocol` |
| ② 端口再导出（Dependencies/ChatEngine/RuntimePort…） | 下沉 `contract/`，门面仅再导出 |
| ③ 用例方法面 | 保留，但必须收窄成五组窄接口 |
| ④ 构造函数（`New`/`NewEventHub`/`NewApprovalBroker`/`NewPromptStack`/`NewEffortManager`/`WebSearch`） | 迁出到 `wiring/`；前端不得自造引擎 |

**门面必须包含的五类内容**：

| 组 | 内容（示例来自现有 120 个名字） | 提供方 |
|---|---|---|
| A 类型与事件身份 | `Snapshot`、`SessionInfo`、`SessionStatus*`、`Plan*`、`Node*`、`Event*`、`Subscription`、`ProtocolVersion` | 别名 → `contract` / `middle/protocol` |
| B 读投影 | `Snapshot`、`SnapshotOf`、`Subscribe`、`SubscribeSession[WithReplay]`、`ListSessions`、`SessionMeta`、`LoadLatestHistory`、`LoadMoreHistory`、`SearchHistory`、`GetSessionTranscript`、`PlanState`、`ToolFailureBoard`、`PerfStats`、`RoleSnapshot`、`SessionInputIndex`、`ListSubagentRecovery`、`SubagentDetail`、`TeammateSessionLive[Page]For`、`WorkspaceTree`、`WorkspaceFileContent`、`WorkspaceChanges`、`WorkspaceGitLog`、`WorkspaceGitCommit*` | `middle/view` 端口 |
| C 命令面 | `Submit`、`SubmitToSession`、`CancelChat`、`CancelAllChats`、`ResolveInteraction`、`BeginNewSession`、`ResumeSession`、`ForkSessionLatest`、`ActivateSession`、`DeleteSession`、`ArchiveSession`、`BindWorkspace`、`UnbindWorkspace`、`CreateWorkspace`、`ResumeSubagent`、`ForkSubagents`、`ResumeInterruptedSubagents`、`ClearSubagentTree` | `middle/switch` + `backend/engine` 端口 |
| D 档位与策略 | `SetLoopLimit`、`SetReasoningEffort`、`SwitchEffort`、`SetPermissionTier`、`SetFullAccess`、`SetSessionReleaseWindow`、`SetAccountWindow`、`SelectAccount`、`ConfigureSessionStorage`、`TestSessionStorage`、`SwitchPlugin`、`SetSessionMeta`、`SetLifecycleOrder`、`SetRoleLifecycle`、`CreateRoleSession`、`ScheduleRegister`、`ScheduleCancel`、`ScheduleUpdate`、`ScheduleFire` | `backend/limit` + `backend/engine` |
| E 队列与无状态函数 | `RecallQueuedInput`、`ReorderQueuedInput`、`Suggestions`；`HasSigilPrefix`、`SigilOf`、`RenderDiag`、`ValidEffortLevels` | 中间层 + 纯函数 |

**门面不变量（可机械断言）**：

| ID | 不变量 | 断言方式 |
|---|---|---|
| I1 | 门面依赖闭包 ⊆ {`contract`, protocol} | `go list -deps` 断言测试 |
| I2 | 前端依赖闭包 ⊆ {门面, 前端自己的子包} | 同款断言（现 gui 有 4 条越层边，见 §3.4） |
| I3 | 门面不出现构造函数 | 包内无 `func New*` |
| I4 | 类型用别名而非新类型（保形） | 断言 `type X = Y`，搬迁不产生新类型身份 |
| I5 | 前端零改动 | `git diff` 只允许出现门面内部文件 |

### 1.4 limit：中间件生态位

链路固定为 **engine（toolcalling） → limit → tool**。落点已存在：Seele 提供
`MetaMiddleware func(name string, meta ToolMeta, next ToolHandler) ToolHandler` 与
`WithMetaMiddleware(mw ...MetaMiddleware)`（支持有序多中间件，内部 `chainMeta`）；
Seelex 侧现已在 `seelebridge/tools/registry_state.go` 一次挂载 `r.permission`、
`toolEvents.Middleware()`、`bashDiagnosticMiddleware()`。

目标形态（有序、单一判据各占一格）：

```text
1. subject / tier 解析     （复用 Seele permission.WithEngine / WithSessionID）
2. 路径门 / ProjectScope   （seelebridge/security/project_scope.go）
3. 权限判定                （委托 Seele permission.Gate，Seelex 不自判）
4. 审批                    （`backend/limit/approval`（原 `application/approval`，M2 已归位）→ 端口）
5. 循环预警                （新增，见 §1.5）
```

### 1.5 循环预警与交接

现状（已核实）：已有**事后收尾**——`ReActBudget{MaxToolRounds, MaxToolCalls, MaxNoProgressRounds}`
按 effort 档位分档（15/30/6、48/96/10、384/768/24、768/1536/48），预算耗尽后
`finalizeReActBudget` 保留一次纯文本交付回合，并有 `reactBudgetPresentation` /
`contextExhaustedPresentation` 展示面。**预警是事前触发，属增量**：

```text
BudgetFor(effortLevel) → Budget          # 档位→预算（自 application/prompt/effort.go 迁入）
Reserve(sessionID, n)  → Budget          # 会话级覆盖（自 contract.SessionLoopLimitPort 迁入）
Verdict(budget, used)  → Ok | Warn | Exhausted
  Warn      → 不再派发新工具，让 engine 产出"当前工作总结"
  Exhausted → 保留一次纯文本交付回合（沿用现有 finalizeReActBudget 行为）
总结正文 → workunit.RecoveryNote（唯一一份正文）
总结容器 → resumeNotes（唯一一份容器，读完即消）
交给下一个任务 → 复用既有 resume / pending_tail 通道，不新增字段、不新增文件格式
```

判据收敛要求：循环上限现有 **5 处**判据（`application/prompt/effort.go`、
`application/model/state.go` 的 `LoopLimit`、`contract.SessionLoopLimitPort`、
`seelexctx/limits.go` 的 `InputLoopLimit`、`seelebridge/fork/tool.go` 的 `LoopLimitCeiling`）
必须并成 `backend/limit/loop/` 一处；引擎只消费判据，不自算。

### 1.6 skill 注册模型

- `goal` 等以 **SKILL.md 承载提示词与能力声明**，目录即 Skill 名（现状已如此，见
  `plugins/default/goal/SKILL.md`、`plugins/default/plugin.md` 的 YAML front matter）。
- **注册与调用归 engine**：skill 列表初始化、skill 调用、skill 下能力调用都在 `backend/engine/skill`；
  相关包只做能力实现。
- **权限簇属不在 SKILL.md 里写第二份**：现有 `seelebridge/tools/tool_meta.go` 已把
  "单一事实源 = 路由组表 `DefaultPermissionGroupList`" 写成红线（"两张表必然漂移，而漂移没有任何报错面"）。
  目标是不变量：`skill 声明的能力名 ⊆ 已注册工具名`，且每个名字都能从路由组表推导簇属；
  推导不出者进入 `UndeclaredTools` 并退回默认审批。守卫形式照抄既有先例
  `plugin_doc_sigil_test.go`（文档 ↔ 代码的机械防线）。

### 1.7 store：会话粒度句柄与三层锁

**已核实的既有模型**（`sessionstore/session_granular.go` 开头自述）：原子单位 = session，
键 `session:<id>` 五片（record/history/transcript/toolresults/context）+ 项目索引；
主会话与子代理会话同构；暴露层为 `SessionGranularStore`，`session/ports.go` 以类型别名给出 `StorePort` 契约；
并已定死"本包不得 import seelex/session（反向会成环）"。

目标：**把 `SessionGranularStore` 作为唯一会话句柄模型**（每 sessionID 一个实例、锁自持），
`Router` 降级为数据根/项目索引/模块 head 的持有者。三层锁粒度必须显式并存，迁移时字节级不变：

| 层 | 粒度 | 现有实现 | 可否省 |
|---|---|---|---|
| 数据根独占锁（`lock.owner` + 心跳续租 + stale 判定 + `allow_multi_process` 退化） | 进程（跨进程单写者） | `sessionstore/data_root_lock.go` | 不可省 |
| 会话自持锁 | session | `SessionGranularStore` | 已存在，转正 |
| 模块 head 原子发布 + 未提交草稿 | module | `sessionstore/module_heads.go`、`pending_tail.go` | 不可省 |

### 1.8 派生数据与钩子（定时任务 / 工具记录 / 项目看板）

这三条通道不是新设计，而是既有实现的重排。共同点：**落盘纪律同类**（append-only、残尾按"未收尾即未提交"跳过），
**判定与规划刻意都在上层**，但三者各自实现了一遍追加与折叠。

| 通道 | 事实（谁说了算） | 判据落点（现状，刻意在上层） | 运行期物 |
|---|---|---|---|
| 定时/周期任务 | 定义态 `<数据根>/scheduled-tasks.jsonl`（全局、不按项目分区、后写覆盖 + 墓碑）；会话侧 `schedule.registered/cancelled/fired` 结构事件 | `normalizeSpec` / `scheduledPermissionTier`（`seelebridge/scheduler`）；每次触发的执行参数默认值（`core/service_scheduler.go`） | `time.Ticker` 单循环 actor（两类任务：command / prompt） |
| 工具记录（成功/失败计数 + 失败原因） | 台账 `<数据根>/tool-record.jsonl`（**现状**为全局、成功也落行）→ **已裁决改为会话粒度 + 提交点批次落盘**（见本节末"工具记录的目标形态"） | `toolStatusFailed` / `toolRecordReason` + 原因词表 `dto.ToolReason*`（`core/tool_record.go`） | 内存读数（先落地；落盘失败不回抛、留在 `LedgerError`） |
| 项目看板投影 | 产物 `<项目根>/.seelex/board/<会话 ID>/` + 看板根 `BOARD.md`、`project-known.md` | 零判定（`core/project_board.go` 的 I1/I2/I3：不产出判定、不是输入、可删可重建） | `ProjectBoardSyncer`（标脏 + 唤醒 + flush；`SetProjectBoardSink` 单槽注入） |

**目标切法（三条通道同构）**：

```text
契约    contract/                 端口：Persistence / ToolRecordLedger / ProjectBoardSink / hook observer
机制    store/ledger/             append-only 追加 + 残尾跳过 + 折叠（共用机制，不共用重放语义）
语义    store/schedule/           定义态（后写覆盖 + 墓碑）
        store/session/（module）   计数台账（单调折叠；会话粒度 + 提交点批次落盘）
        store/board/              投影落盘（按项目、可删可重建）
判定    backend/engine/schedule/  下次触发时间与触发动作（运行期 actor）
        backend/tool/record/      工具成败与原因归类（唯一判据，随工具完成段）
        backend/limit/policy/     command 白名单与权限簇属（唯一事实源）
        middle/view/board/        投影装配与读面（零判定）
        middle/view/record/       计数看板读面
钩子    接口在 contract/，注入在 wiring/（装配期交接一次、此后只读），实现只在各上层
```

**store 的边界（红线）**：store 只提供"路径 + 追加 + 残尾 + 原子发布"与**提交点信号**（落盘与唤醒）；
**判定与规划不进 store**，否则 store 从"持久化事实"变成"行为所有者"——那正是当前 `Service` 419 方法的成因。
现有代码已按此口径写死（`project_board.go` 的零判定、`tool_record.go` 的单点判据、`dto.ToolReasonVocabulary()` 的唯一词表），迁移不得破坏。

**必须保留的既有语义**：

- 定时任务定义态**全局**（"每天九点巡检"不因切项目而换一套），与触发产生的会话记录（按项目/会话分区）分流。
- 项目看板是**向项目目录的公开投影**（其它 agent 可读），且不得把机器本地 ID 写入产物（否则看板不可提交）。
- `internal/toolrecord` 与 `scheduler.FileStore` **只共用追加/残尾机制，不合并重放语义**：前者单调累加（落盘失败不丢计数），
  后者是定义态（写入失败则登记不成立）。这条差异已由源码注释按 `AGENTS.md` §8.2 论证过，迁移时原样保留。
- 定时任务运行期 actor 的归属**已裁决**：`backend/engine/schedule/`。

**工具记录的目标形态（已裁决：会话粒度 store + 提交点批次落盘）**

现状写放大（已核实）：`internal/toolrecord.Ledger.RecordToolOutcome` **每次工具调用**做一次
`os.OpenFile(O_APPEND) + Write + file.Sync() + Close`——一回合几十次工具调用就是几十次 fsync，
高频工具（现场读数：`bash_read` 单工具 165 次）下"写繁忙"的担心成立。

目标：工具完成段只往**会话级内存批次**追加（零磁盘），在**提交点统一落盘一次**。

| 项 | 取值 | 依据 |
|---|---|---|
| 落点 | 会话粒度：`store/session/` 下的一个 module（走既有 `publishModuleHead` / `commitModuleHead` 原子发布），随会话删除/归档一起走 | 用户裁决；store 已有 module head 机制 |
| 批次边界 | 会话提交点：消息草稿发布（`appendDraftRows` → `publishDraftTail` → `publishPendingTailLocked`）与回合终态/会话释放处 | 既有提交路径 |
| 批次先例 | 复用 `chat.StreamBatcher`（`FlushSize: 32`、`BufferSize: 128`、`Interval: 40ms`）与 `flushStreamBatcherFor(sessionID)`（它**已经在工具钩子边界 flush**） | `core/chat.go:664`、`:685` |
| 落盘形态 | 一批一次写入（N 条折成一次写 + 一次 sync），而不是一次调用一次写 | 消写放大 |
| 崩溃语义 | 未提交批次**可丢、不得隐式提升**为已提交（与消息通道草稿同款："可以救回，但绝不能被隐式提升"） | `pending_tail.go` 既有红线 |
| 读数口径 | 进程内内存读数仍然实时（看板不因未落盘而空）；持久读数以最后一次提交为准 | 现实现即"内存先落地" |

**由此产生的语义待裁决（V10）**：工具记录的**看板读数是本会话还是整机**。现值是刻意的全局读面
（`dto/tool_record.go` 注释："它是**全局**读数（跨会话、跨项目）…回答的是这台机器上哪个工具最该优化"），
并明确**不进 `Snapshot`**。改为会话粒度后必须二选一：

(a) 会话级台账 + 全局汇总读面（保留"这台机器哪个工具最该优化"，需一层汇总）；
(b) 纯会话级看板（读数语义变更；此时"不进 Snapshot"的原有理由失效，是否进快照需重新裁决）。

---

## 2. 迁移文件的 from → to

约定：`→` 表示物理移动并改包名；`⇒` 表示同位置改名；**删**表示删除（须进删除清单）；
未列出的叶子包（如 `internal/buildinfo`）默认原地不动。所有迁移均为**行为零变化**。

### 2.1 前端（本轮零改动）

| from | to | 动作 |
|---|---|---|
| `gui/`（除 headless 两文件）、`gui/terminal` | `gui/`、`gui/terminal` | 不动 |
| `tui/`、`tui/splash` | `tui/`、`tui/splash` | 不动 |
| `gui/headless.go`、`gui/headless_subagent.go` | `headless/` | 搬（可选，见 D4） |
| `gui/bridge.go` | `gui/bridge.go` | 只改 import：`sessionstore` ⇒ 门面读端口；`seelexctx/search` ⇒ `contract.SearchPort`；`seelebridge` ⇒ 门面能力端口 |
| `application/console` | `console/` | 搬（backend 前端，与 gui/tui/headless 平级） |

### 2.2 门面

| from | to | 动作 |
|---|---|---|
| `application/application.go`（209 行别名 + 函数） | `appface/`（或原地保留 `application/`） | 别名目标指向新位置；构造函数迁出到 `wiring/` |
| `application/application.go` 的 `New*`、`WebSearch` | `wiring/constructors.go`、`backend/tool/websearch` | 搬 |

### 2.3 契约与共享内核（底座）

| from | to | 动作 |
|---|---|---|
| `application/contract`（ports.go 326、workspace_file.go 21、workspace_tree.go 18） | `contract/` | 搬（顶层、零依赖） |
| `application/contract/dto`（35 文件 2161 行） | `contract/dto/` | 搬 |
| `application/model`（5 文件 1033 行） | `middle/protocol/model/` | 搬 |
| `application/event`（346 行） | `middle/protocol/event/` | 搬 |
| `application/core/internal/state`（27 行） | `kernel/state/` | 搬 |
| `application/core/service_state.go`（98） | `kernel/state/` | 搬 |

### 2.4 中间层

| from | to | 动作 |
|---|---|---|
| `application/core/view_state/`（593） | `middle/view/state/` | 搬 |
| `application/core/subagent_view/`（373） | `middle/view/subagent/` | 搬 |
| `application/core/worktable/`（76） | `middle/view/worktable/` | 搬 |
| `application/core/work_table.go`（833）、`work_table_async.go`（237） | `middle/view/worktable/` + `middle/switch/`（终态触发回合段，见 `work_table.go:910`、`:944`） | 拆 |
| `application/core/project_board.go`（662） | `middle/view/board/` | 搬 |
| `application/core/runtime_projection.go`（44）、`diagnostics.go`（82）、`perf_stats.go`（44）、`tool_failures.go`（54） | `middle/view/` | 搬 |
| `application/core/service_snapshot.go`（280） | `middle/view/` | 搬 |
| `application/core/resident_lru.go`（97）、`content_lru.go`（191）、`snapshot_budget.go`（48） | `middle/residency/` | 搬 |
| `application/core/session_catalog.go`（16）、`session_cold_read.go`（80）、`session_input_index.go`（183）、`window.go`（46）、`archive_session.go`（74） | `middle/residency/` | 搬 |
| `application/core/session_history.go`（895）、`history_search.go`（48） | `middle/switch/history/` | 搬 |
| `application/core/session_lifecycle.go`（149）、`session_release.go`（133）、`session_draft.go`（215）、`session_fork.go`（151）、`session_meta.go`（41）、`session_ctx.go`（12）、`session_pending_tail.go`（62）、`session_queue_recover.go`（60） | `middle/switch/` | 搬 |
| `application/core/service_scheduler.go`（257）、`service_queue.go`（110）、`service_release_window.go`（34）、`async_completion.go`（208） | `middle/switch/` | 搬 |
| `application/core/service_interaction.go`（359） | `middle/switch/interaction/` | 搬 |
| `application/core/workspace_usecase.go`（360） | `middle/switch/workspace/` | 搬 |
| `application/core/error_codes.go`（84）、`error_presentation.go`（105） | `middle/view/errors/` | 搬 |
| `session/`（6 文件 957 行） | `middle/switch/sessionusecase/` | 搬（消掉 `application ⇄ session` 双向） |
| `seelebridge/runtime_project_board.go`（50）、`runtime_project_board_read.go`（139） | `middle/view/board/` | 搬 |
| `seelebridge/runtime_session_release.go`（65） | `middle/switch/` | 搬 |

### 2.5 后端 · engine

| from | to | 动作 |
|---|---|---|
| `seelebridge/runtime.go`（702）、`runtime_bundle.go`（45）、`runtime_deps.go`（67）、`runtime_live.go`（223） | `backend/engine/assemble/` | 搬（拓扑装配 + 逆序关机） |
| `application/core/chat.go`（964）、`application/core/chat/`（181） | `backend/engine/react/` | 搬 |
| `application/core/task_context/`（11 文件 3802） | `backend/engine/task/` | 搬 |
| `application/core/context_runtime/`（9 文件 2635）、`context_compact.go`（305）、`context_compact_gate.go`（92）、`history_safety.go`（207） | `backend/engine/context/` | 搬 |
| `seelexctx/`（根 14 文件 3550）与 `seelexctx/{compactor,memory,merger,provider,snapshot,tokens,search,lifecycle}` | `backend/engine/context/seelectx/` | 搬 |
| `seelebridge/runtime_context.go`（270）、`runtime_compaction_index.go`（166） | `backend/engine/context/` | 搬 |
| `application/core/prompt_layer/`（194）、`application/prompt/stack.go` | `backend/engine/prompt/` | 搬 |
| `seelebridge/runtime_role_prompt.go`（136） | `backend/engine/prompt/` | 搬 |
| `internal/promptassets`（286） | `backend/engine/prompt/assets/` | 搬 |
| `application/core/input_router/`、`service_input.go`（295）、`input.go`（117）、`command.go`（206）、`completion.go`（245）、`context_control/`（51） | `backend/engine/input/` | 搬 |
| `seelebridge/runtime_tools.go`（267） | `backend/engine/toolregistry/` | 搬 |
| `seelebridge/runtime_account.go`（216） | `backend/engine/account/` | 搬 |
| `seelebridge/runtime_session.go`（120）、`role_host_session.go`（17） | `backend/engine/session/` | 搬 |
| `application/core/session_runtime/`（13 文件 2315） | `backend/engine/session/` | 搬（会话运行期事实所有者：持久化协调 / 目录 / 项目绑定，一律经 store 端口写） |
| `seelebridge/scheduler`（`scheduler.go` 778 + `persistence.go` 121） | 拆：actor 与触发判定 → `backend/engine/schedule/`（**已裁决**）；`FileStore` → `store/schedule/`；`Persistence` 接口 → `contract/` | 拆搬 |
| `application/core/service_scheduler.go`（257） | 拆：方法面与每次触发的参数默认值 → `backend/engine/schedule/`；参数判据 → `backend/limit/{effort,loop}/` | 拆搬 |
| `sessionstore/schedule_events.go`（44） | `store/schedule/`（`schedule.*` 结构事件通道随迁） | 搬 |
| `seelebridge/runtime_goal_tl.go`（172） | `backend/multiagent/goal/` | 搬 + 反向依赖改端口（§3.4） |
| `seelebridge/{events.go,events_unified.go,session_telemetry.go,request_log.go}`（415） | `backend/engine/obs/` | 搬 |
| `seelebridge/json_object.go`（149） | `internal/jsonobj/` | 搬 |
| `application/core/resume/`（263）、`service_subagent_resume.go`（52） | `backend/multiagent/resume/` | 搬 |
| `plugin/`（7 文件 1667）、`skill/`（2 文件 453）、`mcpstack/`（8 文件 714） | `backend/engine/{plugin,skill,mcp}/` | 搬 |
| `seelebridge/{plugin(237),mcp(385),account(561)}`、`seelebridge/internal/config`（259） | `backend/engine/` 对应子目录 | 搬 |
| `application/core/skill_activate.go`（33）、`skill_context.go`（128） | `backend/engine/skill/` | 搬 |

### 2.6 后端 · tool

| from | to | 动作 |
|---|---|---|
| `seelebridge/tools`（14 文件 4576） | 拆：注册表 / 路由 / 异步作业 / job 契约 / `registry_state.go` → `backend/tool/registry/`、`backend/tool/jobs/`；权限三件套与 `tool_meta.go` 见 §2.8 | 拆搬 |
| `seelebridge/tools/computer`（31 文件 5305）、`tools/computer/mcp`（877） | `backend/tool/computer/` | 搬（三平台原样） |
| `seelebridge/tools/websearch`（72）、`seelebridge/search`（643） | `backend/tool/websearch/` | 搬 |
| `seelebridge/plan`（13 文件 2526）、`seelebridge/runtime_plan.go`（351） | `backend/tool/plan/` | 搬 |
| `seelebridge/worktree`（830） | `backend/tool/worktree/` | 搬 |
| `seelebridge/task`（3 文件 1120） | `backend/tool/task/` | 搬 |
| `seelebridge/node`（5 文件 804） | `backend/tool/node/` | 搬 |
| `seelebridge/fork`（4 文件 553） | `backend/tool/fork/` | 搬 |
| `seelebridge/scheduler`（2 文件 966） | `backend/tool/jobs/`（待核实，见 §3.6） | 搬 |
| `seelebridge/{fs,attachment,imageattach,multimodal}`（≈430）、`runtime_image.go`（150） | `backend/tool/io/` | 搬 |
| `seelebridge/internal/{stream,mapper,model,telemetry,docker}`（≈700） | `backend/tool/internal/` | 搬 |
| `seelebridge/runtime_async.go`（55）、`jobs_events.go`（174） | `backend/tool/jobs/` | 搬 |
| `seelebridge/review_files.go`（100）、`application/core/review_files.go`（129）、`reference_tools.go`（231） | `backend/tool/review/`、`backend/tool/reference/` | 搬 |
| `application/core/tool_hooks.go`（396）、`plan_tools.go`（604） | `backend/tool/` | 搬 |

### 2.7 后端 · multiagent

| from | to | 动作 |
|---|---|---|
| `seelebridge/teamwork`（6 文件 1974）、`runtime_teamwork*.go`（13 文件 ≈2700）、`teamwork_job_signals.go`（93） | `backend/multiagent/teamwork/` | 搬（**冻结，见 §4/§5**） |
| `application/core/teamwork_service.go`（45） | `backend/multiagent/teamwork/` | 搬 |
| `seelebridge/session`（5 文件 1688）、`runtime_subagent_{recovery,resume,session}.go`（698） | `backend/multiagent/subagent/` | 搬 |
| `seelebridge/internal/actor`（122） | `backend/multiagent/actor/` | 搬 |
| `application/core/role_session.go`（148） | `backend/multiagent/team/` | 搬 |
| `seelebridge/runtime_role_plugins.go`（288）、`runtime_role_turn.go`（493） | `backend/multiagent/team/` | 搬 |
| `seelebridge/runtime_teammate_session_live.go`（113） | `backend/multiagent/teamwork/` | 搬 |
| `application/core/goal/`（16 文件 3013）、`goal_coordinator.go`（367）、`goal_service.go`（307）、`goal_team_recorder.go`（139）、`goal_work_summary.go`（128）、`goal_read_session.go`（17） | `backend/multiagent/goal/` | 搬 |
| `application/core/agentteam/`（227） | `backend/multiagent/team/` | 搬 |
| `seelebridge/workunit`（4 文件 595）、`workunit_*.go`（5 文件 1065）、`runtime_unit_recovery.go`（99） | `backend/multiagent/workunit/`（归属待裁决，见 §3.6） | 搬 |
| `seelebridge/resume_notes.go`（67） | `kernel/resume/`（与 `workunit.RecoveryNote` 正文同处） | 搬 |

### 2.8 后端 · limit

| from | to | 动作 |
|---|---|---|
| `seelebridge/security`（7 文件 909） | `backend/limit/gate/` | 搬 |
| `seelebridge/tools/permission_policy.go`（730）、`permission_tiers.go`（56）、`policy.go`（123） | `backend/limit/policy/` | 搬 |
| `application/approval`（265） | `backend/limit/approval/` | 搬 |
| `application/prompt/effort.go`（ReActBudget / MaxLoops / EffortManager） | `backend/limit/{loop,effort}/` | 拆搬（§1.5） |
| `application/core/service_knobs.go`（180）、`internal/limits`（33） | `backend/limit/` | 搬 |
| `seelexctx/limits.go`（476） | `backend/limit/limits.go` | 搬 |
| `application/core/session_permission_tier.go`（95） | `backend/limit/tier/` | 搬 |
| `application/core/service_account_window.go`（45） | `backend/limit/accountwindow/` | 搬 |
| `seelebridge/fork/tool.go` 的 `LoopLimitCeiling` 校验 | `backend/limit/loop/` | 并入判据 |
| `runtime_limits_overlay.go`（119） | `backend/limit/overlay/` | 搬 |

### 2.9 底座 · store

| from | to | 动作 |
|---|---|---|
| `sessionstore/{sessionstore.go,json_layout.go,jsonl_io.go,module_heads.go,data_root_lock.go,lifecycle.go,retention.go,storage_settings.go,pending_tail.go,provider_wire.go,router_storage.go,file_shared_read_*.go,process_alive_*.go}` | `store/root/` | 搬 |
| `sessionstore/session_granular.go`（659） | `store/session/granular.go` | 搬（会话句柄转正） |
| `sessionstore/{message_rows.go,durable_history.go,conversation.go,checkpoint_store.go,compact_frames.go,compaction_records.go,context_state.go,event_store.go,event_range.go,fork_store.go,history_resume_readers.go,material_cache.go,paragraphs.go,queue_consume.go,runtime_api.go,schedule_events.go,search_index.go,session_context.go,session_meta.go,stack_channel.go,stack_journal.go,stack_journal_json.go,structural_events.go,unit_record_writer.go,wire_assembler.go,big_tool_result.go,attempt_cache.go,session_storage.go}` | `store/session/` | 搬 |
| `sessionstore/{board.go,project_board.go,project_board_markdown.go,project_known.go,project_record.go}`（≈1580） | `store/board/` | 搬 |
| `sessionstore/{team_registry.go,teamwork.go,teamwork_items.go}`（≈970） | `store/team/` | 搬 |
| `sessionstore/{role_session.go,role_session_router.go}`（750） | `store/role/` | 搬 |
| `sessionstore/node_session_store.go`（237） | `store/node/` | 搬 |
| `sessionstore/media.go`（466） | `store/media/` | 搬 |
| `sessionstore/legacy_storage.go`（257） | `store/legacy/` | 搬 |
| `workspace/`（8 文件 1616） | `store/workspace/` | 搬 |
| `sessionstore/router_storage.go` 的 `seelexctx/lifecycle` import | —— | 删，改 `contract.LifecycleSinkPort` 反转 |

### 2.10 装配与低层

| from | to | 动作 |
|---|---|---|
| `internal/adapters/`（7 文件 2454） | `wiring/adapters/` | 搬 |
| `main.go` 的 ~30 个 `register*Tool/register*Capability`（约 1000 行） | `wiring/registry/` | 搬 |
| `register_goal_tools.go`（72）、`skill_activate_tool.go`（69） | `wiring/registry/` | 搬 |
| `main.go` 其余 | `main.go` | 只留 flag / 启动 / 关机 / 前端选择 |
| `main_windows.go`、`main_unix.go`、`main_pprof*.go`、`internal/winhide` | 不动 | —— |
| `internal/{bootseed,buildinfo,frontmatter,testutil}` | 不动 | —— |
| `internal/toolrecord`（250，未跟踪新包）、空的 `internal/toolmetrics` | `store/record/`；空目录删 | 搬 + 删 |
| `e2e/`、`e2e/scenario`（7 文件） | 不动 | 只改 import |
| `seelebridge/ports.go`（792，自述"逐行委托与兼容别名"） | —— | **删** |
| 9 组共 19 处 `type X = ...`（`seelebridge`、`plan`、`task`、`scheduler`、`session`、`internal/model`、`tools/computer`、`worktree`、`node`） | —— | **删**（改用上游类型或新层类型） |
| `seelebridge/internal/mapper`（16） | —— | **删** |
| `application/core/aliases.go`（204） | —— | 迁完由门面取代，**删** |

### 2.11 巨包文件级明细

#### 2.11.1 `application/core` 根包 69 个生产文件

| 文件（行数） | 目标 |
|---|---|
| `chat.go`(964) | `backend/engine/react/` |
| `task_context/`、`context_runtime/`、`prompt_layer/`、`session_runtime/`、`internal/*` 子包 | 见 §2.5–2.8 对应行 |
| `goal_*.go`(5 文件 950) | `backend/multiagent/goal/` |
| `agentteam/` | `backend/multiagent/team/` |
| `service.go`(129)、`service_assembler.go`(272)、`service_components.go`(57) | `backend/engine/assemble/` |
| `service_state.go`(98) | `kernel/state/` |
| `service_snapshot.go`(280)、`view_state/`、`subagent_view/` | `middle/view/` |
| `service_interaction.go`(359)、`service_input.go`(295) | `middle/switch/interaction/`、`backend/engine/input/` |
| `service_knobs.go`(180) | `backend/limit/` |
| `service_queue.go`(110)、`service_scheduler.go`(257)、`service_release_window.go`(34)、`service_account_window.go`(45) | `middle/switch/`、`backend/limit/accountwindow/` |
| `session_*`（catalog/cold_read/ctx/draft/fork/history/input_index/lifecycle/meta/pending_tail/permission_tier/queue_recover/release/scope/storage） | `middle/residency/`、`middle/switch/`、`backend/limit/tier/`、`store/` |
| `work_table.go`(833)、`work_table_async.go`(237)、`worktable/` | `middle/view/worktable/` + `middle/switch/` |
| `project_board.go`(662) | `middle/view/board/` |
| `plan_tools.go`(604)、`tool_hooks.go`(396)、`reference_tools.go`(231)、`review_files.go`(129)、`tool_failures.go`(54) | `backend/tool/`、`middle/view/` |
| `skill_activate.go`(33)、`skill_context.go`(128) | `backend/engine/skill/` |
| `teamwork_service.go`(45) | `backend/multiagent/teamwork/` |
| `workspace_usecase.go`(360) | `middle/switch/workspace/` |
| `async_completion.go`(208) | `middle/switch/` |
| `aliases.go`(204) | 删（由门面取代） |
| `archive_session.go`(74) | `middle/residency/` |
| `command.go`(206)、`completion.go`(245)、`input.go`(117) | `backend/engine/input/` |
| `compressed_turn.go`(179) | 待核实（§3.6） |
| `content_lru.go`(191)、`resident_lru.go`(97)、`snapshot_budget.go`(48) | `middle/residency/` |
| `context_compact.go`(305)、`context_compact_gate.go`(92)、`history_safety.go`(207) | `backend/engine/context/` |
| `diagnostics.go`(82)、`perf_stats.go`(44)、`runtime_projection.go`(44) | `middle/view/` |
| `error_codes.go`(84)、`error_presentation.go`(105) | `middle/view/errors/` |
| `fault_guard.go`(46) | 待核实（§3.6） |
| `history_search.go`(48) | `middle/switch/history/` |
| `limits.go`(20) | `backend/limit/` |
| `role_session.go`(148) | `backend/multiagent/team/` |
| `session_history.go`(895) | `middle/switch/history/` |
| `session_scope.go`(542) | 待核实（§3.6） |
| `window.go`(46) | `middle/residency/` |

#### 2.11.2 `seelebridge` 根包 52 个生产文件

装配组：`runtime.go`(702)、`runtime_bundle.go`(45)、`runtime_deps.go`(67)、`runtime_live.go`(223)、
`runtime_session.go`(120)、`role_host_session.go`(17) → `backend/engine/assemble|session/`。
团队组：`runtime_teamwork*.go`(13 文件 ≈2700)、`teamwork_job_signals.go`(93)、
`runtime_teammate_session_live.go`(113) → `backend/multiagent/teamwork/`。
子代理组：`runtime_subagent_*.go`(3 文件 698) → `backend/multiagent/subagent/`。
工作单元组：`workunit_*.go`(5 文件 1065)、`runtime_unit_recovery.go`(99) → `backend/multiagent/workunit/`。
角色组：`runtime_role_turn.go`(493)、`runtime_role_plugins.go`(288)、`runtime_role_prompt.go`(136) →
`backend/multiagent/team/`、`backend/engine/prompt/`。
域装配组：`runtime_tools.go`(267)、`runtime_plan.go`(351)、`runtime_context.go`(270)、
`runtime_account.go`(216)、`runtime_async.go`(55)、`runtime_computer.go`(36)、`runtime_image.go`(150)、
`runtime_compaction_index.go`(166)、`runtime_goal_tl.go`(172)、`runtime_project_board.go`(50)、
`runtime_project_board_read.go`(139)、`runtime_session_release.go`(65) → 各域目标（§2.5–2.8）。
观测/事件组：`events.go`、`events_unified.go`、`jobs_events.go`、`session_telemetry.go`、`request_log.go`(≈620) →
`backend/engine/obs/`、`backend/tool/jobs/`。
其他：`ports.go`(792) **删**；`json_object.go`(149) → `internal/jsonobj/`；
`resume_notes.go`(67) → `kernel/resume/`；`review_files.go`(100) → `backend/tool/review/`。

#### 2.11.3 `sessionstore` 54 个生产文件

见 §2.9 的分组表：`store/root`（16）、`store/session`（29）、`store/board`（5）、`store/team`（3）、
`store/role`（2）、`store/node`（1）、`store/media`（1）、`store/legacy`（1）。
（`router_storage.go` 归 `store/root` 并删掉对 `seelexctx/lifecycle` 的 import。）

#### 2.11.4 `seelexctx` 27 个生产文件

根包（14 文件 3550）与 `compactor/`、`memory/`、`merger/`、`provider/`、`snapshot/`、`search/`、
`tokens/`、`lifecycle/` → `backend/engine/context/seelectx/`；`limits.go`(476) → `backend/limit/limits.go`；
`search/` 被 `gui` 直接 import 的那条边 → 改 `contract.SearchPort`。

### 2.12 派生数据与钩子（三条通道）

#### 2.12.1 定时／周期任务

| from | to | 动作 |
|---|---|---|
| `seelebridge/scheduler/scheduler.go`（778）的 actor 与触发判定 | `backend/engine/schedule/`（**已裁决**） | 搬 |
| `seelebridge/scheduler/persistence.go`（121）的 `FileStore`、`ScheduledTaskRecord` | `store/schedule/` | 搬 |
| `scheduler.Persistence` 接口 | `contract/`（`FileStore` 保持结构满足，装配处写编译期断言） | 搬 |
| `application/core/service_scheduler.go`（257）方法面与触发参数默认值 | `backend/engine/schedule/` | 搬 |
| 同文件的执行参数判据（`withScheduledRunDefaults` / `scheduledRunParams`） | `backend/limit/{effort,loop}/`（并入唯一判据） | 并入 |
| `sessionstore/schedule_events.go`（44） | `store/schedule/`（`schedule.*` 结构事件） | 搬 |
| `seelebridge/security` 的 command 白名单判定 | `backend/limit/policy/`（**以用户正在进行的 security 改动为准**） | 搬 |
| `main.go` 的 `scheduledPromptExecutor`(1014)、`registerScheduledTaskCapability`(1043) | `wiring/registry/` | 搬 |
| 门面 `ScheduleRegister/Cancel/Update/Fire/ScheduleTask/CancelScheduledTask` | 门面 D 组（已在 §1.3） | 不动 |

依赖事实（已核实）：`seelebridge/scheduler` 的**唯一 import 方是 `seelebridge` 根包**，
且它已通过 `Persistence` 接口、`PromptExecutor` 回调、`SetObserver(func())` 三处预留了端口与钩子——迁移是接线重排，不是行为改造。

#### 2.12.2 工具记录看板（成功/失败计数 + 失败原因）

| from | to | 动作 |
|---|---|---|
| `internal/toolrecord`（250，**未跟踪新包**）的 `Ledger` | `store/session/` 的**会话级 module**（**已裁决**：随会话存储、随会话删除/归档走；不再固定写 `<数据根>/tool-record.jsonl`） | 改落点 + 改粒度 |
| 同包的折叠逻辑（内存读数、原因归并） | 留在读侧内存（`middle/view/record/`） | 搬 |
| 追加/残尾机制 | `store/ledger/`（与 schedule 共用机制） | 提取 |
| **写路径（新增）** | 工具完成段只写**会话级内存批次**；在会话提交点（`publishDraftTail` / 回合终态 / 会话释放）**一次落盘**——复用 `chat.StreamBatcher` + `flushStreamBatcherFor` 的边界与 `publishModuleHead` 的原子发布 | 消 fsync 写放大（现状每调用一次 `OpenFile+Write+Sync`） |
| `core.ToolRecordLedger` 端口 | `contract/`（写侧 `Record` 变批次语义；读侧读数不变） | 搬 + 改签名 |
| `application/core/tool_record.go` 的判据（`toolStatusFailed` / `toolRecordReason` / `recordToolOutcome`） | `backend/tool/record/`（唯一判据，随工具完成段） | 搬 |
| `application/contract/dto/tool_record.go`（`ToolRecordBoard` / `ToolRecordStat` / `ToolRecordReason` + `ToolReasonVocabulary()`） | `contract/dto/` | 搬 |
| `Application.ToolRecordBoard()` / `SetToolRecordLedger` / `SetToolRecordEnabled` 的读面 | 门面 D/E 组 + `middle/view/record/` | 拆搬 |
| `Deps.ToolRecordStore`（计数开关，落 `<数据根>/runtime-limits.json`） | `store/root`（覆盖层；开关是**进程级**设置，不随会话粒度改变） | 搬 |
| 批次未落盘时的崩溃口径 | 未提交批次**可丢、不得隐式提升**（与消息通道草稿同款） | 沿用 |
| 看板读数的范围（本会话 / 整机） | —— | **待裁决 V10** |
| `tui/tool_record_panel.go`、`gui` 同源读法 | 前端（不动） | 不动 |
| 编译期断言 `var _ core.ToolRecordLedger = (*toolrecord.Ledger)(nil)`（`main.go`） | `wiring/` | 搬 |
| 空目录 `internal/toolmetrics`（文件已迁走） | —— | **删**（卫生项） |

#### 2.12.3 项目看板投影

| from | to | 动作 |
|---|---|---|
| `application/core/project_board.go`（662，映射 + 同步器 + 只读取数口，零判定） | `middle/view/board/` | 搬 |
| `seelebridge/runtime_project_board.go`（50）、`runtime_project_board_read.go`（139） | `middle/view/board/` | 搬 |
| `sessionstore/{project_board.go(609),project_board_markdown.go(130),project_known.go(166),project_record.go(422),board.go(255)}` | `store/board/` | 搬 |
| `core.ProjectBoardSink` + `SetProjectBoardSink`（单槽注入） | `contract/` 端口 + 门面注入口 | 搬 |
| `main.go` 的 `projectBoardSinkFor`(1769)、`projectBoardHookMode`(1801)、`core.ProjectBoardHookModes`（唯一词表，拼错显式启动失败） | `wiring/registry/` + `contract/` 词表 | 搬 |
| `main.go` 的 `registerProjectBoardReadTool`(842)、`registerProjectRefreshTool`(927)、`projectBoardReadRows`(913) | `wiring/registry/` | 搬 |
| 取数四口（`WorkTableRowsFor` / `ProjectBoardTeamPlanFor` / `ProjectBoardTeamStateFor` / `ProjectRootFor`） | 保留在应用侧只读面（随 §2.4 归位） | 不动 |
| 落点 `<项目根>/.seelex/board/<会话 ID>/` + `BOARD.md` + `project-known.md` | **不变**（公开投影是产品意图） | 不变 |

---

## 3. 当前状态

### 3.1 规模（已核实）

| 项 | 数值 |
|---|---|
| 包 | 85 |
| 生产文件 / 测试文件 | 520 / 729（另 3 个外部测试包） |
| 生产行 / 测试行 | 104,370 / 125,803 |
| 包 README 覆盖 | 85 / 85 |
| 本次迁移影响面 | ≈62,000 生产行（core 13,290 + sessionstore 15,827 + seelebridge 约 33,000） |

### 3.2 复杂度集中在少数节点（已核实）

| 类型 | 方法数 | 导出 | 未导出 | 说明 |
|---|---|---|---|---|
| `application/core.Service` | 419 | 134 | 285 | 56/69 个文件在给它挂方法 |
| `seelebridge.Runtime` | 352 | 186 | 166 | 顶层装配体 |
| `application/core/task_context.Coordinator` | 228 | 106 | 122 | |
| `sessionstore.storeEngine` | 182 | 0 | 182 | 全未导出 |
| `sessionstore.Router` | 130 | 117 | 13 | 目标是降级为数据根持有者 |
| `gui.Bridge` | 92 | 73 | 19 | 前端唯一越层点 |
| `sessionstore.jsonRepository` | 82 | 41 | 41 | |
| `internal/adapters.RuntimePort` / `SessionPort` / `EnginePort` | 77 / 69 / 54 | 全导出 | 0 | 装配层端口实现 |

推断（成立条件：不重写行为）：**边界不能沿公共 API 切**——`Service` 68% 的方法是未导出的，
`storeEngine` 100% 未导出，`*serviceState` 被 10 个文件共享；因此 `application/core` 的切分必须先建
共享内核（`kernel/state`）再下沉域状态，这与
[`../2026-08-22-application-split/design.md`](../2026-08-22-application-split/design.md) 的结论一致
（"不能按文件直接搬；必须先建立共享内核，再把域状态下沉，跨域读写全部走消费方接口"）。

### 3.3 依赖图实况（已核实）

主干方向基本单向：`前端 → application → {seelebridge, sessionstore, seelexctx, session, workspace}`。
需要纠正的边：

| 违规边 | 具体落点 | 规模 |
|---|---|---|
| `gui → sessionstore`、`→ seelexctx`、`→ seelexctx/search`、`→ seelebridge` | 仅 `gui/bridge.go` | 1 个生产文件（+2 个测试） |
| `seelebridge → application/core/goal`、`core/prompt_layer`、`core/resume`、`application/model` | `runtime_goal_tl.go`、`runtime_role_turn.go`、`runtime_role_plugins.go`、`runtime_subagent_resume.go` | 4 个生产文件 |
| `sessionstore → seelexctx/lifecycle` | `router_storage.go` | 1 个生产文件 |
| `application/core → internal/promptassets` | `core`、`core/prompt_layer`、`application/prompt` | 3 处 |
| `application ⇄ session` | 双向各 2 条边 | 由 §2.4 的合并消除 |
| `plugin/skill ⇄ internal` | 低层工具被双向引用 | 由 §2.10 的 `wiring/` 归位消除 |

已核实的共享契约枢纽：`application/contract/dto` 被 **32 个包** import（≈2672 处选择器引用）；
`application/contract` 已有 **24 个端口接口**，可直接复用而非新造。

### 3.4 既有资产（可直接复用，不要重造）

| 资产 | 位置 | 对本次迁移的意义 |
|---|---|---|
| 门面保形层 | `application/application.go` 自述"consumers should keep importing this package" | 前端零改动的机制基础 |
| 会话粒度存储 | `sessionstore/session_granular.go` + `SessionGranularStore` + `session/ports.go` 的 `StorePort` | 你要的"每 sessionID 一个 store + 锁自持"已经存在 |
| 循环预算与收尾 | `application/prompt/effort.go`、`core/chat.go` 的 `finalizeReActBudget`、`core/error_codes.go` 展示面 | 预警是增量，不是重写 |
| 交接容器唯一份 | `seelebridge/resume_notes.go` 注释记录"恢复说明此前写了两遍…容器只有这一份"；正文 = `workunit.RecoveryNote` | 交接不得造第三份 |
| 工具中间件挂点 | Seele `WithMetaMiddleware` / `MetaMiddleware` / `chainMeta`；`seelebridge/tools/registry_state.go` | limit 中间件链的原生落点 |
| 权限簇属唯一事实源 | `seelebridge/tools/tool_meta.go` → `DefaultPermissionGroupList` | skill 声明不得写第二份簇属 |
| 文档 ↔ 代码机械防线先例 | `plugin_doc_sigil_test.go` | skill/注册表守卫的模板 |
| 重构先例 | `docs/2026-08-10-runtime-split`、`2026-08-12-module-split`、`2026-08-14-decoupling`、`2026-08-22-application-split`、`2026-08-30-session-resource-refactor`、`2026-09-01-session-thin-wrapper`、`2026-09-02-session-subsystem-remediation` | 物理归位已做过；本轮要解决的是"搬完之后判据仍分散" |

### 3.5 环境事实

- 依赖：`github.com/RedHuang-0622/Seele v0.3.4`（独立 module、独立 tag），`vendor/` 已落盘（2346 个文件）。
- 构建标签：76 个文件带 `//go:build`，含平台、`gui`、`pprof`、`race` 与十余个一次性探针 tag。
- 影子树：`tmp/head-src` 存有旧源码完整副本（1298 个 `.go`），会污染全局搜索与批量改名。
- 工作区现状：存在未提交改动（含正在进行的 teamwork 修改）。
- 88 个一次性工作包目录已在 `docs/` 下，本次方案同属该序列。

### 3.6 待核实清单（实施前必须补）

| ID | 待核实项 | 影响 |
|---|---|---|
| V1 | `application/core/compressed_turn.go`、`fault_guard.go`、`session_scope.go` 的域归属 | 决定它们进 `middle` 还是 `engine` 还是 `kernel` |
| V2 | ~~`seelebridge/scheduler` 归属~~ | **已裁决**：`backend/engine/schedule/`（见 §2.12.1） |
| V3 | `seelebridge/workunit` 的最终归属（现被 teamwork/subagent/tool/worktree/node/telemetry 6 处引用） | 决定放 `backend/multiagent/workunit` 还是独立 `backend/workunit` |
| V4 | `sessionstore/{board,team,role,node}` 与 `store/session` 的读侧边界 | 决定第二刀拆 `store/reads` 的必要性 |
| V5 | `SessionGranularStore` 与 `Router` 的现有并发关系（哪些路径仍走 Router 写） | 决定 M3 的迁移顺序与并发用例范围 |

待裁决（本轮讨论中，附建议，未获确认前不得按实现方理解落定）：

| ID | 待裁决项 | 建议 |
|---|---|---|
| V6 | `store/ledger`（统一追加/残尾机制）+ `store/{schedule,session-module}`（各自重放语义）的切法是否接受；是否进一步合并 `toolrecord` 与 `scheduler.FileStore` 的语义 | 接受切法；**不合并语义**（源码注释已按 §8.2 论证两者不像） |
| V7 | 定时任务状态同时进 `Snapshot.ScheduledTasks`（`model/state.go:368`）与 `ProcessRuntime.ScheduledTasks`（`:917`）——统一成哪一口径 | 全局资产统一**只进程级**；是否改 `Snapshot` 属会话快照协议，必须由用户裁决 |
| V8 | 项目看板投影**写侧**归 `middle/view/board/` 还是 `backend/engine/board/` | `middle/view/board/`（可删可重建的派生件，与 worktable 同源） |
| V9 | `seelebridge/security` 的终局形状 | 以用户正在进行的 `security` 改动为准，迁移时以其落地结果重新点名 |
| V10 | 工具记录改会话粒度后，**看板读数是本会话还是整机**：(a) 会话级台账 + 全局汇总，(b) 纯会话级看板 | 若"这台机器上哪个工具最该优化"仍是产品口径 → 选 (a)；选 (b) 则"不进 `Snapshot`"的原有理由失效，需重新裁决是否进快照 |

---

## 4. 里程碑（5 个）

里程碑按"退出条件"推进，不带时间承诺；每个里程碑结束时必须产出**删除清单**（原位置 → 唯一位置）。

### M1 契约先行与卫生

- 目标：把契约层提为顶层零依赖包，并让依赖方向可机械断言。
- 动作：P0 清 `tmp/head-src` 影子树并防止再生；`application/contract` + `dto` → `contract/`；
  加 I1/I3/I4 断言测试；`gui/bridge.go` 换 4 条越层 import 并加 I2 断言。
- 前置：无（M1 不涉及行为）。
- 退出条件：契约层依赖闭包 = stdlib；前端三入口依赖闭包 ⊆ 门面；前端行为零变化；
  `go build ./...` 与 `go test ./... -count=1` 通过；前端 4 条越层边消失。
- 回退：契约层为纯别名搬迁，回退 = 还原 import 路径。

### M2 判据归一（limit）

- 目标：limit 成为唯一判据处，并成为工具链上的有序中间件。
- 动作：5 处循环上限判据并入 `backend/limit/loop/`；`backend/limit/{gate,policy,approval,tier}` 归位；
  中间件链按 §1.4 定序；loop 预警与交接按 §1.5 接既有 `ReActBudget` 与 `resumeNotes`；
  定时任务的 command 白名单与触发参数判据并入 limit（§2.12.1）。
- 前置：M1；V3 之外无阻塞（本里程碑不搬 multiagent）。
- 退出条件：循环判据只剩一处；同一输入在旧路径与新路径产生同一判定（跨实现一致性用例）；
  中间件链顺序有测试钉住；预警触发的总结只走 `workunit.RecoveryNote`。
- 回退：保留旧判据函数作为薄封装（限期），不双写判定。

### M3 store 归位与粒度确认

- 目标：`store/` 成为共享底层，会话句柄唯一、三层锁粒度显式。
- 动作：§2.9 的 54 文件归位；`router_storage.go` 的 `seelexctx/lifecycle` 改端口反转；
  `SessionGranularStore` 转正为唯一会话句柄；工具记录落会话级 module 并改为**提交点批次落盘**（§2.12.2）；
  确认 V4/V5/V10 并记录结论。
- 前置（补充）：现有每调用一次 `OpenFile+Write+fsync` 的写路径必须先替换为批次路径，
  再谈落点搬迁——否则搬迁会把写放大一起带进新结构。
- 前置：M1（契约层已可用）。
- 退出条件：**持久化格式、数据根路径、锁文件、module head 语义字节级不变**；
  会话恢复、并发与切换类回归（`repro_session_*`）全绿；`store/` 无向上依赖；
  写放大有可复现的前后对照（同一会话同一回合的工具调用次数 ↔ 实际 fsync 次数），
  且未提交批次的崩溃语义有测试钉住（可丢、不得隐式提升）。
- 回退：按包回滚 import 路径；数据文件不动，因此无需回滚数据。

### M4 tool / engine / multiagent 板块归位

- 目标：解散 `seelebridge` 顶层，按板块落位。
- 动作：§2.5–2.8、§2.12；每子包一个 commit；`ports.go`、19 处别名、`internal/mapper` 删除；
  `seelebridge/scheduler` 的 actor 落 `backend/engine/schedule/`（已裁决）、定义态落 `store/schedule/`；
  工具记录台账落 `store/record/` 并提取 `store/ledger/` 机制；空目录 `internal/toolmetrics` 删除。
- 前置：M2（判据已归一，工具迁移不再搬动权限判定）；M3（store 句柄稳定）。
- 退出条件：`seelebridge` 目录消失；`Runtime` 的 352 方法与 `tools` 的 4576 行按域分到各板块且无跨板块回指；
  三条派生通道（定时任务 / 工具记录 / 项目看板）各有唯一落点且钩子接口在 `contract/`；
  删除清单逐条列出"文件:行 → 新唯一位置"；`go build -tags "gui,desktop,production" ./...` 通过。
- 回退：按子包回滚（每个 commit 独立）。

### M5 中间层切割与装配瘦身

- 目标：中间层独立成层，`Service` 巨型门面收窄，装配根变薄。
- 动作：§2.4 的中间层归位（含 `middle/view/board`、`middle/view/record`）；`application/core` 剩余文件按域切分
  （先 `kernel/state`，再域）；`main.go` 的 ~1000 行注册段迁 `wiring/registry/`；
  门面收窄为五组窄接口并加 I5 校验。
- 前置：M4；teamwork 域解冻（见 §5）。
- 退出条件：`Service` 导出方法面按门面五组收窄且每组建模测试通过；`main.go` < 400 行；
  中间层不写会话事实（断言）；`middle` 与 `backend` 之间无环。
- 回退：`Service` 拆分以"同包内先分文件、再分包"两段走，第一段可独立回退。

---

## 5. 给长程任务的约束

以下约束对本方案的全部后续工作、以及任何 AI 子代理的自主拆分/实现同样生效。

1. **只做物理归位**。行为、协议、JSON 字段、环境变量、CLI flag、持久化格式一律不变；
   任何一次迁移的验收标准是"同输入同结果"，不是"测试变绿"。
2. **不碰数据**。`store/` 的 JSON v8 布局、数据根路径、`lock.owner` 语义、module head 发布点、
   seq 草稿规则字节级不变；涉及 `.seelex/`、`dist/`、`config/` 的任何操作先读根 `MEMORY.md`、
   中文预警（删什么、影响什么、能否恢复）、检查运行中进程、先备份再执行。
3. **删除清单是交付物**。每个里程碑必须列出"原位置（文件:行）→ 新唯一位置"；
   只有"剩下的实现只有一份 / 编译器钉住（编译期断言、结构满足）"才算证据（见 `AGENTS.md` §8）。
4. **一份判据 = 一处函数**。循环上限、权限分类、恢复说明、状态判定、回收动作都属此列；
   出现第二处即视为缺陷，先写会红的用例再修。
5. **不重造 Seele**。ReAct 循环、base 工具（read/write/edit/bash）、权限中间件、jobs、workplan、
   accountpool、seelectx 的原语都在上游；Seelex 侧只做装配、策略与产品语义。
6. **不新造第二份目标架构**。本方案与
   [`../2026-10-15-product-architecture-pre-design/global-review.md`](../2026-10-15-product-architecture-pre-design/global-review.md) §6
   是同一目标的两套命名（本方案：前端/中间层/后端三板块；该文档：Application / Bridge·Runtime / Tool·Job /
   read projection / capability control）。二者必须收敛为一份，不得并行维护。
7. **冻结使用中的域**。当前 `teamwork` 正在被修改：§2.7 中 `teamwork`、`runtime_teamwork*`、
   `workunit_*` 的搬迁推迟到该域改动落地且工作区干净之后；冻结期内不碰这些包的任何文件。
8. **一次一主题、可回滚**。禁止一次性大批量搬迁；每子包/每域一个 commit，只保留一个可审查主题；
   未获明确要求不 commit、不 push。
9. **先内核后域状态**。`application/core` 不得按文件直接搬；顺序固定为
   `kernel/state` → 域状态下沉 → 跨域读写走消费方接口。
10. **并发证据不可省**。`serviceState.mu`、`store` 三层锁、会话切换与释放窗口都有历史回归；
    任何触及这些路径的迁移必须跑对应 `repro_*` 用例；Windows 本地若 `CGO_ENABLED=0`，
    不得声称执行过 race。
11. **文档同步在同一 commit 内**。模块 README、`application/core/README-*.md` 分卷、
    `scripts/gen_core_readme_index.py` 的路径、以及 `docs/arch/` 中的稳定架构说明随代码同步更新；
    禁止把规划写成既成事实。
12. **待裁决项不自行补全**。§3.6 的 V1–V5 与产品向的 D01–D12 未获裁决前，
    实现方不得按自己的理解替用户决定归属或语义。
13. **验证矩阵按风险匹配**：纯搬迁至少跑受影响包的单元测试 + `go build ./...`；
    涉及 tag、平台、持久化的迁移追加 `go build -tags "gui,desktop,production" ./...` 与对应探针用例。
14. **不以改名冒充解耦**。包改名不改变依赖方向；依赖方向必须先由契约与断言钉住，再搬文件。
15. **本方案不含实现**。在用户明确授权前，不新增入口、不新建包目录、不删除任何文件。
