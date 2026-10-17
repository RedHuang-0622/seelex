# P8 详解：契约抽象 / 兼容面 / 冗余僵尸面的甄别（2026-10-16）

本文是 [post-migration-proposals.md](post-migration-proposals.md) 中 **P8** 的展开。
用户对 P8 的裁决是：

1. `workunit`、`policy` 是**契约抽象**（保留并归位），不是兼容面；
2. 对任何别名/转发面的改动，**必须先判定性质**再动手；
3. **冗余或僵尸面不得自行删除**——先暴露清单与详解，由用户决定。

所以本文只做"性质判定 + 证据 + 建议"，**不包含任何已执行的删除**。

## 0. 结论速览

| 面 | 性质 | 判据（证据） | 我的建议 | 状态 |
|---|---|---|---|---|
| `backend/workunit`（`Unit`/`Lifecycle`/`Jobs`/`FinishPolicy` 等） | **契约抽象** | 用户定义：后台工作 agent 的抽象（含 subagent/teammate），并为未来后台 agent 方案（含规划中的远程 agent 调用）提供契约 | 保留、不动内容 | 已裁决 |
| `backend/limit/policy`（`policy.go`/`tiers.go`/`tool_meta.go`/`shellkind.go`） | **契约抽象** | 用户定义：多条件 select 出 bool 决定权限的统一抽象与契约 | 保留、不动内容 | 已裁决 |
| `backend/tool/registry` 的 `GroupBit{Read,Write,Execute,All}` | **兼容面** | 4 条都是 `= policy.Bit*` 的别名，存在理由只有"跨包后用例少改几处"；其中 `Execute`/`All` 全仓零引用、`Read`/`Write` 仅被两个测试文件引用 | 删 2 条死别名；`Read`/`Write` 让用例直接写 `policy.BitRead/BitWrite` 后删 | 待裁决 |
| `store/root_aliases.go`（9 条） | **兼容面**（M3 抽包时开的口子） | 只有 `Settings`(27)/`DataRootLockedByWith`(1) 被生产代码消费；`NewSettings`/`DataRootLockedBy`/`ErrDataRootLocked`/`ErrDataRootStaleLock`/`retentionModeManual` 无外部消费者 | 保留被装配根真正消费的，其余随"用哪里写哪里"清理 | 待裁决 |
| `application/core/aliases.go`（208 行 / 157 条） | **兼容面** | 已在 `m5-slices.md` R10 列为删除目标 | 按 R10 执行（搬迁收口期） | 已裁决 |
| `backend/engine/assemble/ports.go`（792 行） | **兼容面** | 已在 `m5-slices.md` R11 列为删除目标 | 按 R11 执行 | 已裁决 |
| `application/application.go` 的 `SearchHit`/`SearchRecord` | **僵尸面** | 全仓生产代码零引用（`application/README.md:76` 自己写着待迁 `contract.SearchPort`） | 删 | 待裁决 |
| `store/legacy_storage.go` + `store/README-storage.md` | **疑似僵尸面** | `NestedSessionStore` 全仓零引用；`NewSessionStore` 仅 `test/smoke_test.go` 引用；生产装配走 `store.Router` | **先确认旧 shard 数据是否还要回读**，再决定删/降级 | 待裁决（有数据风险） |
| 34 个文件把 `backend/engine/assemble` 导入为 `seelebridge` | **命名残留** | `rg -l '^\s*seelebridge\s'` = 34 文件（`seelebridge` 包已不存在） | 改名 `assemble`，纯机械、零行为变化 | 待裁决 |
| `middle/protocol/model/*`、`internal/model/*` 的 dto 别名（9 + 4 条） | **待定的契约面** | 见 §3 的三条链：同一形状出现 2–3 个名字 | 与 P3（契约层提纯）合并裁决，本文不单独动 | 待裁决（依赖 P3） |

## 1. 口径与证据（可复现）

- 采集脚本（落盘执行）：`tmp/reshape/p8_alias_inventory.py`、`tmp/reshape/p8_zombie_inventory.py`
  ；输出：`tmp/reshape/p8-alias-inventory.tsv`、`tmp/reshape/p8-zombie-v2.tsv`。
- **别名**判据：包级 `type X = Y` / `var X = Y`。
- **僵尸**判据：生产代码（非 `_test.go`）中，**除声明行本身外零引用**（含同文件内）。
  仅被测试引用者单列，不算僵尸。
- 局限（必须知道，否则会误判）：
  1. 只统计 Go 源码标识符；反射、字符串字面量、JSON tag、仓库外调用者看不见；
  2. 通用名（`Message`、`Status`、`Settings`）的**裸计数会被同名标识符污染**，
     因此本文只用限定名（`store.Message`、`model.TurnStatus`）计数；
  3. "零引用"只说明**当前仓库的 Go 代码不引用它**，不等于它没有产品含义（见 §6 的判断）。

## 2. 同一形状的多层出口（真正的问题不是"别名多"）

别名本身不是缺陷；**同一个形状被三层各自命名**才是。据限定名计数：

| 形状（唯一实体） | 出口 1 | 出口 2 | 出口 3 | 证据 | 建议 |
|---|---|---|---|---|---|
| `NodeScope`（节点作用域） | `dto.NodeScope`（6 文件） | `internal/model.NodeScope`（34 文件） | `backend/tool/node.NodeScope`（11 文件） | `rg -l 'dto\.NodeScope'` 等 | 收敛为 dto 单名（依赖 P3 裁决） |
| `SubagentToolEvent` | `dto.SubagentToolEvent`（4 文件） | `middle/protocol/model.SubagentToolEvent`（4 文件） | `backend/multiagent/subagent.SubagentToolEvent` | 同上 | 同 P3 |
| `ScheduledTaskSpec` / `Status` | `dto.ScheduledTask*`（7 文件） | `backend/engine/assemble`（`ports.go`，173 处） | `application/core` 经 `seelebridge.ScheduledTaskSpec`（12 文件） | 同上 | 先做 §5 的别名改名，再随 R11 收口 |
| `ReActBudget` | `backend/limit/loop.Budget` | `application/prompt.ReActBudget` | `application/core.aliases.ReActBudget` | 同上 | 随 R10 收口 |

**为什么这算问题**：三处名字任意一处改口径，另外两处不会跟着变——按 AGENTS §8.3
"同一判据一处"，同一**形状**也应只有一处**命名**（尤其类型身份已由 `=` 别名统一时，
再保留第二、第三个名字只剩"少改调用点"的收益）。

## 3. 僵尸面清单（生产零引用，共 19 条）

| # | 名称 | 位置 | 是什么 | 为什么像僵尸 | 删了会怎样 | 建议 |
|---|---|---|---|---|---|---|
| 1 | `SearchHit` | `application/application.go:115` | 门面对 `seelexctx/search.Hit` 的别名 | 生产零引用；README 自己标注"待迁 `contract.SearchPort`" | 无 | 删 |
| 2 | `SearchRecord` | `application/application.go:116` | 同上（`ChatRecord`） | 同上 | 无 | 删 |
| 3 | `SessionArchiveVersion` | `application/core/session_runtime/archive.go:21` | 会话归档格式版本常量 `= 1` | 生产零引用；真实版本写在 `SessionArchiveResumePrefix` 的 `v1` 里 | 版本事实从代码里消失（格式说明会退化成纯字符串） | **保留**并把前缀改成拼常量（一处事实） |
| 4 | `WithTags` | `backend/engine/mcp/mcpstack/stack.go:124` | MCP 服务器选项（打 tag） | 生产与测试都零引用 | 选项面变窄；若未来要按 tag 选服务器需重加 | 删（需要时再加，历史可查） |
| 5 | `ErrInvalidSignal` | `backend/multiagent/goal/a2a.go:268` | TL 域哨兵错误"非法信号" | 生产零引用（同组另三个哨兵都在用） | 无 | 删 |
| 6 | `GroupBitExecute` | `backend/tool/registry/permission_policy.go:316` | `= policy.BitExecute` | 生产与测试都零引用 | 无 | 删 |
| 7 | `GroupBitAll` | `backend/tool/registry/permission_policy.go:317` | `= policy.BitAll` | 同上 | 无 | 删 |
| 8 | `ModelPolicyInherit` | `contract/dto/agentteam.go:83` | 角色模型口径常量 `""` | 生产零引用 | **有产品含义**（角色模型的继承口径），删了将来要用得重定义 | 待裁决（见 §7 Q4） |
| 9 | `ModelPolicySameAsExec` | `contract/dto/agentteam.go:84` | `"same-as-exec"` | 同上 | 同上 | 待裁决 |
| 10 | `NodeStageStopped` | `internal/model/node_result.go:27` | 第一视角阶段名 `"stopped"` | 生产零引用，注释写明"**预留**" | 预留阶段词消失 | **保留**（预留是显式决策，不是遗漏） |
| 11–19 | `StyleDiffAdd` / `StyleDiffDel` / `StylePrompt` / `StyleStream` / `StyleSessionID` / `StyleTaskItem` / `Border` / `BorderForeground` / `StyleInputPrompt` | `tui/styles.go:20,21,37,39,43,47,58,59,62` | TUI 的 lipgloss 样式变量 | 9 条全部生产零引用（`Border` 的唯一命中是它自己的声明） | TUI 观感不变（本来就没用） | 删；**但**若 TUI 正在重做视觉，先问一句要不要留（见 §7 Q1） |

## 4. 仅被测试引用（20 条，不算僵尸但要点名）

这些只剩测试在用的出口，**要么**是"测试专用 API"（合理），**要么**是"生产已不用、
测试还在守着"（后者意味着测试在守一份没人消费的能力）。

| 组 | 条目 | 位置 | 我的判断 |
|---|---|---|---|
| MCP 选项 | `WithMetadata` | `backend/engine/mcp/mcpstack/stack.go:120` | 仅 `stack_test.go` 使用——**测试守着一份没人消费的选项**，建议连选项与用例一起删 |
| 员工授权位 | `GroupBitRead` / `GroupBitWrite` | `backend/tool/registry/permission_policy.go:314,315` | 仅两个测试用；建议改用例写 `policy.BitRead/BitWrite` 后连同 4 条别名一起删（与 §0 的兼容面判定同一件事） |
| goal 状态 | `StatusReviewing` / `StatusWaitingHuman` | `backend/multiagent/goal/record.go:48,52` | 仅 `record_status_test.go` 用；若状态机确实不产出这两个状态，属**词表残留**，建议删词表条目 |
| 调度词表 | `WeekdayMonday` | `contract/dto/scheduler.go:30` | 仅 `schedule_test.go` 用；其它 6 个星期几显然被生产消费——需要单独看为什么只有周一没被用 |
| 压缩来源 | `CompactionSummarySourceLocal` | `application/core/context_runtime/ports.go:132` | 被 5 个 repro/探针用例用；生产零引用——**属"重放用例的观测口径"**，建议保留（它钉住的是行为） |
| 会话投影 | `TaskContextProjectionSchemaVersion` | `middle/protocol/model/context.go:5` | 仅 `fork_test.go` 用；schema 版本常量一般应被生产写入端引用，需单独确认 |
| 团队板 | `BoardCloseGoalFinish` / `BoardCloseGoalAbort` | `store/board.go:55,56` | 仅 `board_test.go` 用；名字像"板子关闭 goal"的枚举，生产不用=可疑 |
| 测试支撑包（设计如此，不用改） | `middle/view/board/boardtest/*`（8 条）、`e2e/scenario`（`NewHarnessRunner`/`LoadFile`） | — | 它们**就是要给测试用**的包，导出是设计意图 |

## 5. 命名残留：`seelebridge` 作为 import 别名（34 文件）

`seelebridge` 包在 M4 已删除，但 **34 个文件仍把 `backend/engine/assemble` 导入为
`seelebridge`**（`rg -l '^\s*seelebridge\s' --glob '*.go'`），例如
`wiring/bootstrap/{bootstrap,runtime,plugins,application}.go`、`wiring/adapters/*_port.go`、
`application/core/service_scheduler.go`、`test/smoke_test.go` 等。

危害不是编译（能编译），而是**误导**：新人（或下一次重构）会以为 `seelebridge` 还在，
或以为 `seelebridge.X` 与 `backend/engine/assemble` 是两种东西。`application/core/service_scheduler.go:80`
的注释甚至写着"数据与执行都归 seelebridge 调度器"。

**建议**：一次性改成 `assemble`（可加 `sb` 之类的短别名，但建议直接用 `assemble`），
纯机械替换 + 散文里的旧包名一并对齐，零行为变化。这是"搬迁收尾"的一部分，
按 P7/post-migration 收口批一起做即可。

## 6. legacy 存储面详解：`store/legacy_storage.go`（301 行）+ `store/README-storage.md`（146 行）

行数以 `wc -l` / Python 口径为准（本环境下 PowerShell 的 `Measure-Object -Line` 会少算）。
本节与 [post-migration-proposals.md](post-migration-proposals.md) §5.3 的 Z1/Z2 讲的是同一件事：
那边是"裁决层"（判定 + 建议 + 待你决定），这里是"证据层"（清单、逐条详解、可复现脚本）。

它现在是 `store` 包里的**文件分片（shard）会话存储**：

| 符号 | 生产引用 | 测试引用 | 说明 |
|---|---|---|---|
| `SessionStore` / `NewSessionStore` | 无 | `test/smoke_test.go:35` | 每次 `Save` 按会话在 `baseDir` 下写分片文件 |
| `NestedSessionStore` / `NewNestedSessionStore` | **无** | 无 | 按 workspace 路由到不同 `SessionStore`（`stores map[string]*SessionStore`） |
| `Message` / `SessionMeta` | 包内使用 | — | 分别指向 `Seele/types.Message`、`Seele/seelectx/storage.SessionMeta`；限定名 `store.Message`、`store.SessionMeta` 全仓**零引用** |

生产装配早已改走 `store.Router`（`session/manager.go` 只持 `*store.Router`；
`wiring/bootstrap/bootstrap.go` 装配 `store.NewNodeSessionStore`），
所以这条 shard 路径**没有任何生产调用方**。

**为什么我不建议直接删**：`baseDir` 下是**用户历史会话的落盘格式之一**。
删"读路径"之前必须先确认：现网/本机 `.seelex/` 里是否还有需要回读的 shard 数据
（旧版本 Seelex 写出的目录）。这属于 AGENTS §0 的"可能影响用户数据"，不是纯代码清理。

**三种处置**（请裁决，见 §7 Q3）：

1. **删**（含 `README-storage.md` 与 `legacy_storage_test.go`），并同步把
   `test/smoke_test.go` 的那一条用例改成走 `store.Router`（否则会失去"存-读往返"冒烟）；
2. **降级为内部兼容读**：只保留 `Load`/`List` 这类读路径 + `Message`/`SessionMeta` 别名，
   写路径删掉，README 标注"仅为回读 v1 shard，不再写入"；
3. **原样保留**，但在 README 顶部标明"当前无生产调用方，保留原因 = 历史数据回读"。

## 7. 待你裁决（逐条）

| # | 问题 | 选项 | 我的建议 |
|---|---|---|---|
| Q1 | §3 的 19 条僵尸面是否执行删除？ | 全删 / 只删无产品含义的 / 先不动 | 分两批：**无产品含义的 15 条**（`SearchHit`、`SearchRecord`、`WithTags`、`ErrInvalidSignal`、`GroupBitExecute`、`GroupBitAll`、9 条 tui 样式）直接删；`SessionArchiveVersion`、`NodeStageStopped` 保留（一个改成一处事实、一个是显式预留）；`ModelPolicyInherit`/`ModelPolicySameAsExec` 见 Q4 |
| Q2 | §4 的"仅测试引用"里，哪些按"测试守着无人消费的能力"处理？ | 逐条 / 打包 | 先处理 `WithMetadata`（选项+用例一起删）与 `GroupBit*`（用例改写后连别名删）；`StatusReviewing`/`StatusWaitingHuman`、`WeekdayMonday`、`BoardCloseGoal*` 我先出"是否真的不产出"的证据再报你 |
| Q3 | §6 的 legacy shard 存储怎么处置？ | 删 / 降级为只读兼容 / 原样保留并标注 | **降级为只读兼容**（选项 2）：既不留误导的双写路径，也不丢历史数据的回读能力 |
| Q4 | `contract/dto` 里与角色/团队产品模型有关、当前零引用的类型（`ModelPolicyInherit`、`ModelPolicySameAsExec`、`TeamMaterializeResult`、`TeamLibraryEntry`、`TeamComposition`、`TeamGlobalConfig`、`RoleInstantiation` 等） | 全留 / 全删 / 逐条 | **全留**：它们是**产品模型词表**（前端/插件/未来的团队库要用），删了会丢产品语义；建议只在文件头加一行"当前未被 Go 代码消费"的标注 |
| Q5 | §2 的三层同名（`NodeScope` 等）是否收敛为 dto 单名？ | 收敛 / 保留 | 与 P3 一起做（P3 的前置三项产品决定没到之前，收敛会改掉"谁拥有形状"的答案） |
| Q6 | §5 的 `seelebridge` import 别名是否改名 `assemble`？ | 改 / 不改 | 改（34 文件，纯机械，零行为变化） |

## 8. 与其他文档的关系

- 提案与裁决： [post-migration-proposals.md](post-migration-proposals.md)
- 切片与顺序： [m5-slices.md](m5-slices.md)（R10/R11 是本文多处建议的落点）
- 纪律判据：仓库根 `AGENTS.md` §8（一份实现、判据一处、装配优先）
