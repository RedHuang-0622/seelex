# 遗留能力退场：A04 / A05 / A06（2026-10-15）

- 日期：2026-10-15
- 依据：[`docs/2026-10-15-product-architecture-pre-design/legacy-capability-audit.md`](../2026-10-15-product-architecture-pre-design/legacy-capability-audit.md) 的 A04 / A05 / A06
- 范围：`application/core/agentteam/*`、`application/contract/dto/agentteam.go`、
  `sessionstore/team_registry.go`、`sessionstore/README.md`、`application/core/{chat,goal_work_summary,goal_team_wiring_test,agentteam_fixture_test}.go`、
  根 `README.md` / `README_EN.md`
- 前置：A03（`refactor(core): 删除 J-1a 后遗留的团队环接线（A03）`）

## 1. A04：发言环（`Runtime` / `TurnScheduler`）整条删除

**判据**：A03 已摘掉 goal 侧的 `TeamRuntimeFor`，此后 `*agentteam.Runtime` 不再是任何生产
签名的类型；全仓 `.go` 索引对 `agentteam.Runtime` / `TurnScheduler` / `TurnRequest` /
`AutomaticTurnRoles` / `RolesWithoutAutomaticTurn` / `ringOrder` / `cleanOrder` /
`NewRuntime` / `NewTurnScheduler` 只剩注释提及。环本身没有任何建造入口（A03 之前的
store 恒空），也没有任何消费者：`Snapshot()` 的 `dto.TeamSchedule` 无生产者/消费者，
前缀只经调度器交接给自己。

**删除清单**（原位置 → 现状）：

| 删除对象 | 原位置 | 现在 |
|---|---|---|
| `Runtime`（环成员同步 / `SyncOrder` / `Order` / 逃生记账 `NoteTurn`·`Stop`·`Reset`·`Round`·`Stopped` / `Next` / `Snapshot` / `peekNext` / `skipLocked`） | `application/core/agentteam/runtime.go` | 无替代；"下一个谁发言"不是产品能力，让角色说话的是 leader 的 `team_dispatch` |
| `TurnScheduler` + `roleNode` 链表 + `Advance` + `SetOrder`/`Order`/`sessionsLocked` + `SetPrefix` | `application/core/agentteam/scheduler.go` | 同上（顺序事实只剩 `lifecycle` 的历史字段与 team plan 两处） |
| team work 前缀投影（`NoteMainContext` / `renderMainContextPrefix` / `wireMessageLine`） | 同上 `runtime.go` | 无替代（应用层作者已在 A03 删除，前缀从此没有生产者也没有消费者） |
| `AutomaticTurnRoles` / `rolesWithoutAutomaticTurn` / `RolesWithoutAutomaticTurn` | 同上 `runtime.go`（2026-10-12 自 `factory.go` 迁入） | 无替代：它的唯一消费者就是本环 |
| 用例 `runtime_test.go` / `scheduler_test.go` / `scheduler_wiring_test.go` / `prefix_test.go` | 同目录 | 被测对象消失；A03 收窄过的"前缀写面白名单"一并退场 |
| 无消费者的 `dto.TeamSpec` 夹具 `goalTeamFixture`（`application/core`）与 `testspecs_test.go` | `application/core/agentteam_fixture_test.go`、`application/core/agentteam/testspecs_test.go` | 无替代：它们服务的是已删除的 `Factory.Materialize` |

**保留（避免"清包"）**：`spec.go` 一字未动——`RoleSessionID` 仍是
`application/core/goal_team_recorder.go` 的生产调用；`Normalize` / `NormalizeRole` /
`ValidToolPolicy` / `resolveOrderRoles` 保留为写入侧口径的**唯一实现位置**
（当前无生产调用方，已在模块 README 标注）。`RoleSessionID` 与
`teamwork.DefaultRoleSessionID` **不合并**（A12：空 team ID / 空白裁剪 / 默认身份语义不同）。

## 2. A05：旧顺序 / 调度字段的 read-compat 口径与 `TeamSchedule` 兼容期限

**没有删除任何字段**，只做状态标注与期限决策（字段清除属独立版本化迁移，不与删退场面同批）：

| 字段 | 当前状态 | 处理 |
|---|---|---|
| `dto.OrderPolicyGoalLoop` / `OrderPolicyUserMainDecided` / `OrderPolicyScheduledOnly` / `DefaultOrderPolicy` | 历史字段，**只读兼容**；写入面只剩 headless `role.set_order` 与 `internal/adapters` 端口，产品 UI 无入口；不驱动任何行为 | 保留原值（旧会话里的 `goal_loop` 必须可读），已加 read-compat 说明 |
| `dto.TeamSpec.OrderPolicy` / `OrderRoles` | 同上 | 保留，注释更正为"装配/入职/团队库写入面已退场" |
| `dto.TeamView.Schedule` / `dto.TeamSchedule` | **无生产者**（唯一作者 `agentteam.Runtime` 随 A04 删除），当前恒 nil | 保留为只读兼容 wire；**兼容期限 = 下一次 GUI/headless API 版本变更**，届时随该版本一并移除（需要旧载荷 fixture 冷读用例 + 前后端同批改动）。单独删字段禁止 |
| `lifecycle.order_policy` / `order_roles`（`sessionstore`） | 历史字段，不驱动行为；读面 `ReadLifecycleOrderWorkspace` 无生产调用方 | 保留；已标 read-compat |
| `message head.floor`（`Floor` / `ReadMessageFloorWorkspace`） | **活动的历史/归属事实**：sequencer 仍是唯一写者，历史渲染/审计/draft 排序仍读它 | 保留（不随退场面删除）；只标注"面板出口已退场" |
| `role_name` / `role_session_id` / `round_id` / `unit_seq` 等消息身份字段 | 活动的归属/展示事实 | 不动 |

## 3. A06：角色注册表 / 团队库 / 员工库的 legacy 只读兼容与数据政策

**判据**：全仓 `.go` 索引显示 `ReadTeamRegistryWorkspace` / `WriteTeamRegistryWorkspace` /
`RemoveTeamRegistryWorkspace` / `EnsureRoleSessionWorkspace` / `ReadMessageFloorWorkspace` /
`ReadLifecycleOrderWorkspace` **没有任何生产调用方**（只剩自身定义与本包用例；
`SetLifecycleOrderWorkspace` 另由 `internal/adapters` 转发）。`team_global.go` /
`team_library.go` 与 `Read/Write*Global` 已随 J-1a 不存在。`dto.RoleInstantiation` /
`TeamMaterializeResult` / `TeamGlobalConfig` / `TeamComposition` / `TeamLibraryEntry`
在 dto 之外零引用。

**数据兼容政策**（`sessionstore/README.md` 的「旧 Agent Team 数据（legacy）」一节为权威）：

1. **保留读能力**：`session/team/roles.json` 的读/写/删原语一字未改，旧数据仍可读回、
   诊断与迁移；删 reader 不是清理（它会让旧数据失去唯一入口）。
2. **不删文件、不迁移、不停止写入**：任何磁盘清理/迁移必须另行中文预警 + 备份 + 用户确认
   （根 `MEMORY.md`、`AGENTS.md` §0）。本次**没有**触碰任何 `.seelex/`、`config/`、`dist/`。
3. **移除条件**：确认产品不再读这些形状后，先停止写入；DTO/端口按版本化 API 变更移除。
4. **不要混**：teammate 执行链的 `ToolsPolicy` / `PermissionGroups` / `Plugins`
   （`runRoleRound` + WorkerRequest）是活动配置（A07），不属于 legacy 数据。

**文档 / 夹具声称更正**：`agentteam/README.md` 给出退场时间线 + 现行边界（历史正文显式
标注为退场前记录）；`sessionstore/team_registry.go` 文件头与各导出符号标 `legacy`；
`sessionstore/team_registry_test.go`、`application/core/agentteam_fixture_test.go`、
`application/core/goal_team_wiring_test.go` 的"当前生产装配"声称改为 legacy 兼容口径；
根 `README.md` / `README_EN.md` 里引用已删除符号（`TurnScheduler` / `AutomaticTurnRoles` /
`DesignNotice`）的现状描述改为事实口径。

## 4. 回归证据

- `go build ./...`、`go build -tags "gui,desktop,production" ./...`：通过；
- `go vet ./...`：通过（`vet_exit=0`）；
- `go test ./application/core/... ./sessionstore/... ./application/contract/... ./seelebridge/... -count=1 -timeout=600s`：全部 `ok`；
- `python scripts/gen_core_readme_index.py`：刷新 `application/core/**` 的分卷与子包索引（删除文件后无残留条目）；
- 未执行：全仓 `go test ./...`、GUI production build、真实 API 测试、磁盘迁移/清理、用户数据扫描。

## 5. 未做（留给后续条目）

- A11/A15：`docs/arch/*` 与历史报告里的现行叙述（`KindSeat`、席位环、team 环"已接线"）仍需
  按"标注历史 + 替代链接"处理；本次只更正了直接因 A04 变成假事实的架构状态行。
- A05 的字段清除、A06 的写面停用：都需要独立的版本化迁移与旧数据 fixture。
