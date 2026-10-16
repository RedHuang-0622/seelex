# M4 决策简报：四个卡点与一个设计前提

本文只做一件事：把"等用户裁决"的四项 + J 片的设计前提摊成证据 + 选项 + 建议，
让裁决只需一条消息。已落地的部分见 [m4-record.md](m4-record.md)。

## 1. V3：`workunit` 的最终归属

> **已落地（2026-10-16，M4-W）**：按下面"建议"的第二条落地为**独立 `backend/workunit/`**，
> 契约与唯一实现（原 `workunit_parent.go`）同处一个包；落地细节、删除清单与新偏差见
> [m4-record.md §13](m4-record.md)。下面保留当时的证据与选项，供回溯。

**证据（搬后视角的进口方）**：`backend/tool/node`、`backend/tool/worktree`（tool 板）、
`internal/telemetry`（模块底座）、`seelebridge/teamwork` 与 `seelebridge/tools`（尚未搬）、
以及根包的 5 个实现文件（`workunit_assembly.go`、`workunit_parent.go`、`workunit_team.go`、
`workunit_node.go`、`workunit_team_records.go` + `runtime_unit_recovery.go`）。

**关键事实**：`e2e/workunit_ports_test.go` 把这个"契约包 + 根包实现文件"的配对写成了门禁判据
——它按**仓库路径**读 `seelebridge/workunit/contract.go` 与 `seelebridge/workunit_parent.go`，
并断言实现里不出现按层分支。所以 workunit 单独搬走会让这条门禁失效或指错文件。

**建议**：

- 契约包 `workunit` → `backend/multiagent/workunit/`（与 plan §2.7 一致；tool 板 → multiagent
  是"工具调用协作域"的既有方向，不成环）；
- **与 J 片同步做**：把根包那 6 个 `workunit*`/`runtime_unit_recovery` 文件一起并进去；
- 门禁 `e2e/workunit_ports_test.go` 改成"按包名定位"而不是写死仓库路径（否则每次搬迁都要改门禁）。

## 2. `mcp` / `plugin` 的落点（撞名）

**证据**：plan §2.5 同时要求 `mcpstack/` → `backend/engine/mcp/`、`plugin/` → `backend/engine/plugin/`，
以及 `seelebridge/{mcp,plugin,account}` → `backend/engine/` 的对应子目录。一个目录装不下两个包。

**选项**：

| 选项 | 形状 | 代价 |
|---|---|---|
| A（建议） | 主包占目录名：`backend/engine/mcp/`（来自 `mcpstack/`）+ `backend/engine/mcp/seele/`（来自 `seelebridge/mcp`）；plugin 同理 | 多一个 `seele/` 层；两个包都只改路径 |
| B | 适配层占目录名，主包进 `.../stack/` | 主包位置反直觉 |
| C | 把 `seelebridge/mcp` 折叠进主包 | 属于代码合并，不是搬迁（要另立一个主题） |

## 3. `internal/*` 的终局落点（D-M4-8）

**证据**：搬后 `internal/{model,telemetry,config,actor,stream,docker}` 的进口方分布是
`model` 11 处（横跨 tool/multiagent/engine）、`telemetry` 6 处、`config` 3 处、`actor` 3 处、
`stream` 1 处、`docker` 1 处。plan §2.6 写的 `backend/tool/internal/` 只能覆盖 tool 板的进口方，
而 Go 的 `internal` 规则要求同一子树——三个板块要共用就只能放在更上层。

**建议**：**保持模块根 `internal/*` 作为终局**（与既有 `internal/{bootseed,buildinfo,frontmatter,
promptassets,testutil,toolrecord,winhide}` 同一惯例：模块级低层与适配不在任何单一板块内）；
J 片之后不再下沉。若你更希望它们有"底座"名分，替代方案是整体加前缀（`internal/seele*`），
那是一次纯路径改写，可以与 J 片合并做。

## 4. J 片的设计前提：谁持有 `Runtime`

**证据**：根包 **53 个生产文件 / 13,373 行**；`runtime.go` 定义 `Runtime` 结构体，
其余 40+ 文件几乎都是 `func (r *Runtime) ...`，按域分布：

- engine 侧：`runtime.go`、`runtime_bundle/deps/live.go`、`runtime_context.go`、
  `runtime_compaction_index.go`、`runtime_plan.go`、`runtime_tools.go`、`runtime_account.go`、
  `runtime_session*.go`、`runtime_role_*.go`、`runtime_goal_tl.go`、`runtime_image.go`、`runtime_computer.go`、`runtime_async.go`、`jobs_events.go`、`events*.go`、`session_telemetry.go`、`request_log.go`
- multiagent 侧：`runtime_teamwork*.go`（11 文件）、`runtime_subagent_*.go`、`runtime_unit_recovery.go`、
  `workunit_*.go`、`teamwork_job_*.go`、`role_host_session.go`、`resume_notes.go`
- tool 侧：`runtime_plan.go` 的作业适配、`review_files.go`、`runtime_async.go`
- 兼容层：`ports.go`（1020 行，逐行委托 + 19 处 `type X = ...` 别名）

**两条可走的路**：

| 路线 | 做法 | 代价 / 收益 |
|---|---|---|
| B（建议先行） | 把根包**整体**搬到 `backend/engine/assemble/`（一个包，一次改名 + 引用改写），之后逐域把方法下沉到各板块的组件，`Runtime` 逐步退化成装配壳 | 一轮机械改名就能拿到"`seelebridge` 目录消失"这个标志；每域下沉可独立提交、独立回滚 |
| A | 一开始就按域拆类型（每域一个组件 + 门面只做组合），`Runtime` 从第一天起就是薄壳 | 改动最大、单次提交难以审查，但"无跨板块回指"一次到位 |

建议顺序 **B → A**：先让目录消失（可验证、可回滚），再用接口把域面切开以兑现 M5 的
"五组窄接口"。J 片开工前只需要你点头两件事：**目标目录与包名**（建议
`backend/engine/assemble/`，包名 `assemble`）、**顺序 B → A**。

## 5. 不需要裁决、我可以直接继续做的

1. `scheduler` 拆分（plan §2.12.1 **已裁决**：actor + 触发判定 → `backend/engine/schedule/`、
   `FileStore` → `store/schedule/`、`Persistence` 接口 → `contract/`）。这是"拆"不是"搬"，
   我会先写会红的用例钉住现状再用一步完成。
2. `seelebridge/tools`（13 文件 5076 行）里**与权限判定无关**的部分：`internal/mapper` 的删除
   （plan §2.10 明写"删"）、`json_object.go` → `internal/jsonobj/`。
3. 把 M4 的 `ports.go` / 19 处别名清单做成"K 片待删表"（只做清点，不动代码）。

## 6. T4：`backend/tool/registry` 的二次拆分（新，需要落点/接口口径的裁决）

**为什么它不是"再搬一次"**：plan §2.6 要求把这一包切成 `backend/tool/{registry,jobs}`，
再把权限判据段并入 `backend/limit/policy`。实测跨组引用（搬前逐符号数出来的）：

| 组 | 规模 | 依赖另一组的私有符号 |
|---|---|---|
| 作业组（`async_exec.go`、`async_run.go`、`async_probe.go`、`job_{contract,tools,run,subagent}.go`） | 7 文件 2720 行 | **14 个**：`Router`(type)、`Deps`、`ResolveShellFact`、`DialectHint`、`newProcessTreeCommand`、`resolveNodePath`、`scopedBashCommand`、`scopedToolTimeout`、`serialBashBudgetLabel`、`sessionKey`、`startWithProcessTree` 等 |
| 权限组（`permission_policy.go` 601 行、`policy.go` 172 行） | 2 文件 773 行 | **8 个**：`PermissionGate`、`Enforce`、`Middleware`、`rebuildLocked`、`sessionFromContext`（registry_state.go）、`Write`（async_run.go）、`sessionID`（job_contract.go） |
| 注册表组（`router.go`、`registry_state.go`、`shell_fact.go`、`role_plugins.go`） | 4 文件 1583 行 | 反向引用为 0 |

**建议（按"先接口后搬文件"的顺序，三步各一 commit）**：

1. **作业组抽端口**：把作业组需要的 14 个符号收敛成 1–2 个窄接口（例如
   `JobsHost`：会话键、节点路径解析、进程树启动、壳事实、预算标签），定义放 `contract/`
   （或 `backend/tool/jobs/ports.go`），注册表侧用编译期断言钉住"结构满足"。
2. 作业组搬到 `backend/tool/jobs/`（此时它不再 import 注册表组）。
3. 权限判据段并入 `backend/limit/policy/`：`permission_policy.go` 需要执行门的 5 个符号，
   同样先抽端口（`Gate` 端口），再搬；`policy.go`（可见性策略，172 行）是否一起进
   `backend/limit/policy/` 需要你定——它做的是**工具面收窄**（子代理/员工能看到哪些工具），
   与"权限判定表"是两件事，可留 `backend/tool/registry/`。

**需要你裁决的只有两条**：① `policy.go`（可见性）进 `backend/limit/policy/` 还是留注册表板；
② 端口层的落点（`contract/` 还是各自板内的 `ports.go`）。其余我按上面三步做。

## 7. 一页清单：现在卡住 M4/M5 的全部裁决（每条都附实测与建议）

`seelebridge/` 现在只剩 `teamwork` 与 `workunit` 两个包（外加根包 52 个生产文件），
**再往下每一步都撞在这张表上**。每条都给了"批准后我立刻做什么"。

| # | 待裁决 | 实测证据 | 我的建议 | 批准后立刻做 |
|---|---|---|---|---|
| 1 | **J 片：根包的目标目录 / 包名 / 顺序** | 根包 52 生产文件 13,373 行；`Runtime` 单类型跨文件（`runtime.go` 定义、其余 40+ 文件是它的方法） | 目标 `backend/engine/assemble/`、包名 `assemble`（plan §2.5 给这 4 个装配文件点名的落点就是 `backend/engine/assemble/`）；顺序 **B→A**：先整体落位（引用方保留原别名，调用点零改动）再逐域下沉 | 整体搬 + 包名改名（一次，可逆），拿到"根包离开 `seelebridge/`"；随后按域下沉 |
| 2 | **V3：`workunit` 的归属**（**已落地**，见 m4-record §13 与 D-M4-W1） | 契约包 4 文件 869 行；进口方：`backend/multiagent/subagent`(2 文件)、`backend/tool/{node,registry,worktree}`、`internal/telemetry`、`e2e`、根包 9 个 `workunit*` 文件 2,663 行、`seelebridge/teamwork` | **独立 `backend/workunit/`**：进口方横跨 tool/multiagent/internal 三块，放 multiagent 会让 tool 板出现"回指"；且它本来就是共享契约 | 已完成：契约 + 唯一实现（原 `workunit_parent.go`）同落 `backend/workunit/`，门禁改成**按包名 + 标记**定位（含定位器阴性对照） |
| 3 | **`teamwork` 与根包 11 个 `runtime_teamwork*.go`** | 该域代码：`seelebridge/teamwork` 7 文件 2,580 行 + 根包 `runtime_teamwork*.go` ≈3,000 行 | 与 1 合并做：先在 J 的整体落位后，把这一域从 `assemble` 下沉到 `backend/multiagent/teamwork/` | 域内下沉（plan §2.7） |
| 4 | **T4 端口层口径** | 见 §6 的耦合表（作业组引用注册表组 14 个私有符号） | 端口放各板内 `ports.go`（`contract/` 已经背着 29 条污染，不再往里加） | 三步走（抽端口 → 搬 `backend/tool/jobs` → 权限段并入 policy） |
| 5 | **`internal/*` 终局** | 六个包的进口方横跨三块板 | 保持模块根 `internal/*`（与既有 `internal/{bootseed,buildinfo,…}` 同一惯例） | 收口时只更新 README 措辞，不再搬 |
| 6 | **`backend/limit/gate` 的目录名 vs 包名 `security`** | 目录按 plan 取名 `gate`，包名仍是 `security` | 保持现状即可；若要统一，改名成本 = 一次包重命名 + 字符串门禁同步（M3 的 `sessionstore→store` 是同款先例） | 一次机械改名 |
| 7 | **`mcp`/`plugin` 的最终嵌套（A/B）** | 两包已落 `backend/engine/{mcp,plugin}`，板级落点已定 | 方案 A：顶层 `mcpstack/`、`plugin/` 占目录名，适配层进 `.../seele/` | 一次子目录移动 |
| 8 | **`store/schedule_events.go` 是否搬** | 它是 `storeEngine` 的结构事件写方法（D-M4-22） | 留在 `store`（搬出去要暴露 store 内部实现） | 记录为"不搬"即可 |

### 7.1 K 片（收口）待删清单——已清点完（`tmp/reshape/m4_k_inventory.py`）

- `seelebridge/ports.go`：**1020 行**、**11 条 `type X = Y` 兼容别名**、**69 个方法/转发**。
- 别名的消费者集中在 `application/core`（service_scheduler 及其测试桩、`runtime_projection.go`、
  `view_state/coordinator.go`）、`main.go`、`mcpstack/config`、`backend/engine/schedule`，
  以及 **`contract/ports.go`**——契约层现在为了 `RuntimeVisibilityProjection` /
  `ParentEvidenceProjection` 两个别名反向依赖 `seelebridge` 根包（这就是 `contractClosureAllowList`
  里那 29 条污染的一部分）。删掉别名会让契约层闭包**变干净**，所以 K 片不只减行数，还直接
  削 M5 的收窄成本。
- 计划 §2.10 点名的 19 处别名里，`plan`/`task`/`scheduler`/`session`/`tools`/`worktree`/`node`
  那几组已随各自的搬迁消失，**只剩这 11 条**（都在 `ports.go`）。
