# M3 设计：store 归位与粒度确认（设计稿，未实施）

日期：2026-10-16
状态：**设计稿**。本文件不改动任何代码、测试、配置或持久化格式，也没有执行搬迁。
依据：[`plan.md`](plan.md) §1.7（store：会话粒度句柄与三层锁）、§2.9（底座 · store 的 from → to）、
§2.12.2（工具记录通道）、§4 M3（里程碑与退出条件）、§5（长程约束）。
通道：`m3_store_design`（子代理通道；本文件是它的交付物）。

事实口径：**已核实** = 本轮在 `G:\Program\go\seelex` 实跑命令读到的源码/依赖/行数；
**推断** = 由已核实证据推出的判断，附成立条件；**待裁决** = 必须由用户或复核者点头的事项。

---

## 0. 本设计要解决的问题

`plan.md` §4 M3 的目标一句话：**`store/` 成为共享底层，会话句柄唯一、三层锁粒度显式**。
其中两条前置/动作在本轮实测中已经落地（见 §1.6），因此本设计的重点不是"把它们再做一遍"，
而是把 M3 剩下的三件事设计清楚：

1. **归位**：`sessionstore`（57 个生产文件）与 `workspace/` 搬到 `store/`，数据与语义字节级不变。
2. **唯一句柄**：`SessionGranularStore` 转正为唯一会话句柄，`Router` 降级为数据根/项目索引/模块 head 持有者。
3. **依赖方向**：`store/` 无向上依赖，且这条红线由**机械闸**钉住（不是靠口头约定）。

本设计登记 4 条与 `plan.md` 的偏差（D-M3-1 ~ D-M3-4），每条都写明证据、代价与需要谁点头。
按 §5.12「待裁决项不自行补全」，凡属用户口径的事项，本文只给证据与建议，不代替裁决。

---

## 1. 事实基线（2026-10-16 实跑核实）

### 1.1 文件基数与 plan 清单的差集

| 项 | 实测（`Get-ChildItem` + 行数统计） | plan.md §2.9 |
|---|---|---|
| `sessionstore/` 生产文件 | **57** | 表中列 57 条 |
| 生产行数 | **19,460** | §3.1 记 15,827（口径不同，见下） |
| `sessionstore/` 测试文件 | 84（整目录 141 文件） | 未列（§2.11.3 只数生产文件） |
| `workspace/` 生产文件 | **8**，1,968 行 | 8 文件 1,616 行 |

**差集（已核实，机械比对）**：

- plan 有、仓库无：`sessionstore/session_storage.go`（仓库里只有
  `application/core/session_storage.go`，`rg --files -g 'session_storage.go'` 只命中后者）。
- 仓库有、plan 无：`sessionstore/tool_record.go`（129 行，2026-10-15 新增的工具记录落点）。
- 两者**净数相等**（57 = 57），但内容不同——**文件级落点表必须按工作区重新生成，不能照抄 §2.9**。

行数差异（19,460 vs 15,827）来源：§3.1 是方案撰写时的统计，此后 2026-10-15 的
工具记录与项目看板会话分目录等改动让 `sessionstore` 变长。**推断**（成立条件：以本轮实测为准）：
§2.9 的行数只可作为量级参考，不作为验收依据。

### 1.2 依赖方向实况

```text
$ go list -deps ./sessionstore | grep seelex
github.com/RedHuang-0622/seelex/seelexctx/lifecycle
github.com/RedHuang-0622/seelex/sessionstore
```

**已核实**：`sessionstore` 的全部 Seelex 内依赖只有 `seelexctx/lifecycle` 一条，
而上游依赖是 `Seele/types`、`Seele/seelectx/storage`、`Seele/workplan/core/types`
（`sessionstore/{json_layout.go,message_rows.go,legacy_storage.go,…}`），这些是上游 Seele，
不在"反向依赖"之列。

这一条的唯一使用点：

| 位置 | 内容 | 是否有生产调用者 |
|---|---|---|
| `sessionstore/router_storage.go:9` | `import "…/seelexctx/lifecycle"` | —— |
| `sessionstore/router_storage.go:73` | `var _ lifecycle.Storage[types.Message] = (*RouterStorage)(nil)` | —— |
| `sessionstore/router_storage.go:25` | `NewRouterStorage` | **无**（`rg -n 'NewRouterStorage'` 仅命中 `sessionstore/router_storage_test.go:18,:57` 与 `seelebridge/cold_restore_compaction_context_probe_test.go:164`） |

**结论（已核实）**：这条"store → seelexctx"的反向边目前只被测试消费；
生产路径上 `RouterStorage` 是死代码。依赖反转因此可以做到零行为风险（§2.4）。

### 1.3 反向依赖者规模

`rg -l 'seelex/sessionstore'`（排除 `vendor/`、`tmp/`）命中 **约 170 个文件**，
覆盖 `application/`、`seelebridge/`、`internal/adapters/`、`session/`、`seelexctx/`、
`main.go` 与约 20 个根目录测试文件。

其中"直接持有 `*sessionstore.Router`"的生产点是有限集合（§1.7 逐条列出），
这是"Router 降级"真正的动作面；其余绝大多数文件只是 import 了包名（类型/DTO 使用），
**包改名对它们是纯机械替换**。

另外，`e2e/facade_boundary_test.go` 的门面闭包允许清单里已经有
`github.com/RedHuang-0622/seelex/sessionstore` 一条（`facadeClosureAllowList`），
上限常量 `facadeClosureAllowListMax = 68`；契约层上限 `contractClosureAllowListMax = 28`。
这条清单是 **ratchet（只许收缩）**：新模块可以 +1（条件是"自身闭包 = 干净叶子"），
**等量替换不改变规模**。M3 的包切法必须与这条规则对齐（见 D-M3-1）。

### 1.4 包内耦合：拆子包的真实代价

把 `sessionstore` 的 57 个生产文件按 §2.9 的口径分组（root / session / board / team /
role / node / media / legacy），统计"root 组声明的**未导出**顶层符号被其它组引用"的数量：

| 组 | 文件数 | 组内引用 `store.` / `repository.` / `layout.` 的次数 |
|---|---|---|
| root（`sessionstore.go`、`json_layout.go`、`module_heads.go`、`pending_tail.go`、`data_root_lock.go`、`lifecycle.go`、`retention.go`、`storage_settings.go`、`jsonl_io.go`、`provider_wire.go`、`router_storage.go`、`file_shared_read_*.go`、`process_alive_*.go`） | 15 | 407 |
| role（`role_session.go`、`role_session_router.go`） | 2 | 61 |
| team（`team_registry.go`、`teamwork.go`、`teamwork_items.go`） | 3 | 56 |
| media（`media.go`） | 1 | 46 |
| node（`node_session_store.go`） | 1 | 26 |
| board（`board.go`、`project_board*.go`、`project_known.go`、`project_record.go`） | 5 | 20 |
| record（`tool_record.go`、`schedule_events.go`） | 2 | 12 |
| legacy（`legacy_storage.go`） | 1 | 6 |

**关键数字（已核实）**：root 组共有 **205** 个未导出顶层声明，其中 **57 个**被其它组直接引用
（含 `storeEngine`、`jsonRepository`、`sessionModuleLocks`、`storageModule`、`storageSettings`、
`publishModuleHead`、`commitModuleHead`、`readModuleHeadFile`、`sessionRoot`、`ensureLayoutGuide`、
`withRepository`、`hasDraft`、`derivedConversationMessages`、`selectEventTail`、`truncateCrashTail`…）。

耦合是**双向且结构化**的，不是"顺手引用"：

- `role_session.go` 用 `storeEngine` **构造嵌套存储**（`roleStore`、`createRoleSession`、
  `appendRoleDraft`、`syncRoleDraft`、`assembleRoleWire` …）——角色会话是"存储的存储"。
- `media.go` 的方法同时挂在 `*storeEngine` 与 `*jsonRepository` 上（`publishMediaIndexLocked`
  直接调 `store.publishModuleHead`）。
- `node_session_store.go` 的方法全部挂在 `*jsonRepository` 上（装配层直接实现 `Repository` 的扩展面）。
- `tool_record.go` / `board.go` / `teamwork.go` 走 `repository.layout.*` 与 `commitModuleHead`。

**推断（成立条件：不做行为改写）**：把这 8 组拆成 8 个 Go 包，等于"至少导出 57 个内部符号
（还要再造一组跨包锁/引擎句柄）"。这不是物理归位，而是**改动内部 API 形状**。

### 1.5 三层锁与发布机制的现有落点

| 层 | 粒度 | 现有实现（已核实） | 可否省 |
|---|---|---|---|
| 数据根独占锁 | 进程（跨进程单写者） | `sessionstore/data_root_lock.go:36`（`lock.owner`）、`:73`（`acquireDataRootLock`）、`:156`（`DataRootLockedBy`）；心跳按 `stale_after_seconds/3`（夹 10s–120s）、`auto_recover` 默认 false、`allow_multi_process` 退化为诊断 | 不可省 |
| 会话自持锁 | session | `sessionstore/module_heads.go:105`（`storeEngine`）、`:126`（`sessionModuleLocks`）、`:184`（`locks`）、`:203`（`mu`）；`session_granular.go:104`（`SessionGranularStore` 自持 router + 解析器） | 已存在，转正 |
| 模块 head 原子发布 + 未提交草稿 | module | `module_heads.go:344`（`publishModuleHead`）、`:791`（`commitModuleHead`）、`:331`（`headChecksum`）、`:518`（`repairModuleHeadLocked`）；`pending_tail.go:261/:269/:282/:308/:373`（追加草稿 → 发布 → 恢复 → 丢弃） | 不可省 |

### 1.6 工具记录通道：写放大已消除，剩下的是落点与文档

`plan.md` §4 M3 的"前置（补充）"要求：**现有每调用一次 `OpenFile+Write+fsync` 的写路径必须先替换为批次路径**。
本轮实测：**这条前置已达成**（工作区未提交改动内）。

| 环节 | 现状（已核实） | 证据 |
|---|---|---|
| 判定唯一处 | `toolStatusFailed` / `toolRecordReason` / `recordToolOutcome` 全在 `application/core/tool_record.go` | `application/core/tool_record.go:238`、`:264`、`:316` |
| 内存累计 | 每会话一份，零磁盘 | `internal/toolrecord/accumulator.go`（`Accumulator`，`Snapshot()` 返回 `(快照, dirty)`） |
| 批次落盘点 | 回合由忙转闲 `application/core/chat.go:448`；草稿物化 `application/core/session_draft.go:238`；停机兜底 `application/core/service_input.go:366`（`flushAllToolRecords`） | 同左 |
| 落点 | 会话粒度：`<sessionRoot>/tool-record.jsonl`，一行 = 一批（整份快照），残尾/坏行跳过，`file.Sync()` **每批一次** | `sessionstore/tool_record.go`（`ToolRecordFile`、`AppendToolRecordSnapshot`、`ReadToolRecordSnapshot`） |
| 装配 | `main.go:671`（`SetToolRecordSnapshotStore`）→ `main.go:1956` 的 `sessionToolRecordStore` | 同左 |
| 端口形状 | `contract/ports.go:501-523`（`ToolRecordSnapshotStore`） | 同左 |

**同时发现一处文档漂移（已核实，应当修）**：`contract/dto/tool_record.go` 的包级 doc 仍写着
"读数是**进程级台账**（`<数据根>/tool-record.jsonl`，实现 = `internal/toolrecord`）……
它是**全局**读数（跨会话、跨项目），因此刻意**不进** `Snapshot`"。
而实现已经是**会话粒度**：`ToolRecordBoard()` 读的是 `currentViewSessionID()`
（`application/core/tool_record.go:206-234`），落点是会话目录内文件。

也就是说 plan §3.6 的 **V10 事实上已被 2026-10-15 的用户口径裁决为方案 (b) 纯会话级看板**，
但文档没有跟上；而 (b) 带出的第二个问题——"不进 `Snapshot`"的原有理由失效后，
是否进快照——**仍未裁决**（见 §3）。

### 1.7 "上层直连 Router"点清单（Router 降级的动作面）

已核实的生产直连点（`*sessionstore.Router`）：

| 位置 | 用法 | 目标最小端口 |
|---|---|---|
| `application/core/project_board.go:444` / `:457` | `ProjectBoardStore` 端口 + `var _ ProjectBoardStore = (*sessionstore.Router)(nil)` | 已有先例：保持"结构满足 + 编译期断言" |
| `seelebridge/runtime_project_board_read.go:38-39` | `ProjectBoardReader` 只读面 | 同上 |
| `seelebridge/runtime.go:97` / `:637` | `historyRouter` 字段 + `AttachHistoryRouter` | 历史读写窄端口（读面 + 提交面分开） |
| `application/core/compressed_turn.go:35` | 消费方定义的 `SaveCommitWorkspace` 端口（已有） | 保持 |
| `internal/adapters/session_workspace_ports.go:466` / `:500` / `:660` | `granular().SaveCommit` / `roleRouter` | 由 `SessionGranularStore` 承担（转正后自然收窄） |
| `seelebridge/runtime_subagent_recovery.go:186` | `loadSubagentConclusions(ctx, router, …)` | 节点会话读面端口 |
| `seelexctx/search/search.go:127` / `:134` | `NewRouterEventSource(router, …)` | 事件源端口（读） |
| `main.go:1851` / `:861` / `:946` / `:2013` | `initStore`、看板读/刷新工具注册、`initSessionManager` | 装配期持有（允许），但注册段按 §2.10 迁 `wiring/registry/` |
| `session/manager.go:19` / `:31` / `:37` / `:93` / `:97` | `Manager` 直接持 Router 并 `SaveCommitWorkspace` | `session/` 属**中间层**（§2.4 迁 `middle/switch/sessionusecase/`），M3 只登记、不动 |

**M3 的动作口径**：把这些点逐步改成"窄端口 + 编译期断言"，`Router` 仍是唯一实现；
**不改任何行为**，因此不需要动数据或格式。

---

## 2. 目标结构

### 2.1 包切法：两个可选方案（D-M3-1，需复核者点头）

| | 方案 A：单包 `store/` + 分卷 README（**建议**） | 方案 B：按 §2.9 拆 8 个子包 |
|---|---|---|
| 形状 | `store/` 一个 Go 包（`package store`），57 文件全在其中，README 按文件前缀分卷 | `store/root`、`store/session`、`store/board`、`store/team`、`store/role`、`store/node`、`store/media`、`store/legacy` |
| 行为风险 | 零（纯包改名 + import 替换） | 需要导出 §1.4 的 57 个内部符号、重建跨包引擎/锁句柄 → 属**改 API 形状** |
| 门面闭包清单 | **等量替换**（`seelex/sessionstore` → `seelex/store`，规模不变） | +7 条以上（且每包都须是干净叶子），与 ratchet 规则冲突，须作为偏差登记 |
| 与 §5.1/§5.14 的关系 | 直接满足（只做物理归位；不以改名冒充解耦，依赖方向另有机械闸） | 需要先证明"拆包 ≠ 改语义" |
| README 规范 | 合规：`docs/arch/readme-spec.md` 的"大包按文件前缀分卷"正是为这种包准备的 | 合规：每子包一个 README |
| 后续空间 | V4 有结论后再决定是否第二刀 | 一步到位，但把两个风险（归位 + API 变形）绑在一起 |

**建议：方案 A。** 证据是 §1.4 的 205 / 57 计数与 §1.3 的 ratchet 规则；代价是
`store/` 目录下文件较多（用分卷 README 承接，符合仓库既有规范）。

**方案 A 的成立条件**：① 依赖方向红线由新机械闸钉住（§5）；② README 分卷在**同一 commit** 内完成（§5.11）。

**若复核者坚持方案 B**，则必须先做两件事，且它们**不属于 M3 的"只搬"范围**：
① 列出"内部符号 → 导出名"的映射表并逐个评审（预计 57 条起）；
② 说明导出后如何阻止上层绕过会话句柄直接驱动存储引擎（否则与 M3"句柄唯一"自相矛盾）。

### 2.2 文件级落点表（方案 A 口径）

搬迁后：`sessionstore/*.go`（57 生产文件 + 84 测试文件）→ `store/*.go`，包名
`sessionstore` → `store`；`workspace/*.go` → `store/workspace/`（保持独立包，
因为它是文件/仓库操作面，与存储引擎无结构耦合）。

| 来源 | 目标 | 文件数（实测） |
|---|---|---|
| `sessionstore/*.go`（全部） | `store/*.go` | 57 生产 + 84 测试 |
| `workspace/*.go` | `store/workspace/*.go` | 8 生产（1,968 行） |
| `sessionstore/router_storage.go` | 见 §2.4（依赖反转后位置待定） | 1 |

计划中的 `store/schedule/`（`seelebridge/scheduler/persistence.go` 的 `FileStore`）与
`store/record/`（`internal/toolrecord`）**不在本批**：前者属 §2.12.1（M4 与 `backend/engine/schedule/`
同批），后者属 §2.12.2（M4）。M3 只负责把**已经在 `sessionstore` 里的**东西归位，
避免一次搬两个板块（§5.8 一次一主题）。

### 2.3 会话句柄唯一化

目标形态（不改行为）：

```text
消费方（application/core/session_runtime 的端口、internal/adapters）
   └─ 只持 SessionGranularStore（每 sessionID 一个实例、锁自持、项目作用域显式）
Router
   └─ 降级为「数据根 + 项目索引 + 模块 head 的持有者」，继续做 SessionGranularStore 的实现底座
```

已核实的现状支持这一形态：

- `SessionGranularStore` 已经包装 `Router`（`session_granular.go:554-558`：
  `SaveCommit` → `router.SaveCommitWorkspace(store.projectID(projectID), …)`），
  且已显式携带项目作用域（"视图切换不得改变会话的存储归属"，见该文件头部自述）。
- 反向依赖（`sessionstore → seelex/session`）已被源码红线禁止——两者不会成环。
- 上层端口已存在：`application/core/session_runtime/ports.go:119` 明确"适配器由装配根注入"。

**动作**（属 §1.7 清单的收窄，不改行为）：

1. 把 §1.7 表中"直连 Router"的生产点，逐个改为本报文已有的窄端口（或新增最小端口），
   保留 `var _ Port = (*store.Router)(nil)` / `(*store.SessionGranularStore)(nil)` 编译期断言。
2. `session/manager.go` 的 `WithRouter/Router()` 不动（它的归位在 M4/M5），
   但要在 M3 记录里登记"它是 Router 直连点之一，别在 M4 忘掉"。
3. 不给 Router 增加任何新的公开方法（防"降级"名不副实）。

### 2.4 依赖方向纠正：生命周期端口反转（D-M3-2）

现状（§1.2）：`store` 里唯一一条向上依赖是 `RouterStorage` 对 `seelexctx/lifecycle` 的实现声明。

两个子方案：

| | (a) 结构满足 + 消费侧断言（**建议**） | (b) 把 `RouterStorage` 搬出 store |
|---|---|---|
| 做法 | 在 `contract/` 声明通用端口（如 `RecordSink[T any]`：`Append(ctx, []T) error` / `ReadRange(ctx, offset, limit) ([]T, int, error)` / `Count() int`，**只用 `context` + 类型参数，保持契约层闭包 = stdlib**）；`store` 内改为 `var _ contract.RecordSink[types.Message] = (*RouterStorage)(nil)`；"它满足 `lifecycle.Storage[types.Message]`"的编译期断言挪到消费侧（装配或 `seelexctx` 侧测试） | 把 `RouterStorage` 移到消费层，store 只导出 `SaveCommit/LoadRange` |
| 依赖方向 | store → contract（合规） | store 完全不提 lifecycle（也合规） |
| 代价 | 契约层多一个泛型端口（stdlib-only，闭包不变） | 需要把 `messagesToEventRows`（`json_layout.go:509`，未导出）与 `isSessionNotFound`（`durable_history.go:17`，未导出）导出或复制——**复制会造第二份实现**（违反 AGENTS §8.1） |
| 行为变化 | 无 | 无 |

**建议 (a)**，判据是 AGENTS §8.1 的优先级"**结构满足 + 编译期断言 > 适配层 > 复制一份**"。
仓库已有同款先例：`application/core/project_board.go:457` 的
`var _ ProjectBoardStore = (*sessionstore.Router)(nil)`——上层定义端口、存储结构满足。

**测试搬迁**（随本项一起做）：
`sessionstore/router_storage_test.go`（`TestRouterStorageLifecycle` / `TestRouterStorageWithContextActor`）
验收的是"store 的适配器能被 lifecycle 消费"，按 §8.1 的口径它属于**消费侧的一致性用例**，
应当迁到消费侧测试包（或装配侧），使 `store` 的测试不再 import 上游。

### 2.5 工具记录的终局形状（D-M3-3，需复核者点头）

`plan.md` §2.12.2 的裁决是"落点 = `store/session/` 下的一个 module（走既有
`publishModuleHead` / `commitModuleHead` 原子发布）"。实测现状（§1.6）是
**会话目录内的独立 append-only JSONL**（`<sessionRoot>/tool-record.jsonl`，一行一批整份快照），
没有走 module head。这是一条**偏差**，两种走法各自的后果：

| | 现状（独立 JSONL，建议保留） | plan 口径（走 module head） |
|---|---|---|
| 读侧 | "取最后一行有效行"= 当前读数；残尾/坏行跳过（已有测试） | 读 head 取水位 + 读数据行，与其它模块同构 |
| 写侧 | 每批一次 `OpenFile(O_APPEND)+Write+Sync`（**每批**一次，不是每次调用） | 每批写数据行 + 原子发布 head（多一次 rename/校验和） |
| 崩溃语义 | 未提交批次可丢、不得隐式提升（与消息草稿同款） | 同上，另加 head 与数据的一致性保证 |
| 风险 | 与"store 的统一发布机制"不同构，读者要记住两套 | 需要给"整份快照"设计 payload 形状（`moduleHeadFile` 目前装水位类结构） |

**建议**：保留现状（独立 JSONL），并在 M3 记录里**显式登记"为什么不像其它 module 一样走 head"**：
该通道只有**一个写者、一个读者、每批一行**，"最后一行即真相"不需要水位；head 机制的价值在
"多处追加的通道要被读者一致地看到"。若复核者要求统一，则列为 M4 的 `store/record/` 项一起做，
不要在 M3 里同时改通道语义（§5.1 行为不变）。

### 2.6 README 与文档同步

| 项 | 现状 | M3 动作 |
|---|---|---|
| `sessionstore/README.md` | 58,026 字节、中英混排 | 改为 `store/README.md` + 分卷（按文件前缀），补「文件与函数索引」 |
| `sessionstore/README.md` 的「生态位」 | 已符合 `docs/arch/readme-spec.md` 的标题要求 | 改名后重写主要调用方（路径变化） |
| `contract/dto/tool_record.go` | 包级 doc 写"进程级全局台账 / 不进 Snapshot"（漂移，§1.6） | 与实现对齐；"是否进 Snapshot"按 §3 的建议由用户裁决后再定稿 |
| `docs/arch/` 里的稳定架构说明 | 引用了 `sessionstore` 路径 | 同一 commit 内替换路径（`rg -l 'seelex/sessionstore' docs/`） |
| 根 `README.md` / `AGENTS.md` §1 的模块边界表 | `sessionstore/` 是事实来源条目 | 同一 commit 更新为 `store/` |

---

## 3. 待裁决项：事实核实与建议

### V4（`store/{board,team,role,node}` 与 `store/session` 的读侧边界）——建议"本轮不做第二刀"

**已核实**：这四组的读方法全部挂在同一个 `storeEngine` / `jsonRepository` 上（§1.4 的耦合计数），
拆分需要复制存储构造与锁。上层读侧边界**已经**由消费方端口固定
（`application/core/session_runtime/ports.go`、`internal/adapters/session_workspace_ports.go`）。

**建议**：M3 只做归位；"是否再拆 `store/reads`"推迟到 M4/M5 上层端口收敛之后。
理由是 §8.2 的"本质重复"判据在这里**不成立**（不是同一件事两份实现，而是同一实现的多面读口）。

### V5（`SessionGranularStore` 与 `Router` 的并发关系）——已核实，结论见下

**已核实**：

- 写路径已经统一到 `SessionGranularStore`（它再委派 `Router` 的显式项目作用域方法）；
  `Router.SaveCommit(sessionID, commit)`（不带 projectID、走"活跃写作用域"）的**生产调用者是
  `RouterStorage.Append`，而它无生产调用者**（§1.2）。
- 其余 Router 直连点集中在 §1.7 的表里，共 9 处（含 `session/manager.go`）。
- 并发回归用例已存在：`sessionstore/concurrency_race_test.go`、`session_granular_race_test.go`、
  `lock_hotspot_test.go`、`session_context_lock_test.go`、`message_read_lock_test.go` 等。

**结论**：迁移动顺序建议 = 先钉端口与机械闸（B1）→ 再包改名（B2）→ 再依赖反转（B3）→
再逐个收窄直连点（B4）。**不要**先搬文件再补断言（§5.14 "依赖方向必须先由契约与断言钉住，再搬文件"）。

### V10（工具记录看板读数的范围）——一半已裁决，一半待裁决

| 子问题 | 现状 | 建议 |
|---|---|---|
| 读数是本会话还是整机 | **已按 2026-10-15 用户口径实现为会话级**（§1.6）；`dto` 注释仍写全局（漂移） | 修注释：改成"会话粒度（跟会话目录走）" |
| "不进 `Snapshot`"是否仍然成立 | 原理由（"全局计数塞进会话快照会让重放听起来像重放整机计数"）**已失效** | **建议仍不进 Snapshot**，理由换成："读数是工具完成段的派生读数，不是会话运行状态；进快照会造出第二份事实源（内存累计 + 提交点落盘已经是唯一真相）"。**需用户点头** |
| 是否需要"整机汇总读面" | 现状没有 | 若"这台机器哪个工具最该优化"仍是产品口径，另立汇总面（只读聚合，不落盘）——**需用户裁决**，不在 M3 实施 |

### 偏差登记汇总

| ID | 偏差 | 需要谁点头 |
|---|---|---|
| D-M3-1 | 包切法改为单包 `store/` + 分卷 README（§2.1） | 复核者 |
| D-M3-2 | 生命周期端口反转用"契约层通用端口 + 结构满足"（§2.4） | 复核者（影响契约层新增一个端口） |
| D-M3-3 | 工具记录保留独立 JSONL、暂不走 module head（§2.5） | 复核者（与 §2.12.2 裁决不一致） |
| D-M3-4 | `contract/dto/tool_record.go` 文档漂移修正 + "是否进 Snapshot"改理由（§3 V10） | 用户（口径）+ 复核者（文字） |

每条都遵循 M2 的先例（`m2-record.md` 的 D-M2-1 "需复核者点头"），不自行落定。

---

## 4. 迁移批次

每批一个主题、可独立回退（§5.8）；每批交付物都包含**删除清单**（原位置 → 唯一位置，§5.3）。

### B1 端口与机械闸先行（不改文件位置）

- 动作：
  1. `contract/` 新增生命周期通用端口（§2.4 a），只加声明、无实现；
  2. 新增 `store` 层依赖方向断言（§5 的 I6）；
  3. 生成 `sessionstore` 的文件级落点表（按工作区，而非 §2.9）并登记差集（§1.1）。
- 验收：`go build ./...`；`go test ./e2e -run 'Boundary|Deps' -count=1`；断言测试在"搬家前"应**通过**（store 尚未存在时按 allowlist 空实现，或先在
  `e2e` 里写好、等 B2 变红再变绿——按 §8.7 红灯先行，推荐后者）。
- 回退：删新增文件/端口。

### B2 包改名归位

- 动作：`sessionstore/` → `store/`（包名 `store`）；全仓 import 替换；门面/契约闭包清单**等量替换**一条；
  `sessionstore/README.md` → `store/README.md` + 分卷；`docs/`、根 `README.md`、`AGENTS.md` 路径同步。
- 验收：`gofmt -l .`；`go build ./...`；`go build -tags "gui,desktop,production" ./...`；
  `go test ./... -count=1 -timeout=120s`；`go test ./e2e -count=1`（闭包闸）。
- 回退：整体还原 import 路径（数据文件不受影响）。
- 删除清单：`sessionstore/*.go` → `store/*.go`（57+84 文件逐条列出）。

### B3 依赖反转

- 动作：`router_storage.go` 改实现契约层端口；编译期断言挪到消费侧；
  `router_storage_test.go` 的消费侧用例迁到消费方测试包；`go.list` 断言 `store` 的内依赖只剩 `contract`。
- 验收：`go list -deps ./store | Select-String seelex` 只应出现 `contract`（与 `store` 自身）；
  `go test ./store/... ./seelexctx/... -count=1`。
- 回退：单文件回滚。
- 删除清单：`sessionstore/router_storage.go` 的 `seelexctx/lifecycle` 依赖 → 0 条。

### B4 会话句柄唯一化（Router 直连点收窄）

- 动作：按 §1.7 表逐点改窄端口（每点一个小 commit）；不改行为；`session/manager.go` 只登记。
- 验收：受影响包的单元测试 + `go test ./... -count=1`；
  **并发证据**：`go test ./... -run 'Repro.*Session' -count=1`（根目录 `repro_session_*.go` 全跑）。
- 回退：按点回滚（互不依赖）。
- 删除清单：每次收窄都写"哪个直连点 → 哪个端口"，并保留编译期断言。

### B5 `workspace/` 归位

- 动作：`workspace/` → `store/workspace/`（含测试）；`internal/winhide` 依赖不动（低层工具，§2.10 保留）。
- 验收：`go test ./store/workspace/... ./application/... -count=1`；`go build ./...`。
- 回退：目录回滚。

### B6 文档与派生通道对齐

- 动作：`dto/tool_record.go` 注释与实现对齐（D-M3-4）；`docs/arch/` 与模块 README 同步；
  把 V4/V5/V10 的结论写进 M3 记录；`store/README.md` 补「文件与函数索引」。
- 验收：`node --test gui/frontend/dist/*.test.mjs`（若有前端引用面）；文档链接存在性检查。
- 回退：纯文档。

---

## 5. 不变量与机械闸

### 5.1 必须字节级不变的东西（验收硬指标）

| 不变量 | 现有落点 |
|---|---|
| 数据根锁文件名与语义（`lock.owner`、心跳、stale 判定、`auto_recover`、`allow_multi_process`） | `store/data_root_lock.go:36`、`:73`、`:156` |
| JSON v8 布局（先 project_id 分区、再 session_id 隔离、message 事件行分片） | `store/json_layout.go`；`Router.LayoutV8()` |
| 模块 head 文件形状与校验和（只装水位） | `store/module_heads.go:89`、`:331` |
| 草稿/残尾规则（未提交批次可丢、不得隐式提升） | `store/pending_tail.go` |
| 会话目录内的 `tool-record.jsonl`（追加一行 = 一批、残尾跳过） | `store/tool_record.go` |
| 项目看板产物 `<项目根>/.seelex/board/<会话 ID>/`（不含机器本地 ID） | `store/{board.go,project_board*.go,project_known.go}` |
| 定时任务定义态 `<数据根>/scheduled-tasks.jsonl`（全局、后写覆盖 + 墓碑） | `store/schedule_events.go` + `seelebridge/scheduler/persistence.go` |

### 5.2 新机械闸（I6：store 无向上依赖）

与 `e2e/facade_boundary_test.go` 的 I1/I2 同款手法（`go list -deps` + ratchet 允许清单）：

```text
I6  store 的 Seelex 内依赖闭包 ⊆ { contract, contract/dto, store 自身子包 }
    —— 允许清单同样只许收缩；`seelexctx/*`、`application/*`、`seelebridge/*` 一律不许出现
```

实现要点：

- 用 `go list -deps ./store/...`（**不含测试文件**）做判据，与既有闸一致；
- 同时补一条"测试文件也不许向上"的检查（AST 扫 `store/**/*_test.go` 的 import），
  否则 §2.4 的测试搬迁就白做了；
- 把 `sessionstore` 那条从门面清单里**等量替换**为 `store`（方案 A）。

### 5.3 写放大的前后对照（M3 退出条件之一）

因为优化已落地（§1.6），"对照"必须用**可复现的测量**而不是历史数字：

1. `application/core`（或装配侧）已有 `recordingSnapshotStore`（记 `SaveToolRecordSnapshot` 调用次数）：
   写一条用例 —— 一个回合内 N（如 20）次工具调用 ⇒ 落盘调用 **1** 次；
2. `store` 侧：N 个批次 ⇒ `tool-record.jsonl` **N 行**（行数 = 批次数，不是调用数）；
3. 停机兜底：`flushAllToolRecords` 对同一累计重复落盘是幂等的（写两次读数相同）。

三条合起来即"同一会话同一回合的工具调用次数 ↔ 实际写盘次数"的可复现对照。
Windows 本地若 `CGO_ENABLED=0`，**不得声称跑过 race**（§5.10）。

---

## 6. 验收矩阵

| 风险面 | 用例（已存在，搬迁后必须全绿） |
|---|---|
| 数据根锁 | `store/data_root_lock_test.go`、`data_root_lock_multiprocess_test.go`、`data_root_lock_windows_test.go` |
| 模块 head 发布/自愈 | `store/{atomic_publish_windows_test.go,head_publish_probe_test.go,head_rebuild_test.go,head_self_heal_test.go,module_lock_identity_test.go,c1_publish_probe_test.go}` |
| 草稿/残尾 | `store/{pending_tail_test.go,draft_append_test.go,tail_read_amplification_test.go,event_log_append_only_test.go,gap_probe_test.go}` |
| 并发与锁热点 | `store/{concurrency_race_test.go,session_granular_race_test.go,lock_hotspot_test.go,lock_hotspot_probe_test.go,message_read_lock_test.go,session_context_lock_test.go}` |
| 会话粒度句柄 | `store/{session_granular_test.go,session_granular_drift_test.go,absent_state_reads_test.go,ensure_indexed_test.go,retirement_gate_test.go}` |
| 工具记录 | `store/tool_record_test.go`、`application/core/tool_record_test.go`、`internal/toolrecord/accumulator_test.go` |
| 看板投影 | `store/{board_test.go,project_board_landing_test.go,project_board_test.go,project_known_test.go,project_record_test.go,project_refresh_key_test.go}` |
| 会话切换/恢复（根目录） | `repro_*.go`（12 个，其中 `repro_session_*` 5 个：background / disappear / race / switch_real / workspace）、`session_switch_profile_test.go` |
| 层边界闸 | `e2e/facade_boundary_test.go`（I1–I4）+ 新增 I6 |
| 端到端 | `project_board_e2e_test.go`、`teamwork_headless_smoke_test.go`、`tool_full_chain_test.go` |

命令（每批都跑，按 §5.13 的风险匹配）：

```text
gofmt -l .
go build ./...
go build -tags "gui,desktop,production" ./...
go vet ./...
go test ./... -count=1 -timeout=120s
node --test gui/frontend/dist/*.test.mjs
```

---

## 7. 风险

1. **改名风暴**：~170 个文件 import 变更。缓解：一次 commit 只做路径替换（不夹带行为），
   并用 `go build` + 闭包闸双重校验；不手工改代码，改用工具或逐目录批量替换（注意 AGENTS
   §8 / M2 的事故记录 D-M2-13：**禁止经 shell 内联中文做批量替换**）。
2. **别名残留**：`application/core/aliases.go`（204 行）里有大量 `= sessionstore.X` 别名。
   包改名后别名目标要一起改；M5 才删别名，M3 只改目标路径。
3. **两处 `lifecycle` 同名**：`store/lifecycle.go`（会话栈生命周期）与 `seelexctx/lifecycle`
   （上下文 actor）**不是一回事**。搬迁与文档都要明确区分，避免后人"顺手"把两者并成一个。
4. **`tmp/head-src` 影子树**：全仓 `rg` 与批量改名会被它污染（§3.5）。搬迁前确认它仍被忽略、
   且所有替换命令都排除 `tmp/`（M1 已清理过一次，注意防止再生）。
5. **方案 B 的隐性风险**：一旦把 57 个内部符号导出，`store` 就有了"第二套公共 API"，
   与"会话句柄唯一"目标相反（§2.1）。

---

## 8. 未决问题（交给复核者/用户）

1. D-M3-1 包切法（单包 vs 8 子包）——影响面最大，建议先裁决这一条再动 B2。
2. D-M3-3 工具记录是否必须走 module head（与 §2.12.2 裁决不一致）。
3. V10 的"工具记录是否进 `Snapshot`"。
4. `store/schedule/`、`store/record/` 的批次归属（本设计把它们留给 M4，需要确认）。
5. `workspace/` 是否真的应该进 `store/`（它与存储引擎无结构耦合，也可以留在 `wiring` 或
   独立 `workspace/`；plan §2.9 把它列在 store 之下——本设计按 plan 执行，但登记为可复议项）。
